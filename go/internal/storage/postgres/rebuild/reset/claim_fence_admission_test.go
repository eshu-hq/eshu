// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reset

import (
	"context"
	"sync"
	"testing"
	"time"
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
