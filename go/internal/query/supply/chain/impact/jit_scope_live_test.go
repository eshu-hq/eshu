// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10880 PR-B2a disposable PostgreSQL 18 proof of the transaction-local jit switch of the supplement and image routes: inside the scope current_setting('jit') is off, and after commit, after rollback, and after a statement that errors midway the pinned pooled connection reads the server default, through the plain connection and through the guarded read port (BeginReadOnlySnapshot) the production adapter hands the stores; a precise list read through the guarded port opens the scope and leaves the default behind; reads ESHU_GROUPED_INVENTORY_PROOF_DSN with its disposable opt-in
//eshu:readiness:env ESHU_GROUPED_INVENTORY_PROOF_DISPOSABLE ESHU_GROUPED_INVENTORY_PROOF_DSN

import (
	"context"
	"database/sql"
	"testing"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// jitSetting reads the jit setting the connection behind queryer sees.
func jitSetting(ctx context.Context, t *testing.T, queryer db.RowQueryer) string {
	t.Helper()
	var setting string
	if err := queryer.QueryRowContext(ctx, `SELECT current_setting('jit')`).Scan(&setting); err != nil {
		t.Fatalf("read the jit setting: %v", err)
	}
	return setting
}

// sqlRowQueryer adapts a *sql.DB to the row queryer the scope helper returns.
type sqlRowQueryer struct{ database *sql.DB }

func (q sqlRowQueryer) QueryRowContext(ctx context.Context, query string, args ...any) db.Row {
	return q.database.QueryRowContext(ctx, query, args...)
}

// TestJITOffScopeIsTransactionLocalLive is ruling decisions 11 and the JIT proof
// of the index ruling: inside the scope the read sees jit = off; after the scope
// ends by commit, by rollback, or after a statement that errors midway, the one
// pooled connection (pinned so the check lands on it) reads the server default.
// It runs through the plain connection and through the guarded read port.
func TestJITOffScopeIsTransactionLocalLive(t *testing.T) {
	ctx, database := openRouteVDatabase(t)
	database.SetMaxOpenConns(1)
	pool := sqlRowQueryer{database: database}
	def := jitSetting(ctx, t, pool)
	if def != "on" {
		t.Skipf("server jit = %q: the proof needs the default (on) to show the switch", def)
	}
	guarded := storagepostgres.NewSQLReadStore(database)

	for name, open := range map[string]func() (db.Queryer, db.RowQueryer, func(), error){
		"plain connection": func() (db.Queryer, db.RowQueryer, func(), error) {
			return withJITOff(ctx, nil, database, nil, nil)
		},
		"guarded read port": func() (db.Queryer, db.RowQueryer, func(), error) {
			return withJITOff(ctx, guarded, nil, nil, nil)
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Run("commit", func(t *testing.T) {
				queryer, rows, end, err := open()
				if err != nil {
					t.Fatalf("open the scope: %v", err)
				}
				if got := jitSetting(ctx, t, rows); got != "off" {
					t.Fatalf("jit inside the scope = %q, want off", got)
				}
				cursor, err := queryer.QueryContext(ctx, `SELECT 1`)
				if err != nil {
					t.Fatalf("a read inside the scope: %v", err)
				}
				_ = cursor.Close()
				end()
				if got := jitSetting(ctx, t, pool); got != def {
					t.Fatalf("jit after commit = %q, want the server default %q", got, def)
				}
			})
			t.Run("statement errors midway", func(t *testing.T) {
				queryer, _, end, err := open()
				if err != nil {
					t.Fatalf("open the scope: %v", err)
				}
				if cursor, err := queryer.QueryContext(ctx, `SELECT 1/0`); err == nil {
					for cursor.Next() {
					}
					if cursor.Err() == nil {
						t.Fatal("the statement did not error: the rollback path is not exercised")
					}
					_ = cursor.Close()
				}
				end() // commit of an aborted transaction fails and rolls back
				if got := jitSetting(ctx, t, pool); got != def {
					t.Fatalf("jit after an errored read = %q, want the server default %q", got, def)
				}
			})
		})
	}

	t.Run("rollback through the guarded port", func(t *testing.T) {
		tx, err := guarded.BeginReadOnlySnapshot(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		if err := setJITOff(ctx, tx); err != nil {
			_ = tx.Rollback()
			t.Fatalf("set jit off: %v", err)
		}
		if got := jitSetting(ctx, t, tx); got != "off" {
			_ = tx.Rollback()
			t.Fatalf("jit inside the transaction = %q, want off", got)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatalf("rollback: %v", err)
		}
		if got := jitSetting(ctx, t, pool); got != def {
			t.Fatalf("jit after rollback = %q, want the server default %q", got, def)
		}
	})

	t.Run("a precise list through the guarded port opens the scope and leaves the default", func(t *testing.T) {
		store := NewPostgresFindingStoreWithReadStore(guarded, true)
		rows, err := store.ListSupplyChainImpactFindings(ctx, FindingFilter{Limit: 50, Status: "affected_exact", DetectionProfile: ProfilePrecise})
		if err != nil || len(rows) == 0 {
			t.Fatalf("precise list through the guarded port = %d rows, %v", len(rows), err)
		}
		if got := jitSetting(ctx, t, pool); got != def {
			t.Fatalf("jit after the list = %q, want the server default %q", got, def)
		}
	})
}
