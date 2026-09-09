// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

// Cross-repo dead-code ContentReader proofs that live in package query: they
// drive root's ContentReader SQL builders directly, which codequery cannot
// name without importing the root back (#6060). Split from
// codequery/code_dead_code_cross_repo_review_test.go at the lane-A move; the
// handler-level review proofs stay there.

func TestContentReaderCrossRepoDeadCodeEvidenceMarksMissingEntitiesUnknownWhenTruncated(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 29, 13, 0, 0, 0, time.UTC)
	rows := make([][]driver.Value, 0, maxCrossRepoDeadCodeConsumerEvidenceRows+1)
	for i := 0; i < maxCrossRepoDeadCodeConsumerEvidenceRows+1; i++ {
		rows = append(rows, []driver.Value{
			"producer-live", "repo-consumer", "checkout-api", "checkout-root",
			int64(2), "reachable", 0.9, codeprovenance.MethodImportBinding,
			[]byte(`["CALLS:checkout-root->producer-live"]`), []byte(`["go.main_function"]`),
			"gen-a", "active", observedAt, observedAt,
		})
	}
	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{{
		columns: []string{
			"entity_id", "repository_id", "consumer_repo_name", "root_entity_id",
			"depth", "state", "confidence", "min_resolution_method", "evidence",
			"root_kinds", "generation_id", "generation_status", "observed_at", "updated_at",
		},
		rows: rows,
	}})
	reader := NewContentReader(db)

	evidence, _, err := reader.CrossRepoDeadCodeConsumerEvidence(
		context.Background(),
		"repo-producer",
		[]string{"producer-live", "producer-missing"},
		crossRepoDeadCodeConsumerReads{},
	)
	if err != nil {
		t.Fatalf("CrossRepoDeadCodeConsumerEvidence() error = %v, want nil", err)
	}
	missing := evidence["producer-missing"]
	if got, want := len(missing), 1; got != want {
		t.Fatalf("len(evidence[producer-missing]) = %d, want %d", got, want)
	}
	if got, want := missing[0].Reason, "consumer_evidence_truncated"; got != want {
		t.Fatalf("truncation reason = %q, want %q", got, want)
	}
	if !missing[0].NeedsEvidence {
		t.Fatal("truncation evidence NeedsEvidence = false, want true")
	}
	if got, want := missing[0].Citation, "code_reachability_rows:truncated"; got != want {
		t.Fatalf("truncation citation = %q, want %q", got, want)
	}
	// The read stopped inside this entity's own rows, so it is unproven too:
	// it keeps every row that fit and takes the marker on top of them.
	live := evidence["producer-live"]
	if got, want := len(live), maxCrossRepoDeadCodeConsumerEvidenceRows+1; got != want {
		t.Fatalf("len(evidence[producer-live]) = %d, want %d (the rows that fit plus the truncation marker)", got, want)
	}
	if got, want := live[len(live)-1].Reason, "consumer_evidence_truncated"; got != want {
		t.Fatalf("evidence[producer-live] last reason = %q, want %q", got, want)
	}
	if got, want := live[0].ConsumerRepoID, "repo-consumer"; got != want {
		t.Fatalf("evidence[producer-live][0].ConsumerRepoID = %q, want %q; the marker must not replace the rows read", got, want)
	}
	if !containsAllSubstrings(recorder.queries[0], "LIMIT 1001") {
		t.Fatalf("query missing sentinel limit:\n%s", recorder.queries[0])
	}
}
