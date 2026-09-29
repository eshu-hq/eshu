// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordination

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func lockNotAvailable() error {
	return &pgconn.PgError{Code: "55P03", Message: "canceling statement due to lock timeout"}
}

// recordedSleeps records backoffs and advances a fake clock by them, so the
// run deadline is exercised without real time.
type recordedSleeps struct {
	waits []time.Duration
	clock time.Time
}

func (r *recordedSleeps) sleep(_ context.Context, d time.Duration) error {
	r.waits = append(r.waits, d)
	r.clock = r.clock.Add(d)
	return nil
}

func (r *recordedSleeps) now() time.Time { return r.clock }

func retryPolicy(sleeps *recordedSleeps, budget, initial, maxBackoff time.Duration) LockRetryPolicy {
	return LockRetryPolicy{
		Allowance:      NewLockRetryAllowance(budget),
		InitialBackoff: initial,
		MaxBackoff:     maxBackoff,
	}
}

// TestRetryOnLockTimeoutRetriesUntilTheLockClears pins #6956 cause 2: a
// migration statement that hits lock_timeout is retried with doubling
// backoff, and the recovery is logged with the attempt count.
func TestRetryOnLockTimeoutRetriesUntilTheLockClears(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	sleeps := &recordedSleeps{clock: time.Unix(0, 0)}
	attempts := 0
	err := RetryOnLockTimeout(context.Background(), logger, "test/002_index.sql", retryPolicy(sleeps, time.Minute, time.Second, 4*time.Second), sleeps.sleep, sleeps.now, func() error {
		attempts++
		if attempts < 4 {
			return lockNotAvailable()
		}
		return nil
	})
	if err != nil {
		t.Fatalf("RetryOnLockTimeout() = %v, want success once the lock clears", err)
	}
	if attempts != 4 {
		t.Fatalf("attempts = %d, want 4 (three lock timeouts then success)", attempts)
	}
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}
	if len(sleeps.waits) != len(want) {
		t.Fatalf("sleeps = %v, want %v", sleeps.waits, want)
	}
	for i := range want {
		if sleeps.waits[i] != want[i] {
			t.Fatalf("sleeps = %v, want %v (doubling, capped)", sleeps.waits, want)
		}
	}
	text := logs.String()
	if strings.Count(text, "bootstrap.postgres.migration.lock_wait") != 3 {
		t.Fatalf("want three lock_wait log events, got:\n%s", text)
	}
	if !strings.Contains(text, "bootstrap.postgres.migration.lock_recovered") || !strings.Contains(text, "attempts=4") {
		t.Fatalf("want a lock_recovered event naming attempts=4, got:\n%s", text)
	}
}

// TestRetryOnLockTimeoutDoesNotRetryOtherErrors pins the retry boundary:
// only SQLSTATE 55P03 is retried; any other failure surfaces on the first
// attempt untouched.
func TestRetryOnLockTimeoutDoesNotRetryOtherErrors(t *testing.T) {
	t.Parallel()
	sleeps := &recordedSleeps{clock: time.Unix(0, 0)}
	boom := errors.New("syntax error")
	attempts := 0
	err := RetryOnLockTimeout(context.Background(), slog.Default(), "test/003.sql", retryPolicy(sleeps, time.Minute, time.Second, time.Second), sleeps.sleep, sleeps.now, func() error {
		attempts++
		return boom
	})
	if !errors.Is(err, boom) || attempts != 1 || len(sleeps.waits) != 0 {
		t.Fatalf("err=%v attempts=%d sleeps=%v; want the original error after one attempt and no sleep", err, attempts, sleeps.waits)
	}
}

func TestRetryOnLockTimeoutRequiresAllowance(t *testing.T) {
	t.Parallel()
	attempts := 0
	sleeps := 0
	clock := time.Unix(0, 0)
	err := RetryOnLockTimeout(context.Background(), slog.Default(), "test/missing-allowance.sql", LockRetryPolicy{
		InitialBackoff: time.Second,
		MaxBackoff:     time.Second,
	}, func(context.Context, time.Duration) error {
		sleeps++
		return nil
	}, func() time.Time { return clock }, func() error {
		attempts++
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "lock retry allowance is required") {
		t.Fatalf("RetryOnLockTimeout() = %v, want missing-allowance error", err)
	}
	if attempts != 0 || sleeps != 0 {
		t.Fatalf("attempts=%d sleeps=%d, want no work before rejecting missing allowance", attempts, sleeps)
	}
}

// TestRetryOnLockTimeoutGivesUpWhenAllowanceIsSpent pins the bound: failed
// attempt time and backoff consume the shared allowance, and exhaustion keeps
// the 55P03 classification while naming the budget, attempts, and migration.
func TestRetryOnLockTimeoutGivesUpWhenAllowanceIsSpent(t *testing.T) {
	t.Parallel()
	sleeps := &recordedSleeps{clock: time.Unix(0, 0)}
	attempts := 0
	// Each failed attempt burns 5 s before the backoff is considered, as a real
	// statement waiting on lock_timeout does.
	err := RetryOnLockTimeout(context.Background(), slog.Default(), "test/004.sql", retryPolicy(sleeps, 20*time.Second, 5*time.Second, 15*time.Second), sleeps.sleep, sleeps.now, func() error {
		attempts++
		sleeps.clock = sleeps.clock.Add(5 * time.Second)
		return lockNotAvailable()
	})
	if err == nil {
		t.Fatal("RetryOnLockTimeout() = nil, want retry-allowance exhaustion")
	}
	if !IsLockNotAvailable(err) {
		t.Fatalf("error lost its 55P03 classification: %v", err)
	}
	if !strings.Contains(err.Error(), "lock retry budget 20s") || !strings.Contains(err.Error(), "test/004.sql") {
		t.Fatalf("error = %q, want it to name the budget and the migration", err)
	}
	// Attempt one and its 5 s backoff spend 10 s. Attempt two then leaves only
	// 5 s, less than its next 10 s backoff. Two attempts and one sleep prove
	// both failed execution and delay consume the allowance.
	if attempts != 2 || len(sleeps.waits) != 1 {
		t.Fatalf("attempts=%d sleeps=%v, want 2 attempts and 1 sleep", attempts, sleeps.waits)
	}
}

// TestRetryOnLockTimeoutDoesNotRetryAfterBackoffOversleeps pins the allowance
// boundary when the scheduler or sleeper exceeds the requested delay.
func TestRetryOnLockTimeoutDoesNotRetryAfterBackoffOversleeps(t *testing.T) {
	t.Parallel()
	sleeps := &recordedSleeps{clock: time.Unix(0, 0)}
	attempts := 0
	err := RetryOnLockTimeout(context.Background(), slog.Default(), "test/oversleep.sql", retryPolicy(sleeps, time.Second, 500*time.Millisecond, 500*time.Millisecond), func(_ context.Context, requested time.Duration) error {
		sleeps.waits = append(sleeps.waits, requested)
		// The failed attempt uses 500 ms; the scheduler then oversleeps the
		// requested 500 ms backoff by enough to exceed the 1 s allowance.
		sleeps.clock = sleeps.clock.Add(600 * time.Millisecond)
		return nil
	}, sleeps.now, func() error {
		attempts++
		if attempts == 1 {
			sleeps.clock = sleeps.clock.Add(500 * time.Millisecond)
			return lockNotAvailable()
		}
		return nil
	})
	if err == nil || !IsLockNotAvailable(err) {
		t.Fatalf("RetryOnLockTimeout() = %v, want 55P03 allowance exhaustion", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want no SQL retry after the shared allowance expires", attempts)
	}
}

// TestRetryOnLockTimeoutAllowanceIsSharedAcrossStatements pins that the
// same allowance belongs to the run: a second statement gets only what is
// left after the first statement's failed attempts and backoffs.
func TestRetryOnLockTimeoutAllowanceIsSharedAcrossStatements(t *testing.T) {
	t.Parallel()
	sleeps := &recordedSleeps{clock: time.Unix(0, 0)}
	policy := retryPolicy(sleeps, 30*time.Second, 5*time.Second, 5*time.Second)
	failTwice := func() func() error {
		n := 0
		return func() error {
			n++
			sleeps.clock = sleeps.clock.Add(5 * time.Second)
			if n <= 2 {
				return lockNotAvailable()
			}
			return nil
		}
	}
	if err := RetryOnLockTimeout(context.Background(), slog.Default(), "test/a.sql", policy, sleeps.sleep, sleeps.now, failTwice()); err != nil {
		t.Fatalf("first statement: %v", err)
	}
	// The first statement spends 20 s on two failed attempts and two sleeps;
	// its successful 5 s execution is excluded. The second statement uses its
	// remaining allowance and cannot receive a fresh budget.
	secondAttempts := 0
	err := RetryOnLockTimeout(context.Background(), slog.Default(), "test/b.sql", policy, sleeps.sleep, sleeps.now, func() error {
		secondAttempts++
		sleeps.clock = sleeps.clock.Add(5 * time.Second)
		return lockNotAvailable()
	})
	if err == nil || !strings.Contains(err.Error(), "test/b.sql") {
		t.Fatalf("second statement err = %v, want the shared allowance to end it", err)
	}
	if secondAttempts != 1 || len(sleeps.waits) != 3 {
		t.Fatalf("second attempts=%d sleeps=%v, want one attempt and no retry after its backoff spends the remaining allowance", secondAttempts, sleeps.waits)
	}
}

// TestRetryOnLockTimeoutStopsWhenTheContextEnds pins cancellation: a
// canceled context during the backoff returns the context error and does
// not run the statement again.
func TestRetryOnLockTimeoutStopsWhenTheContextEnds(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0
	clock := time.Unix(0, 0)
	err := RetryOnLockTimeout(ctx, slog.Default(), "test/005.sql", LockRetryPolicy{
		Allowance: NewLockRetryAllowance(time.Minute), InitialBackoff: time.Second, MaxBackoff: time.Second,
	}, func(ctx context.Context, _ time.Duration) error {
		cancel()
		return ctx.Err()
	}, func() time.Time { return clock }, func() error {
		attempts++
		return lockNotAvailable()
	})
	if !errors.Is(err, context.Canceled) || attempts != 1 {
		t.Fatalf("err=%v attempts=%d, want context.Canceled after one attempt", err, attempts)
	}
}

type fakeAdvisoryLocker struct {
	answers   []bool
	calls     int
	describes int
	holder    string
}

func (f *fakeAdvisoryLocker) TryLock(context.Context) (bool, error) {
	if f.calls < len(f.answers) {
		got := f.answers[f.calls]
		f.calls++
		return got, nil
	}
	f.calls++
	return false, nil
}

func (f *fakeAdvisoryLocker) DescribeHolder(context.Context) (string, error) {
	f.describes++
	return f.holder, nil
}

// TestWaitForSchemaOwnershipPollsUntilTheOwnerReleases pins #6956 cause 1:
// a bootstrapper behind another owner keeps polling the advisory lock,
// logs who holds it, and proceeds once the try succeeds.
func TestWaitForSchemaOwnershipPollsUntilTheOwnerReleases(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	locker := &fakeAdvisoryLocker{answers: []bool{false, false, true}, holder: "pid=42 application_name=eshu-bootstrap-data-plane"}
	sleeps := &recordedSleeps{}
	clock := time.Unix(0, 0)
	now := func() time.Time { clock = clock.Add(time.Second); return clock }
	err := WaitForOwnership(context.Background(), logger, locker, OwnershipPolicy{
		Wait: time.Minute, Poll: time.Second, LogEvery: time.Second,
	}, sleeps.sleep, now)
	if err != nil {
		t.Fatalf("WaitForOwnership() = %v, want success on the third try", err)
	}
	if locker.calls != 3 || len(sleeps.waits) != 2 {
		t.Fatalf("calls=%d sleeps=%v, want 3 tries and 2 polls", locker.calls, sleeps.waits)
	}
	if locker.describes != 2 {
		t.Fatalf("DescribeHolder ran %d times, want once per logged wait (2)", locker.describes)
	}
	text := logs.String()
	if !strings.Contains(text, "bootstrap.postgres.ownership.waiting") || !strings.Contains(text, "pid=42") {
		t.Fatalf("want a waiting event naming the holder, got:\n%s", text)
	}
	if !strings.Contains(text, "bootstrap.postgres.ownership.acquired") {
		t.Fatalf("want an acquired event after the wait, got:\n%s", text)
	}
}

// TestWaitForSchemaOwnershipGivesUpAfterTheWait pins the bound: an owner
// that never releases makes the waiter fail with an error naming the wait
// and the holder, instead of hanging.
func TestWaitForSchemaOwnershipGivesUpAfterTheWait(t *testing.T) {
	t.Parallel()
	locker := &fakeAdvisoryLocker{holder: "pid=7 application_name=other"}
	clock := time.Unix(0, 0)
	now := func() time.Time { clock = clock.Add(2 * time.Second); return clock }
	err := WaitForOwnership(context.Background(), slog.Default(), locker, OwnershipPolicy{
		Wait: 5 * time.Second, Poll: time.Second, LogEvery: time.Minute,
	}, (&recordedSleeps{}).sleep, now)
	if err == nil {
		t.Fatal("WaitForOwnership() = nil, want a bounded failure")
	}
	if !strings.Contains(err.Error(), "5s") || !strings.Contains(err.Error(), "pid=7") {
		t.Fatalf("error = %q, want it to name the wait and the holder", err)
	}
}

// TestWaitForOwnershipNamesItsSubjectAndLock pins the #7125 reuse of the
// ownership wait for the bulk-load run lock: the messages and the terminal
// error name the configured subject, and the events carry the lock attribute,
// so an operator can tell the two waits apart. An empty policy keeps the
// schema bootstrap text and the lock=schema attribute byte for byte.
func TestWaitForOwnershipNamesItsSubjectAndLock(t *testing.T) {
	t.Parallel()
	run := func(policy OwnershipPolicy, answers []bool) (string, error) {
		var logs bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logs, nil))
		locker := &fakeAdvisoryLocker{answers: answers, holder: "pid=9 application_name=other"}
		clock := time.Unix(0, 0)
		now := func() time.Time { clock = clock.Add(2 * time.Second); return clock }
		policy.Wait, policy.Poll, policy.LogEvery = 5*time.Second, time.Second, time.Second
		err := WaitForOwnership(context.Background(), logger, locker, policy, (&recordedSleeps{}).sleep, now)
		return logs.String(), err
	}

	logs, err := run(OwnershipPolicy{Subject: "secret lines bulk load", Lock: "bulk_load"}, []bool{false, true})
	if err != nil {
		t.Fatalf("WaitForOwnership() = %v, want success", err)
	}
	for _, want := range []string{
		"postgres secret lines bulk load waiting for ownership",
		"postgres secret lines bulk load ownership acquired after waiting",
		"lock=bulk_load",
		"bootstrap.postgres.ownership.waiting",
		"bootstrap.postgres.ownership.acquired",
	} {
		if !strings.Contains(logs, want) {
			t.Fatalf("subject logs missing %q:\n%s", want, logs)
		}
	}
	if strings.Contains(logs, "schema bootstrap") {
		t.Fatalf("subject logs still name the schema bootstrap:\n%s", logs)
	}

	_, err = run(OwnershipPolicy{Subject: "secret lines bulk load", Lock: "bulk_load"}, nil)
	if err == nil || !strings.Contains(err.Error(), "acquire secret lines bulk load ownership") ||
		!strings.Contains(err.Error(), "pid=9") {
		t.Fatalf("give-up error = %v, want the subject and the holder", err)
	}

	logs, err = run(OwnershipPolicy{}, []bool{false, true})
	if err != nil {
		t.Fatalf("WaitForOwnership() = %v, want success", err)
	}
	for _, want := range []string{
		"postgres schema bootstrap waiting for ownership",
		"postgres schema bootstrap ownership acquired after waiting",
		"lock=schema",
	} {
		if !strings.Contains(logs, want) {
			t.Fatalf("default logs missing %q:\n%s", want, logs)
		}
	}
	_, err = run(OwnershipPolicy{}, nil)
	if err == nil || !strings.Contains(err.Error(), "acquire schema bootstrap ownership") {
		t.Fatalf("default give-up error = %v, want the schema bootstrap text", err)
	}
}
