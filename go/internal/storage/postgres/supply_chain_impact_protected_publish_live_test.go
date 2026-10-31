// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// Readiness enrollment suspended under drift-gate option A (PR #9544 fourth
// round): the pins proof below stays in the Go suite and skips clean without
// proof DSNs; the static-publish proof moved byte-identical to
// supply_chain_impact_protected_static_publish_live_test.go, which keeps the
// enrollment marker. Re-enroll post-refit. See live-tests-07b.yaml.

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// TestProtectedPolicyPinsAbsentStateReportsCleanErrorLive is the operator
// message regression for the empty-policy path: with the schema
// bootstrapped but no role policy row, protectedPolicyPins must report
// exactly "protected policy state is absent" instead of wrapping a nil
// rows.Err() operand (which renders as %!w(<nil>) and misleads the 3 AM
// reader about what failed). Fail-closed behavior is unchanged: a non-nil
// error is still returned either way.
func TestProtectedPolicyPinsAbsentStateReportsCleanErrorLive(t *testing.T) {
	adminDSN := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	optIn := os.Getenv("ESHU_POSTGRES_DISPOSABLE")
	if adminDSN == "" {
		t.Skip("set ESHU_POSTGRES_DSN and ESHU_POSTGRES_DISPOSABLE=1 to run the pins proof")
	}
	ctx, admin := postgresproof.OpenDisposableBootstrapDatabase(t, adminDSN, optIn, 5*time.Minute,
		func(ctx context.Context, database *sql.DB) error {
			return ApplyBootstrap(ctx, SQLDB{DB: database})
		})
	tx, err := admin.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin pins proof transaction: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	pins := &supplyChainImpactTx{tx: pinsProofTx{tx: tx}}
	if _, _, err := pins.protectedPolicyPins(ctx); err == nil {
		t.Fatal("protectedPolicyPins() error = nil on absent policy state, want a clean absent-state error")
	} else if got, want := err.Error(), "protected policy state is absent"; got != want {
		t.Fatalf("protectedPolicyPins() error = %q, want exactly %q", got, want)
	}
}

// pinsProofTx adapts *sql.Tx to the narrow db.Transaction surface the
// winners store pins read through. *sql.Rows already satisfies db.Rows,
// so only the query/exec/commit surface needs adapting.
type pinsProofTx struct{ tx *sql.Tx }

func (t pinsProofTx) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	return t.tx.QueryContext(ctx, query, args...)
}

func (t pinsProofTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return t.tx.ExecContext(ctx, query, args...)
}

func (t pinsProofTx) Commit() error { return t.tx.Commit() }

func (t pinsProofTx) Rollback() error { return t.tx.Rollback() }
