// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicdrun

import (
	"context"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite/factwritetest"
)

// TestWriteCICDRunCorrelationsBoundedExecCount is the regression guard for
// issue #3435. Writing N decisions must issue O(N/batchSize) bulk inserts, not
// one ExecContext per decision, so a large generation cannot monopolise a
// reducer worker with serial round-trips. A per-row loop would produce N=400
// calls; the batched writer must stay at ceil(N/factwrite.BatchSize).
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

	db := &factwritetest.FakeExecer{}
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

	wantExecs := factwritetest.ExpectedBatchedExecCount(decisionCount)
	if got := len(db.Execs); got != wantExecs {
		t.Fatalf("ExecContext calls = %d for %d decisions, want %d (bounded batched inserts)", got, decisionCount, wantExecs)
	}
	if rows := factwritetest.DecodeBatchedFactCalls(t, db.Execs); len(rows) != decisionCount {
		t.Fatalf("decoded rows = %d, want %d", len(rows), decisionCount)
	}
}
