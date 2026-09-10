// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codequery/deadcode"
)

// TestCrossRepoDeadCodeConsumerEvidenceBindsTheGrantInTheShippedSQL moved out
// of codequery/auth_scoped_code_dead_code_cross_repo_grant_test.go at the
// #6060 CodeHandler move: it drives the real root *ContentReader over a
// recording SQL fake, which cannot move to codequery without recreating
// ContentReader there.
func TestCrossRepoDeadCodeConsumerEvidenceBindsTheGrantInTheShippedSQL(t *testing.T) {
	t.Parallel()

	t.Run("scoped", func(t *testing.T) {
		t.Parallel()

		db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
			{columns: crossRepoDeadCodeEvidenceColumns()},
			{columns: []string{"entity_id", "hidden_count"}},
		})
		reader := NewContentReader(db)
		if _, _, err := reader.CrossRepoDeadCodeConsumerEvidence(
			context.Background(),
			codeGrantGrantedRepo,
			[]string{"entity-1"},
			crossRepoDeadCodeConsumerReads{
				PageRepositoryIDs: []string{codeGrantConsumerRepo},
				SignalGrant:       []string{codeGrantConsumerRepo},
			},
		); err != nil {
			t.Fatalf("CrossRepoDeadCodeConsumerEvidence() error = %v, want nil", err)
		}
		if len(recorder.queries) != 2 {
			t.Fatalf("query count = %d, want 2 (grant-bound evidence page plus ungranted-consumer probe)", len(recorder.queries))
		}
		want := "AND row.repository_id = ANY($3)"
		if !strings.Contains(recorder.queries[0], want) {
			t.Fatalf("consumer-evidence SQL is missing %q, so the LIMIT is still drawn from every tenant's rows:\n%s", want, recorder.queries[0])
		}
		if recorder.queries[1] != deadcode.CrossRepoDeadCodeUngrantedConsumerProbeQuery {
			t.Fatalf("second statement is not the ungranted-consumer probe:\n%s", recorder.queries[1])
		}
		bound := fmt.Sprintf("%s", recorder.args[0][2])
		if !strings.Contains(bound, codeGrantConsumerRepo) {
			t.Fatalf("grant argument = %q, want the encoded Postgres array carrying %q", bound, codeGrantConsumerRepo)
		}
	})

	t.Run("unscoped", func(t *testing.T) {
		t.Parallel()

		db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
			{columns: crossRepoDeadCodeEvidenceColumns()},
		})
		reader := NewContentReader(db)
		if _, _, err := reader.CrossRepoDeadCodeConsumerEvidence(
			context.Background(),
			codeGrantGrantedRepo,
			[]string{"entity-1"},
			crossRepoDeadCodeConsumerReads{},
		); err != nil {
			t.Fatalf("CrossRepoDeadCodeConsumerEvidence() error = %v, want nil", err)
		}
		if len(recorder.queries) != 1 {
			t.Fatalf("query count = %d, want 1 -- an unscoped caller must not pay for the probe", len(recorder.queries))
		}
		if strings.Contains(recorder.queries[0], "ANY(") {
			t.Fatalf("unscoped consumer-evidence SQL gained a grant clause:\n%s", recorder.queries[0])
		}
	})
}
