// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import "testing"

// TestBoltPropertyStringJoinsListValues is the shaped regression for the
// #5820 P2 review finding: the golden-corpus gate's new PackageArtifact
// `hashes` required_nodes assertion (testdata/golden/e2e-20repo-snapshot.json)
// asserts a LIST-typed property (a sorted "algorithm:digest" string list,
// packageRegistryHashPairs in
// go/internal/storage/cypher/package_registry_artifact_writer.go). Before this
// fix, boltPropertyString returned "" for anything that was not already a
// scalar Go string, so ListNodeProperty/EvaluateNodeProperty would see every
// PackageArtifact node as NOT carrying `hashes` and the assertion would fail
// on a live run even when the writer is correct -- an always-red assertion,
// strictly worse than no assertion. A Neo4j/NornicDB Bolt list property
// decodes off the wire as []any of strings (mirrors edgeEvidenceContainsAll's
// evidence_kinds handling), so this proves boltPropertyString now joins a
// list property's elements into one string the non-empty/allowed-value check
// can compare against a pinned exact value.
func TestBoltPropertyStringJoinsListValues(t *testing.T) {
	cases := []struct {
		name string
		raw  any
		want string
	}{
		{"scalar string unchanged", "sha256:abc", "sha256:abc"},
		{"bolt list any joins with pipe", []any{"sha256:abc", "sha512:def"}, "sha256:abc|sha512:def"},
		{"single-element list", []any{"sha256:abc"}, "sha256:abc"},
		{"string slice form joins with pipe", []string{"sha256:abc", "sha512:def"}, "sha256:abc|sha512:def"},
		{"nil property", nil, ""},
		{"empty list", []any{}, ""},
		{"non-string element yields empty", []any{"sha256:abc", 42}, ""},
		{"non-string non-list scalar", 42, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := boltPropertyString(tc.raw); got != tc.want {
				t.Errorf("boltPropertyString(%#v) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}
