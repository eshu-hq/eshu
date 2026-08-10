// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"regexp"
	"strings"
	"testing"
)

// hashesMapDigestWidths is the exhaustive set of hash algorithms this gate
// knows how to validate when they appear as a key under a "hashes" object
// (package_registry.package_artifact's hashes.sha256/hashes.sha512 shape,
// sdk/go/factschema/decode_packageregistry.go). Unlike every digest field
// isDigestField's naming convention already covers, a hashes-map entry
// carries NO algorithm prefix -- the JSON key already names the algorithm --
// so the value is a bare lowercase-hex digest at that algorithm's exact
// width, not a "sha256:"/"sha512:"-prefixed digestPattern match.
//
// An algorithm not listed here is REJECTED, not skipped: hashesMapAlgorithm
// still recognizes it as a hashes-map key, so digestFieldValueIsWellFormed
// fails it outright. That is the deliberate choice for an unsupported
// hashes.sha1 or hashes.md5 -- fail closed until this map is extended with
// the correct width, rather than let an unrecognized algorithm's value ride
// through unchecked the way #6011 rode through isDigestField never firing on
// "sha256"/"sha512" segments at all.
var hashesMapDigestWidths = map[string]*regexp.Regexp{
	"sha256": regexp.MustCompile(`^[0-9a-f]{64}$`),
	"sha512": regexp.MustCompile(`^[0-9a-f]{128}$`),
}

// hashesMapAlgorithm reports the algorithm name a location names, and whether
// that location is a key living directly under a "hashes" object. The check
// requires the trailing path component to be a plain dotted object key ("tail"
// containing no "["): a "hashes" object's own value being read as a joined
// list ("...hashes[0]", the B-12 snapshot's allowed_node_property_values pin)
// is reached via an array index appended to "hashes" itself, not a distinct
// key nested one level under it, and must keep being excluded here so that
// joined "alg:digest|alg:digest" pin keeps being skipped by isDigestField.
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
	return tail, true
}

// isHashesMapDigestField reports whether location names a bare digest value
// keyed by algorithm name directly under a "hashes" object. See
// hashesMapAlgorithm for the exact rule and hashesMapDigestWidths for why an
// unrecognized algorithm still counts as a digest field (so it is validated
// and rejected, not silently skipped).
func isHashesMapDigestField(location string) bool {
	_, ok := hashesMapAlgorithm(location)
	return ok
}

// TestIsDigestFieldHandlesHashesMapShape is the P2-1 regression: the gate's
// own motivating defect (#6011 — 76 truncated digests in the golden
// snapshot) reintroduced against package_registry.package_artifact's
// hashes.sha256/hashes.sha512 shape must be CAUGHT, not silently skipped.
// digestFieldSegment yields "sha256"/"sha512" for these locations — the
// algorithm name, not a "digest"/"*_digest"/"digest_or_version" suffix — so
// isDigestField never fired on them before this fix, and a lockstep
// truncation across the cassette and both snapshot copies of the joined
// value left the gate green.
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
// entries isHashesMapDigestField now catches are validated as bare lowercase
// hex at the algorithm's exact width — no "sha256:"/"sha512:" prefix, since
// the JSON key already names the algorithm — and that an algorithm this gate
// does not recognize is rejected outright rather than silently skipped.
func TestDigestFieldValueIsWellFormedHandlesHashesMapShape(t *testing.T) {
	t.Parallel()

	const sha256Field = "scopes[1].facts[4].payload.hashes.sha256"
	const sha512Field = "scopes[1].facts[4].payload.hashes.sha512"
	const unsupportedField = "scopes[1].facts[4].payload.hashes.sha1"

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
			name:     "unrecognized algorithm rejected outright, not skipped",
			location: unsupportedField,
			value:    strings.Repeat("a", 40), // a well-formed SHA-1 hex width
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
