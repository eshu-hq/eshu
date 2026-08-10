// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"regexp"
	"strings"
	"testing"
)

// hashesMapDigestWidths is the set of hash algorithms this gate validates by
// exact hex width when they appear as a key under a "hashes" object
// (package_registry.package_artifact's hashes.sha256/hashes.sha512 shape,
// sdk/go/factschema/decode_packageregistry.go). Unlike every digest field
// isDigestField's naming convention already covers, a hashes-map entry
// carries NO algorithm prefix -- the JSON key already names the algorithm --
// so the value is a bare lowercase-hex digest at that algorithm's exact
// width, not a "sha256:"/"sha512:"-prefixed digestPattern match.
//
// An algorithm not listed here is SKIPPED, not rejected. The v1 payload
// contract accepts ANY string key under "hashes", including one containing
// ':' (sdk/go/factschema/decode_packageregistry.go,
// packageregistry/v1/doc.go) -- an earlier version of the DECODE path
// rejected a colon-bearing key on the same reasoning this gate once used, and
// that was reverted as a silent v1 contract narrowing (#5820 P2 review
// finding). Failing a hashes-map key this gate does not recognize would
// reintroduce that same defect one layer up: the gate would be stricter than
// the contract it exists to guard. isHashesMapDigestField therefore only
// treats a hashes-map key as checked when its TrimSpace-normalized name (the
// same, and only, normalization packageRegistryTrimmedStringMap in
// go/internal/projector/package_registry_canonical.go applies before merging
// duplicate keys) is listed here; everything else is skipped by
// isDigestField, not routed into digestFieldValueIsWellFormed at all. The
// accepted cost: a future hashes.sha1 (or any other algorithm outside this
// map) with a malformed value is not validated by this gate.
var hashesMapDigestWidths = map[string]*regexp.Regexp{
	"sha256": regexp.MustCompile(`^[0-9a-f]{64}$`),
	"sha512": regexp.MustCompile(`^[0-9a-f]{128}$`),
}

// hashesMapAlgorithm reports the algorithm name a location names -- trimmed
// with strings.TrimSpace, matching the ONE normalization
// packageRegistryTrimmedStringMap applies to a hashes-map key before merging
// it into the canonical Hashes map (no case-folding: "SHA256" stays distinct
// from "sha256" on both sides, since the projector does not fold either) --
// and whether that location is a key living directly under a "hashes"
// object. The check requires the trailing path component to be a plain
// dotted object key ("tail" containing no "["): a "hashes" object's own value
// being read as a joined list ("...hashes[0]", the B-12 snapshot's
// allowed_node_property_values pin) is reached via an array index appended to
// "hashes" itself, not a distinct key nested one level under it, and must
// keep being excluded here so that joined "alg:digest|alg:digest" pin keeps
// being skipped by isDigestField.
//
// ok reports only whether location names a hashes-map key at all -- not
// whether algorithm is one this gate recognizes. Call
// isHashesMapDigestField to ask whether the entry is actually checked.
func hashesMapAlgorithm(location string) (algorithm string, ok bool) {
	index := strings.LastIndex(location, ".")
	if index < 0 {
		return "", false
	}
	tail := location[index+1:]
	if strings.Contains(tail, "[") {
		return "", false
	}
	if digestFieldSegment(location[:index]) != "hashes" {
		return "", false
	}
	return strings.TrimSpace(tail), true
}

// isHashesMapDigestField reports whether location names a hashes-map entry
// this gate validates: a key living directly under a "hashes" object (see
// hashesMapAlgorithm) whose normalized name is one hashesMapDigestWidths
// knows the exact digest width for. A hashes-map key that normalizes to
// anything else -- a colon-bearing algorithm name the v1 contract explicitly
// permits, "sha1", "SHA256", or any other string -- is not treated as a
// digest field at all here, so isDigestField skips it instead of failing it.
// See hashesMapDigestWidths for why skipping, not failing, is the correct
// behavior for this gate.
func isHashesMapDigestField(location string) bool {
	algorithm, ok := hashesMapAlgorithm(location)
	if !ok {
		return false
	}
	_, recognized := hashesMapDigestWidths[algorithm]
	return recognized
}

// TestIsDigestFieldHandlesHashesMapShape is the P2-1 regression: the gate's
// own motivating defect (#6011 — 76 truncated digests in the golden
// snapshot) reintroduced against package_registry.package_artifact's
// hashes.sha256/hashes.sha512 shape must be CAUGHT, not silently skipped.
// digestFieldSegment yields "sha256"/"sha512" for these locations — the
// algorithm name, not a "digest"/"*_digest"/"digest_or_version" suffix — so
// isDigestField never fired on them before this fix.
//
// That is true of this walker predicate specifically, not of "the gate" as a
// whole: TestLoadSnapshotPackageArtifactHashesEntryMatchesCassette
// (property_assertions_package_artifact_hashes_test.go, added in da6f5cab7
// itself) derives the expected hashes value straight from the cassette and
// already catches a truncation of the corpus's one existing hashes-map entry
// through a different mechanism. Today's corpus has exactly one `hashes`
// map, already pinned and cassette-derived by that test, so this walker fix
// adds ZERO retroactive detection for it -- its value is forward-looking: a
// NEW hashes-map fact added without its own snapshot pin is now caught by
// this walker where it previously was not.
func TestIsDigestFieldHandlesHashesMapShape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		location string
		want     bool
	}{
		{
			name:     "cassette hashes.sha256 key",
			location: "scopes[1].facts[4].payload.hashes.sha256",
			want:     true,
		},
		{
			name:     "cassette hashes.sha512 key",
			location: "scopes[1].facts[4].payload.hashes.sha512",
			want:     true,
		},
		{
			name: "snapshot joined pin under hashes must stay skipped",
			// The B-12 snapshot's allowed_node_property_values.hashes[0] is a
			// single joined "alg:digest|alg:digest" string, not an
			// algorithm-keyed entry — its trailing segment is "hashes"
			// itself, reached via an array index, not a distinct object key
			// nested one level under "hashes".
			location: "graph.required_nodes[0].allowed_node_property_values.hashes[0]",
			want:     false,
		},
		{
			name:     "hashes object itself, not a nested key",
			location: "scopes[1].facts[4].payload.hashes",
			want:     false,
		},
		{
			name:     "unrelated field named sha256 not under hashes",
			location: "scopes[1].facts[4].payload.sha256",
			want:     false,
		},
		{
			// The v1 payload contract accepts any string key under "hashes",
			// including one containing ':' (decode_packageregistry.go,
			// packageregistry/v1/doc.go). Failing this key would make the
			// gate stricter than the contract it guards -- exactly the
			// defect #5820 P2 caught and reverted in the decode path itself.
			name:     "colon-bearing hashes-map key skipped, not failed (v1 contract permits it, #5820 P2)",
			location: "scopes[1].facts[4].payload.hashes.sha256:extra",
			want:     false,
		},
		{
			// packageRegistryTrimmedStringMap normalizes a hashes-map key
			// with strings.TrimSpace before merging it into the canonical
			// Hashes map; this gate must recognize the same normalized key
			// as checked, or it validates a different entry than the one
			// that actually reaches the graph.
			name:     "whitespace-padded hashes-map key normalizes like the projector and stays checked",
			location: "scopes[1].facts[4].payload.hashes. sha256 ",
			want:     true,
		},
		{
			// hashesMapDigestWidths does not list "sha1" -- an unsupported
			// algorithm is skipped, not failed, per hashesMapDigestWidths.
			name:     "unrecognized hashes-map algorithm skipped, not failed",
			location: "scopes[1].facts[4].payload.hashes.sha1",
			want:     false,
		},
		{
			// The projector's TrimSpace-only normalization never case-folds,
			// so "SHA256" stays a distinct, unrecognized key rather than
			// merging with "sha256" -- and this gate must agree, or it would
			// check a key the projector treats as something else.
			name:     "uppercase algorithm name not case-folded and skipped",
			location: "scopes[1].facts[4].payload.hashes.SHA256",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isDigestField(tt.location); got != tt.want {
				t.Errorf("isDigestField(%q) = %v, want %v", tt.location, got, tt.want)
			}
		})
	}
}

// TestDigestFieldValueIsWellFormedHandlesHashesMapShape proves the hashes-map
// entries isHashesMapDigestField catches are validated as bare lowercase hex
// at the algorithm's exact width — no "sha256:"/"sha512:" prefix, since the
// JSON key already names the algorithm — and that an algorithm this gate does
// not recognize is reported well-formed (skipped) rather than rejected: this
// gate must not fail a hashes-map key the v1 payload contract permits, per
// hashesMapDigestWidths.
func TestDigestFieldValueIsWellFormedHandlesHashesMapShape(t *testing.T) {
	t.Parallel()

	const sha256Field = "scopes[1].facts[4].payload.hashes.sha256"
	const sha512Field = "scopes[1].facts[4].payload.hashes.sha512"
	const unsupportedField = "scopes[1].facts[4].payload.hashes.sha1"
	const colonBearingField = "scopes[1].facts[4].payload.hashes.sha256:extra"
	const whitespaceField = "scopes[1].facts[4].payload.hashes. sha256 "
	const uppercaseField = "scopes[1].facts[4].payload.hashes.SHA256"

	tests := []struct {
		name     string
		location string
		value    string
		want     bool
	}{
		{
			name:     "well-formed sha256 hashes-map entry",
			location: sha256Field,
			value:    strings.Repeat("a", 64),
			want:     true,
		},
		{
			name:     "well-formed sha512 hashes-map entry",
			location: sha512Field,
			value:    strings.Repeat("f", 128),
			want:     true,
		},
		{
			// The exact #6011 truncation, replayed against the field the
			// original fixture-wide sweep never reached.
			name:     "truncated sha256 hashes-map entry, 63 hex",
			location: sha256Field,
			value:    strings.Repeat("a", 63),
			want:     false,
		},
		{
			name:     "truncated sha512 hashes-map entry, 127 hex",
			location: sha512Field,
			value:    strings.Repeat("f", 127),
			want:     false,
		},
		{
			name:     "sha256-prefixed value rejected for a hashes-map key",
			location: sha256Field,
			value:    "sha256:" + strings.Repeat("a", 64),
			want:     false,
		},
		{
			name:     "uppercase hex rejected for a hashes-map key",
			location: sha256Field,
			value:    strings.Repeat("A", 64),
			want:     false,
		},
		{
			// hashesMapDigestWidths does not list "sha1"; the accepted cost
			// of not narrowing the v1 contract is that a malformed value
			// under an unrecognized algorithm is not validated here.
			name:     "unrecognized algorithm skipped (well-formed vacuously), not rejected",
			location: unsupportedField,
			value:    "not-even-hex-shaped-garbage",
			want:     true,
		},
		{
			// decode_packageregistry.go/packageregistry/v1/doc.go: the v1
			// schema's hashes.additionalProperties accepts any string key,
			// including one containing ':' -- so this key is skipped
			// regardless of how malformed its value is, not narrowed back to
			// a gate failure (#5820 P2).
			name:     "colon-bearing algorithm skipped regardless of value shape",
			location: colonBearingField,
			value:    "not-a-digest-at-all",
			want:     true,
		},
		{
			// packageRegistryTrimmedStringMap trims " sha256 " to "sha256"
			// before merging it into the canonical Hashes map; this gate
			// must validate the same normalized entry at the sha256 width.
			name:     "whitespace-padded key normalizes like the projector and is validated at sha256 width",
			location: whitespaceField,
			value:    strings.Repeat("a", 64),
			want:     true,
		},
		{
			name:     "whitespace-padded key still fails a truncated digest once normalized",
			location: whitespaceField,
			value:    strings.Repeat("a", 63),
			want:     false,
		},
		{
			// The projector's TrimSpace-only normalization never case-folds,
			// so "SHA256" is a distinct, unrecognized key and is skipped
			// regardless of value shape -- consistent with "sha1" above.
			name:     "uppercase algorithm name not case-folded and skipped regardless of value",
			location: uppercaseField,
			value:    strings.Repeat("A", 64),
			want:     true,
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
