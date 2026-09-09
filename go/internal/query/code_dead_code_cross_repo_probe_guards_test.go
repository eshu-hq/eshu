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
// codequery/dead_code_cross_repo_probe_guards_test.go at the lane-A
// move; the handler-level guard stays there.

func TestCrossRepoDeadCodeProbeRefusesAnEmptyGrant(t *testing.T) {
	t.Parallel()

	// A request that named its consumers gets no signal read at all: the
	// handler's plan leaves SignalGrant empty, and the reader takes that as
	// "do not run it" rather than as "run it unbounded". One statement, no
	// hidden rows. This is the reader's half of the contract; the plan's half
	// -- deciding when SignalGrant is empty -- is TestCrossRepoDeadCodeConsumerReadPlan's.
	t.Run("named consumers skip the signal read", func(t *testing.T) {
		t.Parallel()

		db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
			{columns: crossRepoDeadCodeEvidenceColumns()},
		})
		reader := NewContentReader(db)
		_, hidden, err := reader.CrossRepoDeadCodeConsumerEvidence(
			context.Background(),
			codeGrantGrantedRepo,
			[]string{"entity-1"},
			crossRepoDeadCodeConsumerReads{PageRepositoryIDs: []string{codeGrantConsumerRepo}},
		)
		if err != nil {
			t.Fatalf("CrossRepoDeadCodeConsumerEvidence() error = %v, want nil", err)
		}
		if len(hidden) != 0 {
			t.Fatalf("hidden = %#v, want empty", hidden)
		}
		if len(recorder.queries) != 1 {
			t.Fatalf("query count = %d, want 1; an empty SignalGrant must not reach the probe", len(recorder.queries))
		}
	})

	// The probe refuses an empty grant itself as well, so a caller that reaches
	// it directly gets the same refusal rather than a statement whose every
	// range is empty.
	t.Run("at the read itself", func(t *testing.T) {
		t.Parallel()

		db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
			{columns: []string{"entity_id"}, rows: [][]driver.Value{{"entity-1"}}},
		})
		reader := NewContentReader(db)
		hidden, err := reader.crossRepoDeadCodeUngrantedConsumers(
			context.Background(),
			codeGrantGrantedRepo,
			[]string{"entity-1"},
			nil,
		)
		if err != nil {
			t.Fatalf("crossRepoDeadCodeUngrantedConsumers() error = %v, want nil", err)
		}
		if len(hidden) != 0 {
			t.Fatalf("hidden = %#v, want empty; an empty grant hides everything, not nothing", hidden)
		}
		if len(recorder.queries) != 0 {
			t.Fatalf("query count = %d, want 0; the probe must not run without a grant", len(recorder.queries))
		}
	})
}

func TestCrossRepoDeadCodeProbeLeavesNoEntityUnproven(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 29, 13, 0, 0, 0, time.UTC)
	pageRows := [][]driver.Value{{
		"producer-late", codeGrantConsumerRepo, "checkout-api", "checkout-root",
		int64(1), "reachable", 0.95, codeprovenance.MethodImportBinding,
		[]byte(`["CALLS:checkout-root->producer-late"]`), []byte(`["go.main_function"]`),
		"gen-a", "active", observedAt, observedAt,
	}}
	db, _ := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{columns: crossRepoDeadCodeEvidenceColumns(), rows: pageRows},
		{columns: []string{"entity_id"}, rows: [][]driver.Value{{"producer-early"}}},
	})
	reader := NewContentReader(db)

	evidence, hidden, err := reader.CrossRepoDeadCodeConsumerEvidence(
		context.Background(),
		codeGrantGrantedRepo,
		[]string{"producer-early", "producer-late"},
		crossRepoDeadCodeConsumerReads{
			PageRepositoryIDs: []string{codeGrantConsumerRepo},
			SignalGrant:       []string{codeGrantConsumerRepo},
		},
	)
	if err != nil {
		t.Fatalf("CrossRepoDeadCodeConsumerEvidence() error = %v, want nil", err)
	}
	for _, entityID := range []string{"producer-early", "producer-late"} {
		if crossRepoDeadCodeTruncationMarked(evidence[entityID]) {
			t.Fatalf("evidence[%s] = %#v, want no truncation marker: the page was complete and the probe covers every entity", entityID, evidence[entityID])
		}
	}
	if got, want := len(evidence["producer-late"]), 1; got != want {
		t.Fatalf("len(evidence[producer-late]) = %d, want %d (the granted page row, nothing added)", got, want)
	}
	if !hidden.Has("producer-early") {
		t.Fatalf("hidden = %#v, want producer-early flagged", hidden)
	}
	if hidden.Has("producer-late") {
		t.Fatalf("hidden = %#v, want producer-late unflagged; the probe reported only producer-early", hidden)
	}
}
