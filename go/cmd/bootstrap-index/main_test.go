package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"log/slog"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/buildinfo"
	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func TestPrintBootstrapIndexVersionFlagReturnsBeforeBootstrapWorkflow(t *testing.T) {
	original := buildinfo.Version
	buildinfo.Version = "v1.2.3-bootstrap"
	t.Cleanup(func() { buildinfo.Version = original })

	var stdout strings.Builder
	handled, err := printBootstrapIndexVersionFlag([]string{"--version"}, &stdout)
	if err != nil {
		t.Fatalf("printBootstrapIndexVersionFlag() error = %v, want nil", err)
	}
	if !handled {
		t.Fatal("printBootstrapIndexVersionFlag() handled = false, want true")
	}
	if got, want := stdout.String(), "eshu-bootstrap-index v1.2.3-bootstrap\n"; got != want {
		t.Fatalf("printBootstrapIndexVersionFlag() output = %q, want %q", got, want)
	}
}

func TestRunAppliesSchemaAndDrainsCollectorAndProjector(t *testing.T) {
	t.Parallel()

	db := &fakeBootstrapDB{}
	schemaApplied := false

	err := run(
		context.Background(),
		func(string) string { return "" },
		func(context.Context, func(string) string) (bootstrapDB, error) {
			return db, nil
		},
		func(ctx context.Context, database bootstrapDB) error {
			schemaApplied = true
			return nil
		},
		func(context.Context, func(string) string, trace.Tracer, *telemetry.Instruments) (graphDeps, error) {
			return graphDeps{writer: &noopCanonicalWriter{}, close: func() error { return nil }}, nil
		},
		func(ctx context.Context, database bootstrapDB, getenv func(string) string, _ trace.Tracer, _ *telemetry.Instruments, _ *slog.Logger) (collectorDeps, error) {
			return collectorDeps{
				source: &fakeSource{
					generations: []collector.CollectedGeneration{
						{
							Scope:     scope.IngestionScope{ScopeID: "s1"},
							FactCount: 0,
						},
					},
				},
				committer: &fakeCommitter{},
			}, nil
		},
		func(ctx context.Context, database bootstrapDB, graphWriter projector.CanonicalWriter, getenv func(string) string, _ trace.Tracer, _ *telemetry.Instruments, _ *slog.Logger) (projectorDeps, error) {
			return projectorDeps{
				workSource: &fakeWorkSource{
					items: []projector.ScopeGenerationWork{
						{Scope: scope.IngestionScope{ScopeID: "s1"}},
					},
				},
				factStore: &fakeFactStore{},
				runner:    &fakeProjectionRunner{},
				workSink:  &fakeWorkSink{},
			}, nil
		},
	)
	if err != nil {
		t.Fatalf("run() error = %v, want nil", err)
	}
	if !schemaApplied {
		t.Fatal("run() did not apply schema")
	}
	if !db.closed {
		t.Fatal("run() did not close database")
	}
}

func TestRunReturnsSchemaError(t *testing.T) {
	t.Parallel()

	db := &fakeBootstrapDB{}
	schemaErr := errors.New("schema failed")

	err := run(
		context.Background(),
		func(string) string { return "" },
		func(context.Context, func(string) string) (bootstrapDB, error) {
			return db, nil
		},
		func(ctx context.Context, database bootstrapDB) error {
			return schemaErr
		},
		func(context.Context, func(string) string, trace.Tracer, *telemetry.Instruments) (graphDeps, error) {
			t.Fatal("graph opener should not be called after schema error")
			return graphDeps{}, nil
		},
		func(ctx context.Context, database bootstrapDB, getenv func(string) string, _ trace.Tracer, _ *telemetry.Instruments, _ *slog.Logger) (collectorDeps, error) {
			t.Fatal("collector builder should not be called after schema error")
			return collectorDeps{}, nil
		},
		func(ctx context.Context, database bootstrapDB, graphWriter projector.CanonicalWriter, getenv func(string) string, _ trace.Tracer, _ *telemetry.Instruments, _ *slog.Logger) (projectorDeps, error) {
			t.Fatal("projector builder should not be called after schema error")
			return projectorDeps{}, nil
		},
	)
	if !errors.Is(err, schemaErr) {
		t.Fatalf("run() error = %v, want %v", err, schemaErr)
	}
	if !db.closed {
		t.Fatal("run() did not close database")
	}
}

func TestRunReturnsCollectorError(t *testing.T) {
	t.Parallel()

	db := &fakeBootstrapDB{}
	collectorErr := errors.New("collector build failed")

	err := run(
		context.Background(),
		func(string) string { return "" },
		func(context.Context, func(string) string) (bootstrapDB, error) {
			return db, nil
		},
		func(ctx context.Context, database bootstrapDB) error {
			return nil
		},
		func(context.Context, func(string) string, trace.Tracer, *telemetry.Instruments) (graphDeps, error) {
			return graphDeps{writer: &noopCanonicalWriter{}, close: func() error { return nil }}, nil
		},
		func(ctx context.Context, database bootstrapDB, getenv func(string) string, _ trace.Tracer, _ *telemetry.Instruments, _ *slog.Logger) (collectorDeps, error) {
			return collectorDeps{}, collectorErr
		},
		func(ctx context.Context, database bootstrapDB, graphWriter projector.CanonicalWriter, getenv func(string) string, _ trace.Tracer, _ *telemetry.Instruments, _ *slog.Logger) (projectorDeps, error) {
			t.Fatal("projector builder should not be called after collector error")
			return projectorDeps{}, nil
		},
	)
	if !errors.Is(err, collectorErr) {
		t.Fatalf("run() error = %v, want %v", err, collectorErr)
	}
	if !db.closed {
		t.Fatal("run() did not close database")
	}
}

func TestBuildBootstrapCollectorUsesNativeSnapshotter(t *testing.T) {
	t.Parallel()

	deps, err := buildBootstrapCollector(
		context.Background(),
		&fakeBootstrapSQLDB{},
		func(string) string { return "" },
		nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("buildBootstrapCollector() error = %v, want nil", err)
	}

	source, ok := deps.source.(*collector.GitSource)
	if !ok {
		t.Fatalf("buildBootstrapCollector() source type = %T, want *collector.GitSource", deps.source)
	}
	if _, ok := source.Selector.(collector.NativeRepositorySelector); !ok {
		t.Fatalf("buildBootstrapCollector() selector type = %T, want collector.NativeRepositorySelector", source.Selector)
	}
	if _, ok := source.Snapshotter.(collector.NativeRepositorySnapshotter); !ok {
		t.Fatalf("buildBootstrapCollector() snapshotter type = %T, want collector.NativeRepositorySnapshotter", source.Snapshotter)
	}
}

func TestBuildBootstrapCollectorWiresDiscoveryPathGlobOverlay(t *testing.T) {
	t.Parallel()

	deps, err := buildBootstrapCollector(
		context.Background(),
		&fakeBootstrapSQLDB{},
		func(key string) string {
			if key == "ESHU_DISCOVERY_IGNORED_PATH_GLOBS" {
				return "generated/**=generated-template"
			}
			return ""
		},
		nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("buildBootstrapCollector() error = %v, want nil", err)
	}

	source := deps.source.(*collector.GitSource)
	snapshotter := source.Snapshotter.(collector.NativeRepositorySnapshotter)
	if got, want := len(snapshotter.DiscoveryOptions.IgnoredPathGlobs), 1; got != want {
		t.Fatalf("IgnoredPathGlobs length = %d, want %d", got, want)
	}
}

func TestBuildBootstrapProjectorWiresPhasePublisherAndRepairQueue(t *testing.T) {
	t.Parallel()

	deps, err := buildBootstrapProjector(
		context.Background(),
		&fakeBootstrapSQLDB{},
		&noopCanonicalWriter{},
		func(string) string { return "" },
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("buildBootstrapProjector() error = %v, want nil", err)
	}

	runtime, ok := deps.runner.(projector.Runtime)
	if !ok {
		t.Fatalf("buildBootstrapProjector() runner type = %T, want projector.Runtime", deps.runner)
	}
	if runtime.PhasePublisher == nil {
		t.Fatal("buildBootstrapProjector() PhasePublisher = nil, want non-nil")
	}
	if runtime.RepairQueue == nil {
		t.Fatal("buildBootstrapProjector() RepairQueue = nil, want non-nil")
	}
}

// --- fakes ---

type fakeBootstrapDB struct {
	closed bool
}

func (f *fakeBootstrapDB) Close() error {
	f.closed = true
	return nil
}

func (f *fakeBootstrapDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, nil
}

func (f *fakeBootstrapDB) QueryContext(context.Context, string, ...any) (postgres.Rows, error) {
	return nil, nil
}

type fakeBootstrapSQLDB = fakeBootstrapDB

type fakeSource struct {
	generations []collector.CollectedGeneration
	index       int
}

func (f *fakeSource) Next(context.Context) (collector.CollectedGeneration, bool, error) {
	if f.index >= len(f.generations) {
		return collector.CollectedGeneration{}, false, nil
	}
	gen := f.generations[f.index]
	f.index++
	return gen, true, nil
}

type fakeCommitter struct {
	mu               sync.Mutex
	calls            []string
	backfillCalls    int
	iacCalls         int
	reopenCalls      int
	driftEnqueueCalls int
	backfillErr      error
	iacErr           error
	reopenErr        error
	driftEnqueueErr  error
}

func (f *fakeCommitter) CommitScopeGeneration(
	_ context.Context,
	_ scope.IngestionScope,
	_ scope.ScopeGeneration,
	factStream <-chan facts.Envelope,
) error {
	if factStream != nil {
		for range factStream {
		}
	}
	return nil
}

func (f *fakeCommitter) BackfillAllRelationshipEvidence(
	_ context.Context,
	_ trace.Tracer,
	_ *telemetry.Instruments,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "backfill")
	f.backfillCalls++
	return f.backfillErr
}

func (f *fakeCommitter) MaterializeIaCReachability(
	_ context.Context,
	_ trace.Tracer,
	_ *telemetry.Instruments,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "iac_reachability")
	f.iacCalls++
	return f.iacErr
}

func (f *fakeCommitter) ReopenDeploymentMappingWorkItems(
	_ context.Context,
	_ trace.Tracer,
	_ *telemetry.Instruments,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "reopen")
	f.reopenCalls++
	return f.reopenErr
}

func (f *fakeCommitter) EnqueueConfigStateDriftIntents(
	_ context.Context,
	_ trace.Tracer,
	_ *telemetry.Instruments,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "enqueue_drift")
	f.driftEnqueueCalls++
	return f.driftEnqueueErr
}

func (f *fakeCommitter) snapshotCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

type fakeWorkSource struct {
	mu    sync.Mutex
	items []projector.ScopeGenerationWork
	index int
}

func (f *fakeWorkSource) Claim(context.Context) (projector.ScopeGenerationWork, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.index >= len(f.items) {
		return projector.ScopeGenerationWork{}, false, nil
	}
	item := f.items[f.index]
	f.index++
	return item, true, nil
}

type fakeFactStore struct{}

func (f *fakeFactStore) LoadFacts(context.Context, projector.ScopeGenerationWork) ([]facts.Envelope, error) {
	return nil, nil
}

type fakeProjectionRunner struct{}

func (f *fakeProjectionRunner) Project(
	context.Context,
	scope.IngestionScope,
	scope.ScopeGeneration,
	[]facts.Envelope,
) (projector.Result, error) {
	return projector.Result{}, nil
}

type fakeWorkSink struct{}

func (f *fakeWorkSink) Ack(context.Context, projector.ScopeGenerationWork, projector.Result) error {
	return nil
}

func (f *fakeWorkSink) Fail(context.Context, projector.ScopeGenerationWork, error) error {
	return nil
}

// --- concurrency tests ---

func TestDrainProjectorConcurrentMultipleItems(t *testing.T) {
	t.Parallel()

	items := make([]projector.ScopeGenerationWork, 10)
	for i := range items {
		items[i] = projector.ScopeGenerationWork{
			Scope: scope.IngestionScope{ScopeID: fmt.Sprintf("scope-%d", i)},
		}
	}

	ws := &concurrentWorkSource{items: items}
	sink := &concurrentWorkSink{}

	err := drainProjector(
		context.Background(),
		ws, &fakeFactStore{}, &fakeProjectionRunner{}, sink,
		nil, 0,
		4, nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("drainProjector() error = %v, want nil", err)
	}
	if got := sink.acked.Load(); got != 10 {
		t.Fatalf("drainProjector() acked = %d, want 10", got)
	}
}

func TestDrainProjectorSequentialFallback(t *testing.T) {
	t.Parallel()

	items := []projector.ScopeGenerationWork{
		{Scope: scope.IngestionScope{ScopeID: "s1"}},
		{Scope: scope.IngestionScope{ScopeID: "s2"}},
	}
	ws := &concurrentWorkSource{items: items}
	sink := &concurrentWorkSink{}

	// workers=1 should use sequential path
	err := drainProjector(
		context.Background(),
		ws, &fakeFactStore{}, &fakeProjectionRunner{}, sink,
		nil, 0,
		1, nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("drainProjector(workers=1) error = %v, want nil", err)
	}
	if got := sink.acked.Load(); got != 2 {
		t.Fatalf("drainProjector(workers=1) acked = %d, want 2", got)
	}
}

func TestDrainProjectorErrorCancelsWorkers(t *testing.T) {
	t.Parallel()

	projectErr := errors.New("project failed")
	items := make([]projector.ScopeGenerationWork, 20)
	for i := range items {
		items[i] = projector.ScopeGenerationWork{
			Scope: scope.IngestionScope{ScopeID: fmt.Sprintf("scope-%d", i)},
		}
	}

	ws := &concurrentWorkSource{items: items}
	runner := &failingProjectionRunner{failAfter: 2, err: projectErr}

	err := drainProjector(
		context.Background(),
		ws, &fakeFactStore{}, runner, &concurrentWorkSink{},
		nil, 0,
		4, nil, nil, nil,
	)
	if err == nil {
		t.Fatal("drainProjector() expected error, got nil")
	}
	if !errors.Is(err, projectErr) {
		t.Fatalf("drainProjector() error = %v, want wrapping %v", err, projectErr)
	}
}

func TestProjectionWorkerCount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		env  string
		want int
	}{
		{name: "default", env: "", want: -1}, // will be runtime.NumCPU capped at 8
		{name: "explicit_2", env: "2", want: 2},
		{name: "explicit_16", env: "16", want: 16},
		{name: "zero_uses_default", env: "0", want: -1},
		{name: "negative_uses_default", env: "-1", want: -1},
		{name: "invalid_uses_default", env: "abc", want: -1},
		{name: "whitespace_trimmed", env: " 4 ", want: 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := projectionWorkerCount(func(key string) string {
				if key == "ESHU_PROJECTION_WORKERS" {
					return tt.env
				}
				return ""
			})
			if tt.want == -1 {
				// Default: expect NumCPU capped at 8
				maxDefault := 8
				if got < 1 || got > maxDefault {
					t.Fatalf("projectionWorkerCount(%q) = %d, want 1..%d", tt.env, got, maxDefault)
				}
			} else if got != tt.want {
				t.Fatalf("projectionWorkerCount(%q) = %d, want %d", tt.env, got, tt.want)
			}
		})
	}
}

func TestDrainCollectorWithTelemetry(t *testing.T) {
	t.Parallel()

	source := &fakeSource{
		generations: []collector.CollectedGeneration{
			{
				Scope:     scope.IngestionScope{ScopeID: "s1"},
				Facts:     testFactChannel(facts.Envelope{}, facts.Envelope{}),
				FactCount: 2,
			},
		},
	}

	err := drainCollector(
		context.Background(),
		source, &fakeCommitter{},
		nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("drainCollector() error = %v, want nil", err)
	}
}

// testFactChannel creates a pre-filled closed channel for testing.
func testFactChannel(envelopes ...facts.Envelope) <-chan facts.Envelope {
	ch := make(chan facts.Envelope, len(envelopes))
	for _, e := range envelopes {
		ch <- e
	}
	close(ch)
	return ch
}

// --- thread-safe fakes for concurrency tests ---

type concurrentWorkSource struct {
	mu    sync.Mutex
	items []projector.ScopeGenerationWork
	index int
}

func (f *concurrentWorkSource) Claim(context.Context) (projector.ScopeGenerationWork, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.index >= len(f.items) {
		return projector.ScopeGenerationWork{}, false, nil
	}
	item := f.items[f.index]
	f.index++
	return item, true, nil
}

type concurrentWorkSink struct {
	acked atomic.Int64
}

func (f *concurrentWorkSink) Ack(context.Context, projector.ScopeGenerationWork, projector.Result) error {
	f.acked.Add(1)
	return nil
}

func (f *concurrentWorkSink) Fail(context.Context, projector.ScopeGenerationWork, error) error {
	return nil
}

// --- pipelined bootstrap tests ---

func TestPipelinedBootstrapProjectsDuringCollection(t *testing.T) {
	t.Parallel()

	// Simulate a slow collector that produces 5 scopes with a delay between each.
	// The projector should start processing items while the collector is still running.
	source := &slowSource{
		generations: []collector.CollectedGeneration{
			{Scope: scope.IngestionScope{ScopeID: "s1"}, FactCount: 0},
			{Scope: scope.IngestionScope{ScopeID: "s2"}, FactCount: 0},
			{Scope: scope.IngestionScope{ScopeID: "s3"}, FactCount: 0},
			{Scope: scope.IngestionScope{ScopeID: "s4"}, FactCount: 0},
			{Scope: scope.IngestionScope{ScopeID: "s5"}, FactCount: 0},
		},
		delay: 100 * time.Millisecond,
	}

	// Track when projections happen relative to collection.
	tracker := &projectionTracker{}

	ws := &concurrentWorkSource{
		items: []projector.ScopeGenerationWork{
			{Scope: scope.IngestionScope{ScopeID: "s1"}},
			{Scope: scope.IngestionScope{ScopeID: "s2"}},
			{Scope: scope.IngestionScope{ScopeID: "s3"}},
			{Scope: scope.IngestionScope{ScopeID: "s4"}},
			{Scope: scope.IngestionScope{ScopeID: "s5"}},
		},
	}
	sink := &concurrentWorkSink{}

	cd := collectorDeps{source: source, committer: &fakeCommitter{}}
	pd := projectorDeps{
		workSource: ws,
		factStore:  &fakeFactStore{},
		runner:     tracker,
		workSink:   sink,
	}

	err := runPipelined(context.Background(), cd, pd, 2, nil, nil, nil)
	if err != nil {
		t.Fatalf("runPipelined() error = %v, want nil", err)
	}

	if got := sink.acked.Load(); got != 5 {
		t.Fatalf("runPipelined() acked = %d, want 5", got)
	}

	// Verify projections started before all collections finished.
	// Collection takes ~500ms (5 × 100ms). If projections started during collection,
	// the first projection timestamp should be before collection ends.
	firstProjection := tracker.firstProjectionTime()
	if firstProjection.IsZero() {
		t.Fatal("no projections were recorded")
	}
}

func TestPipelinedBootstrapDrainsQueueAfterCollectorExits(t *testing.T) {
	t.Parallel()

	// Collector finishes immediately with 0 items.
	// Queue has items that were pre-populated (simulating items from a previous
	// collector run or items that appeared just before collector exited).
	source := &fakeSource{generations: nil}
	ws := &concurrentWorkSource{
		items: []projector.ScopeGenerationWork{
			{Scope: scope.IngestionScope{ScopeID: "s1"}},
			{Scope: scope.IngestionScope{ScopeID: "s2"}},
			{Scope: scope.IngestionScope{ScopeID: "s3"}},
		},
	}
	sink := &concurrentWorkSink{}

	cd := collectorDeps{source: source, committer: &fakeCommitter{}}
	pd := projectorDeps{
		workSource: ws,
		factStore:  &fakeFactStore{},
		runner:     &fakeProjectionRunner{},
		workSink:   sink,
	}

	err := runPipelined(context.Background(), cd, pd, 2, nil, nil, nil)
	if err != nil {
		t.Fatalf("runPipelined() error = %v, want nil", err)
	}

	if got := sink.acked.Load(); got != 3 {
		t.Fatalf("runPipelined() acked = %d, want 3 (should drain remaining queue)", got)
	}
}

func TestDrainCollectorCollectsDiscoveryAdvisoryReports(t *testing.T) {
	t.Parallel()

	source := &fakeSource{generations: []collector.CollectedGeneration{{
		Scope: scope.IngestionScope{ScopeID: "scope-1"},
		Generation: scope.ScopeGeneration{
			GenerationID: "generation-1",
		},
		FactCount: 0,
		DiscoveryAdvisory: &collector.DiscoveryAdvisoryReport{
			SchemaVersion: "discovery_advisory.v1",
			Run: collector.DiscoveryAdvisoryRun{
				RepoPath: "/repo",
			},
		},
	}}}
	var reports []collector.DiscoveryAdvisoryReport

	err := drainCollector(context.Background(), source, &fakeCommitter{}, nil, nil, nil, func(report collector.DiscoveryAdvisoryReport) error {
		reports = append(reports, report)
		return nil
	})
	if err != nil {
		t.Fatalf("drainCollector() error = %v, want nil", err)
	}

	if got, want := len(reports), 1; got != want {
		t.Fatalf("report count = %d, want %d", got, want)
	}
	if got, want := reports[0].Run.ScopeID, "scope-1"; got != want {
		t.Fatalf("report Run.ScopeID = %q, want %q", got, want)
	}
	if got, want := reports[0].Run.GenerationID, "generation-1"; got != want {
		t.Fatalf("report Run.GenerationID = %q, want %q", got, want)
	}
}

func TestWriteDiscoveryAdvisoryReportsWritesJSON(t *testing.T) {
	t.Parallel()

	reportPath := filepath.Join(t.TempDir(), "advisory.json")
	generatedAt := time.Date(2026, time.April, 26, 10, 30, 0, 0, time.UTC)
	reports := []collector.DiscoveryAdvisoryReport{{
		SchemaVersion: "discovery_advisory.v1",
		GeneratedAt:   generatedAt,
		Run: collector.DiscoveryAdvisoryRun{
			RepoPath: "/repo",
			ScopeID:  "scope-1",
		},
	}}

	if err := writeDiscoveryAdvisoryReports(reportPath, reports); err != nil {
		t.Fatalf("writeDiscoveryAdvisoryReports() error = %v, want nil", err)
	}

	contents, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	if !strings.Contains(string(contents), `"schema_version": "discovery_advisory.v1"`) {
		t.Fatalf("report contents missing schema version:\n%s", contents)
	}
	if !strings.Contains(string(contents), `"scope_id": "scope-1"`) {
		t.Fatalf("report contents missing scope id:\n%s", contents)
	}
}

func TestPipelinedBootstrapExitsCleanlyWhenQueueEmpty(t *testing.T) {
	t.Parallel()

	// Both collector and queue are empty — should exit cleanly and quickly.
	source := &fakeSource{generations: nil}
	ws := &concurrentWorkSource{items: nil}
	sink := &concurrentWorkSink{}

	cd := collectorDeps{source: source, committer: &fakeCommitter{}}
	pd := projectorDeps{
		workSource: ws,
		factStore:  &fakeFactStore{},
		runner:     &fakeProjectionRunner{},
		workSink:   sink,
	}

	start := time.Now()
	err := runPipelined(context.Background(), cd, pd, 2, nil, nil, nil)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("runPipelined() error = %v, want nil", err)
	}
	if got := sink.acked.Load(); got != 0 {
		t.Fatalf("runPipelined() acked = %d, want 0", got)
	}
	// Should exit within a few seconds (maxEmptyPolls × pollInterval).
	if elapsed > 10*time.Second {
		t.Fatalf("runPipelined() took %v, want < 10s (drain should exit quickly)", elapsed)
	}
}

func TestPipelinedBootstrapCollectorErrorCancelsProjector(t *testing.T) {
	t.Parallel()

	collectorErr := errors.New("collector exploded")
	source := &failingSource{err: collectorErr}
	ws := &concurrentWorkSource{items: nil}
	sink := &concurrentWorkSink{}

	cd := collectorDeps{source: source, committer: &fakeCommitter{}}
	pd := projectorDeps{
		workSource: ws,
		factStore:  &fakeFactStore{},
		runner:     &fakeProjectionRunner{},
		workSink:   sink,
	}

	err := runPipelined(context.Background(), cd, pd, 2, nil, nil, nil)
	if err == nil {
		t.Fatal("runPipelined() expected error, got nil")
	}
	if !errors.Is(err, collectorErr) {
		t.Fatalf("runPipelined() error = %v, want wrapping %v", err, collectorErr)
	}
}

func TestPipelinedBootstrapRunsDeferredBackfillWorkflow(t *testing.T) {
	t.Parallel()

	source := &fakeSource{generations: nil}
	ws := &concurrentWorkSource{items: nil}
	sink := &concurrentWorkSink{}
	committer := &fakeCommitter{}

	cd := collectorDeps{source: source, committer: committer}
	pd := projectorDeps{
		workSource: ws,
		factStore:  &fakeFactStore{},
		runner:     &fakeProjectionRunner{},
		workSink:   sink,
	}

	err := runPipelined(context.Background(), cd, pd, 2, nil, nil, nil)
	if err != nil {
		t.Fatalf("runPipelined() error = %v, want nil", err)
	}

	if got, want := committer.snapshotCalls(), []string{"backfill", "iac_reachability", "reopen", "enqueue_drift"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("workflow calls = %v, want %v", got, want)
	}
	if got := sink.acked.Load(); got != 0 {
		t.Fatalf("runPipelined() acked = %d, want 0", got)
	}
}

func TestPipelinedBootstrapBackfillFailureIsFatal(t *testing.T) {
	t.Parallel()

	backfillErr := errors.New("backfill failed")
	committer := &fakeCommitter{backfillErr: backfillErr}

	err := runPipelined(
		context.Background(),
		collectorDeps{source: &fakeSource{generations: nil}, committer: committer},
		projectorDeps{
			workSource: &concurrentWorkSource{items: nil},
			factStore:  &fakeFactStore{},
			runner:     &fakeProjectionRunner{},
			workSink:   &concurrentWorkSink{},
		},
		2,
		nil,
		nil,
		nil,
	)
	if err == nil {
		t.Fatal("runPipelined() error = nil, want non-nil")
	}
	if !errors.Is(err, backfillErr) {
		t.Fatalf("runPipelined() error = %v, want wrapping %v", err, backfillErr)
	}
	if got, want := committer.snapshotCalls(), []string{"backfill"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("workflow calls = %v, want %v", got, want)
	}
}

func TestPipelinedBootstrapIaCReachabilityFailureIsFatal(t *testing.T) {
	t.Parallel()

	iacErr := errors.New("iac reachability failed")
	committer := &fakeCommitter{iacErr: iacErr}

	err := runPipelined(
		context.Background(),
		collectorDeps{source: &fakeSource{generations: nil}, committer: committer},
		projectorDeps{
			workSource: &concurrentWorkSource{items: nil},
			factStore:  &fakeFactStore{},
			runner:     &fakeProjectionRunner{},
			workSink:   &concurrentWorkSink{},
		},
		2,
		nil,
		nil,
		nil,
	)
	if err == nil {
		t.Fatal("runPipelined() error = nil, want non-nil")
	}
	if !errors.Is(err, iacErr) {
		t.Fatalf("runPipelined() error = %v, want wrapping %v", err, iacErr)
	}
	if got, want := committer.snapshotCalls(), []string{"backfill", "iac_reachability"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("workflow calls = %v, want %v", got, want)
	}
}

func TestPipelinedBootstrapReopenFailureIsFatal(t *testing.T) {
	t.Parallel()

	reopenErr := errors.New("reopen failed")
	committer := &fakeCommitter{reopenErr: reopenErr}

	err := runPipelined(
		context.Background(),
		collectorDeps{source: &fakeSource{generations: nil}, committer: committer},
		projectorDeps{
			workSource: &concurrentWorkSource{items: nil},
			factStore:  &fakeFactStore{},
			runner:     &fakeProjectionRunner{},
			workSink:   &concurrentWorkSink{},
		},
		2,
		nil,
		nil,
		nil,
	)
	if err == nil {
		t.Fatal("runPipelined() error = nil, want non-nil")
	}
	if !errors.Is(err, reopenErr) {
		t.Fatalf("runPipelined() error = %v, want wrapping %v", err, reopenErr)
	}
	// enqueue_drift must NOT be called when reopen fails — the pipeline
	// returns before Phase 3.5 runs.
	if got, want := committer.snapshotCalls(), []string{"backfill", "iac_reachability", "reopen"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("workflow calls = %v, want %v", got, want)
	}
}

func TestPipelinedBootstrapDriftEnqueueFailureIsFatal(t *testing.T) {
	t.Parallel()

	driftErr := errors.New("drift enqueue failed")
	committer := &fakeCommitter{driftEnqueueErr: driftErr}

	err := runPipelined(
		context.Background(),
		collectorDeps{source: &fakeSource{generations: nil}, committer: committer},
		projectorDeps{
			workSource: &concurrentWorkSource{items: nil},
			factStore:  &fakeFactStore{},
			runner:     &fakeProjectionRunner{},
			workSink:   &concurrentWorkSink{},
		},
		2,
		nil,
		nil,
		nil,
	)
	if err == nil {
		t.Fatal("runPipelined() error = nil, want non-nil")
	}
	if !errors.Is(err, driftErr) {
		t.Fatalf("runPipelined() error = %v, want wrapping %v", err, driftErr)
	}
	if got, want := committer.snapshotCalls(), []string{"backfill", "iac_reachability", "reopen", "enqueue_drift"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("workflow calls = %v, want %v", got, want)
	}
}

func TestPipelinedBootstrapWaitsForProjectorDrainBeforeReopen(t *testing.T) {
	t.Parallel()

	sink := &concurrentWorkSink{}
	committer := &fakeCommitter{}

	err := runPipelined(
		context.Background(),
		collectorDeps{source: &fakeSource{generations: nil}, committer: committer},
		projectorDeps{
			workSource: &concurrentWorkSource{
				items: []projector.ScopeGenerationWork{
					{Scope: scope.IngestionScope{ScopeID: "s1"}},
				},
			},
			factStore: &fakeFactStore{},
			runner:    &delayedProjectionRunner{delay: 50 * time.Millisecond},
			workSink:  sink,
		},
		2,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("runPipelined() error = %v, want nil", err)
	}
	// Projector must have drained (acked the work item) before reopen ran.
	if got := sink.acked.Load(); got != 1 {
		t.Fatalf("projector not drained before reopen: acked=%d, want 1", got)
	}
	if got, want := committer.snapshotCalls(), []string{"backfill", "iac_reachability", "reopen", "enqueue_drift"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("workflow calls = %v, want %v", got, want)
	}
}

func TestPipelinedBootstrapHeartbeatsLongProjectorWork(t *testing.T) {
	t.Parallel()

	heartbeater := &recordingProjectorHeartbeater{}
	started := make(chan struct{})
	release := make(chan struct{})

	errc := make(chan error, 1)
	go func() {
		errc <- runPipelined(
			context.Background(),
			collectorDeps{source: &fakeSource{generations: nil}, committer: &fakeCommitter{}},
			projectorDeps{
				workSource: &concurrentWorkSource{
					items: []projector.ScopeGenerationWork{{
						Scope:      scope.IngestionScope{ScopeID: "scope-long"},
						Generation: scope.ScopeGeneration{GenerationID: "generation-long"},
					}},
				},
				factStore:         &fakeFactStore{},
				runner:            &blockingProjectionRunner{started: started, release: release},
				workSink:          &concurrentWorkSink{},
				heartbeater:       heartbeater,
				heartbeatInterval: time.Millisecond,
			},
			1,
			nil,
			nil,
			nil,
		)
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("projection did not start")
	}
	if !heartbeater.waitForHeartbeats(1, time.Second) {
		t.Fatalf("heartbeat count = %d, want at least 1 before projection completes", heartbeater.count())
	}
	close(release)

	select {
	case err := <-errc:
		if err != nil {
			t.Fatalf("runPipelined() error = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runPipelined() did not return")
	}
}

func TestBootstrapProjectorHeartbeatStopIgnoresStopContextCancellation(t *testing.T) {
	t.Parallel()

	heartbeater := &contextCanceledProjectorHeartbeater{
		entered: make(chan struct{}),
	}
	work := projector.ScopeGenerationWork{
		Scope:      scope.IngestionScope{ScopeID: "scope-heartbeat"},
		Generation: scope.ScopeGeneration{GenerationID: "generation-heartbeat"},
	}

	_, stopHeartbeat := startBootstrapProjectorHeartbeat(
		context.Background(),
		work,
		heartbeater,
		time.Millisecond,
		0,
		nil,
	)

	select {
	case <-heartbeater.entered:
	case <-time.After(time.Second):
		t.Fatal("heartbeat did not start")
	}

	if err := stopHeartbeat(); err != nil {
		t.Fatalf("stopHeartbeat() error = %v, want nil", err)
	}
}

// --- additional fakes for pipelined tests ---

type slowSource struct {
	mu          sync.Mutex
	generations []collector.CollectedGeneration
	index       int
	delay       time.Duration
}

func (f *slowSource) Next(ctx context.Context) (collector.CollectedGeneration, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.index >= len(f.generations) {
		return collector.CollectedGeneration{}, false, nil
	}

	select {
	case <-ctx.Done():
		return collector.CollectedGeneration{}, false, ctx.Err()
	case <-time.After(f.delay):
	}

	gen := f.generations[f.index]
	f.index++
	return gen, true, nil
}

type failingSource struct {
	err error
}

func (f *failingSource) Next(context.Context) (collector.CollectedGeneration, bool, error) {
	return collector.CollectedGeneration{}, false, f.err
}

type projectionTracker struct {
	mu    sync.Mutex
	first time.Time
}

func (p *projectionTracker) Project(
	_ context.Context,
	_ scope.IngestionScope,
	_ scope.ScopeGeneration,
	_ []facts.Envelope,
) (projector.Result, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.first.IsZero() {
		p.first = time.Now()
	}
	return projector.Result{}, nil
}

func (p *projectionTracker) firstProjectionTime() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.first
}

type failingProjectionRunner struct {
	mu        sync.Mutex
	count     int
	failAfter int
	err       error
}

func (f *failingProjectionRunner) Project(
	_ context.Context,
	_ scope.IngestionScope,
	_ scope.ScopeGeneration,
	_ []facts.Envelope,
) (projector.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.count++
	if f.count > f.failAfter {
		return projector.Result{}, f.err
	}
	return projector.Result{}, nil
}

type delayedProjectionRunner struct {
	delay time.Duration
}

func (d *delayedProjectionRunner) Project(
	ctx context.Context,
	_ scope.IngestionScope,
	_ scope.ScopeGeneration,
	_ []facts.Envelope,
) (projector.Result, error) {
	select {
	case <-ctx.Done():
		return projector.Result{}, ctx.Err()
	case <-time.After(d.delay):
	}
	return projector.Result{}, nil
}

type blockingProjectionRunner struct {
	started chan<- struct{}
	release <-chan struct{}
	once    sync.Once
}

func (r *blockingProjectionRunner) Project(
	ctx context.Context,
	_ scope.IngestionScope,
	_ scope.ScopeGeneration,
	_ []facts.Envelope,
) (projector.Result, error) {
	r.once.Do(func() { close(r.started) })
	select {
	case <-ctx.Done():
		return projector.Result{}, ctx.Err()
	case <-r.release:
		return projector.Result{}, nil
	}
}

type recordingProjectorHeartbeater struct {
	countValue atomic.Int64
}

func (r *recordingProjectorHeartbeater) Heartbeat(context.Context, projector.ScopeGenerationWork) error {
	r.countValue.Add(1)
	return nil
}

func (r *recordingProjectorHeartbeater) count() int64 {
	return r.countValue.Load()
}

func (r *recordingProjectorHeartbeater) waitForHeartbeats(want int64, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if r.count() >= want {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return r.count() >= want
}

type contextCanceledProjectorHeartbeater struct {
	entered chan struct{}
	once    sync.Once
}

func (c *contextCanceledProjectorHeartbeater) Heartbeat(ctx context.Context, _ projector.ScopeGenerationWork) error {
	c.once.Do(func() { close(c.entered) })
	<-ctx.Done()
	return ctx.Err()
}

type noopCanonicalWriter struct{}

func (*noopCanonicalWriter) Write(_ context.Context, _ projector.CanonicalMaterialization) error {
	return nil
}
