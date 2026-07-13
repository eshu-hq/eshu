// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package costcounting_test

import (
	"context"
	"encoding/json"
	"os"
	"sync/atomic"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// loadBudgetFrom reads a committed cost budget from an arbitrary path, unlike
// loadBudget (cost_counting_test.go) which is pinned to the nested-directory
// scenario's budget file. The semantic-entity and documentation-edges cost
// scenarios have no committed cassette (their production writers operate over
// flat reducer rows, not a CanonicalMaterialization), so their budgets live
// next to the cassette-driven ones under testdata/cassettes/replayoffline/
// with an explanatory "cassette" field instead of a real cassette path.
func loadBudgetFrom(t *testing.T, path string) costBudget {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read budget file %s: %v", path, err)
	}
	var b costBudget
	if err := json.Unmarshal(data, &b); err != nil {
		t.Fatalf("parse budget file %s: %v", path, err)
	}
	if len(b.Budgets) == 0 {
		t.Fatalf("budget file %s has no budget entries", path)
	}
	return b
}

// groupCountingExecutor is an in-memory Executor+GroupExecutor that records
// each Execute and ExecuteGroup call and the total statement count. It is
// shared by the semantic-entity and documentation-edges cost scenarios: each
// drives a different production writer (SemanticEntityWriter,
// EdgeWriter) over the SAME counting primitive so both scenarios prove their
// PRIMARY assertion off a real eshu_dp_* instrument recorded by production
// instrumentation layers (InstrumentedExecutor / EdgeWriter.Instruments), not
// off this executor's own counts, which are secondary diagnostics only.
type groupCountingExecutor struct {
	executeCalls atomic.Int64
	groupCalls   atomic.Int64
	stmtCount    atomic.Int64
}

// Execute records one non-grouped statement.
func (e *groupCountingExecutor) Execute(_ context.Context, _ cypher.Statement) error {
	e.executeCalls.Add(1)
	e.stmtCount.Add(1)
	return nil
}

// ExecuteGroup records one grouped-transaction call and its statement count.
func (e *groupCountingExecutor) ExecuteGroup(_ context.Context, stmts []cypher.Statement) error {
	e.groupCalls.Add(1)
	e.stmtCount.Add(int64(len(stmts)))
	return nil
}

// totalStatements returns the combined Execute + ExecuteGroup statement count.
func (e *groupCountingExecutor) totalStatements() int64 { return e.stmtCount.Load() }

// newManualReaderInstruments builds a real sdkmetric.ManualReader-backed
// telemetry.Instruments registry, the same production instrument set
// newInstrumentedWriter wires for the canonical-writer scenario, so the new
// scenarios read genuine eshu_dp_* values instead of a re-implemented
// counter.
func newManualReaderInstruments(t *testing.T) (*telemetry.Instruments, *sdkmetric.ManualReader) {
	t.Helper()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	meter := provider.Meter("costcounting")

	inst, err := telemetry.NewInstruments(meter)
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	return inst, reader
}
