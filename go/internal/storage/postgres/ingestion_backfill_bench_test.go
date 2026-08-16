// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// latencyBackfillDB models the deferred-backfill write path with a fixed
// per-statement Postgres round-trip latency so a benchmark can compare serial and
// concurrent batch processing. Each Begin returns an independent transaction (its
// own goroutine-local state), mirroring the real pool where concurrent batches
// run on separate connections. Every ExecContext and QueryContext sleeps
// stmtLatency to stand in for the network + execution cost that dominates the
// client-side long pole (issue #3704).
type latencyBackfillDB struct {
	activeGenRows [][]any
	stmtLatency   time.Duration
}

func (db *latencyBackfillDB) QueryContext(_ context.Context, query string, args ...any) (Rows, error) {
	time.Sleep(db.stmtLatency)
	rows, err := db.rowsFor(query, args)
	if err != nil {
		return nil, err
	}
	return &queueFakeRows{rows: rows}, nil
}

// rowsFor answers with the column shape the requested query actually scans, and
// refuses anything it does not recognize.
//
// Column arity differs per query and the compiler cannot see it: the corpus-wide
// active-generation load scans three columns (repo_id, scope_id, generation_id),
// the fan-in's per-scope re-read (loadActiveGenerationForScope) scans one, and
// the fan-in's ArgoCD probe (listArgoCDBearingPartitionsQuery) scans two. A stub
// that answers everything with activeGenRows compiles and fails only when a
// benchmark executes, as "scan destination count = N, want 3".
//
// The default arm therefore ERRORS rather than guessing. The probe in particular
// is unreached today only because every benchmark here passes an empty
// catalogFingerprint, which skips the memo write that would issue it -- so the
// first person to benchmark the fan-in's real cost, which requires a non-empty
// fingerprint precisely to measure that probe, gets a named error telling them
// what to add instead of a confusing arity mismatch.
func (db *latencyBackfillDB) rowsFor(query string, args []any) ([][]any, error) {
	switch {
	case strings.HasPrefix(query, activeRepositoryGenerationsQuery):
		return db.activeGenRows, nil
	case strings.HasPrefix(query, activeScopeGenerationQuery):
		return db.activeGenerationRowFor(args), nil
	default:
		return nil, fmt.Errorf(
			"latencyBackfillDB: unrecognized query, add its column shape to rowsFor before benchmarking this path: %s",
			firstLine(query),
		)
	}
}

// activeGenerationRowFor projects the seeded (repo_id, scope_id, generation_id)
// rows down to the single generation column loadActiveGenerationForScope scans,
// for the scope bound as $1.
func (db *latencyBackfillDB) activeGenerationRowFor(args []any) [][]any {
	if len(args) == 0 {
		return nil
	}
	scopeID, ok := args[0].(string)
	if !ok {
		return nil
	}
	for _, row := range db.activeGenRows {
		if len(row) < 3 {
			continue
		}
		if rowScope, ok := row[1].(string); ok && rowScope == scopeID {
			return [][]any{{row[2]}}
		}
	}
	return nil
}

// firstLine trims a SQL constant to its first non-blank line so the error names
// the query without pasting the whole statement.
func firstLine(query string) string {
	for _, line := range strings.Split(query, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return "(empty query)"
}

func (db *latencyBackfillDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	time.Sleep(db.stmtLatency)
	return fakeResult{}, nil
}

func (db *latencyBackfillDB) Begin(context.Context) (Transaction, error) {
	return &latencyBackfillTx{db: db}, nil
}

type latencyBackfillTx struct{ db *latencyBackfillDB }

func (tx *latencyBackfillTx) QueryContext(_ context.Context, query string, args ...any) (Rows, error) {
	time.Sleep(tx.db.stmtLatency)
	rows, err := tx.db.rowsFor(query, args)
	if err != nil {
		return nil, err
	}
	return &queueFakeRows{rows: rows}, nil
}

func (tx *latencyBackfillTx) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	time.Sleep(tx.db.stmtLatency)
	return fakeResult{}, nil
}

func (tx *latencyBackfillTx) Commit() error   { return nil }
func (tx *latencyBackfillTx) Rollback() error { return nil }

// benchmarkDeferredBackfill runs the per-repository batch write path over a fixed
// repository count at the given worker level. It isolates the
// partition-by-repository write throughput the #3704 concurrency fix targets;
// fact load and DiscoverEvidence are out of scope (their content index is global
// and not scope-partitionable, so they stay serial by design).
func benchmarkDeferredBackfill(b *testing.B, workers int) {
	const repoCount = 256
	activeGen := make([][]any, 0, repoCount)
	evidence := make(map[string][]relationships.EvidenceFact, repoCount)
	for i := 0; i < repoCount; i++ {
		id := "repo-" + itoa(i)
		activeGen = append(activeGen, []any{id, "scope-" + itoa(i), "gen-" + itoa(i)})
		evidence[id] = []relationships.EvidenceFact{{
			EvidenceKind:     relationships.EvidenceKind("terraform_module"),
			RelationshipType: relationships.RelationshipType("depends_on"),
			SourceRepoID:     id,
			TargetRepoID:     "repo-target",
			Confidence:       0.9,
			Rationale:        "module reference",
		}}
	}
	db := &latencyBackfillDB{activeGenRows: activeGen, stmtLatency: 50 * time.Microsecond}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return time.Unix(0, 0).UTC() }
	store.maintenanceBatchSize = 8
	store.maintenanceWorkers = workers

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		if _, err := store.writeDeferredBackfillInBatches(context.Background(), evidence, nil, "", nil); err != nil {
			b.Fatalf("writeDeferredBackfillInBatches() error = %v", err)
		}
	}
}

// BenchmarkDeferredBackfillSerial is the worker=1 baseline.
func BenchmarkDeferredBackfillSerial(b *testing.B) { benchmarkDeferredBackfill(b, 1) }

// BenchmarkDeferredBackfillConcurrent4 runs the same workload at four workers; the
// per-batch round-trips overlap, so wall time drops toward serial/workers.
func BenchmarkDeferredBackfillConcurrent4(b *testing.B) { benchmarkDeferredBackfill(b, 4) }

// BenchmarkDeferredBackfillConcurrent8 runs at the hard worker cap.
func BenchmarkDeferredBackfillConcurrent8(b *testing.B) { benchmarkDeferredBackfill(b, 8) }
