// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// #7324: the path-conflict retirement (canonicalNodeRepositoryPathCleanupCypher)
// is the one statement contract A lets DETACH DELETE a Repository, and it
// drops every relationship on the retired node. These hermetic tests pin how
// the writer reports it from the executor's write summary on every executor
// path. The live half is TestLiveRepositoryPathConflictRetirementReportsDroppedIncomingEdges
// in internal/reducer.

const retirementMetric = "eshu_dp_canonical_repository_retirements_total"

// retirementReportingExecutor reports cleanupCounters for the path-conflict
// statement (and zero counters for every other statement) through
// ReportWriteCounts, as the production Bolt executors do. reportsPerStatement
// > 1 simulates a driver retry of a managed transaction function, which
// re-runs and re-reports every statement before the final commit.
type retirementReportingExecutor struct {
	cleanupCounters     WriteCounters
	reportsPerStatement int
	failCleanup         bool
	// failAfterCleanup fails the repository upsert, the statement right after
	// the cleanup, so the cleanup has already reported its counters when the
	// group fails and rolls back.
	failAfterCleanup bool
	statements       int
}

func (e *retirementReportingExecutor) report(ctx context.Context, stmt Statement) error {
	e.statements++
	if stmt.Cypher == canonicalNodeRepositoryPathCleanupCypher && e.failCleanup {
		return errors.New("injected cleanup failure")
	}
	if stmt.Cypher == canonicalNodeRepositoryUpsertCypher && e.failAfterCleanup {
		return errors.New("injected failure after the cleanup reported")
	}
	counters := WriteCounters{}
	if stmt.Cypher == canonicalNodeRepositoryPathCleanupCypher {
		counters = e.cleanupCounters
	}
	reports := e.reportsPerStatement
	if reports < 1 {
		reports = 1
	}
	for i := 0; i < reports; i++ {
		ReportWriteCounts(ctx, stmt.Cypher, stmt.Parameters, counters)
	}
	return nil
}

func (e *retirementReportingExecutor) Execute(ctx context.Context, stmt Statement) error {
	return e.report(ctx, stmt)
}

type retirementGroupExecutor struct{ retirementReportingExecutor }

func (e *retirementGroupExecutor) ExecuteGroup(ctx context.Context, stmts []Statement) error {
	for _, stmt := range stmts {
		if err := e.report(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

type retirementPhaseGroupExecutor struct{ retirementReportingExecutor }

func (e *retirementPhaseGroupExecutor) ExecutePhaseGroup(ctx context.Context, stmts []Statement) error {
	for _, stmt := range stmts {
		if err := e.report(ctx, SanitizeStatement(stmt)); err != nil {
			return err
		}
	}
	return nil
}

// retirementHarness captures the writer's JSON logs and metrics.
type retirementHarness struct {
	logs        *bytes.Buffer
	reader      *metric.ManualReader
	instruments *telemetry.Instruments
}

func newRetirementHarness(t *testing.T) retirementHarness {
	t.Helper()
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	reader := metric.NewManualReader()
	instruments, err := telemetry.NewInstruments(metric.NewMeterProvider(metric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	return retirementHarness{logs: &logs, reader: reader, instruments: instruments}
}

func (h retirementHarness) retiredLogs(t *testing.T) []map[string]any {
	t.Helper()
	return h.logsWithMessage(t, "canonical repository retired")
}

func (h retirementHarness) logsWithMessage(t *testing.T, message string) []map[string]any {
	t.Helper()
	var entries []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(h.logs.Bytes()), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal(line, &entry); err != nil {
			t.Fatalf("decode log line %q: %v", line, err)
		}
		if entry["msg"] == message {
			entries = append(entries, entry)
		}
	}
	return entries
}

// counter returns the retirement counter for outcome, 0 when absent.
func (h retirementHarness) counter(t *testing.T, outcome string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := h.reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	var total int64
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != retirementMetric {
				continue
			}
			for _, point := range m.Data.(metricdata.Sum[int64]).DataPoints {
				if v, ok := point.Attributes.Value(telemetry.MetricDimensionOutcome); ok && v.AsString() == outcome {
					total += point.Value
				}
			}
		}
	}
	return total
}

func retirementMaterialization(first, delta bool) canonical.CanonicalMaterialization {
	return canonical.CanonicalMaterialization{
		ScopeID: "scope-1", GenerationID: "gen-2", RepoID: "repository:r_new", RepoPath: "/repos/service",
		FirstGeneration: first, DeltaProjection: delta,
		Repository: &canonical.RepositoryRow{RepoID: "repository:r_new", Name: "service", Path: "/repos/service", LocalPath: "/repos/service"},
		Files: []canonical.FileRow{{
			Path: "/repos/service/main.go", RelativePath: "main.go", Name: "main.go", Language: "go", RepoID: "repository:r_new",
		}},
	}
}

func assertRetirementLog(t *testing.T, entry map[string]any, want map[string]any) {
	t.Helper()
	base := map[string]any{
		"scope_id": "scope-1", "generation_id": "gen-2", "repo_id": "repository:r_new", "path": "/repos/service",
	}
	for key, value := range want {
		base[key] = value
	}
	for key, value := range base {
		if entry[key] != value {
			t.Fatalf("retirement log %s = %#v, want %#v; entry = %v", key, entry[key], value, entry)
		}
	}
}

func TestCanonicalNodeWriterReportsRetirementThatDroppedRelationshipsOnceAcrossDriverRetries(t *testing.T) {
	h := newRetirementHarness(t)
	exec := &retirementGroupExecutor{retirementReportingExecutor{
		cleanupCounters:     WriteCounters{NodesDeleted: 1, RelationshipsDeleted: 20},
		reportsPerStatement: 2,
	}}
	if err := NewCanonicalNodeWriter(exec, 500, h.instruments).Write(context.Background(), retirementMaterialization(false, false)); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	logs := h.retiredLogs(t)
	if len(logs) != 1 {
		t.Fatalf("retirement log lines = %d, want 1; logs = %s", len(logs), h.logs.String())
	}
	// The last attempt's counters, not the sum of the retried attempts.
	assertRetirementLog(t, logs[0], map[string]any{
		"level": "WARN", "nodes_deleted": float64(1), "relationships_deleted": float64(20),
		"deletes_counted": true, "outcome": telemetry.RepositoryRetirementOutcomeDroppedRelationships,
	})
	if got := h.counter(t, telemetry.RepositoryRetirementOutcomeDroppedRelationships); got != 1 {
		t.Fatalf("%s{outcome=dropped_relationships} = %d, want 1", retirementMetric, got)
	}
	if got := h.counter(t, telemetry.RepositoryRetirementOutcomeClean); got != 0 {
		t.Fatalf("%s{outcome=clean} = %d, want 0", retirementMetric, got)
	}
}

func TestCanonicalNodeWriterReportsCleanRetirementOnThePhaseGroupPath(t *testing.T) {
	h := newRetirementHarness(t)
	exec := &retirementPhaseGroupExecutor{retirementReportingExecutor{
		cleanupCounters: WriteCounters{NodesDeleted: 1},
	}}
	if err := NewCanonicalNodeWriter(exec, 500, h.instruments).Write(context.Background(), retirementMaterialization(false, false)); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	logs := h.retiredLogs(t)
	if len(logs) != 1 {
		t.Fatalf("retirement log lines = %d, want 1; logs = %s", len(logs), h.logs.String())
	}
	assertRetirementLog(t, logs[0], map[string]any{
		"level": "INFO", "nodes_deleted": float64(1), "relationships_deleted": float64(0),
		"deletes_counted": true, "outcome": telemetry.RepositoryRetirementOutcomeClean,
	})
	if got := h.counter(t, telemetry.RepositoryRetirementOutcomeClean); got != 1 {
		t.Fatalf("%s{outcome=clean} = %d, want 1", retirementMetric, got)
	}
}

func TestCanonicalNodeWriterRecordsNothingForSteadyStateRetirementOnTheSequentialPath(t *testing.T) {
	h := newRetirementHarness(t)
	exec := &retirementReportingExecutor{}
	if err := NewCanonicalNodeWriter(exec, 500, h.instruments).Write(context.Background(), retirementMaterialization(false, false)); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if logs := h.retiredLogs(t); len(logs) != 0 {
		t.Fatalf("retirement log lines = %d, want 0 for a retirement that matched no node", len(logs))
	}
	if logs := h.logsWithMessage(t, "canonical repository retirement not counted: executor reported no write summary"); len(logs) != 0 {
		t.Fatalf("uncounted lines = %d, want 0: the executor did report a summary", len(logs))
	}
	if got := h.counter(t, telemetry.RepositoryRetirementOutcomeClean) +
		h.counter(t, telemetry.RepositoryRetirementOutcomeDroppedRelationships); got != 0 {
		t.Fatalf("%s = %d, want 0", retirementMetric, got)
	}
}

// With no write summary nothing is known, so the writer must not claim a
// retirement: no `canonical repository retired` line at any level, only a
// distinct DEBUG line, and no counter.
func TestCanonicalNodeWriterSaysDeletesWereNotCountedWithoutAWriteSummary(t *testing.T) {
	h := newRetirementHarness(t)
	if err := NewCanonicalNodeWriter(&mockExecutor{}, 500, h.instruments).Write(context.Background(), retirementMaterialization(false, false)); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if logs := h.retiredLogs(t); len(logs) != 0 {
		t.Fatalf("`canonical repository retired` lines = %d, want 0 when no summary was reported: %v", len(logs), logs)
	}
	logs := h.logsWithMessage(t, "canonical repository retirement not counted: executor reported no write summary")
	if len(logs) != 1 {
		t.Fatalf("uncounted-retirement DEBUG lines = %d, want 1; logs = %s", len(logs), h.logs.String())
	}
	assertRetirementLog(t, logs[0], map[string]any{"level": "DEBUG", "deletes_counted": false})
	if _, ok := logs[0]["relationships_deleted"]; ok {
		t.Fatalf("uncounted retirement log carries relationships_deleted: %v", logs[0])
	}
	if got := h.counter(t, telemetry.RepositoryRetirementOutcomeClean) +
		h.counter(t, telemetry.RepositoryRetirementOutcomeDroppedRelationships); got != 0 {
		t.Fatalf("%s = %d, want 0 when deletes were not counted", retirementMetric, got)
	}
}

func TestCanonicalNodeWriterDoesNotReportRetirementWithoutTheCleanupPhase(t *testing.T) {
	for name, mat := range map[string]canonical.CanonicalMaterialization{
		"first generation": retirementMaterialization(true, false),
		"delta generation": retirementMaterialization(false, true),
	} {
		t.Run(name, func(t *testing.T) {
			h := newRetirementHarness(t)
			exec := &retirementGroupExecutor{retirementReportingExecutor{cleanupCounters: WriteCounters{NodesDeleted: 1}}}
			writer := NewCanonicalNodeWriter(exec, 500, h.instruments)
			if cleanup := writer.buildRepositoryCleanupStatements(mat); len(cleanup) != 0 {
				t.Fatalf("repository cleanup statements = %d, want 0 for a %s", len(cleanup), name)
			}
			if err := writer.Write(context.Background(), mat); err != nil {
				t.Fatalf("Write() error = %v", err)
			}
			if logs := h.retiredLogs(t); len(logs) != 0 {
				t.Fatalf("retirement log lines = %d, want 0 for a %s", len(logs), name)
			}
		})
	}
}

func TestCanonicalNodeWriterDoesNotReportAFailedRetirement(t *testing.T) {
	h := newRetirementHarness(t)
	exec := &retirementPhaseGroupExecutor{retirementReportingExecutor{
		cleanupCounters: WriteCounters{NodesDeleted: 1, RelationshipsDeleted: 3}, failCleanup: true,
	}}
	if err := NewCanonicalNodeWriter(exec, 500, h.instruments).Write(context.Background(), retirementMaterialization(false, false)); err == nil {
		t.Fatal("Write() error = nil, want the injected cleanup failure")
	}
	if logs := h.retiredLogs(t); len(logs) != 0 {
		t.Fatalf("retirement log lines = %d, want 0 for a cleanup that did not commit", len(logs))
	}
}

// On the atomic path the cleanup runs inside the main group. When a later
// statement fails, the group rolls back, so the cleanup's already-reported
// counters describe a delete that never committed and must not be reported.
func TestCanonicalNodeWriterDoesNotReportARolledBackAtomicRetirement(t *testing.T) {
	h := newRetirementHarness(t)
	exec := &retirementGroupExecutor{retirementReportingExecutor{
		cleanupCounters: WriteCounters{NodesDeleted: 1, RelationshipsDeleted: 3}, failAfterCleanup: true,
	}}
	if err := NewCanonicalNodeWriter(exec, 500, h.instruments).Write(context.Background(), retirementMaterialization(false, false)); err == nil {
		t.Fatal("Write() error = nil, want the injected failure after the cleanup")
	}
	if exec.statements < 2 {
		t.Fatalf("executed statements = %d; the cleanup must have run and reported before the failure", exec.statements)
	}
	if logs := h.retiredLogs(t); len(logs) != 0 {
		t.Fatalf("retirement log lines = %d, want 0 for a rolled-back atomic group", len(logs))
	}
	if got := h.counter(t, telemetry.RepositoryRetirementOutcomeClean) +
		h.counter(t, telemetry.RepositoryRetirementOutcomeDroppedRelationships); got != 0 {
		t.Fatalf("%s = %d, want 0 for a rolled-back atomic group", retirementMetric, got)
	}
}

// The retirement collector sits below any collector the caller stashed (the
// #6783 differential recorder) and must not hide a single statement from it.
func TestCanonicalNodeWriterRetirementCaptureForwardsEveryEntryToTheCallersCollector(t *testing.T) {
	h := newRetirementHarness(t)
	exec := &retirementGroupExecutor{retirementReportingExecutor{cleanupCounters: WriteCounters{NodesDeleted: 1}}}
	parent := NewWriteCountsCollector()
	ctx := WithWriteCountsCollector(context.Background(), parent)
	if err := NewCanonicalNodeWriter(exec, 500, h.instruments).Write(ctx, retirementMaterialization(false, false)); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if got := len(parent.Entries()); got != exec.statements || got == 0 {
		t.Fatalf("caller's collector entries = %d, want every executed statement (%d)", got, exec.statements)
	}
	if got := h.counter(t, telemetry.RepositoryRetirementOutcomeClean); got != 1 {
		t.Fatalf("%s{outcome=clean} = %d, want 1", retirementMetric, got)
	}
}
