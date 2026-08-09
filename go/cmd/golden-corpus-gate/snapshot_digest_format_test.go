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

	checked := 0
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
		if !digestPattern.MatchString(value) {
			t.Errorf("%s = %q is not a well-formed digest "+
				"(want sha256: plus 64 lowercase hex, or sha512: plus 128)", location, value)
		}
	})

	// A snapshot that stopped carrying digests would satisfy the loop above
	// without proving anything.
	if checked == 0 {
		t.Fatal("no sha256-prefixed values found in the snapshot; this check passed vacuously")
	}
}

// isDigestField reports whether a dotted JSON location names a digest field.
// The trailing segment is what matters: "digest", "subject_digest",
// "artifact_digest", and "digest_or_version" are digests; "hashes" and
// "kv_path_fingerprint" are not.
func isDigestField(location string) bool {
	segment := location
	if index := strings.LastIndex(segment, "."); index >= 0 {
		segment = segment[index+1:]
	}
	segment = strings.TrimSuffix(segment, "[]")
	if index := strings.Index(segment, "["); index >= 0 {
		segment = segment[:index]
	}
	return segment == "digest" || strings.HasSuffix(segment, "_digest") ||
		segment == "digest_or_version"
}

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
