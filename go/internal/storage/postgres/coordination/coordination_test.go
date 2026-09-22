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

type recordedSleeps struct{ waits []time.Duration }

func (r *recordedSleeps) sleep(_ context.Context, d time.Duration) error {
	r.waits = append(r.waits, d)
	return nil
}

// TestRetryOnLockTimeoutRetriesUntilTheLockClears pins #6956 cause 2: a
// migration statement that hits lock_timeout is retried with doubling
// backoff, and the recovery is logged with the attempt count.
func TestRetryOnLockTimeoutRetriesUntilTheLockClears(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	sleeps := &recordedSleeps{}
	attempts := 0
	err := RetryOnLockTimeout(context.Background(), logger, "test/002_index.sql", LockRetryPolicy{
		Budget:         time.Minute,
		InitialBackoff: time.Second,
		MaxBackoff:     4 * time.Second,
	}, sleeps.sleep, func() error {
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
	sleeps := &recordedSleeps{}
	boom := errors.New("syntax error")
	attempts := 0
	err := RetryOnLockTimeout(context.Background(), slog.Default(), "test/003.sql", LockRetryPolicy{
		Budget: time.Minute, InitialBackoff: time.Second, MaxBackoff: time.Second,
	}, sleeps.sleep, func() error {
		attempts++
		return boom
	})
	if !errors.Is(err, boom) || attempts != 1 || len(sleeps.waits) != 0 {
		t.Fatalf("err=%v attempts=%d sleeps=%v; want the original error after one attempt and no sleep", err, attempts, sleeps.waits)
	}
}

// TestRetryOnLockTimeoutGivesUpWhenTheBudgetIsSpent pins the bound: once
// the next backoff would exceed the budget the migrator fails, naming the
// attempts and the budget, and the error still classifies as 55P03.
func TestRetryOnLockTimeoutGivesUpWhenTheBudgetIsSpent(t *testing.T) {
	t.Parallel()
	sleeps := &recordedSleeps{}
	attempts := 0
	err := RetryOnLockTimeout(context.Background(), slog.Default(), "test/004.sql", LockRetryPolicy{
		Budget: 3 * time.Second, InitialBackoff: time.Second, MaxBackoff: 8 * time.Second,
	}, sleeps.sleep, func() error {
		attempts++
		return lockNotAvailable()
	})
	if err == nil {
		t.Fatal("RetryOnLockTimeout() = nil, want budget exhaustion")
	}
	if !IsLockNotAvailable(err) {
		t.Fatalf("error lost its 55P03 classification: %v", err)
	}
	if !strings.Contains(err.Error(), "lock retry budget 3s") || !strings.Contains(err.Error(), "test/004.sql") {
		t.Fatalf("error = %q, want it to name the budget and the migration", err)
	}
	// backoffs 1s, 2s fit inside 3s of budget (sleeps 1s then 2s = 3s); the
	// next 4s would exceed it, so exactly three attempts run.
	if attempts != 3 || len(sleeps.waits) != 2 {
		t.Fatalf("attempts=%d sleeps=%v, want 3 attempts and 2 sleeps", attempts, sleeps.waits)
	}
}

// TestRetryOnLockTimeoutStopsWhenTheContextEnds pins cancellation: a
// canceled context during the backoff returns the context error and does
// not run the statement again.
func TestRetryOnLockTimeoutStopsWhenTheContextEnds(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0
	err := RetryOnLockTimeout(ctx, slog.Default(), "test/005.sql", LockRetryPolicy{
		Budget: time.Minute, InitialBackoff: time.Second, MaxBackoff: time.Second,
	}, func(ctx context.Context, _ time.Duration) error {
		cancel()
		return ctx.Err()
	}, func() error {
		attempts++
		return lockNotAvailable()
	})
	if !errors.Is(err, context.Canceled) || attempts != 1 {
		t.Fatalf("err=%v attempts=%d, want context.Canceled after one attempt", err, attempts)
	}
}

type fakeAdvisoryLocker struct {
	answers []bool
	calls   int
	holder  string
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

func (f *fakeAdvisoryLocker) DescribeHolder(context.Context) (string, error) { return f.holder, nil }

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
