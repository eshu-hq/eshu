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
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
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
	Variant          string  `json:"variant"`
	Case             string  `json:"case"`
	References       int     `json:"references"`
	Iterations       int     `json:"iterations"`
	StaleWarnings    int     `json:"stale_warnings"`
	MedianMillis     float64 `json:"median_ms"`
	P95Millis        float64 `json:"p95_ms"`
	ThroughputPerSec float64 `json:"throughput_ops_per_sec"`
	QueriesPerOp     float64 `json:"queries_per_op"`
	ExecsPerOp       float64 `json:"execs_per_op"`
	BeginsPerOp      float64 `json:"begins_per_op"`
	CommitsPerOp     float64 `json:"commits_per_op"`
	WALBytesPerOp    float64 `json:"wal_bytes_per_op"`
	DeadTuples       int64   `json:"dead_tuples"`
	UpdatedTuples    int64   `json:"updated_tuples"`
	DeletedTuples    int64   `json:"deleted_tuples"`
	VisibleRows      int     `json:"visible_rows"`
	OutcomeKeyedRows int     `json:"outcome_keyed_rows"`
	LogicalChecksum  string  `json:"logical_checksum"`
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
			result := runContainerImageIdentityPerfCase(t, dsn, variant, scenario)
			encoded, err := json.Marshal(result)
			if err != nil {
				t.Fatalf("marshal performance result: %v", err)
			}
			t.Logf("PERF5854 %s", encoded)
		})
	}
}

func runContainerImageIdentityPerfCase(
	t *testing.T,
	dsn string,
	variant string,
	scenario containerImageIdentityPerfCase,
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
	}
}

type containerImageIdentityPerfStatementCounts struct {
	queries   atomic.Int64
	execs     atomic.Int64
	begins    atomic.Int64
	commits   atomic.Int64
	rollbacks atomic.Int64
}

type containerImageIdentityPerfCountSnapshot struct {
	queries   int64
	execs     int64
	begins    int64
	commits   int64
	rollbacks int64
}

func (c *containerImageIdentityPerfStatementCounts) reset() {
	c.queries.Store(0)
	c.execs.Store(0)
	c.begins.Store(0)
	c.commits.Store(0)
	c.rollbacks.Store(0)
}

func (c *containerImageIdentityPerfStatementCounts) snapshot() containerImageIdentityPerfCountSnapshot {
	return containerImageIdentityPerfCountSnapshot{
		queries:   c.queries.Load(),
		execs:     c.execs.Load(),
		begins:    c.begins.Load(),
		commits:   c.commits.Load(),
		rollbacks: c.rollbacks.Load(),
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
	db.counts.queries.Add(1)
	return db.delegate.QueryContext(ctx, query, args...)
}

func (db containerImageIdentityPerfCountingDB) ExecContext(
	ctx context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	db.counts.execs.Add(1)
	return db.delegate.ExecContext(ctx, query, args...)
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
	tx.counts.queries.Add(1)
	return tx.delegate.QueryContext(ctx, query, args...)
}

func (tx containerImageIdentityPerfCountingTx) ExecContext(
	ctx context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	tx.counts.execs.Add(1)
	return tx.delegate.ExecContext(ctx, query, args...)
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
