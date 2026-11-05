// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestFindRepositoryFindingHoldersQueryShape guards the contract the #10059
// reopen depends on: the finding fact kind is a literal (a bound parameter
// would not prove the partial-index predicate of
// fact_records_supply_chain_impact_repository_lookup_idx under a generic
// plan), the repository predicate names payload->>'repository_id', only
// live rows of the scope's active generation match, and the asking scope is
// excluded.
func TestFindRepositoryFindingHoldersQueryShape(t *testing.T) {
	t.Parallel()

	literal := "finding.fact_kind = '" + facts.ReducerSupplyChainImpactFindingFactKind + "'"
	if !strings.Contains(findRepositoryFindingHoldersQuery, literal) {
		t.Fatalf("repository holders query lacks the fact-kind literal %q", literal)
	}
	for _, want := range []string{
		"finding.payload->>'repository_id' = ANY($1::text[])",
		"finding.is_tombstone = FALSE",
		"scope.active_generation_id = finding.generation_id",
		"finding.scope_id <> $2",
	} {
		if !strings.Contains(findRepositoryFindingHoldersQuery, want) {
			t.Fatalf("repository holders query lacks %q:\n%s", want, findRepositoryFindingHoldersQuery)
		}
	}
}
