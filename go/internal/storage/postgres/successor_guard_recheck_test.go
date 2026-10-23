// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// The tests in this file pin the #8750 window ruling: the guard's prior is
// sized on the base connection before the commit transaction opens, so after
// the scope upsert locks the scope row the commit re-reads the sizing inputs
// on the transaction and re-sizes on any difference, using the in-tx result
// alone. A failed recheck read or count fails the commit closed.

// requireRecheckStage asserts the successor_prior_rechecked stage log.
func requireRecheckStage(t *testing.T, database *guardRoutingDB, changed bool, reads int, want map[string]string) {
	t.Helper()
	record := database.stageLog(t, "successor_prior_rechecked")
	if record["changed"] != changed || record["recheck_reads"] != float64(reads) {
		t.Fatalf("recheck stage changed=%v reads=%v, want changed=%v reads=%d: %v",
			record["changed"], record["recheck_reads"], changed, reads, record)
	}
	for key, value := range want {
		if record[key] != value {
			t.Fatalf("recheck stage %s = %v, want %q: %v", key, record[key], value, record)
		}
	}
	if _, ok := record["resize_duration_seconds"]; ok != changed {
		t.Fatalf("recheck stage resize_duration_seconds present = %v, want %v", ok, changed)
	}
}

// requireNoCommitWork asserts a fail-closed commit: no commit, no
// projector work, no destructive-successor refusal.
func requireNoCommitWork(t *testing.T, database *guardRoutingDB) {
	t.Helper()
	if database.tx.committed {
		t.Fatal("commit transaction committed; a failed recheck must fail closed")
	}
	if database.enqueuedProjectorWork() {
		t.Fatal("failed recheck enqueued projector work")
	}
	if got := telemetry.SkippedRefreshCountByReason(telemetry.RefreshSkipReasonDestructiveSuccessor); got != 0 {
		t.Fatalf("destructive_successor skips = %d, want 0 on a fail-closed commit", got)
	}
}

// HR1: nothing changed and the candidate is active: one recheck read, no
// active read, no count on the transaction; the pre-tx prior decides.
func TestGuardRecheckUnchangedActiveCandidateReadsOnce(t *testing.T) {
	scopeValue, generation := guardDeltaFixture(false)
	database := newGuardRoutingDB(t, 100, 300, 20)
	database.logs = &bytes.Buffer{}
	stream := guardStream(generation, false, nil, nil, nil, nil, 60)

	if _, err := commitGuardScenario(t, database, generation, scopeValue, stream); err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}
	if got := database.countTxQueries(unchangedGenerationCandidateQuery); got != 1 {
		t.Fatalf("in-tx candidate reads = %d, want 1", got)
	}
	if got := database.countTxQueries(successorGuardActiveGenerationQuery); got != 0 {
		t.Fatalf("in-tx active reads = %d, want 0 for an active candidate", got)
	}
	if got := database.countTxQueries(successorGuardCountQuery); got != 0 {
		t.Fatalf("in-tx counts = %d, want 0 when nothing changed", got)
	}
	requireStored(t, database)
	requireRecheckStage(t, database, false, 1, map[string]string{
		"previous_candidate_generation_id": "gen-full", "candidate_generation_id": "gen-full",
	})
	if record := database.stageLog(t, "successor_guard"); record["prior_collector_fact_count"] != float64(100) {
		t.Fatalf("prior_collector_fact_count = %v, want the pre-tx 100", record["prior_collector_fact_count"])
	}
}

// HR2: nothing changed and the candidate is pending: the candidate and the
// active are read on the transaction, nothing is counted.
func TestGuardRecheckUnchangedPendingCandidateReadsTwice(t *testing.T) {
	scopeValue, generation := guardDeltaFixture(false)
	database := newPendingWindowDB(t)
	stream := guardStream(generation, false, nil, nil, nil, nil, 40)

	if _, err := commitGuardScenario(t, database, generation, scopeValue, stream); err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}
	if got := database.countTxQueries(unchangedGenerationCandidateQuery); got != 1 {
		t.Fatalf("in-tx candidate reads = %d, want 1", got)
	}
	if got := database.countTxQueries(successorGuardActiveGenerationQuery); got != 1 {
		t.Fatalf("in-tx active reads = %d, want 1 for a pending candidate", got)
	}
	if got := database.countTxQueries(successorGuardCountQuery); got != 0 {
		t.Fatalf("in-tx counts = %d, want 0 when nothing changed", got)
	}
	requireRefused(t, database)
	requireRecheckStage(t, database, false, 2, map[string]string{
		"previous_active_generation_id": "gen-full", "active_generation_id": "gen-full",
	})
}

// HR3: a larger pending full G2 committed between the pre-tx sizing and the
// scope lock. It is the in-tx candidate, so the prior is re-sized on the
// transaction and G2 decides: the 60-row full is refused.
func TestGuardRecheckResizesWhenTheCandidateChanged(t *testing.T) {
	scopeValue, generation := guardDeltaFixture(false)
	database := newGuardRoutingDB(t, 100, 300, 20)
	database.logs = &bytes.Buffer{}
	database.inTx = &guardTxState{
		candidate: []any{"gen-g2", "hint-g2", "commit-g2", "pending", false, true},
		activeID:  "gen-full", activeSet: true,
		targets: map[string][]any{"gen-g2": {false, ""}},
		counts:  map[string][]any{"gen-g2": {1000, 1000, 500}},
	}
	stream := guardStream(generation, false, nil, nil, nil, nil, 60)

	decisions, err := commitGuardScenario(t, database, generation, scopeValue, stream)
	if err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}
	counted := database.txCountedGenerations()
	if counted["gen-g2"] != 1 || counted["gen-full"] != 1 {
		t.Fatalf("in-tx counts by generation = %v, want gen-g2 and gen-full once each", counted)
	}
	if got := database.baseGuardReadsAfterBegin(); got != 0 {
		t.Fatalf("guard reads on the base connection after Begin = %d, want 0", got)
	}
	requireRefused(t, database)
	requireDecisions(t, decisions, guardDecisionKey{
		telemetry.SuccessorGuardKindFull, telemetry.SuccessorGuardRuleCollectorRatio, telemetry.SuccessorGuardOutcomeRefused,
	})
	requireRecheckStage(t, database, true, 2, map[string]string{
		"previous_candidate_generation_id": "gen-full", "candidate_generation_id": "gen-g2",
		"previous_active_generation_id": "", "active_generation_id": "gen-full",
	})
	record := database.stageLog(t, "successor_guard")
	if record["prior_sizing_generation_id"] != "gen-g2" || record["prior_collector_fact_count"] != float64(1000) {
		t.Fatalf("prior sizing = %v (%v), want gen-g2 (1000)", record["prior_sizing_generation_id"], record["prior_collector_fact_count"])
	}
}

// HR4: the candidate is unchanged but a newer full at the delta's baseline
// activated in the window. The active side and the delta successor's anchor
// are both re-resolved on the transaction, and nothing is read on the base
// connection after Begin.
func TestGuardRecheckResizesWhenTheActiveChanged(t *testing.T) {
	scopeValue, generation := guardDeltaFixture(true)
	database := &guardRoutingDB{
		t: t, tx: &fakeTx{}, logs: &bytes.Buffer{},
		candidate: []any{"gen-p", "hint-p", "commit-p", "pending", false, true},
		activeID:  "gen-full",
		targets:   map[string][]any{"gen-p": {false, ""}, "gen-full": {false, ""}, "gen-a2": {false, ""}},
		bases:     map[string][]any{"commit-base": {"gen-full", "active"}},
		counts:    map[string][]any{"gen-p": {100, 100, 50}, "gen-full": {100, 100, 50}, "gen-a2": {300, 300, 150}},
	}
	database.inTx = &guardTxState{
		activeID: "gen-a2", activeSet: true,
		bases: map[string][]any{"commit-base": {"gen-a2", "active"}},
	}
	changed := []string{"a.go", "b.go"}
	stream := guardStream(generation, true, changed, nil, changed, nil, 2)

	if _, err := commitGuardScenario(t, database, generation, scopeValue, stream); err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}
	if got := database.countTxQueries(deltaUnionBaseQuery); got < 1 {
		t.Fatalf("in-tx anchor resolutions = %d, want the anchor re-resolved on the transaction", got)
	}
	if got := database.baseGuardReadsAfterBegin(); got != 0 {
		t.Fatalf("guard reads on the base connection after Begin = %d, want 0", got)
	}
	requireStored(t, database)
	requireRecheckStage(t, database, true, 2, map[string]string{
		"previous_candidate_generation_id": "gen-p", "candidate_generation_id": "gen-p",
		"previous_active_generation_id": "gen-full", "active_generation_id": "gen-a2",
	})
	record := database.stageLog(t, "successor_guard")
	if record["prior_sizing"] != "active" || record["prior_sizing_generation_id"] != "gen-a2" {
		t.Fatalf("prior sizing = %v (%v), want active (gen-a2)", record["prior_sizing"], record["prior_sizing_generation_id"])
	}
	if record["anchor_file_count"] != float64(150) {
		t.Fatalf("anchor_file_count = %v, want 150 from the in-tx anchor gen-a2", record["anchor_file_count"])
	}
}

// HR4b: the re-size runs on one connection, so a pending delta candidate is
// sized there too: its target read, its base read, and the retention probe
// of a now-superseded base each finish before the next starts (pgx refuses a
// statement while a cursor on the same connection is open). The newer full
// that superseded the base is the in-tx active and decides.
func TestGuardRecheckResizesAPendingDeltaOnTheTransaction(t *testing.T) {
	scopeValue, generation := guardDeltaFixture(false)
	database := &guardRoutingDB{
		t: t, tx: &fakeTx{}, logs: &bytes.Buffer{},
		candidate: []any{"gen-p", "hint-p", "commit-p", "pending", false, true},
		activeID:  "gen-full",
		targets:   map[string][]any{"gen-p": {true, "commit-base"}, "gen-full": {false, ""}, "gen-a2": {false, ""}},
		bases:     map[string][]any{"commit-base": {"gen-full", "active"}},
		counts:    map[string][]any{"gen-p": {6, 6, 5}, "gen-full": {100, 100, 50}, "gen-a2": {300, 300, 150}},
	}
	database.inTx = &guardTxState{
		activeID: "gen-a2", activeSet: true,
		bases: map[string][]any{"commit-base": {"gen-full", "superseded"}},
	}
	stream := guardStream(generation, false, nil, nil, nil, nil, 60)

	if _, err := commitGuardScenario(t, database, generation, scopeValue, stream); err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}
	if got := database.countTxQueries(deltaUnionBasePrunedQuery); got != 1 {
		t.Fatalf("in-tx retention probes = %d, want 1 for the superseded base", got)
	}
	requireRefused(t, database)
	record := database.stageLog(t, "successor_guard")
	if record["prior_sizing_generation_id"] != "gen-a2" || record["prior_candidate_collector_fact_count"] != float64(106) {
		t.Fatalf("prior sizing = %v, candidate side %v; want gen-a2 and the candidate's 106-row union",
			record["prior_sizing_generation_id"], record["prior_candidate_collector_fact_count"])
	}
}

// HR5: the pending candidate's projector work stopped being live in the
// window. Next to an active generation a wedged pending is not sized (F1
// decision 2), so the in-tx prior is the active alone and the successor
// stores; max(pre-tx, in-tx) would keep the stale 1000 and refuse it.
func TestGuardRecheckDropsAPendingWhoseWorkWentIdle(t *testing.T) {
	scopeValue, generation := guardDeltaFixture(false)
	database := &guardRoutingDB{
		t: t, tx: &fakeTx{}, logs: &bytes.Buffer{},
		candidate: []any{"gen-p", "hint-p", "commit-p", "pending", false, true},
		activeID:  "gen-full",
		targets:   map[string][]any{"gen-p": {false, ""}, "gen-full": {false, ""}},
		counts:    map[string][]any{"gen-p": {1000, 1000, 500}, "gen-full": {100, 100, 50}},
	}
	database.inTx = &guardTxState{candidate: []any{"gen-p", "hint-p", "commit-p", "pending", false, false}}
	stream := guardStream(generation, false, nil, nil, nil, nil, 60)

	if _, err := commitGuardScenario(t, database, generation, scopeValue, stream); err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}
	requireStored(t, database)
	requireRecheckStage(t, database, true, 2, nil)
	record := database.stageLog(t, "successor_guard")
	if record["prior_sizing"] != "active" || record["prior_collector_fact_count"] != float64(100) {
		t.Fatalf("prior sizing = %v (%v), want active (100)", record["prior_sizing"], record["prior_collector_fact_count"])
	}
}

// HR6: a failed recheck read or re-size count fails the commit closed: the
// error returns, nothing is committed, refused, or enqueued.
func TestGuardRecheckFailureFailsTheCommitClosed(t *testing.T) {
	boom := errors.New("recheck read failed")
	changedCandidate := []any{"gen-g2", "hint-g2", "commit-g2", "pending", false, true}
	for _, tc := range []struct {
		name  string
		state *guardTxState
	}{
		{name: "candidate read", state: &guardTxState{candidateErr: boom}},
		{name: "active read", state: &guardTxState{candidate: changedCandidate, activeErr: boom}},
		{name: "re-size count", state: &guardTxState{
			candidate: changedCandidate, activeID: "gen-full", activeSet: true,
			targets:   map[string][]any{"gen-g2": {false, ""}},
			countErrs: map[string]error{"gen-g2": boom},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scopeValue, generation := guardDeltaFixture(false)
			database := newGuardRoutingDB(t, 100, 300, 20)
			database.inTx = tc.state
			stream := guardStream(generation, false, nil, nil, nil, nil, 60)

			_, err := commitGuardScenario(t, database, generation, scopeValue, stream)
			if !errors.Is(err, boom) {
				t.Fatalf("CommitScopeGeneration() error = %v, want %v", err, boom)
			}
			requireNoCommitWork(t, database)
		})
	}
}

// HR7: the recheck runs after the scope upsert takes the scope row lock and
// before the generation upsert, after which the successor's own pending row
// would be the newest candidate.
func TestGuardRecheckRunsBetweenTheScopeLockAndTheGenerationUpsert(t *testing.T) {
	scopeValue, generation := guardDeltaFixture(false)
	database := newGuardRoutingDB(t, 100, 300, 20)
	stream := guardStream(generation, false, nil, nil, nil, nil, 60)

	if _, err := commitGuardScenario(t, database, generation, scopeValue, stream); err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}
	scopeAt := database.orderIndex(upsertIngestionScopeQuery)
	recheckAt := database.orderIndex(unchangedGenerationCandidateQuery)
	generationAt := database.orderIndex(upsertScopeGenerationQuery)
	if scopeAt < 0 || recheckAt < 0 || generationAt < 0 || scopeAt >= recheckAt || recheckAt >= generationAt {
		t.Fatalf("tx order: scope upsert %d, recheck %d, generation upsert %d; want scope < recheck < generation",
			scopeAt, recheckAt, generationAt)
	}
}

// HR8: parity with the pre-tx path. A trusted deletion and a generation that
// skips the duplicate check never use the prior, so nothing is re-read; with
// no candidate before or after the lock, only the candidate is re-read.
func TestGuardRecheckParityWithThePreTxPath(t *testing.T) {
	t.Run("trusted deletion", func(t *testing.T) {
		scopeValue, generation := guardDeltaFixture(false)
		generation.TrustedDeletion = true
		database := newGuardRoutingDB(t, 100, 300, 20)
		if _, err := commitGuardScenario(t, database, generation, scopeValue, guardStream(generation, false, nil, nil, nil, nil, 1)); err != nil {
			t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
		}
		if got := len(database.txQueries); got != 0 {
			t.Fatalf("in-tx guard reads = %d, want 0 for a trusted deletion", got)
		}
	})
	t.Run("no duplicate check", func(t *testing.T) {
		scopeValue, generation := guardDeltaFixture(false)
		generation.DefaultBranchChanged = true
		database := newGuardRoutingDB(t, 100, 300, 20)
		if _, err := commitGuardScenario(t, database, generation, scopeValue, guardStream(generation, false, nil, nil, nil, nil, 1)); err != nil {
			t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
		}
		if got := len(database.txQueries); got != 0 {
			t.Fatalf("in-tx guard reads = %d, want 0 when the duplicate check is skipped", got)
		}
	})
	t.Run("no candidate either side", func(t *testing.T) {
		scopeValue, generation := guardDeltaFixture(false)
		database := newGuardRoutingDB(t, 100, 300, 20)
		database.candidateMissing = true
		database.logs = &bytes.Buffer{}
		if _, err := commitGuardScenario(t, database, generation, scopeValue, guardStream(generation, false, nil, nil, nil, nil, 1)); err != nil {
			t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
		}
		if got, cand := len(database.txQueries), database.countTxQueries(unchangedGenerationCandidateQuery); got != 1 || cand != 1 {
			t.Fatalf("in-tx guard reads = %d (candidate %d), want exactly the one candidate read", got, cand)
		}
		requireStored(t, database)
		requireRecheckStage(t, database, false, 1, nil)
	})
}
