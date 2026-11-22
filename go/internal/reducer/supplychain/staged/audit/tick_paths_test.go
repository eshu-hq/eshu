// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package audit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	pgaccess "github.com/eshu-hq/eshu/go/internal/runtime/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// scriptedStore wraps sweepStore and lets a test fail the cursor read or the
// writer verify, or answer a NoLock verify as a plain advance (every
// candidate was already queued, so no lock was needed).
type scriptedStore struct {
	*sweepStore
	readErr        error
	verifyErr      error // returned by every verify
	noLockVerifyIs error // returned by the NoLock verify only
	noLockAdvances bool  // a NoLock verify advances instead of yielding
}

func (s *scriptedStore) ReadWinnersAuditSweep(ctx context.Context) (postgres.WinnersAuditSweep, error) {
	if s.readErr != nil {
		return postgres.WinnersAuditSweep{}, s.readErr
	}
	return s.sweepStore.ReadWinnersAuditSweep(ctx)
}

func (s *scriptedStore) VerifyWinnersAuditRange(ctx context.Context, v postgres.WinnersAuditRangeVerify) (postgres.WinnersAuditRangeOutcome, error) {
	if s.verifyErr != nil {
		return postgres.WinnersAuditRangeOutcome{}, s.verifyErr
	}
	if v.NoLock && s.noLockVerifyIs != nil {
		return postgres.WinnersAuditRangeOutcome{}, s.noLockVerifyIs
	}
	if v.NoLock && s.noLockAdvances {
		v.Range.Candidates = nil // all queued already: nothing needs the lock
	}
	return s.sweepStore.VerifyWinnersAuditRange(ctx, v)
}

func TestStagedAuditSweepReadFailureIsAnErrorNotADeferral(t *testing.T) {
	t.Parallel()
	boom := errors.New("cursor row unreadable")
	c := &corpus{n: 5, factsPer: 10}
	store := &scriptedStore{sweepStore: &sweepStore{}, readErr: boom}
	a, tel := newSweepAuditor(c, store.sweepStore, nil)
	a.Store = store
	_, err := a.AuditWinners(context.Background())
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the cursor read error wrapped", err)
	}
	if len(c.budgets) != 0 || len(store.verifies) != 0 {
		t.Fatalf("reads = %d, verifies = %d; a failed cursor read must touch neither", len(c.budgets), len(store.verifies))
	}
	if tel.Deferrals(pgaccess.StagedAuditDeferBatchAbort) != 0 {
		t.Fatal("a writer cursor read failure must not be recorded as a standby deferral")
	}
}

// TestStagedAuditRangeReadFailuresDeferWithTheirReason pins the standby read
// error classification: a recovery conflict is conflict_exhausted, anything
// else batch_abort, and neither retries at a smaller budget.
func TestStagedAuditRangeReadFailuresDeferWithTheirReason(t *testing.T) {
	t.Parallel()
	conflict := &pgconn.PgError{Code: "40001", Message: "canceling statement due to conflict with recovery"}
	cases := []struct {
		name string
		err  error
		want pgaccess.StagedAuditDeferralReason
	}{
		{"recovery conflict", conflict, pgaccess.StagedAuditDeferConflictExhausted},
		{"transport loss", errors.New("connection reset by peer"), pgaccess.StagedAuditDeferBatchAbort},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &sweepStore{}
			a, tel := newSweepAuditor(&corpus{n: 5, factsPer: 10}, store, nil)
			reads := 0
			a.ReadRange = func(context.Context, db.Queryer, *postgres.WinnersAuditRangeIdent, int) (postgres.WinnersAuditRange, error) {
				reads++
				return postgres.WinnersAuditRange{}, tc.err
			}
			got, err := a.AuditWinners(context.Background())
			if err != nil || !got.Deferred || got.More || got.Outcome != OutcomeDeferred {
				t.Fatalf("AuditWinners = %+v, %v; want a deferred pass with no More", got, err)
			}
			if tel.Deferrals(tc.want) != 1 || reads != 1 || len(store.verifies) != 0 {
				t.Fatalf("deferrals(%s) = %d, reads = %d, verifies = %d; want 1, 1, 0", tc.want, tel.Deferrals(tc.want), reads, len(store.verifies))
			}
		})
	}
}

// TestStagedAuditSQLStateTimeoutHalvesTheBudget pins that SQLSTATE 57014
// (query_canceled) counts as the statement guard expiring, not only the
// context deadline: pgx raises it when the server cancels the statement.
func TestStagedAuditSQLStateTimeoutHalvesTheBudget(t *testing.T) {
	t.Parallel()
	c := &corpus{n: 50, factsPer: 10}
	a, tel := newSweepAuditor(c, &sweepStore{}, nil)
	var budgets []int
	a.ReadRange = func(ctx context.Context, q db.Queryer, lo *postgres.WinnersAuditRangeIdent, budget int) (postgres.WinnersAuditRange, error) {
		budgets = append(budgets, budget)
		if budget > 2500 {
			return postgres.WinnersAuditRange{}, fmt.Errorf("read range: %w", &pgconn.PgError{Code: "57014"})
		}
		return c.read(ctx, q, lo, budget)
	}
	if got, err := a.AuditWinners(context.Background()); err != nil || got.Deferred {
		t.Fatalf("AuditWinners = %+v, %v; want a committed range", got, err)
	}
	if fmt.Sprint(budgets) != fmt.Sprint([]int{10000, 5000, 2500}) || tel.RangeRetries() != 2 {
		t.Fatalf("budgets = %v, retries = %d; want [10000 5000 2500] and 2", budgets, tel.RangeRetries())
	}
}

// TestStagedAuditCancelledContextDoesNotRetryATimeout pins that a deadline
// error under a cancelled caller context is the caller leaving, not a slow
// statement: no halved retry, and the pass defers.
func TestStagedAuditCancelledContextDoesNotRetryATimeout(t *testing.T) {
	t.Parallel()
	a, tel := newSweepAuditor(&corpus{n: 5, factsPer: 10}, &sweepStore{}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	reads := 0
	a.ReadRange = func(context.Context, db.Queryer, *postgres.WinnersAuditRangeIdent, int) (postgres.WinnersAuditRange, error) {
		reads++
		cancel()
		return postgres.WinnersAuditRange{}, context.DeadlineExceeded
	}
	got, err := a.AuditWinners(ctx)
	if err != nil || !got.Deferred {
		t.Fatalf("AuditWinners = %+v, %v; want a deferred pass", got, err)
	}
	if reads != 1 || tel.RangeRetries() != 0 {
		t.Fatalf("reads = %d, retries = %d; want one read and no retry", reads, tel.RangeRetries())
	}
}

// TestStagedAuditProbeFailuresDefer pins the NULL probe error paths: a
// statement timeout defers as timeout, any other failure by its classified
// reason, at sweep start (before any range read) and at the last range
// (before any writer contact).
func TestStagedAuditProbeFailuresDefer(t *testing.T) {
	t.Parallel()
	conflict := &pgconn.PgError{Code: "40001", Message: "canceling statement due to conflict with recovery"}
	t.Run("start timeout", func(t *testing.T) {
		c := &corpus{n: 5, factsPer: 10}
		a, tel := newSweepAuditor(c, &sweepStore{}, nil)
		a.ProbeNullKeys = func(context.Context, db.Queryer) (postgres.WinnersAuditNullKeys, error) {
			return postgres.WinnersAuditNullKeys{}, &pgconn.PgError{Code: "57014"}
		}
		got, err := a.AuditWinners(context.Background())
		if err != nil || !got.Deferred || len(c.budgets) != 0 || tel.Deferrals(pgaccess.StagedAuditDeferTimeout) != 1 {
			t.Fatalf("start probe timeout = %+v, %v, reads %d; want a timeout deferral before any range read", got, err, len(c.budgets))
		}
	})
	t.Run("start conflict", func(t *testing.T) {
		c := &corpus{n: 5, factsPer: 10}
		a, tel := newSweepAuditor(c, &sweepStore{}, nil)
		a.ProbeNullKeys = func(context.Context, db.Queryer) (postgres.WinnersAuditNullKeys, error) {
			return postgres.WinnersAuditNullKeys{}, conflict
		}
		got, err := a.AuditWinners(context.Background())
		if err != nil || !got.Deferred || len(c.budgets) != 0 || tel.Deferrals(pgaccess.StagedAuditDeferConflictExhausted) != 1 {
			t.Fatalf("start probe conflict = %+v, %v, reads %d; want a conflict_exhausted deferral", got, err, len(c.budgets))
		}
	})
	t.Run("cancelled caller", func(t *testing.T) {
		a, tel := newSweepAuditor(&corpus{n: 5, factsPer: 10}, &sweepStore{}, nil)
		ctx, cancel := context.WithCancel(context.Background())
		a.ProbeNullKeys = func(context.Context, db.Queryer) (postgres.WinnersAuditNullKeys, error) {
			cancel()
			return postgres.WinnersAuditNullKeys{}, context.DeadlineExceeded
		}
		got, err := a.AuditWinners(ctx)
		if err != nil || !got.Deferred || tel.Deferrals(pgaccess.StagedAuditDeferTimeout) != 0 {
			t.Fatalf("cancelled probe = %+v, %v, timeout deferrals %d; want a deferral that is not a slow-statement timeout", got, err, tel.Deferrals(pgaccess.StagedAuditDeferTimeout))
		}
	})
	t.Run("end failure", func(t *testing.T) {
		store := &sweepStore{}
		probes := 0
		a, tel := newSweepAuditor(&corpus{n: 5, factsPer: 10}, store, nil)
		a.ProbeNullKeys = func(context.Context, db.Queryer) (postgres.WinnersAuditNullKeys, error) {
			probes++
			if probes > 1 {
				return postgres.WinnersAuditNullKeys{}, errors.New("standby went away")
			}
			return postgres.WinnersAuditNullKeys{}, nil
		}
		got, err := a.AuditWinners(context.Background())
		if err != nil || !got.Deferred || got.More || len(store.verifies) != 0 {
			t.Fatalf("end probe failure = %+v, %v, verifies %d; want a deferral with no writer contact", got, err, len(store.verifies))
		}
		if tel.Deferrals(pgaccess.StagedAuditDeferBatchAbort) != 1 {
			t.Fatalf("batch_abort deferrals = %d, want 1", tel.Deferrals(pgaccess.StagedAuditDeferBatchAbort))
		}
	})
}

// TestStagedAuditVerifyFailureIsAnError pins that a writer verify failure is
// returned (the maintainer's failure path), plain and under the keyed yield.
func TestStagedAuditVerifyFailureIsAnError(t *testing.T) {
	t.Parallel()
	boom := errors.New("writer verify failed")
	t.Run("plain", func(t *testing.T) {
		store := &scriptedStore{sweepStore: &sweepStore{}, verifyErr: boom}
		a, _ := newSweepAuditor(&corpus{n: 5, factsPer: 10}, store.sweepStore, nil)
		a.Store = store
		if _, err := a.AuditWinners(context.Background()); !errors.Is(err, boom) {
			t.Fatalf("err = %v, want the verify error wrapped", err)
		}
	})
	t.Run("keyed yield attempt", func(t *testing.T) {
		store := &scriptedStore{sweepStore: &sweepStore{}, noLockVerifyIs: boom}
		a, _ := newSweepAuditor(&corpus{n: 4000, factsPer: 10}, store.sweepStore, nil)
		a.Store = store
		a.KeyedInterval = time.Hour
		if _, err := a.AuditWinners(context.Background()); err != nil {
			t.Fatalf("locked pass: %v", err)
		}
		if _, err := a.AuditWinners(context.Background()); !errors.Is(err, boom) {
			t.Fatalf("err = %v, want the NoLock verify error wrapped", err)
		}
	})
}

// TestStagedAuditNoLockVerifyThatNeededNoLockAdvancesWithoutYielding pins the
// cheap yield path: when every candidate of the next range was already queued,
// the NoLock verify advances the cursor, so the range neither waits for a
// keyed commit, nor counts a keyed_yield, nor verifies a second time.
func TestStagedAuditNoLockVerifyThatNeededNoLockAdvancesWithoutYielding(t *testing.T) {
	t.Parallel()
	store := &scriptedStore{sweepStore: &sweepStore{}, noLockAdvances: true}
	a, tel := newSweepAuditor(&corpus{n: 4000, factsPer: 10}, store.sweepStore, nil)
	a.Store = store
	a.KeyedInterval, a.YieldPoll = time.Hour, time.Hour // a wait would hang the test
	if got, err := a.AuditWinners(context.Background()); err != nil || got.Outcome != OutcomeAdvanced {
		t.Fatalf("locked pass = %+v, %v", got, err)
	}
	got, err := a.AuditWinners(context.Background())
	if err != nil || got.Outcome != OutcomeAdvanced || !got.More {
		t.Fatalf("yielding pass = %+v, %v; want advanced", got, err)
	}
	var flags []bool
	for _, v := range store.verifies {
		flags = append(flags, v.NoLock)
	}
	if fmt.Sprint(flags) != fmt.Sprint([]bool{false, true}) {
		t.Fatalf("NoLock per verify = %v; want the second range verified once, without the lock", flags)
	}
	if n := tel.Deferrals(pgaccess.StagedAuditDeferKeyedYield); n != 0 {
		t.Fatalf("keyed_yield = %d, want 0", n)
	}
	if store.sweep.RangesDone != 2 {
		t.Fatalf("ranges done = %d, want the cursor advanced twice", store.sweep.RangesDone)
	}
}

// TestStagedAuditYieldWaitStopsWhenTheCallerLeaves pins that the keyed-yield
// wait honors cancellation: the pass returns the context error and never
// takes the lock for the yielded range.
func TestStagedAuditYieldWaitStopsWhenTheCallerLeaves(t *testing.T) {
	t.Parallel()
	store := &sweepStore{}
	a, _ := newSweepAuditor(&corpus{n: 4000, factsPer: 10}, store, nil)
	a.KeyedInterval, a.YieldPoll = time.Hour, time.Millisecond
	if _, err := a.AuditWinners(context.Background()); err != nil {
		t.Fatalf("locked pass: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := a.AuditWinners(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the caller's deadline", err)
	}
	if len(store.verifies) != 2 || !store.verifies[1].NoLock {
		t.Fatalf("verifies = %+v; want the locked range plus one NoLock attempt and no locked retry", store.verifies)
	}
}

// TestStagedAuditKeyedYieldDefaults pins the zero-value defaults: a keyed
// interval of 30 s keeps an unmoved yield armed, and a yield poll of 1 s is
// what the wait sleeps between watermark reads.
func TestStagedAuditKeyedYieldDefaults(t *testing.T) {
	t.Parallel()
	t.Run("interval holds the yield", func(t *testing.T) {
		store := &sweepStore{}
		a, _ := newSweepAuditor(&corpus{n: 4000, factsPer: 10}, store, nil)
		a.KeyedInterval = 0
		if _, err := a.AuditWinners(context.Background()); err != nil {
			t.Fatalf("locked pass: %v", err)
		}
		if !a.yielding(context.Background()) {
			t.Fatal("yield ended with an unmoved watermark well inside the 30 s default interval")
		}
	})
	t.Run("a failed later read is not a move", func(t *testing.T) {
		store := &sweepStore{watermark: time.Unix(1_800_000_000, 0)}
		a, _ := newSweepAuditor(&corpus{n: 4000, factsPer: 10}, store, nil)
		a.KeyedInterval = time.Hour
		if _, err := a.AuditWinners(context.Background()); err != nil {
			t.Fatalf("locked pass: %v", err)
		}
		store.mu.Lock()
		store.markFails = 1
		store.mu.Unlock()
		if !a.yielding(context.Background()) {
			t.Fatal("a failed watermark read ended the yield; only a moved watermark or the interval may")
		}
	})
	t.Run("default poll", func(t *testing.T) {
		// startYield reads once; the pass's yielding check, the wait's first
		// check, then the check after one poll: the fourth read moves it.
		store := &sweepStore{moveMarkAt: 4}
		a, _ := newSweepAuditor(&corpus{n: 4000, factsPer: 10}, store, nil)
		a.KeyedInterval, a.YieldPoll = time.Hour, 0
		if _, err := a.AuditWinners(context.Background()); err != nil {
			t.Fatalf("locked pass: %v", err)
		}
		start := time.Now()
		if got, err := a.AuditWinners(context.Background()); err != nil || got.Outcome != OutcomeAdvanced {
			t.Fatalf("yielding pass = %+v, %v", got, err)
		}
		if waited := time.Since(start); waited < 900*time.Millisecond {
			t.Fatalf("yield waited %v; want about the 1 s default poll", waited)
		}
	})
}

// TestStagedAuditLogsEveryRangeAndDeferral pins the operator log lines: one
// per range with its outcome and counters, one per completed sweep, and one
// per deferral with its reason and cause.
func TestStagedAuditLogsEveryRangeAndDeferral(t *testing.T) {
	t.Parallel()
	t.Run("range and completed sweep", func(t *testing.T) {
		var buf bytes.Buffer
		a, _ := newSweepAuditor(&corpus{n: 3, factsPer: 10}, &sweepStore{}, nil)
		a.Logger = slog.New(slog.NewTextHandler(&buf, nil))
		got, err := a.AuditWinners(context.Background())
		if err != nil || got.Outcome != OutcomeCompleted {
			t.Fatalf("AuditWinners = %+v, %v; want the one-range sweep completed", got, err)
		}
		out := buf.String()
		for _, want := range []string{
			`msg="staged winners audit range"`, "outcome=completed", "sweep_id=1", "last=true", "facts=30", "candidates=1", "repairs=1", "locked=true",
			`msg="staged winners audit sweep completed"`,
		} {
			if !strings.Contains(out, want) {
				t.Errorf("log missing %q:\n%s", want, out)
			}
		}
	})
	t.Run("skipped range logs no completion", func(t *testing.T) {
		var buf bytes.Buffer
		store := &sweepStore{lockScript: []string{"skip"}}
		a, _ := newSweepAuditor(&corpus{n: 500, factsPer: 100}, store, nil)
		a.Logger = slog.New(slog.NewTextHandler(&buf, nil))
		if _, err := a.AuditWinners(context.Background()); err != nil {
			t.Fatalf("AuditWinners: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "outcome=skipped") || strings.Contains(out, "sweep completed") {
			t.Fatalf("log = %s; want a skipped range line and no completion line", out)
		}
	})
	t.Run("deferral with cause", func(t *testing.T) {
		var buf bytes.Buffer
		a, _ := newSweepAuditor(&corpus{n: 5, factsPer: 10, pipeAt: 1}, &sweepStore{}, nil)
		a.Logger = slog.New(slog.NewTextHandler(&buf, nil))
		if _, err := a.AuditWinners(context.Background()); err != nil {
			t.Fatalf("AuditWinners: %v", err)
		}
		out := buf.String()
		for _, want := range []string{`msg="staged winners audit deferred"`, "reason=pipe", `error="a range fact has`} {
			if !strings.Contains(out, want) {
				t.Errorf("log missing %q:\n%s", want, out)
			}
		}
	})
	t.Run("deferral without cause", func(t *testing.T) {
		var buf bytes.Buffer
		a, _ := newSweepAuditor(&corpus{n: 5, factsPer: 10}, &sweepStore{}, func() postgres.WinnersAuditNullKeys {
			return postgres.WinnersAuditNullKeys{PackageID: true}
		})
		a.Logger = slog.New(slog.NewTextHandler(&buf, nil))
		if _, err := a.AuditWinners(context.Background()); err != nil {
			t.Fatalf("AuditWinners: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "reason=null_key") || strings.Contains(out, "error=") {
			t.Fatalf("log = %s; want a null_key line with no error attribute", out)
		}
	})
}
