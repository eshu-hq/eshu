// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codeshaping"
)

// TestDeadCodeCandidateRowsBindTheGrantInTheShippedSQL moved out of
// codequery/auth_scoped_code_dead_code_grant_test.go at the #6060
// CodeHandler move: it drives the real root *ContentReader over a recording
// SQL fake, which cannot move to codequery without recreating ContentReader
// there.
func TestDeadCodeCandidateRowsBindTheGrantInTheShippedSQL(t *testing.T) {
	t.Parallel()

	t.Run("scoped", func(t *testing.T) {
		t.Parallel()

		db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{{
			columns: []string{
				"entity_id", "entity_name", "entity_type", "repo_id", "relative_path",
				"language", "start_line", "end_line", "metadata",
			},
		}})
		reader := NewContentReader(db)
		if _, err := reader.DeadCodeCandidateRows(context.Background(), codeshaping.DeadCodeCandidateQuery{
			Label:                "Function",
			Limit:                10,
			AllowedRepositoryIDs: []string{codeGrantGrantedRepo},
		}); err != nil {
			t.Fatalf("DeadCodeCandidateRows() error = %v, want nil", err)
		}
		if len(recorder.queries) != 1 {
			t.Fatalf("query count = %d, want 1", len(recorder.queries))
		}
		if want := "AND repo_id = ANY($4)"; !strings.Contains(recorder.queries[0], want) {
			t.Fatalf("candidate SQL is missing %q; a scoped caller's grant is bound but never applied:\n%s", want, recorder.queries[0])
		}
		// The grant argument must land at $4, ahead of LIMIT/OFFSET, or the
		// placeholders the statement renders point at the wrong values.
		bound := fmt.Sprintf("%s", recorder.args[0][3])
		if !strings.Contains(bound, codeGrantGrantedRepo) {
			t.Fatalf("grant argument = %q, want the encoded Postgres array carrying %q", bound, codeGrantGrantedRepo)
		}
	})

	t.Run("unscoped", func(t *testing.T) {
		t.Parallel()

		db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{{
			columns: []string{
				"entity_id", "entity_name", "entity_type", "repo_id", "relative_path",
				"language", "start_line", "end_line", "metadata",
			},
		}})
		reader := NewContentReader(db)
		if _, err := reader.DeadCodeCandidateRows(context.Background(), codeshaping.DeadCodeCandidateQuery{
			Label: "Function",
			Limit: 10,
		}); err != nil {
			t.Fatalf("DeadCodeCandidateRows() error = %v, want nil", err)
		}
		if strings.Contains(recorder.queries[0], "ANY(") {
			t.Fatalf("unscoped candidate SQL gained a grant clause:\n%s", recorder.queries[0])
		}
		if got, want := len(recorder.args[0]), 5; got != want {
			t.Fatalf("argument count = %d, want %d for an unscoped scan", got, want)
		}
	})
}
