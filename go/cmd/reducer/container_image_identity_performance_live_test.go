//go:build perf5854_head || perf5854_main

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel/metric/noop"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	containerImageIdentityPerfRepresentativeRefs       = 500
	containerImageIdentityPerfRepresentativeIterations = 20
	containerImageIdentityPerfWorstCaseRefs            = 99500
	containerImageIdentityPerfWorstCaseIterations      = 3
)

type containerImageIdentityPerfCase struct {
	name          string
	references    int
	iterations    int
	staleWarnings int
}

type containerImageIdentityPerfResult struct {
	Variant          string             `json:"variant"`
	Case             string             `json:"case"`
	References       int                `json:"references"`
	Iterations       int                `json:"iterations"`
	StaleWarnings    int                `json:"stale_warnings"`
	MedianMillis     float64            `json:"median_ms"`
	P95Millis        float64            `json:"p95_ms"`
	ThroughputPerSec float64            `json:"throughput_ops_per_sec"`
	QueriesPerOp     float64            `json:"queries_per_op"`
	ExecsPerOp       float64            `json:"execs_per_op"`
	BeginsPerOp      float64            `json:"begins_per_op"`
	CommitsPerOp     float64            `json:"commits_per_op"`
	WALBytesPerOp    float64            `json:"wal_bytes_per_op"`
	DeadTuples       int64              `json:"dead_tuples"`
	UpdatedTuples    int64              `json:"updated_tuples"`
	DeletedTuples    int64              `json:"deleted_tuples"`
	VisibleRows      int                `json:"visible_rows"`
	OutcomeKeyedRows int                `json:"outcome_keyed_rows"`
	LogicalChecksum  string             `json:"logical_checksum"`
	QueryBreakdown   map[string]int64   `json:"query_breakdown"`
	ExecBreakdown    map[string]int64   `json:"exec_breakdown"`
	QueryMillis      map[string]float64 `json:"query_call_ms"`
	ExecMillis       map[string]float64 `json:"exec_ms"`
}

func TestContainerImageIdentityProductionPathPerformanceLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_5854_PERF_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_5854_PERF_DSN to run the #5854 production-path benchmark")
	}
	variant := strings.TrimSpace(os.Getenv("ESHU_5854_PERF_VARIANT"))
	if variant == "" {
		t.Fatal("set ESHU_5854_PERF_VARIANT to main or head")
	}

	for _, scenario := range []containerImageIdentityPerfCase{
		{
			name:       "steady_zero_legacy",
			references: containerImageIdentityPerfRepresentativeRefs,
			iterations: containerImageIdentityPerfRepresentativeIterations,
		},
		{
			name:          "stale_warning_heavy",
			references:    containerImageIdentityPerfRepresentativeRefs,
			iterations:    containerImageIdentityPerfRepresentativeIterations,
			staleWarnings: 100000,
		},
		{
			name:       "worst_cardinality_zero_legacy",
			references: containerImageIdentityPerfWorstCaseRefs,
			iterations: containerImageIdentityPerfWorstCaseIterations,
		},
	} {
		scenario := scenario
		t.Run(scenario.name, func(t *testing.T) {
			result := runContainerImageIdentityPerfCase(t, dsn, variant, scenario, false)
			encoded, err := json.Marshal(result)
			if err != nil {
				t.Fatalf("marshal performance result: %v", err)
			}
			t.Logf("PERF5854 %s", encoded)
		})
	}
}

func TestContainerImageIdentityProductionCacheWarmPerformanceLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_5854_PERF_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_5854_PERF_DSN to run the #5854 cache-warm benchmark")
	}
	variant := strings.TrimSpace(os.Getenv("ESHU_5854_PERF_VARIANT"))
	if variant == "" {
		t.Fatal("set ESHU_5854_PERF_VARIANT to main or head")
	}
	scenario := containerImageIdentityPerfCase{
		name:       "cache_warm_worst_cardinality",
		references: containerImageIdentityPerfWorstCaseRefs,
		iterations: containerImageIdentityPerfWorstCaseIterations,
	}
	result := runContainerImageIdentityPerfCase(t, dsn, variant, scenario, true)
	if got, want := result.QueryBreakdown["identity_epoch_probe"], int64(scenario.iterations); got != want {
		t.Fatalf(
			"cache-warm epoch probes = %d, want %d (one validated hit per measured call)",
			got,
			want,
		)
	}
	if got := result.QueryBreakdown["identity_paginated_load"]; got != 0 {
		t.Fatalf("cache-warm paginated identity loads = %d, want 0", got)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal cache-warm performance result: %v", err)
	}
	t.Logf("PERF5854 %s", encoded)
}

func runContainerImageIdentityPerfCase(
	t *testing.T,
	dsn string,
	variant string,
	scenario containerImageIdentityPerfCase,
	useIdentityCache bool,
) containerImageIdentityPerfResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	db := openContainerImageIdentityPerfSchema(t, ctx, dsn, variant, scenario.name)
	seedContainerImageIdentityPerfFixture(
		t,
		ctx,
		db,
		scenario.references,
		scenario.staleWarnings,
	)

	counts := &containerImageIdentityPerfStatementCounts{}
	countingDB := containerImageIdentityPerfCountingDB{
		delegate: postgres.SQLDB{DB: db},
		counts:   counts,
	}
	factStore := postgres.NewFactStore(countingDB)
	if useIdentityCache {
		instruments, err := telemetry.NewInstruments(noop.NewMeterProvider().Meter("perf5854"))
		if err != nil {
			t.Fatalf("create cache performance instruments: %v", err)
		}
		cache, err := postgres.NewIdentityEpochCache(instruments, 0)
		if err != nil {
			t.Fatalf("create identity epoch cache: %v", err)
		}
		factStore = postgres.NewFactStoreWithIdentityCache(countingDB, cache)
	}
	writer := containerImageIdentityPerfWriter(countingDB)
	if writer == nil {
		t.Fatal("build production container-image-identity writer = nil")
	}
	var clockTick atomic.Int64
	handler := reducer.ContainerImageIdentityHandler{
		FactLoader: factStore,
		Writer:     writer,
		Now: func() time.Time {
			tick := clockTick.Add(1)
			return time.Date(2026, time.July, 29, 20, 0, 0, 0, time.UTC).
				Add(time.Duration(tick) * time.Microsecond)
		},
	}
	intent := reducer.Intent{
		IntentID:     "intent-5854-performance",
		Domain:       reducer.DomainContainerImageIdentity,
		ScopeID:      containerImageIdentityPerfRepoScope,
		GenerationID: containerImageIdentityPerfRepoGeneration,
		SourceSystem: "git",
		Cause:        "synthetic production-path performance proof",
	}
	containerImageIdentityPerfPrepareIntent(&intent)

	if _, err := handler.Handle(ctx, intent); err != nil {
		t.Fatalf("warm production handler: %v", err)
	}
	assertContainerImageIdentityPerfAccuracy(
		t,
		ctx,
		db,
		scenario.references,
		containerImageIdentityPerfHeadVariant,
	)
	prepareContainerImageIdentityPerfStats(t, ctx, db)
	counts.reset()
	walBefore := currentContainerImageIdentityPerfWAL(t, ctx, db)

	latencies := make([]time.Duration, 0, scenario.iterations)
	started := time.Now()
	for range scenario.iterations {
		runStarted := time.Now()
		if _, err := handler.Handle(ctx, intent); err != nil {
			t.Fatalf("measured production handler: %v", err)
		}
		latencies = append(latencies, time.Since(runStarted))
	}
	total := time.Since(started)
	walAfter := currentContainerImageIdentityPerfWAL(t, ctx, db)
	walBytes := containerImageIdentityPerfWALDiff(t, ctx, db, walAfter, walBefore)
	stats := readContainerImageIdentityPerfTableStats(t, ctx, db)
	accuracy := assertContainerImageIdentityPerfAccuracy(
		t,
		ctx,
		db,
		scenario.references,
		containerImageIdentityPerfHeadVariant,
	)
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	statementCounts := counts.snapshot()
	iterations := float64(scenario.iterations)

	return containerImageIdentityPerfResult{
		Variant:          variant,
		Case:             scenario.name,
		References:       scenario.references,
		Iterations:       scenario.iterations,
		StaleWarnings:    scenario.staleWarnings,
		MedianMillis:     durationMillis(latencies[len(latencies)/2]),
		P95Millis:        durationMillis(latencies[(len(latencies)*95-1)/100]),
		ThroughputPerSec: iterations / total.Seconds(),
		QueriesPerOp:     float64(statementCounts.queries) / iterations,
		ExecsPerOp:       float64(statementCounts.execs) / iterations,
		BeginsPerOp:      float64(statementCounts.begins) / iterations,
		CommitsPerOp:     float64(statementCounts.commits) / iterations,
		WALBytesPerOp:    float64(walBytes) / iterations,
		DeadTuples:       stats.dead,
		UpdatedTuples:    stats.updated,
		DeletedTuples:    stats.deleted,
		VisibleRows:      accuracy.visibleRows,
		OutcomeKeyedRows: accuracy.outcomeKeyedRows,
		LogicalChecksum:  accuracy.checksum,
		QueryBreakdown:   statementCounts.queryBreakdown,
		ExecBreakdown:    statementCounts.execBreakdown,
		QueryMillis:      statementCounts.queryMillis,
		ExecMillis:       statementCounts.execMillis,
	}
}

type containerImageIdentityPerfStatementCounts struct {
	queries          atomic.Int64
	execs            atomic.Int64
	begins           atomic.Int64
	commits          atomic.Int64
	rollbacks        atomic.Int64
	mu               sync.Mutex
	queriesByShape   map[string]int64
	execsByShape     map[string]int64
	queryTimeByShape map[string]time.Duration
	execTimeByShape  map[string]time.Duration
}

type containerImageIdentityPerfCountSnapshot struct {
	queries        int64
	execs          int64
	begins         int64
	commits        int64
	rollbacks      int64
	queryBreakdown map[string]int64
	execBreakdown  map[string]int64
	queryMillis    map[string]float64
	execMillis     map[string]float64
}

func (c *containerImageIdentityPerfStatementCounts) reset() {
	c.queries.Store(0)
	c.execs.Store(0)
	c.begins.Store(0)
	c.commits.Store(0)
	c.rollbacks.Store(0)
	c.mu.Lock()
	c.queriesByShape = make(map[string]int64)
	c.execsByShape = make(map[string]int64)
	c.queryTimeByShape = make(map[string]time.Duration)
	c.execTimeByShape = make(map[string]time.Duration)
	c.mu.Unlock()
}

func (c *containerImageIdentityPerfStatementCounts) snapshot() containerImageIdentityPerfCountSnapshot {
	c.mu.Lock()
	queryBreakdown := cloneContainerImageIdentityPerfCounts(c.queriesByShape)
	execBreakdown := cloneContainerImageIdentityPerfCounts(c.execsByShape)
	queryMillis := cloneContainerImageIdentityPerfDurations(c.queryTimeByShape)
	execMillis := cloneContainerImageIdentityPerfDurations(c.execTimeByShape)
	c.mu.Unlock()
	return containerImageIdentityPerfCountSnapshot{
		queries:        c.queries.Load(),
		execs:          c.execs.Load(),
		begins:         c.begins.Load(),
		commits:        c.commits.Load(),
		rollbacks:      c.rollbacks.Load(),
		queryBreakdown: queryBreakdown,
		execBreakdown:  execBreakdown,
		queryMillis:    queryMillis,
		execMillis:     execMillis,
	}
}

func (c *containerImageIdentityPerfStatementCounts) recordQuery(
	query string,
	elapsed time.Duration,
) {
	c.queries.Add(1)
	c.mu.Lock()
	if c.queriesByShape == nil {
		c.queriesByShape = make(map[string]int64)
		c.queryTimeByShape = make(map[string]time.Duration)
	}
	shape := containerImageIdentityPerfStatementShape(query)
	c.queriesByShape[shape]++
	c.queryTimeByShape[shape] += elapsed
	c.mu.Unlock()
}

func (c *containerImageIdentityPerfStatementCounts) recordExec(
	query string,
	elapsed time.Duration,
) {
	c.execs.Add(1)
	c.mu.Lock()
	if c.execsByShape == nil {
		c.execsByShape = make(map[string]int64)
		c.execTimeByShape = make(map[string]time.Duration)
	}
	shape := containerImageIdentityPerfStatementShape(query)
	c.execsByShape[shape]++
	c.execTimeByShape[shape] += elapsed
	c.mu.Unlock()
}

func cloneContainerImageIdentityPerfCounts(input map[string]int64) map[string]int64 {
	result := make(map[string]int64, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func cloneContainerImageIdentityPerfDurations(
	input map[string]time.Duration,
) map[string]float64 {
	result := make(map[string]float64, len(input))
	for key, value := range input {
		result[key] = durationMillis(value)
	}
	return result
}

func containerImageIdentityPerfStatementShape(query string) string {
	switch {
	case strings.Contains(query, "container_image_identity_cutovers") &&
		strings.Contains(query, "SELECT EXISTS"):
		return "cutover_exists"
	case strings.Contains(query, "ORDER BY fact_id") &&
		strings.Contains(query, "identity_format"):
		return "legacy_cleanup_probe"
	case strings.Contains(query, "current_claim AS MATERIALIZED"):
		return "current_claim"
	case strings.Contains(query, "INSERT INTO fact_records"):
		return "fact_insert"
	case strings.Contains(query, "SELECT count(*) AS cnt, max(observed_at)") &&
		strings.Contains(query, "FROM ingestion_scopes"):
		return "identity_epoch_probe"
	case strings.Contains(query, "active_generation_id = fact.generation_id") &&
		strings.Contains(query, "oci_registry.image_tag_observation") &&
		strings.Contains(query, "aws_image_reference") &&
		strings.Contains(query, "ORDER BY fact.observed_at"):
		return "identity_paginated_load"
	case strings.Contains(query, "FROM fact_records"):
		return "fact_read"
	default:
		return "other"
	}
}

type containerImageIdentityPerfCountingDB struct {
	delegate postgres.SQLDB
	counts   *containerImageIdentityPerfStatementCounts
}

func (db containerImageIdentityPerfCountingDB) QueryContext(
	ctx context.Context,
	query string,
	args ...any,
) (postgres.Rows, error) {
	started := time.Now()
	rows, err := db.delegate.QueryContext(ctx, query, args...)
	db.counts.recordQuery(query, time.Since(started))
	return rows, err
}

func (db containerImageIdentityPerfCountingDB) ExecContext(
	ctx context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	started := time.Now()
	result, err := db.delegate.ExecContext(ctx, query, args...)
	db.counts.recordExec(query, time.Since(started))
	return result, err
}

func (db containerImageIdentityPerfCountingDB) Begin(
	ctx context.Context,
) (postgres.Transaction, error) {
	db.counts.begins.Add(1)
	tx, err := db.delegate.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return containerImageIdentityPerfCountingTx{
		delegate: tx,
		counts:   db.counts,
	}, nil
}

type containerImageIdentityPerfCountingTx struct {
	delegate postgres.Transaction
	counts   *containerImageIdentityPerfStatementCounts
}

func (tx containerImageIdentityPerfCountingTx) QueryContext(
	ctx context.Context,
	query string,
	args ...any,
) (postgres.Rows, error) {
	started := time.Now()
	rows, err := tx.delegate.QueryContext(ctx, query, args...)
	tx.counts.recordQuery(query, time.Since(started))
	return rows, err
}

func (tx containerImageIdentityPerfCountingTx) ExecContext(
	ctx context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	started := time.Now()
	result, err := tx.delegate.ExecContext(ctx, query, args...)
	tx.counts.recordExec(query, time.Since(started))
	return result, err
}

func (tx containerImageIdentityPerfCountingTx) Commit() error {
	tx.counts.commits.Add(1)
	return tx.delegate.Commit()
}

func (tx containerImageIdentityPerfCountingTx) Rollback() error {
	tx.counts.rollbacks.Add(1)
	return tx.delegate.Rollback()
}

func durationMillis(duration time.Duration) float64 {
	return float64(duration) / float64(time.Millisecond)
}

func containerImageIdentityPerfVariantSchema(variant string, scenario string) string {
	normalized := strings.NewReplacer("-", "_", "/", "_").Replace(variant + "_" + scenario)
	return fmt.Sprintf("eshu_5854_perf_%s_%d", normalized, time.Now().UnixNano())
}
