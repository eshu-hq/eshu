// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package links_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/freshness/links"
	store "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
)

// testClock is the injected clock shared by the writer and the journal, so a
// test can step past a backoff without sleeping.
type testClock struct{ now time.Time }

func (c *testClock) Now() time.Time { return c.now }

func newRunner(l *ledgerDB, clock *testClock, maxAttempts int) *links.Runner {
	writer := store.NewLinkWriter(l.store)
	writer.Now = clock.Now
	journal := store.NewJournalStore(l.store)
	journal.Now = clock.Now
	return &links.Runner{
		Linker:  writer,
		Journal: journal,
		Config:  links.Config{Workers: 2, MaxAttempts: maxAttempts, BackfillScopesPerCycle: -1},
	}
}

// seedTwoGenerations seeds a scope with two full generations journaled in
// order and returns the second activation's sequence.
func (l *ledgerDB) seedTwoGenerations(t *testing.T, scopeID string) int64 {
	t.Helper()
	l.seedScope(t, scopeID)
	l.seedGeneration(t, scopeID, scopeID+"-g0", false, "superseded", fixtureEpoch, fixtureEpoch.Add(time.Hour))
	l.seedGeneration(t, scopeID, scopeID+"-g1", false, "active", fixtureEpoch.Add(time.Hour), time.Time{})
	l.insertFacts(t, scopeID, scopeID+"-g0", []fact{{kind: "file", key: "file:a", payload: `{"v":1}`, uri: "a"}})
	l.insertFacts(t, scopeID, scopeID+"-g1", []fact{{kind: "file", key: "file:a", payload: `{"v":2}`, uri: "a"}})
	l.journal(t, scopeID, scopeID+"-g0", "")
	l.journal(t, scopeID, scopeID+"-g1", scopeID+"-g0")
	return l.queryInt(t, `SELECT activation_seq FROM changed_since_activations WHERE generation_id = $1`, scopeID+"-g1")
}

func (l *ledgerDB) plantLinkFailure(t *testing.T, scopeID string) {
	t.Helper()
	l.exec(t, `CREATE OR REPLACE FUNCTION planted_link_failure() RETURNS trigger LANGUAGE plpgsql AS
$$ BEGIN RAISE EXCEPTION 'planted link failure'; END $$`)
	l.exec(t, `CREATE TRIGGER planted_link_failure BEFORE INSERT ON changed_since_key_state FOR EACH ROW
WHEN (NEW.scope_id = '`+scopeID+`') EXECUTE FUNCTION planted_link_failure()`)
}

func (l *ledgerDB) cursorInt(t *testing.T, column, scopeID string) int64 {
	t.Helper()
	return l.queryInt(t, `SELECT COALESCE(`+column+`, 0) FROM changed_since_scope_cursor WHERE scope_id = $1`, scopeID)
}

// poisonEpisode drives a runner over a scope whose link fails every time
// and a healthy scope, stepping the clock past each backoff, for at most
// cycles cycles. It returns what G16a asserts.
type poisonObservation struct {
	failedTries     int
	poisoned        int
	cursorAdvances  int64
	healthyLinkedAt int
	stateKept       bool
}

func poisonEpisode(t *testing.T, maxAttempts, cycles int) poisonObservation {
	t.Helper()
	l := openLedgerDB(t)
	clock := &testClock{now: fixtureEpoch.Add(24 * time.Hour)}
	runner := newRunner(l, clock, maxAttempts)
	// The failing scope: g0 roots cleanly, then g1 fails every time. g1 is
	// pending until the root is in so the sweeper does not journal it early.
	l.seedScope(t, "poison")
	l.seedGeneration(t, "poison", "poison-g0", false, "active", fixtureEpoch, time.Time{})
	l.seedGeneration(t, "poison", "poison-g1", false, "pending", fixtureEpoch.Add(time.Hour), time.Time{})
	l.insertFacts(t, "poison", "poison-g0", []fact{{kind: "file", key: "file:a", payload: `{"v":1}`, uri: "a"}})
	l.insertFacts(t, "poison", "poison-g1", []fact{{kind: "file", key: "file:a", payload: `{"v":2}`, uri: "a"}})
	l.journal(t, "poison", "poison-g0", "")
	if _, err := runner.RunOnce(l.ctx); err != nil {
		t.Fatalf("root cycle: %v", err)
	}
	stateBefore := l.queryStrings(t, `SELECT stable_fact_key || encode(state, 'hex') FROM changed_since_key_state WHERE scope_id = 'poison' ORDER BY 1`)
	if len(stateBefore) != 1 {
		t.Fatalf("root of poison-g0 left %d state rows", len(stateBefore))
	}
	l.plantLinkFailure(t, "poison")
	l.journal(t, "poison", "poison-g1", "poison-g0")
	seqBefore := l.cursorInt(t, "state_activation_seq", "poison")
	l.seedTwoGenerations(t, "healthy")

	obs := poisonObservation{healthyLinkedAt: -1}
	for cycle := 0; cycle < cycles; cycle++ {
		result, err := runner.RunOnce(l.ctx)
		if err != nil {
			t.Fatalf("cycle %d: %v", cycle, err)
		}
		obs.failedTries += result.Failures
		obs.poisoned += result.Poisoned
		if obs.healthyLinkedAt < 0 && l.queryInt(t, `SELECT count(*) FROM changed_since_links WHERE scope_id = 'healthy'`) == 2 {
			obs.healthyLinkedAt = cycle
		}
		clock.now = clock.now.Add(31 * time.Minute) // past the longest backoff
	}
	// Activations of the scope the cursor passed during the episode.
	obs.cursorAdvances = l.queryInt(t, `SELECT count(*) FROM changed_since_activations
WHERE scope_id = 'poison' AND activation_seq > $1 AND activation_seq <= $2`,
		seqBefore, l.cursorInt(t, "state_activation_seq", "poison"))
	stateAfter := l.queryStrings(t, `SELECT stable_fact_key || encode(state, 'hex') FROM changed_since_key_state WHERE scope_id = 'poison' ORDER BY 1`)
	obs.stateKept = fmt.Sprint(stateBefore) == fmt.Sprint(stateAfter) &&
		l.queryStrings(t, `SELECT state_generation_id FROM changed_since_scope_cursor WHERE scope_id = 'poison'`)[0] == "poison-g0"
	return obs
}

// poisonBoundViolations lists how an observation breaks gate G16a.
func poisonBoundViolations(obs poisonObservation, maxAttempts int) []string {
	var v []string
	if obs.failedTries+obs.poisoned != maxAttempts {
		v = append(v, fmt.Sprintf("tried %d times, want exactly %d", obs.failedTries+obs.poisoned, maxAttempts))
	}
	if obs.poisoned != 1 {
		v = append(v, fmt.Sprintf("%d link_poisoned breaks, want 1", obs.poisoned))
	}
	if obs.cursorAdvances != 1 {
		v = append(v, fmt.Sprintf("cursor advanced %d times, want 1", obs.cursorAdvances))
	}
	if !obs.stateKept {
		v = append(v, "state rows or state_generation_id changed")
	}
	if obs.healthyLinkedAt != 0 {
		v = append(v, fmt.Sprintf("healthy scope linked at cycle %d, want the first cycle", obs.healthyLinkedAt))
	}
	return v
}

// TestRunnerPoisonsAFailingLinkAfterMaxAttempts is gate G16a on the runner,
// with its RED: a planted runner without the limit fails the same check.
func TestRunnerPoisonsAFailingLinkAfterMaxAttempts(t *testing.T) {
	const maxAttempts, cycles = 5, 9
	if v := poisonBoundViolations(poisonEpisode(t, maxAttempts, cycles), maxAttempts); len(v) > 0 {
		t.Fatalf("G16a: %v", v)
	}
	// RED: no effective limit.
	if v := poisonBoundViolations(poisonEpisode(t, 1_000_000, cycles), maxAttempts); len(v) == 0 {
		t.Fatalf("a runner without the attempt limit passed G16a; the gate cannot see an unbounded retry")
	}
}

// countingSlotBusy is the planted classifier of G16b's RED: it counts
// slot_busy as a failure.
func countingSlotBusy(err error) *store.FailureError {
	var retry *store.RetryError
	if errors.As(err, &retry) && retry.Reason == store.RetrySlotBusy {
		return &store.FailureError{Class: store.FailureInternal, ScopeID: retry.ScopeID, ActivationSeq: retry.ActivationSeq, Err: err}
	}
	return links.CountingFailure(err)
}

// nonCountingEpisode holds each lock a link can miss, runs maxAttempts+2
// cycles per lock, and returns the attempt count and poison marker it left.
func nonCountingEpisode(t *testing.T, classify func(error) *store.FailureError) (attempts, poisoned int64) {
	t.Helper()
	l := openLedgerDB(t)
	clock := &testClock{now: fixtureEpoch.Add(24 * time.Hour)}
	const maxAttempts = 3
	runner := newRunner(l, clock, maxAttempts)
	runner.Classify = classify
	l.seedScope(t, "busy")
	l.seedGeneration(t, "busy", "busy-g0", false, "active", fixtureEpoch, time.Time{})
	l.insertFacts(t, "busy", "busy-g0", []fact{{kind: "file", key: "file:a", payload: `{}`, uri: "a"}})
	l.journal(t, "busy", "busy-g0", "")
	l.exec(t, `INSERT INTO changed_since_scope_cursor (scope_id, digest_version, updated_at) VALUES ('busy', $1, now())`,
		store.DigestVersion)
	for _, hold := range []string{
		`SELECT pg_advisory_xact_lock(7127, 1), pg_advisory_xact_lock(7127, 2)`,
		`SELECT 1 FROM changed_since_scope_cursor WHERE scope_id = 'busy' FOR UPDATE`,
		`SELECT 1 FROM scope_generations WHERE generation_id = 'busy-g0' FOR UPDATE`,
	} {
		holder, err := l.raw.BeginTx(l.ctx, nil)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		if _, err := holder.ExecContext(l.ctx, hold); err != nil {
			t.Fatalf("hold %q: %v", hold, err)
		}
		for range maxAttempts + 2 {
			if _, err := runner.RunOnce(l.ctx); err != nil {
				t.Fatalf("cycle: %v", err)
			}
			clock.now = clock.now.Add(31 * time.Minute)
		}
		_ = holder.Rollback()
	}
	return l.cursorInt(t, "attempt_count", "busy"), l.cursorInt(t, "poisoned_activation_seq", "busy")
}

// TestNonCountingOutcomesNeverPoison is gate G16b with its RED.
func TestNonCountingOutcomesNeverPoison(t *testing.T) {
	if attempts, poisoned := nonCountingEpisode(t, nil); attempts != 0 || poisoned != 0 {
		t.Fatalf("lock misses left attempt_count=%d poisoned=%d, want 0 and 0", attempts, poisoned)
	}
	if attempts, poisoned := nonCountingEpisode(t, countingSlotBusy); attempts == 0 && poisoned == 0 {
		t.Fatalf("a classifier that counts slot_busy passed G16b; the gate cannot see it")
	}
}

// TestRunnerMovesOnPastABusySlot is gate G12 on the runner: with every slot
// held, a full link is skipped without counting and a delta activation of
// another scope still completes in the same cycle.
func TestRunnerMovesOnPastABusySlot(t *testing.T) {
	l := openLedgerDB(t)
	clock := &testClock{now: fixtureEpoch.Add(24 * time.Hour)}
	runner := newRunner(l, clock, 5)
	l.seedScope(t, "full")
	l.seedGeneration(t, "full", "full-g0", false, "active", fixtureEpoch, time.Time{})
	l.journal(t, "full", "full-g0", "")
	l.seedScope(t, "delta")
	l.seedGeneration(t, "delta", "delta-g0", true, "active", fixtureEpoch, time.Time{})
	l.journal(t, "delta", "delta-g0", "")
	holder, err := l.raw.BeginTx(l.ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = holder.Rollback() }()
	if _, err := holder.ExecContext(l.ctx, `SELECT pg_advisory_xact_lock(7127, 1), pg_advisory_xact_lock(7127, 2)`); err != nil {
		t.Fatalf("hold slots: %v", err)
	}
	result, err := runner.RunOnce(l.ctx)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	// The bootstrap's eshu:global scope also waits for a slot, so assert per
	// scope: the delta activation broke and advanced, the full one did not
	// link and counted nothing.
	if result.Failures != 0 || result.Breaks < 1 || result.Retries < 1 {
		t.Fatalf("RunOnce with the slots held = %+v, want non-counting retries and the delta break", result)
	}
	if seq := l.cursorInt(t, "state_activation_seq", "delta"); seq == 0 {
		t.Fatalf("delta activation did not complete while the slots were held")
	}
	if seq := l.cursorInt(t, "state_activation_seq", "full"); seq != 0 {
		t.Fatalf("full activation advanced to %d with every slot held", seq)
	}
	if n := l.cursorInt(t, "attempt_count", "full"); n != 0 {
		t.Fatalf("slot_busy counted an attempt (%d)", n)
	}
}
