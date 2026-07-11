// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestGraphNodeOwnerStoreIntegration exercises the owner ledger against a live
// Postgres: single-writer ownership, cross-batch max resolution, and the
// per-uid advisory-lock concurrency invariant that many concurrent batches
// racing one uid converge the ledger to the max order key with no lost update.
//
// Skipped by default; set ESHU_GRAPH_NODE_OWNER_LIVE=1 and ESHU_POSTGRES_DSN.
func TestGraphNodeOwnerStoreIntegration(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_GRAPH_NODE_OWNER_LIVE")) == "" {
		t.Skip("set ESHU_GRAPH_NODE_OWNER_LIVE=1 and ESHU_POSTGRES_DSN to run the owner-ledger integration proof")
	}
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("ESHU_POSTGRES_DSN not set")
	}
	ctx := context.Background()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer func() { _ = db.Close() }()

	store := NewGraphNodeOwnerStore()
	if err := store.EnsureSchema(ctx, db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	t.Run("single_writer_owns", func(t *testing.T) {
		uid := ownerTestUID(t, "single")
		owned := resolveInTx(t, db, store, []GraphNodeOwnerEntry{ownerEntry(uid, "1000-a", "a")})
		if _, ok := owned[uid]; !ok {
			t.Fatalf("sole writer must own uid, owned=%v", owned)
		}
		assertOwnerLedger(t, db, uid, "1000-a", "a")
	})

	t.Run("cross_batch_max_resolution", func(t *testing.T) {
		uid := ownerTestUID(t, "crossbatch")
		// low first
		lowOwned := resolveInTx(t, db, store, []GraphNodeOwnerEntry{ownerEntry(uid, "1000-low", "low")})
		if _, ok := lowOwned[uid]; !ok {
			t.Fatal("low writer should own on first insert")
		}
		// high next — wins
		highOwned := resolveInTx(t, db, store, []GraphNodeOwnerEntry{ownerEntry(uid, "2000-high", "high")})
		if _, ok := highOwned[uid]; !ok {
			t.Fatal("high writer should own (greater order key)")
		}
		assertOwnerLedger(t, db, uid, "2000-high", "high")
		// low again — now loses (not owned), ledger unchanged
		lowAgain, lost := resolveInTxWithLost(t, db, store, []GraphNodeOwnerEntry{ownerEntry(uid, "1000-low", "low")})
		if _, ok := lowAgain[uid]; ok {
			t.Fatal("low writer must NOT own after high won")
		}
		if lost != 1 {
			t.Fatalf("contendedLost = %d, want 1", lost)
		}
		assertOwnerLedger(t, db, uid, "2000-high", "high")
	})

	t.Run("concurrent_converges_to_max", func(t *testing.T) {
		const trials = 50
		for trial := 0; trial < trials; trial++ {
			uid := ownerTestUID(t, fmt.Sprintf("conc-%d", trial))
			keys := []string{"1000-a", "2000-b", "3000-c", "4000-d"}
			var wg sync.WaitGroup
			errs := make([]error, len(keys))
			wg.Add(len(keys))
			for i, k := range keys {
				i, k := i, k
				go func() {
					defer wg.Done()
					_, errs[i] = resolveInTxErr(ctx, db, store, []GraphNodeOwnerEntry{ownerEntry(uid, k, k)})
				}()
			}
			wg.Wait()
			for i, e := range errs {
				if e != nil {
					t.Fatalf("trial %d writer %d: %v", trial, i, e)
				}
			}
			assertOwnerLedger(t, db, uid, "4000-d", "4000-d")
		}
	})
}

func ownerEntry(uid, orderKey, value string) GraphNodeOwnerEntry {
	raw, _ := json.Marshal(map[string]any{"uid": uid, "value": value})
	return GraphNodeOwnerEntry{UID: uid, SourceOrderKey: orderKey, WinningRow: raw}
}

func ownerTestUID(t *testing.T, tag string) string {
	t.Helper()
	uid := fmt.Sprintf("owner-int-%s-%d", tag, time.Now().UnixNano())
	t.Cleanup(func() {})
	return uid
}

func resolveInTx(t *testing.T, db *sql.DB, store GraphNodeOwnerStore, entries []GraphNodeOwnerEntry) map[string]struct{} {
	t.Helper()
	owned, _ := resolveInTxWithLost(t, db, store, entries)
	return owned
}

func resolveInTxWithLost(t *testing.T, db *sql.DB, store GraphNodeOwnerStore, entries []GraphNodeOwnerEntry) (map[string]struct{}, int) {
	t.Helper()
	owned, lost, err := resolveInTxErrLost(context.Background(), db, store, entries)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	return owned, lost
}

func resolveInTxErr(ctx context.Context, db *sql.DB, store GraphNodeOwnerStore, entries []GraphNodeOwnerEntry) (map[string]struct{}, error) {
	owned, _, err := resolveInTxErrLost(ctx, db, store, entries)
	return owned, err
}

// resolveInTxErrLost mirrors the production decorator's transaction shape: open
// a tx, resolve ownership (which holds the advisory locks), then commit (which
// releases them). In production the graph write happens between resolve and
// commit; here there is no graph write, but the lock lifetime is identical.
func resolveInTxErrLost(ctx context.Context, db *sql.DB, store GraphNodeOwnerStore, entries []GraphNodeOwnerEntry) (map[string]struct{}, int, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, 0, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	owned, lost, err := store.ResolveOwnedUIDs(ctx, sqlTxExecQueryer{tx}, entries, time.Now().UTC())
	if err != nil {
		return nil, 0, err
	}
	if err := tx.Commit(); err != nil {
		return nil, 0, err
	}
	committed = true
	return owned, lost, nil
}

func assertOwnerLedger(t *testing.T, db *sql.DB, uid, wantKey, wantValue string) {
	t.Helper()
	var key string
	var rawRow []byte
	if err := db.QueryRowContext(context.Background(),
		"SELECT source_order_key, winning_row FROM graph_node_owner WHERE uid = $1", uid).Scan(&key, &rawRow); err != nil {
		t.Fatalf("read ledger for %q: %v", uid, err)
	}
	if key != wantKey {
		t.Fatalf("ledger order key = %q, want %q", key, wantKey)
	}
	var row map[string]any
	if err := json.Unmarshal(rawRow, &row); err != nil {
		t.Fatalf("winning_row not JSON: %v", err)
	}
	if row["value"] != wantValue {
		t.Fatalf("winning_row value = %v, want %q", row["value"], wantValue)
	}
}

// sqlTxExecQueryer adapts *sql.Tx to the postgres.ExecQueryer surface for the
// integration test (the production decorator uses the package's own tx type).
type sqlTxExecQueryer struct{ tx *sql.Tx }

func (a sqlTxExecQueryer) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return a.tx.ExecContext(ctx, query, args...)
}

func (a sqlTxExecQueryer) QueryContext(ctx context.Context, query string, args ...any) (Rows, error) {
	return a.tx.QueryContext(ctx, query, args...)
}
