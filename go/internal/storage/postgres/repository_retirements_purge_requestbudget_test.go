// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// deriveBusyLogMessage is the WARN line a derive_lock_busy exit writes.
const deriveBusyLogMessage = "repository retirement blocked: derive lock busy"

// capturedLog is one record the purge logged, with the wall time the handler
// received it.
type capturedLog struct {
	at    time.Time
	level slog.Level
	msg   string
	attrs map[string]any
}

// logCapture is a slog.Handler that keeps every record, so a test reads the
// structured fields instead of parsing text.
type logCapture struct {
	mu   sync.Mutex
	logs []capturedLog
}

func (c *logCapture) Enabled(context.Context, slog.Level) bool { return true }

func (c *logCapture) Handle(_ context.Context, record slog.Record) error {
	entry := capturedLog{at: time.Now(), level: record.Level, msg: record.Message, attrs: map[string]any{}}
	record.Attrs(func(attr slog.Attr) bool {
		entry.attrs[attr.Key] = attr.Value.Any()
		return true
	})
	c.mu.Lock()
	defer c.mu.Unlock()
	c.logs = append(c.logs, entry)
	return nil
}

func (c *logCapture) WithAttrs([]slog.Attr) slog.Handler { return c }

func (c *logCapture) WithGroup(string) slog.Handler { return c }

// named returns the records with the message, oldest first.
func (c *logCapture) named(msg string) []capturedLog {
	c.mu.Lock()
	defer c.mu.Unlock()
	var matched []capturedLog
	for _, entry := range c.logs {
		if entry.msg == msg {
			matched = append(matched, entry)
		}
	}
	return matched
}

// stepClock is a clock that moves only when a test advances it.
type stepClock struct{ now time.Time }

func newStepClock() *stepClock {
	return &stepClock{now: time.Date(2026, time.November, 17, 0, 0, 0, 0, time.UTC)}
}

func (c *stepClock) Now() time.Time { return c.now }

func (c *stepClock) advance(d time.Duration) { c.now = c.now.Add(d) }

// lockOutcome scripts what one derive advisory lock statement does.
type lockOutcome int

const (
	// lockFree acquires at once.
	lockFree lockOutcome = iota
	// lockHeld waits out the whole lock_timeout in force, then fails with 55P03.
	lockHeld
	// lockReleasedLate acquires 1 ms before the lock_timeout in force would end.
	lockReleasedLate
)

// repoLocks scripts one repository's two derive locks. The zero value is two
// free locks.
type repoLocks struct {
	infra, config lockOutcome
}

// budgetDB hands out one scripted transaction per repository, in the order the
// purge runs them, and models PostgreSQL's lock_timeout against a fake clock:
// a lock statement that waits advances the clock by the wait it models. It
// records every lock_timeout the purge set, in order.
type budgetDB struct {
	clock    *stepClock
	plans    []repoLocks
	begun    int
	settings []time.Duration
	waited   time.Duration
}

func newBudgetDB(plans ...repoLocks) *budgetDB {
	return &budgetDB{clock: newStepClock(), plans: plans}
}

func (d *budgetDB) Begin(context.Context) (db.Transaction, error) {
	if d.begun >= len(d.plans) {
		return nil, fmt.Errorf("fake: transaction %d began, only %d repositories are scripted", d.begun+1, len(d.plans))
	}
	_, staged := stagePurgeScriptedDB()
	tx := &budgetTx{fakeTx: staged, world: d, locks: d.plans[d.begun]}
	d.begun++
	return tx, nil
}

func (d *budgetDB) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return nil, errors.New("unexpected QueryContext on outer db")
}

// wait spends d of fake time on a lock wait.
func (d *budgetDB) wait(spent time.Duration) {
	d.clock.advance(spent)
	d.waited += spent
}

// budgetTx is one repository's scripted purge transaction. The queries, commit
// and rollback come from the shared fakeTx; the exec path adds the lock model.
type budgetTx struct {
	*fakeTx
	world *budgetDB
	locks repoLocks
	// timeout is the lock_timeout in force. Zero disables it, as in PostgreSQL.
	timeout time.Duration
}

func (tx *budgetTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	result, err := tx.fakeTx.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	if err := tx.model(query, args); err != nil {
		return nil, err
	}
	return result, nil
}

// model applies the statement's effect on the lock_timeout and the clock.
func (tx *budgetTx) model(query string, args []any) error {
	text := query + " " + fmt.Sprint(args...)
	switch {
	case strings.Contains(query, "'lock_timeout'") && len(args) == 1:
		setting, ok := args[0].(string)
		if !ok {
			return fmt.Errorf("fake: lock_timeout argument %v is not a string", args[0])
		}
		timeout, err := time.ParseDuration(setting)
		if err != nil {
			return fmt.Errorf("fake: lock_timeout %q: %w", setting, err)
		}
		tx.timeout = timeout
		tx.world.settings = append(tx.world.settings, timeout)
	case strings.Contains(text, "infra_resource_entities:"):
		return tx.take(tx.locks.infra)
	case strings.Contains(text, "config_artifact_paths:"):
		return tx.take(tx.locks.config)
	}
	return nil
}

// take runs one scripted lock statement under the lock_timeout in force.
func (tx *budgetTx) take(outcome lockOutcome) error {
	switch outcome {
	case lockFree:
		return nil
	case lockReleasedLate:
		if tx.timeout > time.Millisecond {
			tx.world.wait(tx.timeout - time.Millisecond)
		}
		return nil
	case lockHeld:
		if tx.timeout <= 0 {
			return errors.New("fake: a lock wait with no lock_timeout lasts as long as the holder, so the purge could wait without bound")
		}
		tx.world.wait(tx.timeout)
		return &pgconn.PgError{Code: "55P03", Message: "canceling statement due to lock timeout"}
	}
	return fmt.Errorf("fake: unknown lock outcome %d", outcome)
}

// newBudgetPurger builds a purger on the fake database and clock, with a log
// capture.
func newBudgetPurger(world *budgetDB) (RetirementPurger, *logCapture) {
	logs := &logCapture{}
	purger := NewRetirementPurger(world, world)
	purger.Options.Now = world.clock.Now
	purger.Options.Logger = slog.New(logs)
	return purger, logs
}

// budgetRepoIDs returns count repository ids that sort in creation order.
func budgetRepoIDs(prefix string, count int) []string {
	ids := make([]string, count)
	for i := range ids {
		ids[i] = fmt.Sprintf("PARTITION:%s-%02d", prefix, i+1)
	}
	return ids
}

// requireBoundedLockTimeouts fails when the purge ever set a lock_timeout
// outside 100 ms to 10 s. PostgreSQL reads 0 as "never time out", and a value
// under the floor would not give a free lock time to be taken. The bounds are
// the arbiter's numbers, not the production constants, so changing a constant
// is a visible test failure.
func requireBoundedLockTimeouts(t *testing.T, settings []time.Duration) {
	t.Helper()
	for i, got := range settings {
		if got < 100*time.Millisecond || got > 10*time.Second {
			t.Errorf("lock_timeout set #%d = %s, want between 100ms and 10s", i+1, got)
		}
	}
}

// TestPurgeRetiredShrinksTheDeriveLockWaitToTheFloor pins the request-level
// budget: three repositories whose infra lock is held each wait the full 10 s,
// which spends the 30 s; the fourth is set to the 100 ms floor and blocked
// after it; a later repository with free locks still completes. The WARN line
// carries the lock_timeout in force so a 100 ms attempt is distinguishable from
// a 10 s wait.
func TestPurgeRetiredShrinksTheDeriveLockWaitToTheFloor(t *testing.T) {
	t.Parallel()

	repoIDs := budgetRepoIDs("shrink", 5)
	world := newBudgetDB(
		repoLocks{infra: lockHeld}, repoLocks{infra: lockHeld},
		repoLocks{infra: lockHeld}, repoLocks{infra: lockHeld},
		repoLocks{},
	)
	purger, logs := newBudgetPurger(world)

	result, err := purger.PurgeRetired(context.Background(), PurgeRequest{RepoIDs: repoIDs})
	if err != nil {
		t.Fatalf("PurgeRetired: %v", err)
	}
	if len(result.Completed) != 1 || result.Completed[0] != repoIDs[4] {
		t.Fatalf("completed = %v, want only %s: a repository with free locks must still purge after the budget is spent",
			result.Completed, repoIDs[4])
	}
	if len(result.Blocked) != 4 {
		t.Fatalf("blocked = %+v, want the four held repositories", result.Blocked)
	}
	for i, blocked := range result.Blocked {
		if blocked.RepoID != repoIDs[i] || blocked.Reason != PurgeReasonDeriveLockBusy {
			t.Errorf("blocked[%d] = %s/%s, want %s/%s", i, blocked.RepoID, blocked.Reason, repoIDs[i], PurgeReasonDeriveLockBusy)
		}
	}

	wantSettings := []time.Duration{
		10 * time.Second, 10 * time.Second, 10 * time.Second, // three full waits spend the 30 s
		100 * time.Millisecond,                         // the fourth is held to the floor
		100 * time.Millisecond, 100 * time.Millisecond, // the fifth's two free locks, still at the floor
	}
	if fmt.Sprint(world.settings) != fmt.Sprint(wantSettings) {
		t.Errorf("lock_timeout settings = %v, want %v", world.settings, wantSettings)
	}
	requireBoundedLockTimeouts(t, world.settings)
	if want := 30*time.Second + 100*time.Millisecond; world.waited != want {
		t.Errorf("lock waiting = %s, want %s", world.waited, want)
	}

	busy := logs.named(deriveBusyLogMessage)
	wantTimeoutMS := []int64{10000, 10000, 10000, 100}
	if len(busy) != len(wantTimeoutMS) {
		t.Fatalf("derive_lock_busy WARN lines = %d, want %d", len(busy), len(wantTimeoutMS))
	}
	for i, entry := range busy {
		if got := entry.attrs["derive_lock_timeout_ms"]; got != wantTimeoutMS[i] {
			t.Errorf("WARN %d derive_lock_timeout_ms = %v, want %d", i+1, got, wantTimeoutMS[i])
		}
	}
}

// TestPurgeRetiredBoundsTheRequestDeriveLockWait pins the worst case the budget
// exists for: 25 repositories, each acquiring its first lock a millisecond
// before the timeout and timing out on its second. Without a request budget
// that is about 500 s of waiting; with it, 30 s plus one floor attempt per lock
// statement, at most 35 s.
func TestPurgeRetiredBoundsTheRequestDeriveLockWait(t *testing.T) {
	t.Parallel()

	repoIDs := budgetRepoIDs("worst", PurgeMaxRepositories)
	plans := make([]repoLocks, len(repoIDs))
	for i := range plans {
		plans[i] = repoLocks{infra: lockReleasedLate, config: lockHeld}
	}
	world := newBudgetDB(plans...)
	purger, _ := newBudgetPurger(world)

	result, err := purger.PurgeRetired(context.Background(), PurgeRequest{RepoIDs: repoIDs})
	if err != nil {
		t.Fatalf("PurgeRetired: %v", err)
	}
	if len(result.Completed) != 0 || len(result.Blocked) != len(repoIDs) {
		t.Fatalf("completed %d, blocked %d, want every repository blocked", len(result.Completed), len(result.Blocked))
	}
	for _, blocked := range result.Blocked {
		if blocked.Reason != PurgeReasonDeriveLockBusy || !strings.Contains(blocked.Detail, "config artifact") {
			t.Errorf("blocked %s = %s (%s), want derive_lock_busy on the config artifact lock", blocked.RepoID, blocked.Reason, blocked.Detail)
		}
	}
	requireBoundedLockTimeouts(t, world.settings)
	if world.waited <= 30*time.Second {
		t.Errorf("lock waiting = %s: the worst pattern never used the whole 30 s budget, so the bound below proves nothing", world.waited)
	}
	if limit := 30*time.Second + 2*PurgeMaxRepositories*100*time.Millisecond; world.waited > limit {
		t.Errorf("lock waiting across %d repositories = %s, want at most %s (30 s + 50 x 100 ms)", len(repoIDs), world.waited, limit)
	}
	t.Logf("worst pattern, %d repositories: %s of derive-lock waiting", len(repoIDs), world.waited)
}

// TestPurgeRetiredStartsEachRequestWithAFullDeriveLockBudget proves the budget
// belongs to one PurgeRetired call: a RetirementPurger is shared by concurrent
// requests, so a request that spent its 30 s must not shorten the next one.
func TestPurgeRetiredStartsEachRequestWithAFullDeriveLockBudget(t *testing.T) {
	t.Parallel()

	world := newBudgetDB(
		repoLocks{infra: lockHeld}, repoLocks{infra: lockHeld}, repoLocks{infra: lockHeld},
		repoLocks{infra: lockHeld},
	)
	purger, _ := newBudgetPurger(world)

	if _, err := purger.PurgeRetired(context.Background(), PurgeRequest{RepoIDs: budgetRepoIDs("first", 3)}); err != nil {
		t.Fatalf("first request: %v", err)
	}
	if world.waited != 30*time.Second {
		t.Fatalf("first request waited %s, want the whole 30 s budget spent", world.waited)
	}
	if _, err := purger.PurgeRetired(context.Background(), PurgeRequest{RepoIDs: budgetRepoIDs("second", 1)}); err != nil {
		t.Fatalf("second request: %v", err)
	}
	if got := world.settings[len(world.settings)-1]; got != 10*time.Second {
		t.Errorf("the second request's first lock_timeout = %s, want a full 10 s: the budget leaked across requests", got)
	}
}

// TestDeriveLockBudgetStaysBetweenFloorAndPerLockBudget pins the budget's
// arithmetic at its edges: never above one lock wait, never below the floor,
// whatever is left, and never rendered as a value PostgreSQL reads as
// "no timeout".
func TestDeriveLockBudgetStaysBetweenFloorAndPerLockBudget(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		remaining time.Duration
		want      string
	}{
		{"the whole request budget is capped to one lock wait", 30 * time.Second, "10000ms"},
		{"exactly one lock wait left", 10 * time.Second, "10000ms"},
		{"less than one lock wait left", 9978 * time.Millisecond, "9978ms"},
		{"exactly the floor left", 100 * time.Millisecond, "100ms"},
		{"just under the floor", 99 * time.Millisecond, "100ms"},
		{"spent", 0, "100ms"},
		{"overspent by a lock wait", -10 * time.Second, "100ms"},
	}
	for _, tc := range cases {
		budget := &deriveLockBudget{remaining: tc.remaining}
		if got := lockTimeoutSetting(budget.timeout()); got != tc.want {
			t.Errorf("%s: lock_timeout for %s left = %q, want %q", tc.name, tc.remaining, got, tc.want)
		}
	}
}

// TestDeriveLockBudgetSpendsTheElapsedLockWait pins the accounting: a lock
// statement's elapsed time comes off the budget on the injected clock, and a
// clock that steps backwards never refunds budget.
func TestDeriveLockBudgetSpendsTheElapsedLockWait(t *testing.T) {
	t.Parallel()

	clock := newStepClock()
	budget := newDeriveLockBudget(clock.Now)
	started := clock.Now()
	clock.advance(4 * time.Second)
	budget.spend(started)
	if want := purgeRequestDeriveLockBudget - 4*time.Second; budget.remaining != want {
		t.Fatalf("remaining after a 4 s wait = %s, want %s", budget.remaining, want)
	}
	budget.spend(clock.Now().Add(time.Hour)) // a start in the future: the clock stepped back
	if want := purgeRequestDeriveLockBudget - 4*time.Second; budget.remaining != want {
		t.Fatalf("remaining after a backwards clock = %s, want it unchanged at %s", budget.remaining, want)
	}
}
