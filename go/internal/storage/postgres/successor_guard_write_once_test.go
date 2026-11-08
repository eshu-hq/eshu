// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// requireWriteOnceRefused asserts the benign write-once refusal (#10037):
// rolled back with no projector work, and — unlike a destructive-successor
// refusal — no skipped-refresh record and no successor hold, because the
// prior generation is final by design, not stale.
func requireWriteOnceRefused(t *testing.T, database *guardRoutingDB, decisions map[guardDecisionKey]int64) {
	t.Helper()
	if !database.tx.rolledBack || database.tx.committed {
		t.Fatalf("rolled back = %v, committed = %v, want a rollback with no commit", database.tx.rolledBack, database.tx.committed)
	}
	if database.enqueuedProjectorWork() {
		t.Fatal("write-once refused successor enqueued projector work")
	}
	if got := telemetry.SkippedRefreshCountByReason(telemetry.RefreshSkipReasonDestructiveSuccessor); got != 0 {
		t.Fatalf("destructive_successor skips = %d, want 0 for a benign write-once refusal", got)
	}
	requireDecisions(t, decisions, guardDecisionKey{
		telemetry.SuccessorGuardKindFull, telemetry.SuccessorGuardRuleWriteOnce, telemetry.SuccessorGuardOutcomeRefused,
	})
}

// A write-once successor over an active prior refuses even at full size:
// the first committed generation of a write-once scope is final.
func TestGuardWriteOnceRefusesOverActivePrior(t *testing.T) {
	scopeValue, generation := guardDeltaFixture(false)
	generation.WriteOnce = true
	database := newGuardRoutingDB(t, 100, 300, 20)
	stream := guardStream(generation, false, nil, nil, nil, nil, 100)

	decisions, err := commitGuardScenario(t, database, generation, scopeValue, stream)
	if err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil (refusal is handled)", err)
	}
	requireWriteOnceRefused(t, database, decisions)
}

// A write-once successor over a prior that only failed still stores: the
// failed generation never became final, so the first storable generation
// lands under the ordinary ratio rule.
func TestGuardWriteOnceStoresOverFailedOnlyPrior(t *testing.T) {
	scopeValue, generation := guardDeltaFixture(false)
	generation.WriteOnce = true
	database := newGuardRoutingDB(t, 100, 300, 20)
	database.candidate = []any{"gen-full", "hint-prior", "commit-base", "failed", false, false}
	database.activeID = ""
	stream := guardStream(generation, false, nil, nil, nil, nil, 100)

	decisions, err := commitGuardScenario(t, database, generation, scopeValue, stream)
	if err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}
	requireStored(t, database)
	requireDecisions(t, decisions, guardDecisionKey{
		telemetry.SuccessorGuardKindFull, telemetry.SuccessorGuardRuleCollectorRatio, telemetry.SuccessorGuardOutcomeStored,
	})
}

// The in-tx recheck sees a racing commit: the pre-tx prior only failed, but
// by the scope-row lock a live prior exists, so the write-once successor
// refuses on the re-sized prior instead of storing over the winner.
func TestGuardWriteOnceRecheckSeesRacingLivePrior(t *testing.T) {
	scopeValue, generation := guardDeltaFixture(false)
	generation.WriteOnce = true
	database := newGuardRoutingDB(t, 100, 300, 20)
	database.candidate = []any{"gen-full", "hint-prior", "commit-base", "failed", false, false}
	database.activeID = ""
	database.inTx = &guardTxState{
		candidate: []any{"gen-racer", "hint-racer", "commit-racer", "active", false, false},
		counts:    map[string][]any{"gen-racer": {100, 300, 20}},
		targets:   map[string][]any{"gen-racer": {false, ""}},
	}
	stream := guardStream(generation, false, nil, nil, nil, nil, 100)

	decisions, err := commitGuardScenario(t, database, generation, scopeValue, stream)
	if err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil (refusal is handled)", err)
	}
	requireWriteOnceRefused(t, database, decisions)
}
