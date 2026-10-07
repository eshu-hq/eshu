// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	projectorruntime "github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	statussummary "github.com/eshu-hq/eshu/go/internal/reducer/status/summary"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	store "github.com/eshu-hq/eshu/go/internal/storage/postgres/status/summary"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	contentionWorkItems = 2000
	writerAppName       = "status-summary-writer-proof"
	quietPhase          = 8 * time.Second
	writerPhase         = 30 * time.Second
	spikeAfter          = 12 * time.Second
)

// TestWritersBesideTheProductionClaimLoopLive is D3.2 items 6 and 10: two
// writers at the 5 s minimum cadence (the default is 10 s) run for 30 s beside two workers
// that claim and Ack through the production ReducerQueue, at 80 % live work
// rising to 100 % mid-run. It requires zero claim errors, zero lock waits
// caused by a writer backend, a monotone stored as_of, writer passes that
// only ever write or skip the lock, computes that never overlap across the
// two writers, and overruns only when a pass outlasted the interval.
func TestWritersBesideTheProductionClaimLoopLive(t *testing.T) {
	ctx, database := openWriterDatabase(t)
	database.SetMaxOpenConns(24)
	seedQueue(ctx, t, database)
	// 80 % live: a fifth of the rows start succeeded; the spike re-opens them.
	mustExec(ctx, t, database, `UPDATE fact_work_items SET status = 'succeeded'
		WHERE stage = 'reducer' AND domain = 'workload_identity' AND random() < 0.2`)

	claims := startClaimLoop(ctx, t, database)
	time.Sleep(quietPhase)
	quiet := claims.phase()

	writerPool := openAppPool(ctx, t, database, writerAppName)
	assertSamplerSeesAWriterWait(ctx, t, database, writerPool)
	var computes computeLog
	writers := make([]*statussummary.Runner, 2)
	readers := make([]*sdkmetric.ManualReader, 2)
	for i := range writers {
		writers[i] = newLiveWriter(writerPool)
		writers[i].Interval = statussummary.MinInterval // the fastest allowed cadence is the worst case
		writers[i].Statement.Compute = computes.wrap(i, writers[i].Statement.Compute)
		writers[i].Instruments, readers[i] = proofInstruments(t)
	}
	writerCtx, stopWriters := context.WithTimeout(ctx, writerPhase)
	defer stopWriters()
	var wg sync.WaitGroup
	for _, writer := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := writer.Run(writerCtx); err != nil {
				t.Errorf("writer Run() error = %v", err)
			}
		}()
	}
	sampler := startLockSampler(writerCtx, t, database)
	asOfs := startAsOfReader(writerCtx, t, database)
	time.Sleep(spikeAfter)
	mustExec(ctx, t, database, `UPDATE fact_work_items SET status = 'pending', updated_at = now()
		WHERE stage = 'reducer' AND domain = 'workload_identity' AND status = 'succeeded'`)
	wg.Wait()
	sampler.wait()
	asOfs.wait()
	loaded := claims.phase()
	claims.stop()

	if claims.errorCount() != 0 {
		t.Fatalf("claim loop errors = %d (first: %v), want 0", claims.errorCount(), claims.firstError())
	}
	if quiet.count == 0 || loaded.count == 0 {
		t.Fatalf("claims quiet/loaded = %d/%d, want both > 0", quiet.count, loaded.count)
	}
	if sampler.writerWaits != 0 {
		t.Fatalf("writer-attributable lock waits = %d of %d samples, want 0", sampler.writerWaits, sampler.samples)
	}
	if !asOfs.monotone || asOfs.reads == 0 {
		t.Fatalf("stored as_of monotone = %v over %d reads, want true", asOfs.monotone, asOfs.reads)
	}
	if overlap := computes.overlap(); overlap != "" {
		t.Fatalf("two writers computed at once: %s", overlap)
	}
	var okPasses int64
	for i, reader := range readers {
		for _, outcome := range []string{statussummary.OutcomeError, statussummary.OutcomeRejectedGuard, statussummary.OutcomeSkippedMissingTable} {
			if got := sumInt(t, reader, "eshu_dp_status_summary_writer_passes_total", outcome); got != 0 {
				t.Fatalf("writer %d %s passes = %d, want 0", i, outcome, got)
			}
		}
		okPasses += sumInt(t, reader, "eshu_dp_status_summary_writer_passes_total", statussummary.OutcomeOK)
		overruns := sumInt(t, reader, "eshu_dp_status_summary_writer_overrun_total", "")
		if maxPass := maxDuration(t, reader); (overruns > 0) != (maxPass > statussummary.MinInterval.Seconds()) {
			t.Fatalf("writer %d overruns = %d with max pass %.3fs; overruns must match passes over the interval", i, overruns, maxPass)
		}
	}
	if okPasses < 4 {
		t.Fatalf("ok passes across both writers = %d in %s, want at least 4", okPasses, writerPhase)
	}
	t.Logf("claims quiet: %d (%.1f/s, p95 %s); with writers: %d (%.1f/s, p95 %s); ok passes %d; max compute %s; max as_of age %s; lock samples %d",
		quiet.count, quiet.rate, quiet.p95, loaded.count, loaded.rate, loaded.p95, okPasses,
		computes.maxDuration(), asOfs.maxAge, sampler.samples)

	if pass := newLiveWriter(database).RunOnce(ctx); pass.Outcome != statussummary.OutcomeOK {
		t.Fatalf("pass after the run = %+v, want ok", pass)
	}
	assertRowEqualsLive(ctx, t, database)
}

// seedQueue enqueues contentionWorkItems reducer intents through the
// production enqueue path, spread over every fixture scope.
func seedQueue(ctx context.Context, t *testing.T, database *sql.DB) {
	t.Helper()
	intents := make([]projectorruntime.ReducerIntent, 0, contentionWorkItems)
	for i := 0; i < contentionWorkItems; i++ {
		scope := i % fixtureScopes
		intents = append(intents, projectorruntime.ReducerIntent{
			ScopeID:      fmt.Sprintf("scope-%d", scope),
			GenerationID: fmt.Sprintf("gen-%d", scope),
			Domain:       reducer.DomainWorkloadIdentity,
			EntityKey:    fmt.Sprintf("entity-%d", i),
			Reason:       "status summary writer proof",
			FactID:       fmt.Sprintf("fact-%d", i),
			SourceSystem: "git",
		})
	}
	queue := postgres.NewReducerQueue(postgres.SQLDB{DB: database}, "seed", time.Minute)
	if _, err := queue.Enqueue(ctx, intents); err != nil {
		t.Fatalf("enqueue reducer intents: %v", err)
	}
}

// openAppPool opens a second pool on the same database whose connections
// carry applicationName, so the lock sampler can attribute waits.
func openAppPool(ctx context.Context, t *testing.T, database *sql.DB, applicationName string) *sql.DB {
	t.Helper()
	var name string
	if err := database.QueryRowContext(ctx, `SELECT current_database()`).Scan(&name); err != nil {
		t.Fatalf("read database name: %v", err)
	}
	config, err := pgx.ParseConfig(os.Getenv(proofDSNEnv))
	if err != nil {
		t.Fatalf("parse proof DSN: %v", err)
	}
	config.Database = name
	config.RuntimeParams["application_name"] = applicationName
	pool := stdlib.OpenDB(*config)
	t.Cleanup(func() { _ = pool.Close() })
	return pool
}

func proofInstruments(t *testing.T) (*telemetry.Instruments, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("proof"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	return instruments, reader
}

// sumInt sums an Int64 counter's points, filtered by outcome when given.
func sumInt(t *testing.T, reader *sdkmetric.ManualReader, name, outcome string) int64 {
	t.Helper()
	var total int64
	for _, m := range collect(t, reader) {
		if m.Name != name {
			continue
		}
		for _, point := range m.Data.(metricdata.Sum[int64]).DataPoints {
			value, _ := point.Attributes.Value(telemetry.MetricDimensionOutcome)
			if outcome == "" || value.AsString() == outcome {
				total += point.Value
			}
		}
	}
	return total
}

// maxDuration returns the longest recorded pass in seconds.
func maxDuration(t *testing.T, reader *sdkmetric.ManualReader) float64 {
	t.Helper()
	var longest float64
	for _, m := range collect(t, reader) {
		if m.Name != "eshu_dp_status_summary_writer_pass_duration_seconds" {
			continue
		}
		for _, point := range m.Data.(metricdata.Histogram[float64]).DataPoints {
			if value, ok := point.Max.Value(); ok && value > longest {
				longest = value
			}
		}
	}
	return longest
}

func collect(t *testing.T, reader *sdkmetric.ManualReader) []metricdata.Metrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	var out []metricdata.Metrics
	for _, scope := range rm.ScopeMetrics {
		out = append(out, scope.Metrics...)
	}
	return out
}

// computeLog records every writer's compute window so a test can prove two
// writers never computed at the same time.
type computeLog struct {
	mu      sync.Mutex
	windows []computeWindow
}

type computeWindow struct {
	writer     int
	start, end time.Time
}

func (l *computeLog) wrap(
	writer int,
	compute func(context.Context, db.Queryer, time.Time) ([]store.Entry, error),
) func(context.Context, db.Queryer, time.Time) ([]store.Entry, error) {
	return func(ctx context.Context, q db.Queryer, asOf time.Time) ([]store.Entry, error) {
		start := time.Now()
		entries, err := compute(ctx, q, asOf)
		l.mu.Lock()
		l.windows = append(l.windows, computeWindow{writer: writer, start: start, end: time.Now()})
		l.mu.Unlock()
		return entries, err
	}
}

func (l *computeLog) overlap() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	windows := slices.Clone(l.windows)
	slices.SortFunc(windows, func(a, b computeWindow) int { return a.start.Compare(b.start) })
	for i := 1; i < len(windows); i++ {
		if windows[i].start.Before(windows[i-1].end) {
			return fmt.Sprintf("writer %d started at %s before writer %d ended at %s",
				windows[i].writer, windows[i].start, windows[i-1].writer, windows[i-1].end)
		}
	}
	return ""
}

func (l *computeLog) maxDuration() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	var longest time.Duration
	for _, window := range l.windows {
		longest = max(longest, window.end.Sub(window.start))
	}
	return longest
}
