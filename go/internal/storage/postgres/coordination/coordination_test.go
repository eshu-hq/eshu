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
	return LockRetryPolicy{Budget: budget, Deadline: sleeps.clock.Add(budget), InitialBackoff: initial, MaxBackoff: maxBackoff}
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

// TestRetryOnLockTimeoutGivesUpWhenTheDeadlinePasses pins the bound: the
// deadline is wall clock, so time an attempt itself burned (the lock_timeout
// it waited) counts as much as the sleeps; once the next backoff would end
// after the deadline the migrator fails, naming the budget, the attempts and
// the path, and the error still classifies as 55P03.
func TestRetryOnLockTimeoutGivesUpWhenTheDeadlinePasses(t *testing.T) {
	t.Parallel()
	sleeps := &recordedSleeps{clock: time.Unix(0, 0)}
	attempts := 0
	// Each failed attempt burns 5 s of lock_timeout on the clock before the
	// backoff is considered, as the real statement does.
	err := RetryOnLockTimeout(context.Background(), slog.Default(), "test/004.sql", retryPolicy(sleeps, 20*time.Second, 5*time.Second, 15*time.Second), sleeps.sleep, sleeps.now, func() error {
		attempts++
		sleeps.clock = sleeps.clock.Add(5 * time.Second)
		return lockNotAvailable()
	})
	if err == nil {
		t.Fatal("RetryOnLockTimeout() = nil, want deadline exhaustion")
	}
	if !IsLockNotAvailable(err) {
		t.Fatalf("error lost its 55P03 classification: %v", err)
	}
	if !strings.Contains(err.Error(), "lock retry budget 20s") || !strings.Contains(err.Error(), "test/004.sql") {
		t.Fatalf("error = %q, want it to name the budget and the migration", err)
	}
	// t=5 after attempt 1: 5+5=10 <= 20, sleep 5 (t=10); attempt 2 burns to
	// t=15: 15+10=25 > 20, fail. Two attempts, one sleep: the 5 s each attempt
	// burned is what ended the run, not the sleeps alone.
	if attempts != 2 || len(sleeps.waits) != 1 {
		t.Fatalf("attempts=%d sleeps=%v, want 2 attempts and 1 sleep", attempts, sleeps.waits)
	}
}

// TestRetryOnLockTimeoutDeadlineIsSharedAcrossStatements pins that the
// deadline belongs to the run: a second statement retried with the same
// policy after the first spent most of the budget gets only what is left.
func TestRetryOnLockTimeoutDeadlineIsSharedAcrossStatements(t *testing.T) {
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
	// The first statement consumed 25 s of the 30 s (three 5 s attempts, two
	// 5 s sleeps); the second gets 5 s and must fail on its first lock timeout.
	err := RetryOnLockTimeout(context.Background(), slog.Default(), "test/b.sql", policy, sleeps.sleep, sleeps.now, failTwice())
	if err == nil || !strings.Contains(err.Error(), "test/b.sql") {
		t.Fatalf("second statement err = %v, want the shared deadline to end it", err)
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
		Budget: time.Minute, Deadline: clock.Add(time.Minute), InitialBackoff: time.Second, MaxBackoff: time.Second,
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
