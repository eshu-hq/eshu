// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reset

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/recovery"
)

func TestFenceAdmissionBudgetExpiryAndAcceptanceRace(t *testing.T) {
	for range 100 {
		parent := context.Background()
		budget := NewFenceAdmissionBudget(parent, 2*time.Millisecond)
		budget.start()
		time.Sleep(time.Millisecond)

		start := make(chan struct{})
		var wg sync.WaitGroup
		var admitErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			admitErr = budget.admit()
		}()
		go func() {
			defer wg.Done()
			<-start
			budget.expire()
		}()
		close(start)
		wg.Wait()

		if admitErr == nil {
			if budget.Expired() || budget.Context().Err() != nil {
				t.Fatal("accepted fence admission was canceled by the racing timer")
			}
		} else if !budget.Expired() || budget.Context().Err() == nil {
			t.Fatalf("expired admission error = %v, but the budget context remains active", admitErr)
		}
		budget.Close()
	}
}

func TestFenceAdmissionBudgetStopsAfterAcceptance(t *testing.T) {
	budget := NewFenceAdmissionBudget(context.Background(), 5*time.Millisecond)
	budget.start()
	if err := budget.admit(); err != nil {
		t.Fatalf("admit fence: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	if budget.Expired() || budget.Context().Err() != nil {
		t.Fatal("the admission deadline canceled an already admitted transaction")
	}
	budget.Close()
}

func TestFenceAdmissionBudgetExpiresBeforeAcceptance(t *testing.T) {
	budget := NewFenceAdmissionBudget(context.Background(), 5*time.Millisecond)
	budget.start()
	time.Sleep(10 * time.Millisecond)
	if err := budget.admit(); err == nil {
		t.Fatal("admit fence after deadline = nil, want rejection")
	}
	if !budget.Expired() || budget.Context().Err() == nil {
		t.Fatal("expired admission did not cancel its transaction context")
	}
	budget.Close()
}

func TestRefinalizeClaimFenceWithRetryRetriesOnlyFence55P03(t *testing.T) {
	attempts := 0
	want := recovery.RefinalizeResult{Enqueued: 1}
	got, err := RefinalizeClaimFenceWithRetry(
		context.Background(), time.Second, nil,
		func(_ context.Context, budget *FenceAdmissionBudget) (recovery.RefinalizeResult, error) {
			attempts++
			budget.start()
			if attempts == 1 {
				return recovery.RefinalizeResult{}, &ClaimFenceLockTimeoutError{
					err: &pgconn.PgError{Code: "55P03"},
				}
			}
			if err := budget.admit(); err != nil {
				return recovery.RefinalizeResult{}, err
			}
			return want, nil
		},
	)
	if err != nil {
		t.Fatalf("RefinalizeClaimFenceWithRetry() error = %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if got.Enqueued != want.Enqueued {
		t.Fatalf("result = %+v, want %+v", got, want)
	}
}

func TestRefinalizeClaimFenceWithRetryPreservesOtherErrors(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
	}{
		{name: "untyped 55P03", err: &pgconn.PgError{Code: "55P03"}},
		{name: "typed different state", err: &ClaimFenceLockTimeoutError{err: &pgconn.PgError{Code: "40001"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			attempts := 0
			_, err := RefinalizeClaimFenceWithRetry(
				context.Background(), time.Second, nil,
				func(_ context.Context, budget *FenceAdmissionBudget) (recovery.RefinalizeResult, error) {
					attempts++
					budget.start()
					return recovery.RefinalizeResult{}, test.err
				},
			)
			if !errors.Is(err, test.err) {
				t.Fatalf("error = %v, want %v", err, test.err)
			}
			if attempts != 1 {
				t.Fatalf("attempts = %d, want 1", attempts)
			}
		})
	}
}

func TestRefinalizeClaimFenceWithRetryReturnsCallerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lockErr := &ClaimFenceLockTimeoutError{err: &pgconn.PgError{Code: "55P03"}}
	_, err := RefinalizeClaimFenceWithRetry(
		ctx, time.Second, nil,
		func(_ context.Context, budget *FenceAdmissionBudget) (recovery.RefinalizeResult, error) {
			budget.start()
			cancel()
			return recovery.RefinalizeResult{}, lockErr
		},
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want caller cancellation", err)
	}
}

func TestRefinalizeClaimFenceWithRetryExhaustsAdmissionBudget(t *testing.T) {
	attempts := 0
	_, err := RefinalizeClaimFenceWithRetry(
		context.Background(), time.Nanosecond, nil,
		func(_ context.Context, budget *FenceAdmissionBudget) (recovery.RefinalizeResult, error) {
			attempts++
			budget.start()
			time.Sleep(time.Millisecond)
			return recovery.RefinalizeResult{}, &ClaimFenceLockTimeoutError{
				err: &pgconn.PgError{Code: "55P03"},
			}
		},
	)
	if !errors.Is(err, recovery.ErrRefinalizeClaimFenceBusy) {
		t.Fatalf("error = %v, want busy sentinel", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestRefinalizeClaimFenceWithRetryLogsLastSQLStateForAdmissionExpiry(t *testing.T) {
	for _, test := range []struct {
		name       string
		attemptErr error
		wantState  string
	}{
		{name: "non postgres error", attemptErr: errors.New("retry begin failed"), wantState: "none"},
		{name: "fence timeout", attemptErr: &ClaimFenceLockTimeoutError{err: &pgconn.PgError{Code: "55P03"}}, wantState: "55P03"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var logs bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logs, nil))
			_, err := RefinalizeClaimFenceWithRetry(
				context.Background(), time.Nanosecond, logger,
				func(_ context.Context, budget *FenceAdmissionBudget) (recovery.RefinalizeResult, error) {
					budget.start()
					time.Sleep(time.Millisecond)
					return recovery.RefinalizeResult{}, test.attemptErr
				},
			)
			if !errors.Is(err, recovery.ErrRefinalizeClaimFenceBusy) {
				t.Fatalf("error = %v, want busy sentinel", err)
			}
			if !strings.Contains(logs.String(), "last_sqlstate="+test.wantState) {
				t.Fatalf("logs = %q, want last_sqlstate=%s", logs.String(), test.wantState)
			}
		})
	}
}
