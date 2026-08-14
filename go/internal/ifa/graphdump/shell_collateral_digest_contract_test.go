// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphdump

import "testing"

func TestShellCollateralFixtureNodeDigestContract(t *testing.T) {
	t.Parallel()

	got, err := nodeDigest(Node{
		Labels: []string{"Repository"},
		Props: map[string]any{
			"generation_id": "gen1",
			"repo_id":       "repo-ifa-sql-family",
		},
	})
	if err != nil {
		t.Fatalf("nodeDigest() error = %v", err)
	}
	const want = "8ea9d5d8c0eabf08ef3c18ad4b6617a6466c707f7f579bac7017a7b6497d129a"
	if got != want {
		t.Fatalf("nodeDigest() = %q, want shell collateral fixture contract %q", got, want)
	}
}
