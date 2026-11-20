// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// TestReducerQueue10863BatchClaimReplay drains the #10862 QA waiting mix
// (8,713 rows) through the shipped ClaimBatch/Ack path and logs batch
// claim p50/p95 plus one rolled-back EXPLAIN (ANALYZE, BUFFERS) of the
// batch claim over the same seed. It is the before/after surface for
// #10863 acceptance 1/2/6 on the built code: run it on both builds and
// pair drain percentiles only with drain percentiles (they fall as depth
// falls). The recorded same-method pair lives in
// (#10863). Operator-gated on
// ESHU_REDUCER_CLAIM_BENCH_DSN like the other claim benches; it never
// touches QA.
func TestReducerQueue10863BatchClaimReplay(t *testing.T) {
	dsn := reducerClaimBenchmarkDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_CLAIM_BENCH_DSN or ESHU_POSTGRES_DSN to run the 10863 batch replay")
	}
	ctx := context.Background()
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer func() { _ = database.Close() }()
	sqlConn, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("open dedicated postgres connection: %v", err)
	}
	defer func() { _ = sqlConn.Close() }()
	conn := reducerClaimBenchmarkConn{conn: sqlConn}

	schemaName := fmt.Sprintf("reducer_claim_10863_%d", time.Now().UnixNano())
	defer func() { _, _ = conn.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE") }()
	if err := createReducerClaimBenchmarkSchema(ctx, conn, schemaName); err != nil {
		t.Fatalf("create replay schema: %v", err)
	}
	if _, err := conn.ExecContext(ctx, MigrationSQL("fact_records")); err != nil {
		t.Fatalf("create replay fact_records: %v", err)
	}
	if _, err := conn.ExecContext(ctx, MigrationSQL("cross_scope_completion_queue")); err != nil {
		t.Fatalf("create completion events table: %v", err)
	}

	now := time.Now().UTC()
	want := seedReducerQueue10862Replay(t, ctx, conn, schemaName, now)
	if err := analyzeReducerClaimBenchmarkTables(ctx, conn); err != nil {
		t.Fatalf("analyze replay tables: %v", err)
	}
	if _, err := conn.ExecContext(ctx, "SET search_path TO "+schemaName); err != nil {
		t.Fatalf("set replay search_path: %v", err)
	}

	// Plan + buffers for one batch claim over the untouched seed, rolled
	// back so the drain below sees the identical queue.
	explainArgs := []any{now, nil, "replay-10863-explain", now.Add(time.Minute), false, 0, 0, 16}
	var execSamples []float64
	var bufferSamples []int64
	for i := 0; i < 5; i++ {
		tx, err := sqlConn.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin explain tx: %v", err)
		}
		var raw []byte
		err = tx.QueryRowContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+claimReducerWorkBatchQuery, explainArgs...).Scan(&raw)
		_ = tx.Rollback()
		if err != nil {
			t.Fatalf("explain batch claim: %v", err)
		}
		var docs []struct {
			ExecutionTime float64 `json:"Execution Time"`
			Plan          struct {
				SharedHit     int64 `json:"Shared Hit Blocks"`
				SharedRead    int64 `json:"Shared Read Blocks"`
				SharedDirtied int64 `json:"Shared Dirtied Blocks"`
				SharedWritten int64 `json:"Shared Written Blocks"`
			} `json:"Plan"`
		}
		if err := json.Unmarshal(raw, &docs); err != nil {
			t.Fatalf("parse explain: %v", err)
		}
		d := docs[0]
		execSamples = append(execSamples, d.ExecutionTime)
		bufferSamples = append(bufferSamples, d.Plan.SharedHit+d.Plan.SharedRead+d.Plan.SharedDirtied+d.Plan.SharedWritten)
	}
	sort.Float64s(execSamples)
	sort.Slice(bufferSamples, func(i, j int) bool { return bufferSamples[i] < bufferSamples[j] })

	queue := ReducerQueue{
		database:      conn,
		LeaseOwner:    "replay-10863",
		LeaseDuration: time.Minute,
	}
	var latencies []float64
	drained := 0
	start := time.Now()
	for range want + 100 {
		claimStart := time.Now()
		intents, err := queue.ClaimBatch(ctx, 16)
		latencies = append(latencies, time.Since(claimStart).Seconds()*1000)
		if err != nil {
			t.Fatalf("batch claim: %v", err)
		}
		if len(intents) == 0 {
			break
		}
		for _, intent := range intents {
			if err := queue.Ack(ctx, intent, reducercontract.Result{}); err != nil {
				t.Fatalf("ack %s: %v", intent.IntentID, err)
			}
			drained++
		}
	}
	wall := time.Since(start).Seconds()
	if drained != want {
		t.Fatalf("batch replay drained = %d, want %d", drained, want)
	}
	t.Logf("10863 batch replay: depth=%d drained=%d wall=%.1fs claimbatch_p50_ms=%.3f claimbatch_p95_ms=%.3f explain_exec_ms_median=%.3f explain_buffers_median=%d",
		want, drained, wall,
		percentileSorted(latencies, 0.50), percentileSorted(latencies, 0.95),
		execSamples[len(execSamples)/2], bufferSamples[len(bufferSamples)/2])
}
