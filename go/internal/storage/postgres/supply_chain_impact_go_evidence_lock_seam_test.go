// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

type seamRows struct{}

func (seamRows) Next() bool        { return false }
func (seamRows) Scan(...any) error { return nil }
func (seamRows) Err() error        { return nil }
func (seamRows) Close() error      { return nil }

type seamTx struct{}

func (seamTx) ExecContext(context.Context, string, ...any) (sql.Result, error) { return nil, nil }
func (seamTx) QueryContext(context.Context, string, ...any) (supplychaincore.SupplyChainImpactRows, error) {
	return seamRows{}, nil
}
func (seamTx) Commit() error   { return nil }
func (seamTx) Rollback() error { return nil }

// TestRecheckHookTxToleratesUnsetSeams pins the live-test wrapper itself: a wrapper built
// with only onEnd or only beforeLock (the footprint and the 3 s proofs) must not panic
// when the recheck or the lock statement passes through it, or one live proof would
// kill the whole live test binary.
func TestRecheckHookTxToleratesUnsetSeams(t *testing.T) {
	t.Parallel()

	tx := &recheckHookTx{tx: seamTx{}, onEnd: func([]string, bool) {}}
	if _, err := tx.ExecContext(context.Background(), supplychaincore.SupplyChainImpactGoEvidenceSharedLockQuery); err != nil {
		t.Fatalf("lock statement: %v", err)
	}
	if _, err := tx.QueryContext(context.Background(), "SELECT 1 FROM unnest($1::text[], $2::text[]) AS pair(scope_id, generation_id)"); err != nil {
		t.Fatalf("recheck statement: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
}
