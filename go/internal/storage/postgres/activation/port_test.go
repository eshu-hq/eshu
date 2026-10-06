// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package activation

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance"
)

// TestFinalizeErrorMapsTheConsumerSentinels (#7584 ruling P2-F): SQLSTATE
// 55P03 from Finalize's lock_timeout maps to
// maintenance.ErrActivationFinalizeLockTimeout with the PostgreSQL error kept
// in the chain; a lease lost inside the transaction maps to
// maintenance.ErrActivationLeaseLost; every other error is returned as is.
func TestFinalizeErrorMapsTheConsumerSentinels(t *testing.T) {
	t.Parallel()
	lockTimeout := fmt.Errorf("finalize activation obligation: lock scope: %w",
		&pgconn.PgError{Code: "55P03", Message: "canceling statement due to lock timeout"})
	got := finalizeError(lockTimeout)
	if !errors.Is(got, maintenance.ErrActivationFinalizeLockTimeout) {
		t.Fatalf("55P03 maps to %v, want ErrActivationFinalizeLockTimeout", got)
	}
	var pgErr *pgconn.PgError
	if !errors.As(got, &pgErr) || pgErr.Code != "55P03" {
		t.Fatalf("mapped lock timeout lost its PostgreSQL error: %v", got)
	}

	serialization := fmt.Errorf("finalize activation obligation: wake: %w", &pgconn.PgError{Code: "40001"})
	if got := finalizeError(serialization); got != serialization {
		t.Fatalf("another SQLSTATE maps to %v, want the error unchanged", got)
	}
	if got := finalizeError(fmt.Errorf("finalize activation obligation: completion: %w", ErrLeaseLost)); !errors.Is(got, maintenance.ErrActivationLeaseLost) ||
		errors.Is(got, maintenance.ErrActivationFinalizeLockTimeout) {
		t.Fatalf("a lost lease maps to %v, want ErrActivationLeaseLost only", got)
	}
	if got := finalizeError(nil); got != nil {
		t.Fatalf("nil maps to %v", got)
	}
}
