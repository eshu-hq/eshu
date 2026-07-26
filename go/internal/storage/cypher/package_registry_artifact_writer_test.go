// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"reflect"
	"testing"
)

// TestPackageRegistryHashPairsRoundTripsColonBearingAlgorithm is the shaped
// regression for the #5820 P2 review finding: DecodePackageRegistryPackageArtifact
// used to REJECT a colon-bearing hashes algorithm name as input_invalid, which
// silently narrowed the public package_registry.package_artifact.v1 contract
// (the JSON Schema's hashes.additionalProperties accepts any string key, and
// the same schema version previously decoded these payloads). The fix moves
// unambiguity from rejection to a lossless escaping scheme applied here, at the
// one place hashes is actually flattened into the "algorithm:digest" graph
// encoding: every algorithm/digest pair -- including one whose algorithm name
// itself contains ':' or '\' -- must round-trip through
// packageRegistryHashPairs (encode) and packageRegistrySplitHashPair (decode)
// back to the exact original algorithm and digest.
func TestPackageRegistryHashPairsRoundTripsColonBearingAlgorithm(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		algorithm string
		digest    string
	}{
		{name: "plain", algorithm: "sha256", digest: "abc123"},
		{name: "colon in algorithm", algorithm: "sha256:extra", digest: "abc123"},
		{name: "multiple colons", algorithm: "a:b:c", digest: "def456"},
		{name: "leading colon", algorithm: ":sha256", digest: "abc123"},
		{name: "trailing colon", algorithm: "sha256:", digest: "abc123"},
		{name: "backslash in algorithm", algorithm: `sha\256`, digest: "abc123"},
		{name: "backslash and colon", algorithm: `sha\256:extra`, digest: "abc123"},
		{name: "colon in digest", algorithm: "sha256", digest: "ab:c123"},
		{name: "backslash in digest", algorithm: "sha256", digest: `ab\c123`},
		{name: "empty algorithm", algorithm: "", digest: "abc123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pairs := packageRegistryHashPairs(map[string]string{tt.algorithm: tt.digest})
			if got, want := len(pairs), 1; got != want {
				t.Fatalf("packageRegistryHashPairs() len = %d, want %d (pairs=%#v)", got, want, pairs)
			}
			gotAlgorithm, gotDigest, ok := packageRegistrySplitHashPair(pairs[0])
			if !ok {
				t.Fatalf("packageRegistrySplitHashPair(%q) ok = false, want true", pairs[0])
			}
			if gotAlgorithm != tt.algorithm {
				t.Fatalf("round-tripped algorithm = %q, want %q (encoded %q)", gotAlgorithm, tt.algorithm, pairs[0])
			}
			if gotDigest != tt.digest {
				t.Fatalf("round-tripped digest = %q, want %q (encoded %q)", gotDigest, tt.digest, pairs[0])
			}
		})
	}
}

// TestPackageRegistryHashPairsDistinguishesCollidingAlgorithms proves the
// escaping is unambiguous where the old colon-rejection precondition only
// asserted it BY FORBIDDING the collision: two DIFFERENT (algorithm, digest)
// pairs whose naive "algorithm:digest" concatenation would collide (a
// colon-bearing algorithm shaped like another pair's algorithm+digest) must
// still decode back to their own distinct original values, not cross over.
func TestPackageRegistryHashPairsDistinguishesCollidingAlgorithms(t *testing.T) {
	t.Parallel()

	hashes := map[string]string{
		"sha256":       "extra:abc123", // naive concat: "sha256:extra:abc123"
		"sha256:extra": "abc123",       // naive concat: "sha256:extra:abc123" (same!)
	}
	pairs := packageRegistryHashPairs(hashes)
	if got, want := len(pairs), 2; got != want {
		t.Fatalf("packageRegistryHashPairs() len = %d, want %d (pairs=%#v)", got, want, pairs)
	}

	decoded := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		algorithm, digest, ok := packageRegistrySplitHashPair(pair)
		if !ok {
			t.Fatalf("packageRegistrySplitHashPair(%q) ok = false, want true", pair)
		}
		decoded[algorithm] = digest
	}
	if !reflect.DeepEqual(decoded, hashes) {
		t.Fatalf("round-tripped hashes = %#v, want %#v (the two colliding-when-naively-joined pairs must stay distinct)", decoded, hashes)
	}
}

// TestPackageRegistryHashPairsSortedAndColonFree keeps the pre-existing
// plain-algorithm behavior byte-identical: no escaping artifacts leak into the
// common case, and the sort order stays stable.
func TestPackageRegistryHashPairsSortedAndColonFree(t *testing.T) {
	t.Parallel()

	got := packageRegistryHashPairs(map[string]string{"sha256": "abc123", "sha512": "def456"})
	want := []string{"sha256:abc123", "sha512:def456"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("packageRegistryHashPairs() = %#v, want %#v", got, want)
	}
}
