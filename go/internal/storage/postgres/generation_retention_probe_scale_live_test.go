// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// retentionScaleCandidates is the candidate family shape both scale points
// prune: 4 scopes whose g0..g2 are outside the window and g3 is kept as the
// newest superseded generation.
func retentionScaleCandidates(prefix string) retentionProbeCorpus {
	return retentionProbeCorpus{prefix: prefix, scopes: 4, superseded: 4, entities: 1000, files: 200, filler: 100, old: true}
}

// retentionScaleBystanders is non-candidate data: scopes whose superseded
// generations are inside the window, so no batch selects them. Each scope holds
// enough distinct entity keys that the entity key index already has three
// btree levels at the first scale point, so tripling the bystanders changes the
// table and index size but not the depth each probe descends (a depth change
// is a log-scale step, not the linear growth this test guards against).
func retentionScaleBystanders(prefix string, scopes int) retentionProbeCorpus {
	return retentionProbeCorpus{prefix: prefix, scopes: scopes, superseded: 4, entities: 3000, files: 200, padding: 60}
}

// retentionScaleTolerance is how far a statement's fact_records shared buffers
// may grow when only non-candidate data triples. A drop is allowed: the planner
// may apply the anti-join before or after joining a content table, which
// changes how often a key is probed but never with fact_records size. The probe reads the candidate
// facts and probes each candidate key, so its buffers follow the batch, not the
// table; the grouped #6809 pass read every live fact of a kind and roughly
// tripled. Only fact_records nodes are compared: the content-table side of each
// DELETE is a planner choice (a small content table is cheaper to hash than to
// probe) that is not the scan this issue removes.
const retentionScaleTolerance = 0.10

// retentionScaleGrowthControl is the least the frozen #6809 statements must
// grow on the same fixture. It proves the fixture can show table-size growth,
// so a flat probe measurement is not a fixture that measures nothing.
const retentionScaleGrowthControl = 1.5

// TestGenerationRetentionProbeCostIsIndependentOfTableSizeLive proves the
// #7279 claim that a batch's work, and so the scope-lock hold, follows the
// batch and not fact_records. It measures each lock-window statement's
// fact_records shared buffers for one candidate batch, triples the
// non-candidate facts, measures an identical batch again, and requires no
// statement to grow more than 10%. The frozen #6809 statements are measured on
// the same fixture as a control and must grow at least 1.5x. After each
// measurement it runs a real retention cycle while a second session inserts a
// fact into a locked scope, and checks that the insert waits for the scope lock
// and is released by the commit.
func TestGenerationRetentionProbeCostIsIndependentOfTableSizeLive(t *testing.T) {
	database, ctx := openGenerationRetentionMigratedSchema(t)
	disableRetentionProbeAutovacuum(t, ctx, database)

	first := retentionScaleCandidates("c")
	seedRetentionProbeCorpus(t, ctx, database, first)
	seedRetentionProbeCorpus(t, ctx, database, retentionScaleBystanders("n", 16))
	analyzeRetentionProbeTables(t, ctx, database)
	firstCandidates := retentionProbeCandidates(first, 3)
	before := retentionMeasureStatements(t, ctx, database, retentionPlanStatements, firstCandidates)
	legacyBefore := retentionMeasureStatements(t, ctx, database, legacyRetentionStatements, firstCandidates)
	holdBefore := runRetentionCycleUnderContention(t, ctx, database, "c1", "c1-act")

	second := retentionScaleCandidates("d")
	seedRetentionProbeCorpus(t, ctx, database, second)
	seedRetentionProbeCorpus(t, ctx, database, retentionScaleBystanders("m", 32))
	analyzeRetentionProbeTables(t, ctx, database)
	secondCandidates := retentionProbeCandidates(second, 3)
	after := retentionMeasureStatements(t, ctx, database, retentionPlanStatements, secondCandidates)
	legacyAfter := retentionMeasureStatements(t, ctx, database, legacyRetentionStatements, secondCandidates)
	holdAfter := runRetentionCycleUnderContention(t, ctx, database, "d1", "d1-act")

	for _, statement := range retentionPlanStatements {
		name := statement.name
		ratio := float64(after[name].buffers) / float64(before[name].buffers)
		legacyRatio := float64(legacyAfter[name].buffers) / float64(legacyBefore[name].buffers)
		t.Logf("%s fact_records shared buffers 1x -> 3x: probe %d -> %d (%.3fx, %.1f -> %.1f ms), grouped %d -> %d (%.3fx, %.1f -> %.1f ms)",
			name, before[name].buffers, after[name].buffers, ratio, before[name].milliseconds, after[name].milliseconds,
			legacyBefore[name].buffers, legacyAfter[name].buffers, legacyRatio,
			legacyBefore[name].milliseconds, legacyAfter[name].milliseconds)
		if ratio > 1+retentionScaleTolerance {
			t.Errorf("%s fact_records shared buffers grew %.3fx when only non-candidate facts tripled; want at most +%.0f%%\n1x plan:\n%s\n3x plan:\n%s",
				name, ratio, retentionScaleTolerance*100, before[name].plan, after[name].plan)
		}
		if legacyRatio < retentionScaleGrowthControl {
			t.Errorf("control: grouped %s grew only %.3fx; the fixture no longer shows table-size growth", name, legacyRatio)
		}
	}
	t.Logf("scope-lock hold: %s at 1x, %s at 3x non-candidate facts", holdBefore, holdAfter)
}

// retentionMeasurement is one statement's EXPLAIN (ANALYZE, BUFFERS) result.
type retentionMeasurement struct {
	buffers      int64
	milliseconds float64
	plan         string
}

// retentionMeasureStatements runs each statement under EXPLAIN (ANALYZE,
// BUFFERS) in a rolled-back transaction and returns its fact_records shared hit
// plus read blocks, its execution time, and its text plan for a failure message.
func retentionMeasureStatements(t *testing.T, ctx context.Context, database *sql.DB, statements []retentionStatement, candidates []string) map[string]retentionMeasurement {
	t.Helper()
	measured := map[string]retentionMeasurement{}
	for _, statement := range statements {
		tx, err := database.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin %s: %v", statement.name, err)
		}
		if _, err := tx.ExecContext(ctx, generationRetentionWorkMemStatement); err != nil {
			_ = tx.Rollback()
			t.Fatalf("set work_mem: %v", err)
		}
		var plan []byte
		err = tx.QueryRowContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+statement.sql, candidates).Scan(&plan)
		_ = tx.Rollback()
		if err != nil {
			t.Fatalf("explain analyze %s: %v", statement.name, err)
		}
		var root []struct {
			Plan          map[string]any `json:"Plan"`
			ExecutionTime float64        `json:"Execution Time"`
		}
		if err := json.Unmarshal(plan, &root); err != nil || len(root) != 1 {
			t.Fatalf("decode %s plan: %v", statement.name, err)
		}
		measured[statement.name] = retentionMeasurement{
			buffers:      retentionFactRecordsBuffers(root[0].Plan),
			milliseconds: root[0].ExecutionTime,
			plan:         retentionTextPlan(t, ctx, database, statement.sql, candidates),
		}
	}
	return measured
}

// retentionFactRecordsBuffers sums the shared hit and read blocks of every
// scan node on fact_records in an EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) plan.
// Scan nodes are leaves (a Bitmap Heap Scan's count includes its index child),
// so the sum counts each block read once.
func retentionFactRecordsBuffers(node map[string]any) int64 {
	var total int64
	if relation, _ := node["Relation Name"].(string); relation == "fact_records" {
		hit, _ := node["Shared Hit Blocks"].(float64)
		read, _ := node["Shared Read Blocks"].(float64)
		total += int64(hit + read)
	}
	children, _ := node["Plans"].([]any)
	for _, child := range children {
		if childNode, ok := child.(map[string]any); ok {
			total += retentionFactRecordsBuffers(childNode)
		}
	}
	return total
}

// retentionTextPlan returns the EXPLAIN (ANALYZE, BUFFERS) text plan of
// statement in a rolled-back transaction.
func retentionTextPlan(t *testing.T, ctx context.Context, database *sql.DB, statement string, candidates []string) string {
	t.Helper()
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin text plan: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, generationRetentionWorkMemStatement); err != nil {
		t.Fatalf("set work_mem: %v", err)
	}
	rows, err := tx.QueryContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, COSTS OFF, TIMING OFF) "+statement, candidates)
	if err != nil {
		t.Fatalf("text plan: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan text plan: %v", err)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// runRetentionCycleUnderContention runs one real retention cycle. As soon as
// the cycle holds its scope locks (its row-count statement starts), a second
// session inserts a fact into lockedScope, which needs a FOR KEY SHARE lock on
// that scope row. It fails unless the insert waited for the cycle's commit and
// finished right after it, and returns the cycle's scope-lock hold.
func runRetentionCycleUnderContention(t *testing.T, ctx context.Context, database *sql.DB, lockedScope, generation string) time.Duration {
	t.Helper()
	probe := &retentionLockProbeDB{inner: SQLDB{DB: database}, locked: make(chan struct{})}
	var insertDone time.Time
	var insertErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		select {
		case <-probe.locked:
		case <-ctx.Done():
			insertErr = ctx.Err()
			return
		}
		_, insertErr = database.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
    source_fact_key, observed_at, ingested_at, payload)
VALUES ('contention/' || $1, $1, $2, 'filler', 'contention', 'git', 'contention', now(), now(), '{}'::jsonb)`,
			lockedScope, generation)
		insertDone = time.Now()
	}()
	result, err := NewGenerationRetentionStore(probe).PruneSupersededGenerations(ctx, GenerationRetentionPolicy{
		MinSupersededGenerations: 1,
		MaxSupersededAge:         time.Hour,
		BatchGenerationLimit:     100,
		BatchRowLimit:            10_000_000,
		PolicyScope:              "global",
		PolicyRevision:           "7279-contention",
	})
	probe.release()
	wg.Wait()
	if err != nil {
		t.Fatalf("retention cycle: %v", err)
	}
	if insertErr != nil {
		t.Fatalf("contending insert: %v", insertErr)
	}
	if result.GenerationsPruned != 12 {
		t.Fatalf("GenerationsPruned = %d, want 12", result.GenerationsPruned)
	}
	hold := probe.committedAt.Sub(probe.lockedAt)
	wait := insertDone.Sub(probe.lockedAt)
	t.Logf("scope-lock hold %s; contending insert into %s finished %s after the locks were taken; phases %v",
		hold, lockedScope, wait, result.PhaseDurations)
	// The insert must have waited for the commit (the scope lock is intact) and
	// must not outlive it by more than scheduling noise.
	if insertDone.Before(probe.commitStart) {
		t.Errorf("contending insert finished %s before the retention commit began; the scope lock no longer blocks it",
			probe.commitStart.Sub(insertDone))
	}
	if wait > hold+500*time.Millisecond {
		t.Errorf("contending insert waited %s, longer than the %s scope-lock hold", wait, hold)
	}
	return hold
}

// retentionLockProbeDB wraps the store's database to timestamp the moment the
// retention transaction holds its scope locks (its first row-count statement),
// the moment it starts to commit, and the moment the commit returns.
type retentionLockProbeDB struct {
	inner       SQLDB
	locked      chan struct{}
	once        sync.Once
	lockedAt    time.Time
	commitStart time.Time
	committedAt time.Time
}

func (p *retentionLockProbeDB) release() { p.once.Do(func() { close(p.locked) }) }

func (p *retentionLockProbeDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return p.inner.ExecContext(ctx, query, args...)
}

func (p *retentionLockProbeDB) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	return p.inner.QueryContext(ctx, query, args...)
}

func (p *retentionLockProbeDB) Begin(ctx context.Context) (db.Transaction, error) {
	tx, err := p.inner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &retentionLockProbeTx{Transaction: tx, probe: p}, nil
}

type retentionLockProbeTx struct {
	db.Transaction
	probe *retentionLockProbeDB
}

func (tx *retentionLockProbeTx) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	if strings.Contains(query, "generation_retention_row_counts") && tx.probe.lockedAt.IsZero() {
		tx.probe.lockedAt = time.Now()
		tx.probe.release()
		// Give the contending insert time to reach its lock wait.
		time.Sleep(100 * time.Millisecond)
	}
	return tx.Transaction.QueryContext(ctx, query, args...)
}

func (tx *retentionLockProbeTx) Commit() error {
	tx.probe.commitStart = time.Now()
	err := tx.Transaction.Commit()
	tx.probe.committedAt = time.Now()
	return err
}
