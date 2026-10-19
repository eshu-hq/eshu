// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/workload/handle"
)

type stubDocHandleLookup struct {
	rows []handle.Candidate
}

func (s *stubDocHandleLookup) ListWorkloadHandleCandidates(_ context.Context, _ []string) ([]handle.Candidate, error) {
	return s.rows, nil
}

func docResolver() *handle.Resolver {
	return &handle.Resolver{
		Lookup: &stubDocHandleLookup{rows: []handle.Candidate{
			{ID: "workload:repository:r_alpha:checkout", Name: "checkout", RepoID: "repository:r_alpha"},
			{ID: "workload:repository:r_beta:checkout", Name: "checkout", RepoID: "repository:r_beta"},
			{ID: "workload:repository:r_alpha:billing", Name: "billing", RepoID: "repository:r_alpha"},
		}},
	}
}

func docRow(targetKind, targetID string) map[string]any {
	return map[string]any{
		"section_uid":      "docsection:doc1|sec1",
		"target_entity_id": targetID,
		"target_kind":      targetKind,
	}
}

// TestResolveDocumentationWorkloadTargetsLadder proves the migration T3 row
// for documentation mentions: a full id or unique handle writes the
// DOCUMENTS edge, an ambiguous handle is skipped and counted, never
// silent, and non-workload targets pass through untouched.
func TestResolveDocumentationWorkloadTargetsLadder(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{
		docRow("workload", "workload:repository:r_alpha:checkout"),
		docRow("workload", "workload:billing"),
		docRow("workload", "workload:checkout"),
		docRow("workload", "workload:absent"),
		docRow("repository", "repository:r_alpha"),
	}

	kept, err := ResolveDocumentationWorkloadTargets(context.Background(), rows, "scope-1", "gen-1", docResolver())
	if err != nil {
		t.Fatalf("ResolveDocumentationWorkloadTargets() error = %v", err)
	}
	if len(kept) != 4 {
		t.Fatalf("kept rows = %d, want 4 (ambiguous handle drops)", len(kept))
	}
	byTarget := map[string]string{}
	for _, row := range kept {
		byTarget[anyToString(row["target_entity_id"])] = anyToString(row["target_kind"])
	}
	if byTarget["workload:repository:r_alpha:checkout"] != "workload" {
		t.Fatalf("kept targets = %v, want the full id edge kept", byTarget)
	}
	if byTarget["workload:repository:r_alpha:billing"] != "workload" {
		t.Fatalf("kept targets = %v, want the unique handle rewritten to its node id", byTarget)
	}
	if byTarget["workload:absent"] != "workload" {
		t.Fatalf("kept targets = %v, want the miss kept for the writer no-op", byTarget)
	}
	if byTarget["repository:r_alpha"] != "repository" {
		t.Fatalf("kept targets = %v, want non-workload targets untouched", byTarget)
	}
	for _, row := range kept {
		if anyToString(row["target_entity_id"]) == "workload:checkout" {
			t.Fatalf("kept rows contain the spanning handle: ambiguous must drop, never link")
		}
	}
}

func TestResolveDocumentationWorkloadTargetsNilResolverKeepsLegacy(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{docRow("workload", "workload:checkout")}
	kept, err := ResolveDocumentationWorkloadTargets(context.Background(), rows, "scope-1", "gen-1", nil)
	if err != nil {
		t.Fatalf("ResolveDocumentationWorkloadTargets() error = %v", err)
	}
	if len(kept) != 1 || anyToString(kept[0]["target_entity_id"]) != "workload:checkout" {
		t.Fatalf("kept = %v, want legacy pass-through when unwired", kept)
	}
}
