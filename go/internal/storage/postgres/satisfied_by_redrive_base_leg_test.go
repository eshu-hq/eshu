// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"
	"strings"
	"testing"

	crossplanestore "github.com/eshu-hq/eshu/go/internal/storage/postgres/crossplane"
)

// Markers of the shipped redrive discovery text's DISTINCT subquery branch.
const (
	crossplaneRedriveBranchOpen  = "FROM (\n    SELECT DISTINCT fact.scope_id, fact.generation_id\n    FROM fact_records AS fact\n    WHERE "
	crossplaneRedriveBranchClose = "\n) AS fact\n"
	crossplaneRedriveGenJoin     = "JOIN scope_generations AS generation\n  ON generation.scope_id = fact.scope_id\n AND generation.generation_id = fact.generation_id\n"
)

// crossplaneRedrivePerClaimText derives the per-claim form of the shipped
// discovery text (#10094 differential): the DISTINCT subquery is un-nested,
// so every matching claim row joins the scope and generation before
// DISTINCT ON collapses the scope. It is built from the shipped constant,
// never a hand copy, and fails when the markers drift.
func crossplaneRedrivePerClaimText(shipped string) (string, error) {
	open := strings.Index(shipped, crossplaneRedriveBranchOpen)
	closeAt := strings.Index(shipped, crossplaneRedriveBranchClose)
	if open < 0 || closeAt < open {
		return "", fmt.Errorf("redrive discovery text has no DISTINCT subquery branch")
	}
	inner := shipped[open+len(crossplaneRedriveBranchOpen) : closeAt]
	rest := shipped[closeAt+len(crossplaneRedriveBranchClose):]
	where := strings.Index(rest, "\nWHERE ")
	order := strings.Index(rest, "\nORDER BY")
	if where < 0 || order < where {
		return "", fmt.Errorf("redrive discovery text has no outer WHERE before ORDER BY")
	}
	joins, outer, tail := rest[:where+1], rest[where+len("\nWHERE "):order], rest[order:]
	return shipped[:open] + "FROM fact_records AS fact\n" + joins + "WHERE " + inner + "\n  AND " + outer + tail, nil
}

// crossplaneRedrivePreFixText strips the generation join and its active
// fragment from the per-claim text: the discovery text before #10094.
func crossplaneRedrivePreFixText(perClaim string) (string, error) {
	if strings.Count(perClaim, crossplaneRedriveGenJoin) != 1 || strings.Count(perClaim, "\n  "+deltaOverlayActiveGeneration) != 1 {
		return "", fmt.Errorf("per-claim text has no single generation join")
	}
	text := strings.Replace(perClaim, crossplaneRedriveGenJoin, "", 1)
	return strings.Replace(text, "\n  "+deltaOverlayActiveGeneration, "", 1), nil
}

// TestCrossplaneRedriveDeltaBaseQueryDerivesFromShippedText pins the #10094
// derivation: the shipped discovery text passes the overlay shape check,
// the candidate base generation ids bind once at $6 outside the DISTINCT
// branch (so the planner pushes the ANY into the branch's index scan), and
// the active fragments are gone from the base.
func TestCrossplaneRedriveDeltaBaseQueryDerivesFromShippedText(t *testing.T) {
	t.Parallel()
	shipped := crossplanestore.ListCrossplaneRedriveTargetScopesQuery
	param := crossplanestore.CrossplaneRedriveDeltaBaseGenerationParam
	if placeholderUsed(shipped, param) {
		t.Fatalf("shipped text already binds $%d", param)
	}
	base, err := deriveDeltaOverlayBaseQuery(shipped, param)
	if err != nil {
		t.Fatalf("deriveDeltaOverlayBaseQuery(shipped, %d): %v", param, err)
	}
	if strings.Count(base, "$6::text[]") != 1 {
		t.Fatalf("base binds $6 %d times, want once:\n%s", strings.Count(base, "$6::text[]"), base)
	}
	if bound, branchEnd := strings.Index(base, "$6::text[]"), strings.Index(base, crossplaneRedriveBranchClose); bound < branchEnd {
		t.Fatalf("base binds $6 inside the DISTINCT branch:\n%s", base)
	}
	for _, fragment := range []string{deltaOverlayActiveScopeJoin, deltaOverlayActiveGeneration} {
		if strings.Contains(base, fragment) {
			t.Fatalf("base still carries the active fragment %q:\n%s", fragment, base)
		}
	}
	perClaim, err := crossplaneRedrivePerClaimText(shipped)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deriveDeltaOverlayBaseQuery(perClaim, param); err != nil {
		t.Fatalf("per-claim text does not derive: %v", err)
	}
	if _, err := crossplaneRedrivePreFixText(perClaim); err != nil {
		t.Fatal(err)
	}
}

// TestCrossplaneRedriveDeltaBaseWithoutGenerationsRunsNoStatement pins the
// no-delta path: an empty base-generation set returns before any statement.
func TestCrossplaneRedriveDeltaBaseWithoutGenerationsRunsNoStatement(t *testing.T) {
	t.Parallel()
	store := FactStore{database: SQLDB{}}
	targets, err := store.ListCrossplaneRedriveDeltaBaseTargetScopes(t.Context(), crossplanestore.CrossplaneRedriveTargetPage{Limit: 10}, nil)
	if err != nil || targets != nil {
		t.Fatalf("ListCrossplaneRedriveDeltaBaseTargetScopes(no generations) = (%v, %v), want (nil, nil)", targets, err)
	}
}
