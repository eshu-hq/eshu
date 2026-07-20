// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestBuildCICDRunCorrelationDecisionsPassesThroughCanonicalRepositoryID is a
// locking regression test proving the reducer's CI/CD correlation decode +
// classification path handles a canonical repository:r_<hex> repository_id
// end-to-end: the anchor is accepted, the correlation is produced, and the
// canonical repository_id is carried through into the decision.
func TestBuildCICDRunCorrelationDecisionsPassesThroughCanonicalRepositoryID(t *testing.T) {
	t.Parallel()

	canonicalRepoID := "repository:r_008329d6"

	// Pure classifier: proves canonical id flows through decode -> classify.
	decisions := BuildCICDRunCorrelationDecisions([]facts.Envelope{
		ciRunFact("run-canon", "github_actions", canonicalRepoID, "abc123"),
		ciRunFact("run-derived-canon", "github_actions", canonicalRepoID, "def456"),
		ciEnvironmentFact("env-derived-canon", "run-derived-canon", "staging"),
	})

	got := cicdDecisionsByRun(decisions)
	for _, key := range []string{"github_actions:run-canon:1", "github_actions:run-derived-canon:1"} {
		decision := got[key]
		if decision.RepositoryID != canonicalRepoID {
			t.Fatalf("RepositoryID = %q, want canonical %q for run %s", decision.RepositoryID, canonicalRepoID, key)
		}
		if decision.Outcome == CICDRunCorrelationUnresolved {
			t.Fatalf("Outcome = %q, canonical repo id must not cause unresolved for run %s", decision.Outcome, key)
		}
	}

	// Full handler + writer path: proves canonical id survives Handle -> Write.
	writer := &recordingCICDRunCorrelationWriter{}
	handler := CICDRunCorrelationHandler{
		FactLoader: &stubCICDRunCorrelationFactLoader{
			scopeFacts: []facts.Envelope{
				ciRunFact("run-canon", "github_actions", canonicalRepoID, "abc123"),
			},
		},
		Writer: writer,
	}
	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-canon",
		Domain:       DomainCICDRunCorrelation,
		ScopeID:      "scope-canon",
		GenerationID: "gen-canon",
		SourceSystem: "ci_cd_run",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("Status = %q, want succeeded", result.Status)
	}
	if writer.calls != 1 {
		t.Fatalf("writer calls = %d, want 1", writer.calls)
	}
	writtenDecision := writer.write.Decisions[0]
	if writtenDecision.RepositoryID != canonicalRepoID {
		t.Fatalf("written RepositoryID = %q, want canonical %q", writtenDecision.RepositoryID, canonicalRepoID)
	}
}
