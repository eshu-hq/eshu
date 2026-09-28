// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package links_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/freshness/links"
	store "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
)

// countingLockTimeout is the planted classifier of W5's RED: it counts
// generation_lock_timeout as a failure.
func countingLockTimeout(err error) *store.FailureError {
	var retry *store.RetryError
	if errors.As(err, &retry) && retry.Reason == store.RetryGenerationLockTimeout {
		return &store.FailureError{Class: store.FailureInternal, ScopeID: retry.ScopeID, ActivationSeq: retry.ActivationSeq, Err: err}
	}
	return links.CountingFailure(err)
}

// lockTimeoutEpisode holds the chain-walk fixture of arbiter ruling
// arb-7127-3e-wait (an open transaction that updates a non-key column of the
// row, then locks it FOR UPDATE) first on the activating generation of a
// root link, then on the prior of the next incremental link, each for
// maxAttempts+2 cycles, and returns the highest attempt count and poison
// marker the cursor showed at the end of a held phase (a later full link
// clears both), and the lock timeouts seen.
func lockTimeoutEpisode(t *testing.T, classify func(error) *store.FailureError) (attempts, poisoned int64, timeouts int) {
	t.Helper()
	l := openLedgerDB(t)
	clock := &testClock{now: fixtureEpoch.Add(24 * time.Hour)}
	const maxAttempts = 3
	runner := newRunner(l, clock, maxAttempts)
	runner.Classify = classify
	l.seedScope(t, "lt")
	l.seedGeneration(t, "lt", "lt-g0", false, "superseded", fixtureEpoch, fixtureEpoch.Add(time.Hour))
	l.seedGeneration(t, "lt", "lt-g1", false, "active", fixtureEpoch.Add(time.Hour), time.Time{})
	l.insertFacts(t, "lt", "lt-g0", []fact{{kind: "file", key: "file:a", payload: `{"v":1}`, uri: "a"}})
	l.insertFacts(t, "lt", "lt-g1", []fact{{kind: "file", key: "file:a", payload: `{"v":2}`, uri: "a"}})
	l.journal(t, "lt", "lt-g0", "")
	hold := func(generationID string) {
		t.Helper()
		holder, err := l.raw.BeginTx(l.ctx, nil)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = holder.Rollback() }()
		for _, s := range []string{
			`UPDATE scope_generations SET ingested_at = ingested_at + interval '1 second' WHERE generation_id = $1`,
			`SELECT 1 FROM scope_generations WHERE generation_id = $1 FOR UPDATE`,
		} {
			if _, err := holder.ExecContext(l.ctx, s, generationID); err != nil {
				t.Fatalf("hold %s: %v", generationID, err)
			}
		}
		for range maxAttempts + 2 {
			// A cycle with the holder in place must give way within the lock
			// timeout; 5 s bounds a cycle that waits instead (the RED).
			ctx, cancel := context.WithTimeout(l.ctx, 5*time.Second)
			result, err := runner.RunOnce(ctx)
			waited := ctx.Err() != nil
			cancel()
			if err != nil || waited {
				t.Fatalf("cycle with %s held: %v (context expired: %v)", generationID, err, waited)
			}
			timeouts += result.Retries
			clock.now = clock.now.Add(31 * time.Minute)
		}
		attempts = max(attempts, l.cursorInt(t, "attempt_count", "lt"))
		poisoned = max(poisoned, l.cursorInt(t, "poisoned_activation_seq", "lt"))
	}
	hold("lt-g0") // the activating generation of the root link
	if _, err := runner.RunOnce(l.ctx); err != nil {
		t.Fatalf("cycle after release: %v", err)
	}
	l.journal(t, "lt", "lt-g1", "lt-g0")
	hold("lt-g0") // now the prior of the incremental link to lt-g1
	if _, err := runner.RunOnce(l.ctx); err != nil {
		t.Fatalf("cycle after release: %v", err)
	}
	return attempts, poisoned, timeouts
}

// TestLockTimeoutsNeverCount is W5 of arbiter ruling arb-7127-3e-wait, G16b
// extended to generation_lock_timeout on the activating generation and on the
// prior: every held cycle is a retry, and none counts an attempt or poisons.
// RED: a classifier that counts generation_lock_timeout counts and poisons.
func TestLockTimeoutsNeverCount(t *testing.T) {
	attempts, poisoned, timeouts := lockTimeoutEpisode(t, nil)
	if timeouts != 2*(3+2) {
		t.Fatalf("saw %d lock-timeout retries, want %d (every held cycle)", timeouts, 2*(3+2))
	}
	if attempts != 0 || poisoned != 0 {
		t.Fatalf("lock timeouts left attempt_count=%d poisoned=%d, want 0 and 0", attempts, poisoned)
	}
	if attempts, poisoned, _ := lockTimeoutEpisode(t, countingLockTimeout); attempts == 0 && poisoned == 0 {
		t.Fatal("a classifier that counts generation_lock_timeout passed; the gate cannot see it")
	}
}
