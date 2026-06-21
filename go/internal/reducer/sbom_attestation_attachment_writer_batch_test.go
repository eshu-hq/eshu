package reducer

import (
	"context"
	"fmt"
	"testing"
)

// TestWriteSBOMAttestationAttachmentsBoundedExecCount guards issue #3435: every
// attachment status is persisted, and N decisions must cost O(N/batchSize) bulk
// inserts rather than one ExecContext per decision.
func TestWriteSBOMAttestationAttachmentsBoundedExecCount(t *testing.T) {
	t.Parallel()

	const decisionCount = 400
	decisions := make([]SBOMAttestationAttachmentDecision, decisionCount)
	for i := range decisions {
		decisions[i] = SBOMAttestationAttachmentDecision{
			DocumentID:       fmt.Sprintf("doc-%d", i),
			AttachmentStatus: SBOMAttachmentAttachedVerified,
			CanonicalWrites:  1,
		}
	}

	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresSBOMAttestationAttachmentWriter{DB: db}

	result, err := writer.WriteSBOMAttestationAttachments(context.Background(), SBOMAttestationAttachmentWrite{
		IntentID:     "intent-sbom-batch",
		ScopeID:      "repo:team-api",
		GenerationID: "gen-batch",
		SourceSystem: "sbom",
		Decisions:    decisions,
	})
	if err != nil {
		t.Fatalf("WriteSBOMAttestationAttachments() error = %v", err)
	}
	if got, want := result.FactsWritten, decisionCount; got != want {
		t.Fatalf("FactsWritten = %d, want %d", got, want)
	}

	wantExecs := expectedBatchedExecCount(decisionCount)
	if got := len(db.execs); got != wantExecs {
		t.Fatalf("ExecContext calls = %d for %d decisions, want %d (bounded batched inserts)", got, decisionCount, wantExecs)
	}
	if rows := decodeBatchedFactCalls(t, db.execs); len(rows) != decisionCount {
		t.Fatalf("decoded rows = %d, want %d", len(rows), decisionCount)
	}
}
