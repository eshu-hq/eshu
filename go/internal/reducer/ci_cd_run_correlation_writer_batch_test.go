package reducer

import (
	"context"
	"fmt"
	"testing"
)

// TestWriteCICDRunCorrelationsBoundedExecCount is the regression guard for
// issue #3435. Writing N decisions must issue O(N/batchSize) bulk inserts, not
// one ExecContext per decision, so a large generation cannot monopolise a
// reducer worker with serial round-trips. A per-row loop would produce N=400
// calls; the batched writer must stay at ceil(N/reducerFactBatchSize).
func TestWriteCICDRunCorrelationsBoundedExecCount(t *testing.T) {
	t.Parallel()

	const decisionCount = 400
	decisions := make([]CICDRunCorrelationDecision, decisionCount)
	for i := range decisions {
		decisions[i] = CICDRunCorrelationDecision{
			Provider:        "github_actions",
			RunID:           fmt.Sprintf("run-%d", i),
			RunAttempt:      "1",
			RepositoryID:    "repo-api",
			Outcome:         CICDRunCorrelationDerived,
			CanonicalWrites: 0,
		}
	}

	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresCICDRunCorrelationWriter{DB: db}

	result, err := writer.WriteCICDRunCorrelations(context.Background(), CICDRunCorrelationWrite{
		IntentID:     "intent-cicd-batch",
		ScopeID:      "ci://github-actions/acme/api",
		GenerationID: "gen-batch",
		SourceSystem: "ci_cd_run",
		Decisions:    decisions,
	})
	if err != nil {
		t.Fatalf("WriteCICDRunCorrelations() error = %v", err)
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
