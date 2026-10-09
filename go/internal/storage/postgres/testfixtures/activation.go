// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package testfixtures

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// ActivationRepositoryFact builds a repository fact envelope for the
// activation obligation proofs. It merges the identical
// activationRepositoryFact twins from
// ingestion_targeted_maintenance_terminal_live_test.go and
// activation/helpers_test.go (#7648).
func ActivationRepositoryFact(factID, scopeID, generationID, repoID, remote string) facts.Envelope {
	return facts.Envelope{
		FactID: factID, ScopeID: scopeID, GenerationID: generationID,
		FactKind: "repository", StableFactKey: "repository:" + repoID,
		ObservedAt: time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC),
		Payload:    map[string]any{"graph_id": repoID, "remote_url": remote},
		SourceRef:  facts.Ref{SourceSystem: "git", FactKey: factID},
	}
}

// AssertObligationStateToken asserts one obligation row's state, claim
// token, and finished marker. It merges the identical
// assertObligationStateToken twins from
// ingestion_targeted_maintenance_terminal_live_test.go and
// activation/terminal_live_test.go (#7648).
func AssertObligationStateToken(t *testing.T, ctx context.Context, database *sql.DB,
	scopeID, generationID, wantState string, wantToken int64,
) {
	t.Helper()
	var state string
	var token int64
	var finished bool
	if err := database.QueryRowContext(ctx, `SELECT state, claim_token, finished_at IS NOT NULL
FROM activation_obligations WHERE scope_id = $1 AND generation_id = $2`, scopeID, generationID).Scan(&state, &token, &finished); err != nil {
		t.Fatalf("read obligation %s/%s: %v", scopeID, generationID, err)
	}
	terminal := wantState == "completed" || wantState == "obsolete" || wantState == "inapplicable"
	if state != wantState || token != wantToken || finished != terminal {
		t.Fatalf("obligation %s/%s = state %q token %d finished %t, want %q token %d finished %t",
			scopeID, generationID, state, token, finished, wantState, wantToken, terminal)
	}
}
