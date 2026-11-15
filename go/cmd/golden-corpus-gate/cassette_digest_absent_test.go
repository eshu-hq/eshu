// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import "testing"

// emptyAllowedDigestFields names the digest fields whose contract lets a
// source report no digest at all. The vulnerability collector writes
// response_digest as the empty string in every vulnerability.source_snapshot
// it emits: the payload key is declared, but nothing outside the collector's
// tests sets the value it reads. Eshu's own NVD paging golden
// (sdk/go/factschema/fixturepack/payloads/nvd_paging_golden.json) and the
// readiness live test carry the same empty value. An empty value there is
// absence, not a malformed digest, so the walker does not hold it to the
// digest pattern. Every other digest field stays strict: an empty
// subject_digest is still a bug.
var emptyAllowedDigestFields = map[string]bool{
	"response_digest": true,
}

// isAbsentDigest reports whether value is the empty string in a digest field
// that may legitimately be empty. A non-empty value is never absent, so a
// malformed response_digest is still caught.
func isAbsentDigest(location, value string) bool {
	return value == "" && emptyAllowedDigestFields[digestFieldSegment(location)]
}

func TestIsAbsentDigestAllowsOnlyAnEmptyResponseDigest(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		location string
		value    string
		want     bool
	}{
		{"empty response_digest is absence", "scopes[0].facts[1].payload.response_digest", "", true},
		{"empty response_digest in another cassette shape", "payload.snapshot.response_digest", "", true},
		{"a malformed response_digest is still caught", "scopes[0].facts[1].payload.response_digest", "sha256:abc", false},
		{"a well-formed response_digest is not absent", "scopes[0].facts[1].payload.response_digest", "sha256:" + hex64, false},
		{"an empty subject_digest is still a bug", "scopes[0].facts[0].payload.subject_digest", "", false},
		{"an empty bare digest is still a bug", "scopes[0].facts[0].payload.digest", "", false},
		{"an empty cache_snapshot_digest is still a bug", "scopes[0].facts[0].payload.cache_snapshot_digest", "", false},
	} {
		if got := isAbsentDigest(tc.location, tc.value); got != tc.want {
			t.Errorf("%s: isAbsentDigest(%q, %q) = %v, want %v", tc.name, tc.location, tc.value, got, tc.want)
		}
	}
}

const hex64 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
