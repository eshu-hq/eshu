// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestFindRepositoryFindingHoldersQueryShape guards the contract the #10059
// reopen depends on, now on the supports table (#11197): the read names the
// per-support table and its repository column, never a finding fact; a blank
// repository id never matches the empty id a finding without one stores; only
// supports of the scope's active generation match; and the asking scope is
// excluded.
func TestFindRepositoryFindingHoldersQueryShape(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"FROM supply_chain_impact_finding_supports AS support",
		"support.repository_id = ANY($1::text[])",
		"support.repository_id <> ''",
		"scope.active_generation_id = support.generation_id",
		"support.scope_id <> $2",
	} {
		if !strings.Contains(findRepositoryFindingHoldersQuery, want) {
			t.Fatalf("repository holders query lacks %q:\n%s", want, findRepositoryFindingHoldersQuery)
		}
	}
	for _, banned := range []string{"fact_records", facts.ReducerSupplyChainImpactFindingFactKind, "is_tombstone"} {
		if strings.Contains(findRepositoryFindingHoldersQuery, banned) {
			t.Fatalf("repository holders query still reads the finding facts (%q)", banned)
		}
	}
}
