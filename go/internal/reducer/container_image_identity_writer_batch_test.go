package reducer

import (
	"context"
	"fmt"
	"testing"
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
		Decisions:    decisions,
	})
	if err != nil {
		t.Fatalf("WriteContainerImageIdentityDecisions() error = %v", err)
	}
	if got, want := result.CanonicalWrites, decisionCount; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}

	wantExecs := expectedBatchedExecCount(decisionCount)
	if got := len(db.execs); got != wantExecs {
		t.Fatalf("ExecContext calls = %d for %d decisions, want %d (bounded batched inserts)", got, decisionCount, wantExecs)
	}
	if rows := decodeBatchedFactCalls(t, db.execs); len(rows) != decisionCount {
		t.Fatalf("decoded rows = %d, want %d", len(rows), decisionCount)
	}
}
