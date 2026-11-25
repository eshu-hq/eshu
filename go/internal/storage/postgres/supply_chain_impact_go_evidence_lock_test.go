// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// TestClassifyGoEvidenceLockTimeoutMapsOnlyTheSharedLockStatement pins ruling 2.3: a
// 55P03 on the pass's shared go-evidence lock statement becomes the typed refusal
// with the lock_timeout cause; the same error on any other statement, and any other
// error on that statement, passes through unchanged.
func TestClassifyGoEvidenceLockTimeoutMapsOnlyTheSharedLockStatement(t *testing.T) {
	t.Parallel()

	timeout := &pgconn.PgError{Code: "55P03", Message: "canceling statement due to lock timeout"}
	other := &pgconn.PgError{Code: "40001", Message: "serialization failure"}
	lock := supplychaincore.SupplyChainImpactGoEvidenceSharedLockQuery

	var changed *supplychaincore.SupplyChainImpactGoEvidenceChangedError
	got := classifyGoEvidenceLockTimeout(lock, timeout)
	if !errors.As(got, &changed) || changed.Cause != supplychaincore.GoEvidenceCauseLockTimeout || !errors.Is(got, timeout) {
		t.Fatalf("lock-statement 55P03 = %v, want the typed refusal with cause lock_timeout wrapping the database error", got)
	}
	if again := classifyGoEvidenceLockTimeout(lock, got); again != got {
		t.Fatalf("already classified error was rewrapped: %v", again)
	}
	if got := classifyGoEvidenceLockTimeout("SELECT 1", timeout); got != error(timeout) {
		t.Fatalf("55P03 on another statement = %v, want it unchanged", got)
	}
	if got := classifyGoEvidenceLockTimeout(lock, other); got != error(other) {
		t.Fatalf("other error on the lock statement = %v, want it unchanged", got)
	}
	denied := &pgconn.PgError{Code: "42501", Message: "permission denied for table ingestion_scopes"}
	if got := classifyGoEvidenceLockTimeout(lock, denied); got != error(denied) {
		t.Fatalf("permission error on the lock statement = %v, want it unchanged and counting", got)
	}
	if got := classifyGoEvidenceLockTimeout(lock, nil); got != nil {
		t.Fatalf("nil error = %v, want nil", got)
	}
}
