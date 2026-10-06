// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeDynamicTransaction struct {
	deadline time.Time
	rolled   bool
	err      error
}

func (tx *fakeDynamicTransaction) Rollback(ctx context.Context) error {
	tx.rolled = true
	tx.deadline, _ = ctx.Deadline()
	return tx.err
}

func TestRollbackDynamicTransactionsJoinsFailures(t *testing.T) {
	firstErr := errors.New("first rollback failed")
	secondErr := errors.New("second rollback failed")
	first := &fakeDynamicTransaction{err: firstErr}
	second := &fakeDynamicTransaction{err: secondErr}
	got := rollbackDynamicTransactions([]*fakeDynamicTransaction{first, second})
	if !errors.Is(got, firstErr) || !errors.Is(got, secondErr) {
		t.Fatalf("rollback failures lost: %v", got)
	}
	for _, tx := range []*fakeDynamicTransaction{first, second} {
		if !tx.rolled {
			t.Fatal("rollback skipped")
		}
		remaining := time.Until(tx.deadline)
		if remaining < 9*time.Second || remaining > 10*time.Second {
			t.Fatalf("rollback deadline remaining = %s", remaining)
		}
	}
}
