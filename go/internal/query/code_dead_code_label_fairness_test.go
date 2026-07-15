// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"testing"
)

const laterLabelCandidateID = "later-class-candidate"

type saturatedFirstLabelDeadCodeStore struct {
	fakeDeadCodeContentStore
}

func newSaturatedFirstLabelDeadCodeStore() *saturatedFirstLabelDeadCodeStore {
	return &saturatedFirstLabelDeadCodeStore{
		fakeDeadCodeContentStore: fakeDeadCodeContentStore{
			entities: map[string]EntityContent{
				"function-test-root": {
					EntityID:     "function-test-root",
					RepoID:       "repo-1",
					RelativePath: "internal/payments/root_test.go",
					EntityType:   "Function",
					EntityName:   "testRoot",
					Language:     "go",
					SourceCache:  "func testRoot() {}",
				},
				laterLabelCandidateID: {
					EntityID:     laterLabelCandidateID,
					RepoID:       "repo-1",
					RelativePath: "internal/payments/later.py",
					EntityType:   "Class",
					EntityName:   "_LaterCandidate",
					Language:     "python",
					SourceCache:  "class _LaterCandidate: pass",
				},
			},
		},
	}
}

func (s *saturatedFirstLabelDeadCodeStore) DeadCodeCandidateRows(
	_ context.Context,
	_ string,
	label string,
	_ string,
	limit int,
	offset int,
) ([]map[string]any, error) {
	switch label {
	case "Function":
		rows := make([]map[string]any, limit)
		for i := range rows {
			rows[i] = deadCodeFairnessRow(
				"function-test-root",
				"testRoot",
				"Function",
				"go",
				"internal/payments/root_test.go",
			)
		}
		return rows, nil
	case "Class":
		if offset == 0 {
			return []map[string]any{deadCodeFairnessRow(
				laterLabelCandidateID,
				"_LaterCandidate",
				"Class",
				"python",
				"internal/payments/later.py",
			)}, nil
		}
	}
	return nil, nil
}

func deadCodeFairnessRow(entityID, name, label, language, path string) map[string]any {
	return map[string]any{
		"entity_id":  entityID,
		"name":       name,
		"labels":     []any{label},
		"file_path":  path,
		"repo_id":    "repo-1",
		"repo_name":  "payments",
		"language":   language,
		"start_line": int64(1),
		"end_line":   int64(2),
	}
}

func TestDeadCodeScanContinuesAfterFirstLabelSaturates(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j:   fakeGraphReader{},
		Content: newSaturatedFirstLabelDeadCodeStore(),
	}
	scan, err := handler.scanDeadCodeCandidates(context.Background(), deadCodeRequest{
		RepoID: "repo-1",
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("scanDeadCodeCandidates() error = %v", err)
	}

	assertDeadCodeRowsContainEntity(t, scan.Results, laterLabelCandidateID)
	assertDeadCodeFairnessScanMetadata(
		t,
		scan.CandidateScanTruncated,
		scan.DisplayTruncated,
		scan.CandidateScanRows,
		deadCodeCandidateScanLimit(10)+1,
	)
}

func TestDeadCodeInvestigationContinuesAfterFirstLabelSaturates(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j:   fakeGraphReader{},
		Content: newSaturatedFirstLabelDeadCodeStore(),
	}
	scan, err := handler.scanDeadCodeInvestigation(context.Background(), deadCodeInvestigationRequest{
		RepoID: "repo-1",
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("scanDeadCodeInvestigation() error = %v", err)
	}

	active := append(append([]map[string]any{}, scan.CleanupReady...), scan.Ambiguous...)
	assertDeadCodeRowsContainEntity(t, active, laterLabelCandidateID)
	assertDeadCodeFairnessScanMetadata(
		t,
		scan.CandidateScanTruncated,
		scan.DisplayTruncated,
		scan.CandidateScanRows,
		deadCodeCandidateScanLimit(10)+1,
	)
}

func TestCrossRepoDeadCodeContinuesAfterFirstLabelSaturates(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j:   fakeGraphReader{},
		Content: newSaturatedFirstLabelDeadCodeStore(),
	}
	scan, err := handler.scanCrossRepoDeadCodeCandidates(context.Background(), crossRepoDeadCodeRequest{
		RepoID: "repo-1",
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("scanCrossRepoDeadCodeCandidates() error = %v", err)
	}

	assertDeadCodeRowsContainEntity(t, scan.Active, laterLabelCandidateID)
	assertDeadCodeFairnessScanMetadata(
		t,
		scan.CandidateScanTruncated,
		scan.DisplayTruncated,
		scan.CandidateScanRows,
		deadCodeCandidateScanLimit(10)+1,
	)
}

func assertDeadCodeRowsContainEntity(t *testing.T, rows []map[string]any, entityID string) {
	t.Helper()
	for _, row := range rows {
		if StringVal(row, "entity_id") == entityID {
			return
		}
	}
	t.Fatalf("rows missing entity %q: %#v", entityID, rows)
}

func assertDeadCodeFairnessScanMetadata(
	t *testing.T,
	candidateTruncated bool,
	displayTruncated bool,
	rows int,
	wantRows int,
) {
	t.Helper()
	if !candidateTruncated {
		t.Fatal("candidate scan truncated = false, want true after a per-label bound")
	}
	if displayTruncated {
		t.Fatal("display truncated = true, want false with only one active candidate")
	}
	if rows != wantRows {
		t.Fatalf("candidate scan rows = %d, want %d", rows, wantRows)
	}
}
