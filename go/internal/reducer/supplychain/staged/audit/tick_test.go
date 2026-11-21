// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package audit

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	pgaccess "github.com/eshu-hq/eshu/go/internal/runtime/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// memberFleet pins one member and hands a nil reader to every statement: the
// range reader and NULL probe are fakes, so no SQL runs.
type memberFleet struct{ pinErr error }

func (f memberFleet) PinMember(context.Context) (string, error) { return "member-a", f.pinErr }
func (memberFleet) RunOnMember(ctx context.Context, _ string, fn func(context.Context, db.Queryer) error) error {
	return fn(ctx, nil)
}
func (memberFleet) ReplayLag(context.Context, string) (time.Duration, error) { return 0, nil }

// sweepStore is an in-memory cursor row with the compare-and-set of
// advanceWinnersAuditSweepSQL, plus scripted lock results.
type sweepStore struct {
	watermark  time.Time
	moveMarkAt int // Watermark call that moves it (0 = never)
	markCalls  int
	noCands    bool // ranges carry no candidates, so no verify locks
	mu         sync.Mutex
	sweep      postgres.WinnersAuditSweep
	lockScript []string // per verify: "", "skip", "busy", "race"
	verifies   []postgres.WinnersAuditRangeVerify
	completed  int
}

func (s *sweepStore) ReadWinnersAuditSweep(context.Context) (postgres.WinnersAuditSweep, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sweep.SweepID == 0 {
		s.sweep.SweepID = 1
	}
	return s.sweep, nil
}

func (s *sweepStore) Watermark(context.Context) (time.Time, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.markCalls++
	if s.moveMarkAt > 0 && s.markCalls >= s.moveMarkAt {
		s.watermark = s.watermark.Add(time.Second)
		s.markCalls = 0
	}
	return s.watermark, true, nil
}

func (s *sweepStore) VerifyWinnersAuditRange(_ context.Context, v postgres.WinnersAuditRangeVerify) (postgres.WinnersAuditRangeOutcome, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.noCands {
		v.Range.Candidates = nil
	}
	s.verifies = append(s.verifies, v)
	if v.NoLock && len(v.Range.Candidates) > 0 {
		return postgres.WinnersAuditRangeOutcome{Yielded: true, Candidates: int64(len(v.Range.Candidates))}, nil
	}
	script := ""
	if len(s.lockScript) > 0 {
		script, s.lockScript = s.lockScript[0], s.lockScript[1:]
	}
	switch script {
	case "skip":
		return postgres.WinnersAuditRangeOutcome{Skipped: true}, nil
	case "busy":
		return postgres.WinnersAuditRangeOutcome{LockBusy: true}, nil
	case "race":
		return postgres.WinnersAuditRangeOutcome{CursorRace: true}, nil
	}
	if v.SweepID != s.sweep.SweepID || cursorText(v.Range.Lo) != cursorText(s.sweep.Cursor) {
		return postgres.WinnersAuditRangeOutcome{CursorRace: true}, nil
	}
	out := postgres.WinnersAuditRangeOutcome{Advanced: true, Candidates: int64(len(v.Range.Candidates)), Locked: len(v.Range.Candidates) > 0}
	out.Repairs = out.Candidates
	if v.Range.Last {
		s.sweep.SweepID++
		s.sweep.Cursor, s.sweep.RangesDone = nil, 0
		if v.Complete {
			s.completed++
			out.Completed = true
		}
	} else {
		s.sweep.Cursor = v.Range.Hi
		s.sweep.RangesDone++
	}
	out.Sweep = s.sweep
	return out, nil
}

// corpus fakes the range statement over n identifiers of factsPer facts each
// in index order; identifier i is "p" + zero-padded i.
type corpus struct {
	n, factsPer int
	pipeAt      int // 1-based identifier with '|', 0 for none
	timeoutOver int // budgets above this time out, 0 for never
	budgets     []int
}

func ident(i int) *postgres.WinnersAuditRangeIdent {
	return &postgres.WinnersAuditRangeIdent{PackageID: fmt.Sprintf("p%07d", i), RepositoryID: "r", SubjectDigest: "", C0: "c", AdvisoryID: "a"}
}

func (c *corpus) read(_ context.Context, _ db.Queryer, lo *postgres.WinnersAuditRangeIdent, budget int) (postgres.WinnersAuditRange, error) {
	c.budgets = append(c.budgets, budget)
	if c.timeoutOver > 0 && budget > c.timeoutOver {
		return postgres.WinnersAuditRange{}, context.DeadlineExceeded
	}
	start := 1
	if lo != nil {
		_, _ = fmt.Sscanf(lo.PackageID, "p%07d", &start)
		start++
	}
	whole := max(1, budget/c.factsPer)
	end := start + whole - 1
	out := postgres.WinnersAuditRange{Lo: lo, Budget: budget}
	if end >= c.n {
		end, out.Last = c.n, true
	} else {
		out.Hi = ident(end)
	}
	if start > c.n {
		end = start - 1
	}
	out.Facts = int64((end - start + 1) * c.factsPer)
	out.Oversize = budget < c.factsPer
	out.Pipe = c.pipeAt >= start && c.pipeAt <= end
	out.Candidates = []postgres.WinnersAuditIdent{{PackageID: sql.NullString{String: ident(start).PackageID, Valid: true}}}
	return out, nil
}

func newSweepAuditor(c *corpus, store *sweepStore, nulls func() postgres.WinnersAuditNullKeys) (*StagedAuditor, *pgaccess.StagedAuditTelemetry) {
	tel := pgaccess.NewStagedAuditTelemetry()
	if nulls == nil {
		nulls = func() postgres.WinnersAuditNullKeys { return postgres.WinnersAuditNullKeys{} }
	}
	return &StagedAuditor{
		Fleet: memberFleet{}, Store: store, Telemetry: tel, ReadRange: c.read,
		KeyedInterval: 5 * time.Millisecond, YieldPoll: time.Millisecond,
		ProbeNullKeys: func(context.Context, db.Queryer) (postgres.WinnersAuditNullKeys, error) { return nulls(), nil },
	}, tel
}

// TestStagedAuditSweepCompletesPastTheCorpusCaps is the #11131 proof (a): a
// corpus of 1,200,000 facts, past StagedAuditMaxStagedFacts (1,000,000), is
// read in budgeted ranges and the sweep completes with no budget deferral.
func TestStagedAuditSweepCompletesPastTheCorpusCaps(t *testing.T) {
	t.Parallel()
	c := &corpus{n: 12000, factsPer: 100}
	if c.n*c.factsPer <= pgaccess.StagedAuditMaxStagedFacts {
		t.Fatalf("corpus %d facts must exceed the old cap %d", c.n*c.factsPer, pgaccess.StagedAuditMaxStagedFacts)
	}
	store := &sweepStore{}
	a, tel := newSweepAuditor(c, store, nil)
	var facts int64
	for pass := 0; ; pass++ {
		got, err := a.AuditWinners(context.Background())
		if err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
		if got.Deferred {
			t.Fatalf("pass %d deferred: the sweep must not defer past the old caps", pass)
		}
		if !got.More {
			if got.Partial || got.Outcome != OutcomeCompleted {
				t.Fatalf("final pass = %+v, want a completed, non-partial pass", got)
			}
			break
		}
		if pass > 1000 {
			t.Fatal("sweep did not end")
		}
	}
	var commits int
	for _, v := range store.verifies {
		if v.NoLock {
			continue // a keyed-yield attempt; the same range verifies again
		}
		facts += v.Range.Facts
		commits++
	}
	if store.completed != 1 || facts != int64(c.n*c.factsPer) {
		t.Fatalf("completed sweeps = %d, facts verified = %d; want 1 and %d", store.completed, facts, c.n*c.factsPer)
	}
	if n := tel.Deferrals(pgaccess.StagedAuditDeferBudget); n != 0 {
		t.Fatalf("budget deferrals = %d, want 0", n)
	}
	if want := int64(commits); tel.Ranges() != want || want != 120 {
		t.Fatalf("ranges = %d (verifies %d), want 120 at the default 10,000-row budget", tel.Ranges(), want)
	}
}

// TestStagedAuditRangeTimeoutHalvesTheBudget pins the timeout retry: each
// statement timeout halves the budget, down to one row; a one-row range that
// still times out defers as timeout.
func TestStagedAuditRangeTimeoutHalvesTheBudget(t *testing.T) {
	t.Parallel()
	c := &corpus{n: 50, factsPer: 10, timeoutOver: 1000}
	a, tel := newSweepAuditor(c, &sweepStore{}, nil)
	got, err := a.AuditWinners(context.Background())
	if err != nil || got.Deferred {
		t.Fatalf("AuditWinners = %+v, %v; want a committed range", got, err)
	}
	want := []int{10000, 5000, 2500, 1250, 625}
	if fmt.Sprint(c.budgets) != fmt.Sprint(want) || tel.RangeRetries() != 4 {
		t.Fatalf("budgets = %v, retries = %d; want %v and 4", c.budgets, tel.RangeRetries(), want)
	}

	a2, tel2 := newSweepAuditor(&corpus{n: 50, factsPer: 10}, &sweepStore{}, nil)
	a2.ReadRange = func(context.Context, db.Queryer, *postgres.WinnersAuditRangeIdent, int) (postgres.WinnersAuditRange, error) {
		return postgres.WinnersAuditRange{}, context.DeadlineExceeded
	}
	got, err = a2.AuditWinners(context.Background())
	if err != nil || !got.Deferred || got.More {
		t.Fatalf("one-row timeout = %+v, %v; want a deferred pass with no More", got, err)
	}
	if tel2.Deferrals(pgaccess.StagedAuditDeferTimeout) != 1 {
		t.Fatalf("timeout deferrals = %d, want 1", tel2.Deferrals(pgaccess.StagedAuditDeferTimeout))
	}
}

// TestStagedAuditGuardsFailClosed is the #11131 proof (f), unit half: a '|'
// in a range identifier defers as pipe with no writer contact; a NULL at
// sweep start defers before any range read; a NULL at sweep end verifies the
// last range but does not count the sweep as complete.
func TestStagedAuditGuardsFailClosed(t *testing.T) {
	t.Parallel()
	t.Run("pipe", func(t *testing.T) {
		store := &sweepStore{}
		a, tel := newSweepAuditor(&corpus{n: 5, factsPer: 10, pipeAt: 1}, store, nil)
		got, err := a.AuditWinners(context.Background())
		if err != nil || !got.Deferred || len(store.verifies) != 0 || tel.Deferrals(pgaccess.StagedAuditDeferPipe) != 1 {
			t.Fatalf("pipe = %+v, %v, verifies %d; want deferred, no verify, one pipe deferral", got, err, len(store.verifies))
		}
	})
	t.Run("null at start", func(t *testing.T) {
		c := &corpus{n: 5, factsPer: 10}
		store := &sweepStore{}
		a, tel := newSweepAuditor(c, store, func() postgres.WinnersAuditNullKeys { return postgres.WinnersAuditNullKeys{RepositoryID: true} })
		got, err := a.AuditWinners(context.Background())
		if err != nil || !got.Deferred || len(c.budgets) != 0 || tel.Deferrals(pgaccess.StagedAuditDeferNullKey) != 1 {
			t.Fatalf("null start = %+v, %v, reads %d; want deferred before any range read", got, err, len(c.budgets))
		}
	})
	t.Run("null at end", func(t *testing.T) {
		probes := 0
		store := &sweepStore{}
		a, tel := newSweepAuditor(&corpus{n: 5, factsPer: 10}, store, func() postgres.WinnersAuditNullKeys {
			probes++
			return postgres.WinnersAuditNullKeys{SubjectDigest: probes > 1}
		})
		got, err := a.AuditWinners(context.Background())
		if err != nil || got.Deferred || !got.Partial || got.More || store.completed != 0 {
			t.Fatalf("null end = %+v, %v, completed %d; want a verified last range that does not complete", got, err, store.completed)
		}
		if len(store.verifies) != 1 || store.verifies[0].Complete || tel.Deferrals(pgaccess.StagedAuditDeferNullKey) != 1 {
			t.Fatalf("verifies = %+v; want one with Complete=false and one null_key deferral", store.verifies)
		}
	})
}

// TestStagedAuditLockPolicy pins decision 3: two try-lock misses on the same
// range switch the next verify to the waiting claim; lock_busy and
// cursor_race are counted and keep the tick going.
func TestStagedAuditLockPolicy(t *testing.T) {
	t.Parallel()
	store := &sweepStore{lockScript: []string{"skip", "skip", "", "busy", "race"}}
	a, tel := newSweepAuditor(&corpus{n: 500, factsPer: 100}, store, nil)
	var outcomes []string
	for i := 0; i < 5; i++ {
		got, err := a.AuditWinners(context.Background())
		if err != nil {
			t.Fatalf("pass %d: %v", i, err)
		}
		if !got.More {
			t.Fatalf("pass %d = %+v, want More", i, got)
		}
		outcomes = append(outcomes, got.Outcome)
	}
	if fmt.Sprint(outcomes) != fmt.Sprint([]string{OutcomeSkipped, OutcomeSkipped, OutcomeAdvanced, OutcomeLockBusy, OutcomeCursorRace}) {
		t.Fatalf("outcomes = %v", outcomes)
	}
	waits := []bool{}
	for _, v := range store.verifies {
		if v.NoLock {
			continue // the keyed yield after the locked third verify
		}
		waits = append(waits, v.WaitForLock)
	}
	if fmt.Sprint(waits) != fmt.Sprint([]bool{false, false, true, false, false}) {
		t.Fatalf("WaitForLock per verify = %v, want the third (after two misses on one range) to wait", waits)
	}
	if tel.Deferrals(pgaccess.StagedAuditDeferLockBusy) != 1 || tel.Deferrals(pgaccess.StagedAuditDeferCursorRace) != 1 {
		t.Fatalf("lock_busy = %d, cursor_race = %d; want 1 each", tel.Deferrals(pgaccess.StagedAuditDeferLockBusy), tel.Deferrals(pgaccess.StagedAuditDeferCursorRace))
	}
}

// TestStagedAuditPinFailureDefers keeps a refused member pin a deferral.
func TestStagedAuditPinFailureDefers(t *testing.T) {
	t.Parallel()
	a, tel := newSweepAuditor(&corpus{n: 5, factsPer: 10}, &sweepStore{}, nil)
	a.Fleet = memberFleet{pinErr: errors.New("no member")}
	got, err := a.AuditWinners(context.Background())
	if err != nil || !got.Deferred || tel.Deferrals(pgaccess.StagedAuditDeferBatchAbort) != 1 {
		t.Fatalf("pin failure = %+v, %v; want a batch_abort deferral", got, err)
	}
}

// TestStagedAuditKeyedYieldAfterLockedVerify pins the keyed-yield rule
// (#11131, coordinator decision on arbiter 11120a decision 3): after a verify
// that took the resweep lock, the next range verifies with NoLock; a range
// that still needs the lock yields (counted as keyed_yield) until the
// watermark moves or one keyed interval passes, then verifies normally and
// advances in the same pass. A verify that took no lock sets no yield.
func TestStagedAuditKeyedYieldAfterLockedVerify(t *testing.T) {
	t.Parallel()
	t.Run("interval passes", func(t *testing.T) {
		store := &sweepStore{}
		a, tel := newSweepAuditor(&corpus{n: 4000, factsPer: 10}, store, nil)
		a.KeyedInterval, a.YieldPoll = 30*time.Millisecond, 2*time.Millisecond
		for i := 0; i < 3; i++ {
			got, err := a.AuditWinners(context.Background())
			if err != nil || got.Outcome != OutcomeAdvanced {
				t.Fatalf("pass %d = %+v, %v; want advanced in one pass", i, got, err)
			}
		}
		var flags []bool
		for _, v := range store.verifies {
			flags = append(flags, v.NoLock)
		}
		if fmt.Sprint(flags) != fmt.Sprint([]bool{false, true, false, true, false}) {
			t.Fatalf("NoLock per verify = %v; want each range after a locked verify to try without the lock first", flags)
		}
		if n := tel.Deferrals(pgaccess.StagedAuditDeferKeyedYield); n != 2 {
			t.Fatalf("keyed_yield = %d, want 2", n)
		}
	})
	t.Run("keyed commit observed", func(t *testing.T) {
		store := &sweepStore{moveMarkAt: 2}
		a, _ := newSweepAuditor(&corpus{n: 4000, factsPer: 10}, store, nil)
		a.KeyedInterval, a.YieldPoll = time.Hour, time.Millisecond
		start := time.Now()
		for i := 0; i < 2; i++ {
			if got, err := a.AuditWinners(context.Background()); err != nil || got.Outcome != OutcomeAdvanced {
				t.Fatalf("pass %d = %+v, %v", i, got, err)
			}
		}
		if time.Since(start) > 10*time.Second {
			t.Fatalf("yield waited %v: a moved watermark must end it before the keyed interval", time.Since(start))
		}
	})
	t.Run("unlocked verify sets no yield", func(t *testing.T) {
		store := &sweepStore{noCands: true}
		a, tel := newSweepAuditor(&corpus{n: 4000, factsPer: 10}, store, nil)
		for i := 0; i < 3; i++ {
			if _, err := a.AuditWinners(context.Background()); err != nil {
				t.Fatalf("pass %d: %v", i, err)
			}
		}
		for i, v := range store.verifies {
			if v.NoLock {
				t.Fatalf("verify %d has NoLock after unlocked verifies", i)
			}
		}
		if tel.Deferrals(pgaccess.StagedAuditDeferKeyedYield) != 0 {
			t.Fatal("keyed_yield counted with no locked verify")
		}
	})
}
