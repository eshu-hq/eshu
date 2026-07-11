// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphowner

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestGateWriteChunkedAt20000RowsSucceedsAgainstLivePostgres is the #5007
// P2-1 live before/after proof: driving 20000 distinct-uid rows through the
// real Gate (a real *sql.DB against Postgres, a no-op graph writer — no
// NornicDB involved) must succeed now that Gate.write chunks the critical
// section at lockChunkSize. Before chunking, one transaction resolving all
// 20000 uids' advisory locks reproducibly failed with Postgres "ERROR: out of
// shared memory" on a default max_locks_per_transaction=64 instance (recorded
// in docs/internal/design/5007-cross-scope-node-ownership.md and the P2-1
// theory-proof shim); this test proves the chunked implementation clears that
// same cardinality cleanly, and that the owner ledger converges correctly
// across every chunk boundary.
//
// Skipped by default; set ESHU_GRAPH_NODE_OWNER_LIVE=1 and ESHU_POSTGRES_DSN,
// matching postgres.TestGraphNodeOwnerStoreIntegration's gating.
func TestGateWriteChunkedAt20000RowsSucceedsAgainstLivePostgres(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_GRAPH_NODE_OWNER_LIVE")) == "" {
		t.Skip("set ESHU_GRAPH_NODE_OWNER_LIVE=1 and ESHU_POSTGRES_DSN to run the P2-1 chunked live proof")
	}
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("ESHU_POSTGRES_DSN not set")
	}
	ctx := context.Background()

	rawDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer func() { _ = rawDB.Close() }()
	sqldb := postgres.SQLDB{DB: rawDB}
	if err := postgres.NewGraphNodeOwnerStore().EnsureSchema(ctx, sqldb); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	gate := NewGate(sqldb)
	var written int
	underlying := func(_ context.Context, rows []map[string]any, _ string) error {
		written += len(rows)
		return nil
	}
	writer := NewCloudResourceGatedWriter(gate, underlying)

	const rowCount = 20000
	prefix := fmt.Sprintf("gate-chunk-live-%d", time.Now().UnixNano())
	rows := make([]map[string]any, 0, rowCount)
	for i := 0; i < rowCount; i++ {
		uid := fmt.Sprintf("%s-%06d", prefix, i)
		rows = append(rows, map[string]any{
			"uid":              uid,
			"source_order_key": fmt.Sprintf("2026-01-01T00:00:00.%09dZ|fact-%06d", i, i),
			"value":            "solo",
		})
	}

	start := time.Now()
	if err := writer.WriteCloudResourceNodes(ctx, rows, "test/live-chunk-20k"); err != nil {
		t.Fatalf("WriteCloudResourceNodes(20000 rows) error = %v — chunked gate must clear the cardinality that failed unchunked with \"out of shared memory\"", err)
	}
	elapsed := time.Since(start)
	t.Logf("20000-row chunked owner-ledger resolve+write: %s (%.0f rows/sec)", elapsed, float64(rowCount)/elapsed.Seconds())

	if written != rowCount {
		t.Fatalf("underlying wrote %d rows, want %d — no row may be dropped across chunk boundaries", written, rowCount)
	}

	// Spot-check convergence at the chunk boundaries and interior: uid 0
	// (first row of chunk 0), uid 499/500 (last/first row straddling the
	// chunk-0/chunk-1 boundary at lockChunkSize=500), and the last uid
	// (final chunk). A single, non-contended writer must own every uid it
	// wrote with its own order key.
	for _, i := range []int{0, lockChunkSize - 1, lockChunkSize, rowCount - 1} {
		uid := fmt.Sprintf("%s-%06d", prefix, i)
		wantKey := fmt.Sprintf("2026-01-01T00:00:00.%09dZ|fact-%06d", i, i)
		var gotKey string
		if err := rawDB.QueryRowContext(ctx,
			"SELECT source_order_key FROM graph_node_owner WHERE uid = $1", uid).Scan(&gotKey); err != nil {
			t.Fatalf("read ledger for %q: %v", uid, err)
		}
		if gotKey != wantKey {
			t.Fatalf("uid %q ledger order key = %q, want %q — chunk boundary did not converge", uid, gotKey, wantKey)
		}
	}
}
