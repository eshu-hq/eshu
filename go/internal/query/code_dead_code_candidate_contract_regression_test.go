// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codeshaping"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// Dead-code candidate-row ContentReader proofs that live in package query:
// they drive root's ContentReader SQL builders directly, which codequery
// cannot name without importing the root back (#6060). Split from
// codequery/dead_code_candidate_contract_regression_test.go at the
// lane-A move; the contract proofs stay there.

func TestContentReaderDeadCodeCandidateRowsKeepsTraitTypeAndRepositoryScope(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "entity_name", "entity_type", "repo_id", "relative_path",
				"language", "start_line", "end_line", "metadata",
			},
			rows: [][]driver.Value{
				{"trait-1", "Payments", "Trait", "repo-1", "src/payments.scala", "scala", 10, 20, []byte(`{}`)},
			},
		},
	})

	reader := NewContentReader(db)
	rows, err := reader.DeadCodeCandidateRows(context.Background(), codeshaping.DeadCodeCandidateQuery{RepoID: "repo-1", Label: "Trait", Language: "scala", Limit: 10})
	if err != nil {
		t.Fatalf("DeadCodeCandidateRows() error = %v, want nil", err)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	if got, want := StringSliceVal(rows[0], "labels"), []string{"Trait"}; !querytestutil.EqualStringSlices(got, want) {
		t.Fatalf("labels = %#v, want %#v", got, want)
	}
	if got, want := recorder.args[0][0], driver.Value("repo-1"); got != want {
		t.Fatalf("repo argument = %#v, want %#v", got, want)
	}
	if got, want := recorder.args[0][1], driver.Value("Trait"); got != want {
		t.Fatalf("entity type argument = %#v, want %#v", got, want)
	}
}

func TestContentReaderDeadCodeCandidateRowsRejectsUnknownLabelBeforeQuery(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, nil)
	reader := NewContentReader(db)

	rows, err := reader.DeadCodeCandidateRows(context.Background(), codeshaping.DeadCodeCandidateQuery{RepoID: "repo-1", Label: "Unknown", Limit: 10})
	if err == nil {
		t.Fatalf("DeadCodeCandidateRows() error = nil, want unsupported label error; rows=%#v", rows)
	}
	if got := len(recorder.queries); got != 0 {
		t.Fatalf("database query count = %d, want 0 for an unsupported label", got)
	}
}
