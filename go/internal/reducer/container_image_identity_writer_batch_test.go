// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestWriteContainerImageIdentityDecisionsBoundedExecCount guards issue #3435:
// N canonical decisions must be persisted in O(N/batchSize) bulk inserts rather
// than one ExecContext per decision.
func TestWriteContainerImageIdentityDecisionsBoundedExecCount(t *testing.T) {
	t.Parallel()

	const decisionCount = 400
	decisions := make([]ContainerImageIdentityDecision, decisionCount)
	for i := range decisions {
		decisions[i] = ContainerImageIdentityDecision{
			ImageRef:         fmt.Sprintf("registry.example.com/team/api:tag-%d", i),
			Digest:           testContainerDigest,
			RepositoryID:     "oci-registry://registry.example.com/team/api",
			Outcome:          ContainerImageIdentityTagResolved,
			CanonicalWrites:  1,
			IdentityStrength: "tag_observation_with_digest",
		}
	}

	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresContainerImageIdentityWriter{DB: db}

	result, err := writer.WriteContainerImageIdentityDecisions(context.Background(), ContainerImageIdentityWrite{
		IntentID:     "intent-image-batch",
		ScopeID:      "repo:team-api",
		GenerationID: "gen-batch",
		SourceSystem: "git",
		EvidenceAsOf: time.Date(2026, time.July, 27, 10, 0, 0, 0, time.UTC),
		Decisions:    decisions,
	})
	if err != nil {
		t.Fatalf("WriteContainerImageIdentityDecisions() error = %v", err)
	}
	if got, want := result.CanonicalWrites, decisionCount; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}

	// Insert statements only. The trailing generation-authoritative retire is a
	// single set-based DELETE regardless of decision count, so excluding it here
	// keeps this assertion measuring insert batching — the N+1 regression it
	// guards — rather than total statement count.
	wantExecs := expectedBatchedExecCount(decisionCount)
	if got := len(containerImageIdentityInsertCalls(db.execs)); got != wantExecs {
		t.Fatalf("insert ExecContext calls = %d for %d decisions, want %d (bounded batched inserts)", got, decisionCount, wantExecs)
	}
	if rows := decodeBatchedFactCalls(t, containerImageIdentityInsertCalls(db.execs)); len(rows) != decisionCount {
		t.Fatalf("decoded rows = %d, want %d", len(rows), decisionCount)
	}
}
