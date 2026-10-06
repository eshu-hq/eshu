// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/status/summary"
)

// TestStatusSummaryBloatLive proves the one-row table stays small under the
// writer's update rate: 2,000 upserts at the production row size, then VACUUM,
// leave the heap within eight pages with at most 100 dead tuples and at least
// 90 percent of the updates HOT. It also reads the row back under
// EXPLAIN (ANALYZE, BUFFERS, SERIALIZE) so the TOAST buffers of the read are on
// record, and asserts the read touches no more than a handful of buffers. This
// payload is repetitive, so PostgreSQL compresses it inline and never writes a
// TOAST value.
func TestStatusSummaryBloatLive(t *testing.T) {
	runBloatProof(t, func(i int) summary.Row {
		return proofRow(proofAsOf.Add(time.Duration(i)*time.Second), "bloat", bloatEntries)
	}, false, 3200)
}

// TestStatusSummaryBloatIncompressibleLive is the same proof with a payload that
// does not compress, so the rows value is stored out of line in the TOAST table
// and every upsert writes a new TOAST value. It closes the case the compressible
// payload cannot: bounded heap and TOAST size, HOT updates, and the TOAST
// buffers of the read.
func TestStatusSummaryBloatIncompressibleLive(t *testing.T) {
	random := rand.New(rand.NewSource(7009))
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	randomText := func(n int) string {
		out := make([]byte, n)
		for i := range out {
			out[i] = alphabet[random.Intn(len(alphabet))]
		}
		return string(out)
	}
	runBloatProof(t, func(i int) summary.Row {
		row := summary.Row{
			ModelKey:      summary.ModelActiveWorkSummary,
			SchemaVersion: summary.SchemaVersion,
			SourceSHA256:  randomText(64),
			AsOf:          proofAsOf.Add(time.Duration(i) * time.Second),
			PassDuration:  412 * time.Millisecond,
			RowCount:      bloatEntries,
		}
		for j := 0; j < bloatEntries; j++ {
			row.Entries = append(row.Entries, summary.Entry{
				Section: fmt.Sprintf("section_%d", j%5),
				Ordinal: int64(j / 5),
				JSON:    fmt.Sprintf(`{"k":%q}`, randomText(60)),
			})
		}
		return row
	}, true, 4200)
}

// bloatEntries sizes the proof payload at about the 2.3-2.4 KB the shim
// measured for the active-work summary at 20-100 percent live.
const bloatEntries = 35

// runBloatProof upserts makeRow(0..1999), vacuums, and checks heap, dead tuple,
// HOT, and read-buffer bounds. wantToast says whether the payload is expected
// to be stored out of line, and the proof fails if the TOAST table's presence
// disagrees, so the compressible and incompressible cases cannot both silently
// measure the same storage path. maxPayload bounds the encoded payload size.
func runBloatProof(t *testing.T, makeRow func(i int) summary.Row, wantToast bool, maxPayload int) {
	t.Helper()
	ctx, database := openProofDatabase(t)
	applySummaryMigration(ctx, t, database)
	store := summary.NewStore(poolStore{database})

	// Two rounds of 2,000 upserts, each followed by VACUUM. The first round
	// shows the size after the writer's churn; the second shows that the size
	// is bounded by the write volume between vacuums, not by the number of
	// updates: VACUUM makes the dead versions' space reusable. The proof runs
	// VACUUM by hand because 4,000 upserts finish in about a second, long before
	// autovacuum's naptime; in production the TOAST relation (which does not
	// inherit the table's autovacuum reloptions) is vacuumed by autovacuum's
	// defaults on far less volume per cycle.
	const (
		upserts = 2000
		rounds  = 2
	)
	payloadBytes := 0
	var first, last bloatMeasure
	for round := 0; round < rounds; round++ {
		for i := round * upserts; i < (round+1)*upserts; i++ {
			row := makeRow(i)
			if i == 0 {
				encoded, err := summary.EncodeEntries(row.Entries)
				if err != nil {
					t.Fatalf("EncodeEntries(): %v", err)
				}
				payloadBytes = len(encoded)
				if payloadBytes < 2000 || payloadBytes > maxPayload {
					t.Fatalf("proof payload is %d bytes, want 2000-%d (the production row is about 2.4 KB)", payloadBytes, maxPayload)
				}
			}
			advanced, err := store.Upsert(ctx, row)
			if err != nil || !advanced {
				t.Fatalf("upsert %d = %v, %v, want true, nil", i, advanced, err)
			}
		}
		if _, err := database.ExecContext(ctx, `VACUUM status_summary_snapshots`); err != nil {
			t.Fatalf("VACUUM: %v", err)
		}
		last = measureBloat(ctx, t, database)
		if round == 0 {
			first = last
		}
		t.Logf("round %d: payload=%d bytes upserts=%d heap=%d pages toast=%d pages dead=%d ins=%d upd=%d hot_upd=%d hot_ratio=%.3f",
			round+1, payloadBytes, (round+1)*upserts, last.heapPages, last.toastPages, last.dead,
			last.inserted, last.updated, last.hot, last.hotRatio())
	}

	if last.heapPages > 8 {
		t.Errorf("heap = %d pages after %d upserts and VACUUM, want <= 8", last.heapPages, rounds*upserts)
	}
	if wantToast != (last.toastPages > 0) {
		t.Errorf("TOAST table = %d pages, but the proof expects out-of-line storage = %v", last.toastPages, wantToast)
	}
	// The second round reuses the first round's vacuumed space. The table may
	// still grow a little (VACUUM truncates only trailing empty pages, and dead
	// chunks accumulate until the next VACUUM), but a second full round of
	// writes must not add another round's worth of pages.
	if last.toastPages > first.toastPages+first.toastPages/4 {
		t.Errorf("TOAST table grew from %d to %d pages across a second round of upserts, want reuse of the vacuumed space",
			first.toastPages, last.toastPages)
	}
	if last.dead > 100 {
		t.Errorf("n_dead_tup = %d after VACUUM, want <= 100", last.dead)
	}
	if last.updated < int64(rounds*upserts-1) {
		t.Errorf("n_tup_upd = %d, want %d (every upsert after the first updates)", last.updated, rounds*upserts-1)
	}
	if last.hotRatio() < 0.9 {
		t.Errorf("HOT ratio = %.3f (%d of %d), want >= 0.9", last.hotRatio(), last.hot, last.updated)
	}

	hits, reads, toastHits, toastReads := explainReadBuffers(ctx, t, database)
	t.Logf("read buffers: shared hit=%d read=%d; serialization (TOAST detoast) hit=%d read=%d",
		hits, reads, toastHits, toastReads)
	if total := hits + reads; total > 8 {
		t.Errorf("model read touched %d shared buffers, want a handful (<= 8)", total)
	}
	if wantToast && toastHits+toastReads == 0 {
		t.Error("the TOASTed payload's read reported no serialization buffers; the TOAST read path was not measured")
	}
}

// bloatMeasure is the table's size and update statistics at one point.
type bloatMeasure struct {
	heapPages, toastPages        int64
	dead, inserted, updated, hot int64
}

func (m bloatMeasure) hotRatio() float64 {
	if m.updated == 0 {
		return 0
	}
	return float64(m.hot) / float64(m.updated)
}

func measureBloat(ctx context.Context, t *testing.T, database *sql.DB) bloatMeasure {
	t.Helper()
	if _, err := database.ExecContext(ctx, `SELECT pg_stat_force_next_flush()`); err != nil {
		t.Fatalf("pg_stat_force_next_flush(): %v", err)
	}
	var (
		m                    bloatMeasure
		heapBytes, toastSize int64
		blockSize            int64
	)
	if err := database.QueryRowContext(ctx, `
SELECT pg_relation_size('status_summary_snapshots'),
       coalesce(pg_relation_size(c.reltoastrelid), 0),
       current_setting('block_size')::bigint,
       s.n_dead_tup, s.n_tup_ins, s.n_tup_upd, s.n_tup_hot_upd
FROM pg_class c
JOIN pg_stat_user_tables s ON s.relid = c.oid
WHERE c.oid = 'status_summary_snapshots'::regclass`).Scan(
		&heapBytes, &toastSize, &blockSize, &m.dead, &m.inserted, &m.updated, &m.hot); err != nil {
		t.Fatalf("measure table: %v", err)
	}
	m.heapPages = heapBytes / blockSize
	m.toastPages = toastSize / blockSize
	return m
}

// explainReadBuffers runs the production read under EXPLAIN (ANALYZE, BUFFERS,
// SERIALIZE, FORMAT JSON) and returns the plan's shared buffers and the
// serialization step's shared buffers, which is where a TOASTed rows value is
// fetched.
func explainReadBuffers(ctx context.Context, t *testing.T, database *sql.DB) (hits, reads, toastHits, toastReads int64) {
	t.Helper()
	var document []byte
	if err := database.QueryRowContext(ctx,
		"EXPLAIN (ANALYZE, BUFFERS, SERIALIZE, FORMAT JSON) "+summary.ReadSQL,
		summary.ModelActiveWorkSummary).Scan(&document); err != nil {
		t.Fatalf("EXPLAIN of the model read: %v", err)
	}
	var plans []struct {
		Plan struct {
			NodeType string `json:"Node Type"`
			Relation string `json:"Relation Name"`
			Hit      int64  `json:"Shared Hit Blocks"`
			Read     int64  `json:"Shared Read Blocks"`
		} `json:"Plan"`
		Serialization struct {
			Hit  int64 `json:"Shared Hit Blocks"`
			Read int64 `json:"Shared Read Blocks"`
		} `json:"Serialization"`
	}
	if err := json.Unmarshal(document, &plans); err != nil || len(plans) != 1 {
		t.Fatalf("decode EXPLAIN output: %v (%d plans) in %s", err, len(plans), document)
	}
	t.Logf("model read plan root node: %s on %s", plans[0].Plan.NodeType, plans[0].Plan.Relation)
	// The plan node type is not part of the contract: on a one-page table the
	// planner reads the heap with a sequential scan, not the primary key index.
	// The contract is that the read touches only this table, in a handful of
	// buffers (asserted by the caller), never the work-item queue.
	if plans[0].Plan.Relation != "status_summary_snapshots" {
		t.Errorf("model read plan root reads %q, want only status_summary_snapshots", plans[0].Plan.Relation)
	}
	return plans[0].Plan.Hit, plans[0].Plan.Read, plans[0].Serialization.Hit, plans[0].Serialization.Read
}
