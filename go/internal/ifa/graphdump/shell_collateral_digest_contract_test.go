// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphdump

import "testing"

func TestShellCollateralFixtureNodeDigestContract(t *testing.T) {
	t.Parallel()

	got, err := nodeDigest(Node{
		Labels: []string{"Repository"},
		Props: map[string]any{
			"generation_id": "gen-1",
			"repo_id":       "repo-ifa-sql-family",
		},
	})
	if err != nil {
		t.Fatalf("nodeDigest() error = %v", err)
	}
	const want = "b3af008c122a125c24d4885578d29af6f24471d748db246d9d30a4fd8c1281f0"
	if got != want {
		t.Fatalf("nodeDigest() = %q, want shell collateral fixture contract %q", got, want)
	}
}
