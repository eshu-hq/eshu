// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package costcounting_test

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
	"github.com/eshu-hq/eshu/go/internal/replay/offlinetier"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// cassetteRelPath is the committed nested-directory cassette exercised by the
// R-5 offline replay tier. The cost-counting gate uses the same cassette so the
// two gates share one reviewable corpus.
var cassetteRelPath = filepath.Join(
	"..", "..", "..", "..", "testdata", "cassettes", "replayoffline", "nested-directory-tree.json",
)

// budgetRelPath is the per-scenario cost budget committed alongside the cassette.
// It records the maximum allowed value for each asserted instrument so a
// regression (N+1, quadratic fan-out) exceeds the budget and fails the gate.
var budgetRelPath = filepath.Join(
	"..", "..", "..", "..", "testdata", "cassettes", "replayoffline", "nested-directory-tree.cost-budget.json",
)

// costBudget is the committed per-scenario cost budget.
type costBudget struct {
	Scenario    string           `json:"scenario"`
	Cassette    string           `json:"cassette"`
	Description string           `json:"description"`
	RefreshPath string           `json:"refresh_path"`
	Budgets     map[string]int64 `json:"budgets"`
}

// loadBudget reads the committed cost budget from the JSON file next to the
// cassette. It delegates to loadBudgetFrom (cost_scenario_helpers_test.go),
// the path-parameterized loader shared with the semantic-entity and
// documentation-edges cost scenarios, which have no cassette of their own.
func loadBudget(t *testing.T) costBudget {
	t.Helper()
	return loadBudgetFrom(t, budgetRelPath)
}

// countingExecutor is an in-memory Executor that records each Execute call.
// It implements only cypher.Executor (not GroupExecutor or PhaseGroupExecutor)
// so the canonical writer routes through its sequential path, which is the
// most statement-verbose path and gives the strictest count signal.
//
// The counting executor records no side-effects beyond the counter. A
// production cypher.CanonicalNodeWriter driven through this executor executes
// real phase logic (buildPhases, annotateCanonicalWritePhases, etc.) and
// records real eshu_dp_* instrument values — the only thing that differs from
// the live tier is that the graph backend is absent and no Cypher runs.
type countingExecutor struct {
	n atomic.Int64
}

// Execute records one statement and returns nil (no backend error).
func (e *countingExecutor) Execute(_ context.Context, _ cypher.Statement) error {
	e.n.Add(1)
	return nil
}

// count returns the total number of Execute calls recorded.
func (e *countingExecutor) count() int64 { return e.n.Load() }

// newInstrumentedWriter builds:
//
//  1. A sdkmetric.NewManualReader() + MeterProvider so that instrument values
//     can be collected synchronously after the write without a background
//     exporter.
//  2. telemetry.NewInstruments(meter) — the production instrument registry,
//     which registers eshu_dp_canonical_atomic_writes_total and every other
//     eshu_dp_* counter against the real meter.
//  3. A countingExecutor to record statement counts.
//  4. cypher.NewCanonicalNodeWriter(executor, batchSize, instruments) — the
//     production writer that will call instruments.CanonicalAtomicWrites.Add on
//     every recordAtomicWrite event.
//
// The returned reader is used to collect instrument values after Write returns.
func newInstrumentedWriter(t *testing.T) (
	writer *cypher.CanonicalNodeWriter,
	exec *countingExecutor,
	reader *sdkmetric.ManualReader,
) {
	t.Helper()

	reader = sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	meter := provider.Meter("costcounting")

	inst, err := telemetry.NewInstruments(meter)
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}

	exec = &countingExecutor{}
	writer = cypher.NewCanonicalNodeWriter(exec, 500, inst)
	return writer, exec, reader
}

// collectCounter reads one named Int64 counter sum from a Collect snapshot.
// It returns 0 if the metric is not present (not yet incremented) so the caller
// can still compare against the budget without a fatal error.
func collectCounter(rm metricdata.ResourceMetrics, name string) int64 {
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				return 0
			}
			var total int64
			for _, dp := range sum.DataPoints {
				total += dp.Value
			}
			return total
		}
	}
	return 0
}

// loadCassetteMaterialization loads the committed cassette and returns the
// CanonicalMaterialization for its single scope. It is the same helper used by
// the R-5 offline replay tier (offlinetier.MaterializationFromGeneration).
func loadCassetteMaterialization(t *testing.T) projector.CanonicalMaterialization {
	t.Helper()

	src, err := cassette.NewSource(cassetteRelPath)
	if err != nil {
		t.Fatalf("load cassette %s: %v", cassetteRelPath, err)
	}
	gen, ok, err := src.Next(context.Background())
	if err != nil {
		t.Fatalf("read cassette generation: %v", err)
	}
	if !ok {
		t.Fatal("cassette yielded no generation")
	}
	mat, err := offlinetier.MaterializationFromGeneration(gen)
	if err != nil {
		t.Fatalf("build materialization from cassette: %v", err)
	}
	return mat
}

// TestCostBudget_NestedDirectoryTree is the positive cost-counting gate for
// the nested-directory-tree scenario. It drives the production
// cypher.CanonicalNodeWriter over the committed cassette materialization using
// a real sdkmetric.ManualReader-backed telemetry.Instruments, then asserts
// that each eshu_dp_* instrument value is within the committed budget.
//
// Instrument read: eshu_dp_canonical_atomic_writes_total. This counter is
// incremented inside cypher.CanonicalNodeWriter.recordAtomicWrite — once per
// non-empty canonical write phase PLUS once for the overall sequential write.
// For the nested-directory cassette (1 repository + 3 directories,
// FirstGeneration=true) the non-empty phases are repository, directories, and
// directory_edges, giving 3 phase events plus 1 overall = 4 increments. The
// committed budget is exactly 4 — the deterministic count — so any increase
// (an N+1 or an extra-phase regression) trips the gate and forces a deliberate
// budget refresh; the N+1 negative control in TestCostBudget_N1_ExceedsBudget
// emits 12, proving the gate has teeth.
//
// This test runs in every `go test ./internal/replay/...` pass without a
// graph backend, Docker, or network access.
func TestCostBudget_NestedDirectoryTree(t *testing.T) {
	t.Parallel()

	budget := loadBudget(t)
	mat := loadCassetteMaterialization(t)

	writer, exec, reader := newInstrumentedWriter(t)

	if err := writer.Write(context.Background(), mat); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	// PRIMARY assertion: read eshu_dp_canonical_atomic_writes_total off the
	// real otel reader. This counter is incremented by the production
	// canonical writer, NOT by a hand-counted statement slice.
	atomicWrites := collectCounter(rm, "eshu_dp_canonical_atomic_writes_total")
	maxWrites, ok := budget.Budgets["eshu_dp_canonical_atomic_writes_total"]
	if !ok {
		// Unconditional guard: a missing key must fail loudly, never silently
		// skip the primary assertion and the false-green guard below.
		t.Fatal("budget missing required key eshu_dp_canonical_atomic_writes_total")
	}
	if atomicWrites > maxWrites {
		t.Fatalf(
			"eshu_dp_canonical_atomic_writes_total = %d exceeds budget %d "+
				"(scenario=%s): algorithmic regression detected",
			atomicWrites, maxWrites, budget.Scenario,
		)
	}
	if atomicWrites == 0 {
		t.Fatal("eshu_dp_canonical_atomic_writes_total = 0: instrument not recording (false green guard)")
	}

	// SECONDARY assertion: raw statement count from the counting executor.
	// This is a secondary diagnostic signal, not the primary budget gate.
	stmts := exec.count()
	if maxStmts, ok := budget.Budgets["statements_executed"]; ok {
		if stmts > maxStmts {
			t.Fatalf(
				"statements_executed = %d exceeds budget %d "+
					"(scenario=%s): too many Cypher write operations",
				stmts, maxStmts, budget.Scenario,
			)
		}
		if stmts == 0 {
			t.Fatal("statements_executed = 0: executor not recording (false green guard)")
		}
	}

	t.Logf(
		"scenario=%s cassette=%s "+
			"eshu_dp_canonical_atomic_writes_total=%d (budget=%d) "+
			"statements_executed=%d (budget=%d)",
		budget.Scenario, budget.Cassette,
		atomicWrites, budget.Budgets["eshu_dp_canonical_atomic_writes_total"],
		stmts, budget.Budgets["statements_executed"],
	)
}

// TestCostBudget_N1_ExceedsBudget is the mandatory negative control. It builds
// a deliberately N+1 projection by calling writer.Write once per directory item
// in a loop, rather than once for the whole materialization. This is the classic
// N+1 anti-pattern: write cost scales linearly with input size instead of
// staying constant.
//
// The test asserts that the accumulated eshu_dp_canonical_atomic_writes_total
// from N separate Write calls EXCEEDS the committed budget for the scenario. If
// this assertion passes (i.e., the count is indeed over budget), the gate has
// proven it would catch the regression. If it fails (count is within budget),
// the budget is too loose and must be tightened.
//
// This is a REAL negative control, not a tautology:
//   - The N+1 variant drives the SAME production writer as the positive test.
//   - It reads the SAME eshu_dp_canonical_atomic_writes_total instrument off
//     the SAME real otel reader.
//   - Removing the budget assertion from TestCostBudget_NestedDirectoryTree
//     (or setting budget to MaxInt64) would make this test report
//     "budget too loose" and fail, proving the gate has teeth.
func TestCostBudget_N1_ExceedsBudget(t *testing.T) {
	t.Parallel()

	budget := loadBudget(t)
	mat := loadCassetteMaterialization(t)

	writer, _, reader := newInstrumentedWriter(t)

	// The N+1 control only exceeds the single-write budget when there are at
	// least 2 directories (each per-dir Write costs the same as the whole-batch
	// write). Guard so a future single-directory cassette fails with a clear
	// message instead of a misleading "budget too loose".
	if len(mat.Directories) < 2 {
		t.Fatalf("N+1 control needs >=2 directories to exceed the budget; cassette has %d", len(mat.Directories))
	}

	// N+1 projection: call Write once per directory instead of once for all.
	// Each call emits a full write cycle (retract phase + repository + per-dir
	// phases), so eshu_dp_canonical_atomic_writes_total accumulates N times
	// what a correct single-batch write would produce.
	for _, dir := range mat.Directories {
		perDirMat := projector.CanonicalMaterialization{
			ScopeID:         mat.ScopeID,
			GenerationID:    mat.GenerationID,
			RepoID:          mat.RepoID,
			RepoPath:        mat.RepoPath,
			FirstGeneration: mat.FirstGeneration,
			Repository:      mat.Repository,
			Directories:     []projector.DirectoryRow{dir},
		}
		if err := writer.Write(context.Background(), perDirMat); err != nil {
			t.Fatalf("N+1 Write() error = %v", err)
		}
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	atomicWrites := collectCounter(rm, "eshu_dp_canonical_atomic_writes_total")
	maxWrites, ok := budget.Budgets["eshu_dp_canonical_atomic_writes_total"]
	if !ok {
		t.Fatal("budget has no eshu_dp_canonical_atomic_writes_total entry")
	}

	// The N+1 projection MUST exceed the budget. If it does not, either the
	// budget is too loose to catch real regressions or the negative control
	// is not generating enough writes. Both are gate failures.
	if atomicWrites <= maxWrites {
		t.Fatalf(
			"N+1 negative control: eshu_dp_canonical_atomic_writes_total = %d "+
				"did NOT exceed budget %d — budget is too loose to catch N+1 regressions "+
				"or the negative control is generating too few writes; tighten the budget "+
				"or increase the N+1 fanout",
			atomicWrites, maxWrites,
		)
	}

	t.Logf(
		"N+1 negative control passed: eshu_dp_canonical_atomic_writes_total = %d > budget %d "+
			"(N=%d directories, scenario=%s)",
		atomicWrites, maxWrites, len(mat.Directories), budget.Scenario,
	)
}
