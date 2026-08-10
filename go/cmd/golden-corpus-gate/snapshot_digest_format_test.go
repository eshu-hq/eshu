// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// A digest is an algorithm, a colon, and that algorithm's exact hex width.
// Anything else is not a digest, whatever it looks like.
var digestPattern = regexp.MustCompile(`^(sha256:[0-9a-f]{64}|sha512:[0-9a-f]{128})$`)

// isVersionValue reports whether a digest_or_version field is holding a version
// rather than attempting a digest. Only that field may carry a non-digest, and
// only when it makes no claim to be one — a value with an algorithm prefix is
// claiming to be a digest and is held to the digest rule.
func isVersionValue(location, value string) bool {
	segment := location
	if index := strings.LastIndex(segment, "."); index >= 0 {
		segment = segment[index+1:]
	}
	if segment != "digest_or_version" {
		return false
	}
	return !strings.Contains(value, ":")
}

// Every digest FIELD in the golden snapshot must hold a well-formed digest,
// because nothing downstream checks.
//
// Scope, stated precisely so the comment does not overstate it: this walks
// string values whose JSON field name is `digest`, ends in `_digest`, or is
// `digest_or_version`. It does not inspect object keys, and it does not judge
// digest-looking strings living under other field names.
//
// The CVE-2026-00010 demo finding carried a 66-character subject_digest — a
// valid-looking string with two extra hex characters on the end. It survived
// because every consumer compares digests with string equality, so a value that
// is not a digest at all round-trips cleanly through the cassettes, the B-12
// snapshot, the MCP query shape and the HTTP assertions. The first thing to
// break would have been a future digest-format validation, and it would have
// broken against a fixture rather than against real data (#5788).
//
// This asserts the shape rather than pinning specific values, so it keeps
// holding as fixtures are added.
func TestSnapshotDigestsAreWellFormedSHA256(t *testing.T) {
	t.Parallel()

	const path = "../../../testdata/golden/e2e-20repo-snapshot.json"
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var document any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}

	checked, violations := 0, 0
	walkJSONStrings(document, "", func(location, value string) {
		// Scoped to fields that ARE digests, by name. Two other snapshot values
		// carry a "sha256:" prefix without being one, deliberately: a combined
		// "sha256:...|sha512:..." hash pair, and a synthetic
		// kv_path_fingerprint that encodes a path rather than a hash. Checking
		// every sha256-prefixed string would fail on both and force the check
		// to be weakened until it caught nothing.
		if !isDigestField(location) {
			return
		}
		// A digest field is validated whatever it contains — NOT only when it
		// already carries a "sha256:" prefix. Returning early on a wrong prefix
		// let a missing prefix, a misspelled one, another algorithm, or
		// arbitrary text pass unchecked, and the vacuity guard below stayed
		// satisfied by the other fields, so the gate reported green over a
		// malformed digest (#6011 review, codex).
		//
		// The single exception is digest_or_version, which legitimately holds a
		// version string. It is checked only when it is trying to be a digest.
		if isVersionValue(location, value) {
			return
		}
		checked++
		if !digestFieldValueIsWellFormed(location, value) {
			violations++
			t.Errorf("%s = %q is not a well-formed digest "+
				"(want sha256: plus 64 lowercase hex, or sha512: plus 128, "+
				"or reference@digest for a field in refDigestAllowedFields, "+
				"or bare lowercase hex at the algorithm's width for a hashes-map key)", location, value)
		}
	})
	t.Logf("%d digest values checked, %d violations", checked, violations)

	// A snapshot that stopped carrying digests would satisfy the loop above
	// without proving anything.
	if checked == 0 {
		t.Fatal("no digest values checked in the snapshot; this check passed vacuously")
	}
}

// digestFieldSegment extracts the trailing JSON field-name segment from a
// dotted walkJSONStrings location, stripping any trailing array index so
// "foo.digest[3]" and "foo.digest" both resolve to "digest".
func digestFieldSegment(location string) string {
	segment := location
	if index := strings.LastIndex(segment, "."); index >= 0 {
		segment = segment[index+1:]
	}
	segment = strings.TrimSuffix(segment, "[]")
	if index := strings.Index(segment, "["); index >= 0 {
		segment = segment[:index]
	}
	return segment
}

// isDigestNamedSegment reports whether a trailing JSON field-name segment
// (as digestFieldSegment or hashesMapAlgorithm yields it) follows the naming
// convention that makes a field a digest on its own: "digest", a "_digest"
// suffix, or "digest_or_version". It is shared by isDigestField and
// digestFieldValueIsWellFormed so a segment that qualifies as a
// naming-convention digest field is never also treated as a hashes-map
// algorithm key -- see digestFieldValueIsWellFormed for why that precedence
// matters.
func isDigestNamedSegment(segment string) bool {
	return segment == "digest" || strings.HasSuffix(segment, "_digest") ||
		segment == "digest_or_version"
}

// isDigestField reports whether a dotted JSON location names a digest field.
// The trailing segment is what matters: "digest", "subject_digest",
// "artifact_digest", and "digest_or_version" are digests; "hashes" and
// "kv_path_fingerprint" are not -- but a key living directly under a "hashes"
// object (e.g. "...payload.hashes.sha256") is, via isHashesMapDigestField.
func isDigestField(location string) bool {
	if isDigestNamedSegment(digestFieldSegment(location)) {
		return true
	}
	return isHashesMapDigestField(location)
}

// hashesMapDigestWidths, hashesMapAlgorithm, isHashesMapDigestField, and the
// tests that prove them live in hashes_map_digest_format_test.go. This file
// was approaching the 500-line cap; the hashes-map carve-out is a
// self-contained addition on top of the naming-convention check above and
// splits out cleanly.

// refDigestAllowedFields is the allowlist of digest field names whose
// committed contract documents the "reference@digest" shape. Today that is
// only kuberneteslive's resolved_image_digest, which is CRI-normalized into
// the bare "repo@sha256:<digest>" form
// (sdk/go/factschema/kuberneteslive/v1/pod_template.go,
// go/internal/collector/kuberneteslive/envelope.go). Every other digest
// field — ociregistry's digest/resolved_digest/subject_digest,
// vulnerabilityintelligence's image_digest/cache_snapshot_digest,
// sbomattestation's subject_digest, and so on — is a bare digest by
// contract.
//
// Gating the carve-out on the field name, rather than on whether a value
// happens to contain "@", matters: kuberneteslive resolved_image_digest and
// vulnerabilityintelligence image_digest hold the same digest and are meant
// to join on literal string equality. An unconditional carve-out let a
// ref-qualified value slip into the bare-digest side of that join
// undetected, and let a malformed digest ride into ANY digest field
// disguised as the reference portion of a reference@digest pair.
var refDigestAllowedFields = map[string]bool{
	"resolved_image_digest": true,
}

// digestFieldValueIsWellFormed is the one definition of "well-formed digest
// field value", shared by the snapshot and cassette walkers so there is
// exactly one rule rather than two that can drift apart. A field in
// refDigestAllowedFields may carry the documented "reference@digest" shape;
// every other digest field must be a bare digestPattern match, whatever it
// contains — including a value that merely looks like a reference@digest
// pair. A hashes-map key (see hashesMapAlgorithm) whose TrimSpace-normalized
// name is not one hashesMapDigestWidths recognizes is reported well-formed
// here too: isHashesMapDigestField already keeps it out of isDigestField's
// checked set for every production caller, so this branch is reached only by
// a caller that bypasses that gate directly (as some tests do), and it must
// give the same "skip, don't fail" answer rather than silently narrowing the
// v1 payload contract a second way. See hashesMapDigestWidths for why an
// unrecognized algorithm is skipped rather than rejected.
//
// The hashes-map branch is only taken when the algorithm segment is NOT
// itself digest-named (isDigestNamedSegment). A location such as
// "...payload.hashes.subject_digest" both names a hashes-map key
// (hashesMapAlgorithm matches its "hashes" parent) and ends in "_digest", so
// isDigestField counts it via the naming-convention rule regardless of the
// hashes-map carve-out. Without this guard the hashes-map branch would run
// first, find "subject_digest" absent from hashesMapDigestWidths, and report
// it well-formed unconditionally -- counting the value toward the vacuity
// guard while never validating it, the same counted-but-unchecked shape this
// gate exists to close. Deferring to the naming-convention digestPattern
// check instead means the value is actually validated.
//
// The hashes-map branch's parity with packageRegistryTrimmedStringMap's
// normalization is KEY-only, not value -- see hashesMapDigestWidths for the
// gap and why it is fail-closed rather than a correctness bug.
func digestFieldValueIsWellFormed(location, value string) bool {
	if algorithm, ok := hashesMapAlgorithm(location); ok && !isDigestNamedSegment(algorithm) {
		pattern, recognized := hashesMapDigestWidths[algorithm]
		if !recognized {
			return true
		}
		return pattern.MatchString(value)
	}
	if !refDigestAllowedFields[digestFieldSegment(location)] {
		return digestPattern.MatchString(value)
	}
	// The split is on the LAST "@". The OCI distribution reference grammar
	// permits exactly one "@" in a valid reference@digest value — a
	// registry/repository path cannot itself contain one. Splitting on the
	// last "@" is still the right choice: it stays correct for a value that
	// violates that grammar (more than one "@"), since the digest CRI
	// resolution appends is always the final segment.
	index := strings.LastIndex(value, "@")
	if index < 0 {
		return digestPattern.MatchString(value)
	}
	reference, digest := value[:index], value[index+1:]
	if reference == "" {
		return false
	}
	return digestPattern.MatchString(digest)
}

// TestDigestFieldValueIsWellFormedHandlesRefDigestShape proves the
// "reference@digest" carve-out is validated, not exempted, and gated on the
// field name: a well-formed digest after the last "@" is accepted only for a
// field in refDigestAllowedFields; every other field is held to a bare
// digestPattern match even when the value merely looks like a
// reference@digest pair. It also proves the carve-out rejects malformed
// shapes within the allowed field itself — uppercase hex and a trailing "@"
// with no digest both still fail — and that more than one "@" is accepted BY
// DESIGN, not a gap: the split is always on the LAST "@" (see the comment on
// digestFieldValueIsWellFormed), so an earlier "@" becomes part of the opaque
// reference portion rather than breaking the check, as long as the segment
// after the final "@" is itself a well-formed digest.
func TestDigestFieldValueIsWellFormedHandlesRefDigestShape(t *testing.T) {
	t.Parallel()

	const refDigestField = "resolved_image_digest"
	const bareDigestField = "image_digest" // e.g. vulnerabilityintelligence's; not in refDigestAllowedFields

	tests := []struct {
		name     string
		location string
		value    string
		want     bool
	}{
		{
			name:     "bare digest, ref-digest field",
			location: refDigestField,
			value:    "sha256:" + strings.Repeat("a", 64),
			want:     true,
		},
		{
			name:     "reference@digest, well-formed",
			location: refDigestField,
			value:    "ghcr.io/eshu-hq/supply-chain-demo@sha256:" + strings.Repeat("a", 64),
			want:     true,
		},
		{
			name:     "sha512 reference@digest",
			location: refDigestField,
			value:    "ghcr.io/eshu-hq/supply-chain-demo@sha512:" + strings.Repeat("f", 128),
			want:     true,
		},
		{
			name:     "bare digest, too short",
			location: refDigestField,
			value:    "sha256:" + strings.Repeat("a", 63),
			want:     false,
		},
		{
			name:     "reference@digest, digest too short",
			location: refDigestField,
			value:    "ghcr.io/eshu-hq/supply-chain-demo@sha256:" + strings.Repeat("a", 63),
			want:     false,
		},
		{
			name:     "reference@digest, digest too long",
			location: refDigestField,
			value:    "ghcr.io/eshu-hq/supply-chain-demo@sha256:" + strings.Repeat("a", 65),
			want:     false,
		},
		{
			name:     "empty reference before @",
			location: refDigestField,
			value:    "@sha256:" + strings.Repeat("a", 64),
			want:     false,
		},
		{
			name:     "reference@non-digest",
			location: refDigestField,
			value:    "ghcr.io/eshu-hq/supply-chain-demo@not-a-digest",
			want:     false,
		},
		{
			name:     "reference@digest, uppercase hex",
			location: refDigestField,
			value:    "ghcr.io/eshu-hq/supply-chain-demo@sha256:" + strings.Repeat("A", 64),
			want:     false,
		},
		{
			name:     "reference@digest, more than one @",
			location: refDigestField,
			value:    "ghcr.io/eshu-hq/supply-chain-demo@2@sha256:" + strings.Repeat("a", 64),
			want:     true,
		},
		{
			name:     "reference@digest, trailing @ with empty digest",
			location: refDigestField,
			value:    "ghcr.io/eshu-hq/supply-chain-demo@",
			want:     false,
		},
		{
			// #6011-class smuggling: a too-short digest hiding as the
			// reference, in front of a well-formed one. Accepted for the
			// field whose contract documents reference@digest — the
			// reference portion is opaque and unvalidated by design.
			name:     "smuggled short digest as reference, ref-digest field",
			location: refDigestField,
			value:    "sha256:" + strings.Repeat("a", 63) + "@sha256:" + strings.Repeat("a", 64),
			want:     true,
		},
		{
			// The same value against a field NOT in refDigestAllowedFields
			// must reject: before the field gate, "anything@sha256:<64hex>"
			// passed as a well-formed digest in every digest field, letting
			// a malformed digest ride in disguised as a reference.
			name:     "smuggled short digest as reference, bare-digest field",
			location: bareDigestField,
			value:    "sha256:" + strings.Repeat("a", 63) + "@sha256:" + strings.Repeat("a", 64),
			want:     false,
		},
		{
			// The concrete #F1 failure: pasting the ref-qualified form of a
			// digest into a bare-digest field (e.g. vulnerabilityintelligence
			// image_digest, meant to join kuberneteslive resolved_image_digest
			// by literal string equality) must be rejected, not silently
			// accepted.
			name:     "well-formed reference@digest rejected outside the allowlist",
			location: bareDigestField,
			value:    "ghcr.io/eshu-hq/supply-chain-demo@sha256:" + strings.Repeat("a", 64),
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := digestFieldValueIsWellFormed(tt.location, tt.value); got != tt.want {
				t.Errorf("digestFieldValueIsWellFormed(%q, %q) = %v, want %v", tt.location, tt.value, got, tt.want)
			}
		})
	}
}

// TestIsDigestFieldHandlesHashesMapShape and
// TestDigestFieldValueIsWellFormedHandlesHashesMapShape live in
// hashes_map_digest_format_test.go, next to the predicates they prove.

// walkJSONStrings visits every string in a decoded JSON document, passing a
// dotted path so a failure names the field rather than just the value.
func walkJSONStrings(node any, location string, visit func(location, value string)) {
	switch typed := node.(type) {
	case map[string]any:
		for key, child := range typed {
			next := key
			if location != "" {
				next = location + "." + key
			}
			walkJSONStrings(child, next, visit)
		}
	case []any:
		for index, child := range typed {
			next := location + "[" + strconv.Itoa(index) + "]"
			walkJSONStrings(child, next, visit)
		}
	case string:
		visit(location, typed)
	}
}

// The gate must reject a digest field whatever it holds, not only when it
// already looks like a sha256. An early return on a wrong prefix let a missing
// prefix, another algorithm, or arbitrary text through, and the vacuity guard
// stayed satisfied by the snapshot's other digest fields — so the gate reported
// green over a malformed digest (#6011 review, codex).
func TestDigestPatternRejectsMalformedShapes(t *testing.T) {
	t.Parallel()

	reject := []string{
		"abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890", // no prefix
		"sha256:" + strings.Repeat("a", 63),                                // too short
		"sha256:" + strings.Repeat("a", 65),                                // too long
		"sha-256:" + strings.Repeat("a", 64),                               // misspelled
		"md5:" + strings.Repeat("a", 32),                                   // wrong algorithm
		"sha256:" + strings.Repeat("A", 64),                                // uppercase
		"not-a-digest-at-all",
		"",
	}
	for _, value := range reject {
		if digestPattern.MatchString(value) {
			t.Errorf("digestPattern accepted %q, want reject", value)
		}
	}

	accept := []string{
		"sha256:" + strings.Repeat("a", 64),
		"sha512:" + strings.Repeat("f", 128),
	}
	for _, value := range accept {
		if !digestPattern.MatchString(value) {
			t.Errorf("digestPattern rejected %q, want accept", value)
		}
	}

	// digest_or_version may hold a version, but only when it makes no claim to
	// be a digest. A value carrying an algorithm prefix is held to the rule.
	if !isVersionValue("x.digest_or_version", "1.2.3") {
		t.Error("digest_or_version holding a plain version should be treated as a version")
	}
	if isVersionValue("x.digest_or_version", "sha256:deadbeef") {
		t.Error("digest_or_version carrying an algorithm prefix must be validated as a digest")
	}
	if isVersionValue("x.subject_digest", "1.2.3") {
		t.Error("only digest_or_version may hold a version; subject_digest must not")
	}
}
