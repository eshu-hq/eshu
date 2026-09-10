// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/deadcode"
)

// Cross-repo dead-code ContentReader proofs that live in package query: they
// drive root's ContentReader SQL builders directly, which codequery cannot
// name without importing the root back (#6060). Split from
// codequery/dead_code_cross_repo_classification_test.go at the lane-A
// move; the handler-level classification proofs stay there.

func TestCrossRepoDeadCodeCompletesTheEntityTheSentinelMovedPast(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 29, 13, 0, 0, 0, time.UTC)
	row := func(entityID string) []driver.Value {
		return []driver.Value{
			entityID, codeGrantConsumerRepo, "checkout-api", "checkout-root",
			int64(1), "reachable", 0.95, codeprovenance.MethodImportBinding,
			[]byte(`["CALLS:checkout-root->` + entityID + `"]`), []byte(`["go.main_function"]`),
			"gen-a", "active", observedAt, observedAt,
		}
	}
	rows := make([][]driver.Value, 0, maxCrossRepoDeadCodeConsumerEvidenceRows+1)
	for i := 0; i < maxCrossRepoDeadCodeConsumerEvidenceRows; i++ {
		rows = append(rows, row("producer-complete"))
	}
	rows = append(rows, row("producer-next"))

	db, _ := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{columns: crossRepoDeadCodeEvidenceColumns(), rows: rows},
	})
	reader := NewContentReader(db)

	evidence, _, err := reader.CrossRepoDeadCodeConsumerEvidence(
		context.Background(),
		codeGrantGrantedRepo,
		[]string{"producer-complete", "producer-next"},
		crossRepoDeadCodeConsumerReads{},
	)
	if err != nil {
		t.Fatalf("CrossRepoDeadCodeConsumerEvidence() error = %v, want nil", err)
	}
	if crossRepoDeadCodeTruncationMarked(evidence["producer-complete"]) {
		t.Fatalf("evidence[producer-complete] carries consumer_evidence_truncated, but the sentinel row belonged to the next entity, which proves this one was read in full")
	}
	if got, want := len(evidence["producer-complete"]), maxCrossRepoDeadCodeConsumerEvidenceRows; got != want {
		t.Fatalf("len(evidence[producer-complete]) = %d, want %d", got, want)
	}
	if !crossRepoDeadCodeTruncationMarked(evidence["producer-next"]) {
		t.Fatalf("evidence[producer-next] = %#v, want the marker: its rows start at the dropped sentinel", evidence["producer-next"])
	}
}

func crossRepoDeadCodeTruncationMarked(rows []deadcode.CrossRepoDeadCodeEvidence) bool {
	for _, row := range rows {
		if row.NeedsEvidence && row.Reason == "consumer_evidence_truncated" {
			return true
		}
	}
	return false
}
