// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nornicdb

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// probeReader scripts a probed bare-label drain: each probe returns the next
// scripted element ID (or none), and each drain reports the next scripted
// __drained count. It records every call so tests can assert the sequence.
type probeReader struct {
	probeIDs []string // "" means the probe finds nothing
	drained  []int64
	probeErr error
	drainErr error
	calls    []probeCall
	probes   int
	drains   int
}

type probeCall struct {
	kind   string
	cypher string
	params map[string]any
}

// RunProbe records and answers the bounded existence probe (#6822): the
// production drain loop now routes the probe through this method, not
// RunWrite, so gate/timeout wrappers can label it separately from a drain.
func (r *probeReader) RunProbe(_ context.Context, cypher string, params map[string]any) (DrainWriteResult, error) {
	r.calls = append(r.calls, probeCall{kind: "probe", cypher: cypher, params: params})
	if r.probeErr != nil {
		return DrainWriteResult{}, r.probeErr
	}
	id := ""
	if r.probes < len(r.probeIDs) {
		id = r.probeIDs[r.probes]
	}
	r.probes++
	if id == "" {
		return DrainWriteResult{}, nil
	}
	return DrainWriteResult{Rows: []map[string]any{{"__id": id}}}, nil
}

// RunWrite records and answers one drain iteration. It no longer branches on
// probe cypher text: the probe is routed through RunProbe instead.
func (r *probeReader) RunWrite(_ context.Context, cypher string, params map[string]any) (DrainWriteResult, error) {
	r.calls = append(r.calls, probeCall{kind: "drain", cypher: cypher, params: params})
	if r.drainErr != nil {
		return DrainWriteResult{}, r.drainErr
	}
	var drained int64
	if r.drains < len(r.drained) {
		drained = r.drained[r.drains]
	}
	r.drains++
	return DrainWriteResult{
		Rows:                 []map[string]any{{"__drained": drained}},
		NodesDeleted:         drained,
		RelationshipsDeleted: 2 * drained,
	}, nil
}

func (r *probeReader) kinds() []string {
	out := make([]string, 0, len(r.calls))
	for _, c := range r.calls {
		out = append(out, c.kind)
	}
	return out
}

func bareLabelRetract() sourcecypher.Statement {
	return sourcecypher.Statement{
		Operation: sourcecypher.OperationCanonicalRetract,
		Cypher: "MATCH (n:Function)\n" +
			"WHERE n.repo_id = $repo_id AND n.evidence_source = 'projector/canonical' AND n.generation_id <> $generation_id\n" +
			"DETACH DELETE n",
		Parameters: map[string]any{"repo_id": "repo-1", "generation_id": "gen-2"},
		Drain:      true,
		DrainVar:   "n",
	}
}

// TestExecuteDrainLoopProbesOnceThenDrainsBacklog proves a bare-label retract
// probes once and, when the probe finds a node, runs the unchanged
// single-statement drain loop until it drains nothing (#6822). The probe is
// not repeated per batch: on large labels it costs a label scan.
func TestExecuteDrainLoopProbesOnceThenDrainsBacklog(t *testing.T) {
	t.Parallel()

	reader := &probeReader{probeIDs: []string{"4:a:1"}, drained: []int64{2, 1, 0}}
	executor := PhaseGroupExecutor{RetractBatchSize: 2, DrainReader: reader}

	if err := executor.executeDrainLoop(context.Background(), bareLabelRetract(), 1, 1, "retract"); err != nil {
		t.Fatalf("executeDrainLoop() error = %v, want nil", err)
	}
	if got, want := reader.kinds(), []string{"probe", "drain", "drain", "drain"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("call sequence = %v, want %v", got, want)
	}
	probe := reader.calls[0]
	if !strings.Contains(probe.cypher, "WITH n ORDER BY elementId(n) LIMIT 1") || probe.params["repo_id"] != "repo-1" {
		t.Fatalf("probe = %q params %v, want the bounded LIMIT 1 read with the statement parameters", probe.cypher, probe.params)
	}
	drain := reader.calls[1]
	if !strings.Contains(drain.cypher, "WITH n ORDER BY elementId(n) LIMIT $__retract_batch") ||
		!strings.Contains(drain.cypher, "n.generation_id <> $generation_id") ||
		!strings.Contains(drain.cypher, "DETACH DELETE n") {
		t.Fatalf("drain cypher is not the predicate-checking single statement:\n%s", drain.cypher)
	}
	if drain.params["__retract_batch"] != int64(2) || drain.params["generation_id"] != "gen-2" {
		t.Fatalf("drain params = %v, want batch 2 plus the statement parameters", drain.params)
	}
}

// TestExecuteDrainLoopNoMatchRunsOneProbe proves the common case — a retract
// with nothing to delete — finishes after a single read and never issues the
// DETACH DELETE, which costs a whole-store scan on NornicDB v1.3.3 even when
// nothing matches (#6822).
func TestExecuteDrainLoopNoMatchRunsOneProbe(t *testing.T) {
	t.Parallel()

	reader := &probeReader{}
	executor := PhaseGroupExecutor{DrainReader: reader}

	if err := executor.executeDrainLoop(context.Background(), bareLabelRetract(), 1, 1, "retract"); err != nil {
		t.Fatalf("executeDrainLoop() error = %v, want nil", err)
	}
	if got, want := reader.kinds(), []string{"probe"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("call sequence = %v, want %v", got, want)
	}
}

// malformedProbeReader returns a probe row whose __id is missing or not a
// string, then scripts drains like probeReader.
type malformedProbeReader struct {
	probeReader
	row map[string]any
}

// RunProbe overrides the embedded probeReader.RunProbe to always return the
// malformed row; RunWrite (drain-only) is inherited unchanged.
func (r *malformedProbeReader) RunProbe(_ context.Context, cypher string, params map[string]any) (DrainWriteResult, error) {
	r.calls = append(r.calls, probeCall{kind: "probe", cypher: cypher, params: params})
	return DrainWriteResult{Rows: []map[string]any{r.row}}, nil
}

// TestExecuteDrainLoopDrainsWhenProbeRowIsMalformed keeps the probe fail
// closed: any returned row means something matched, so a row without a usable
// __id must still run the drain instead of being reported as probe_skipped.
func TestExecuteDrainLoopDrainsWhenProbeRowIsMalformed(t *testing.T) {
	t.Parallel()

	for name, row := range map[string]map[string]any{
		"missing id":    {},
		"non-string id": {"__id": int64(7)},
		"empty id":      {"__id": ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			reader := &malformedProbeReader{row: row, probeReader: probeReader{drained: []int64{3, 0}}}
			executor := PhaseGroupExecutor{DrainReader: reader}

			if err := executor.executeDrainLoop(context.Background(), bareLabelRetract(), 1, 1, "retract"); err != nil {
				t.Fatalf("executeDrainLoop() error = %v, want nil", err)
			}
			if got, want := reader.kinds(), []string{"probe", "drain", "drain"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("call sequence = %v, want %v", got, want)
			}
		})
	}
}

// TestExecuteDrainLoopSucceedsWhenMatchesVanishBeforeDrain proves a node that
// another writer removed or refreshed between the probe and the drain is not
// a failure: the drain rechecks its WHERE clause, deletes nothing, and the
// loop ends.
func TestExecuteDrainLoopSucceedsWhenMatchesVanishBeforeDrain(t *testing.T) {
	t.Parallel()

	reader := &probeReader{probeIDs: []string{"4:a:1"}, drained: []int64{0}}
	executor := PhaseGroupExecutor{DrainReader: reader}

	if err := executor.executeDrainLoop(context.Background(), bareLabelRetract(), 1, 1, "retract"); err != nil {
		t.Fatalf("executeDrainLoop() error = %v, want nil", err)
	}
	if got, want := reader.kinds(), []string{"probe", "drain"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("call sequence = %v, want %v", got, want)
	}
}

// TestExecuteDrainLoopPropagatesDrainErrors keeps a failed drain visible to
// the caller with the statement context. A failed probe deliberately does not
// propagate: it falls through to the drain, which
// TestExecuteDrainLoopDrainsWhenProbeFails covers.
func TestExecuteDrainLoopPropagatesDrainErrors(t *testing.T) {
	t.Parallel()

	reader := &probeReader{probeIDs: []string{"4:a:1"}, drainErr: errors.New("bolt: connection reset")}
	executor := PhaseGroupExecutor{DrainReader: reader}
	err := executor.executeDrainLoop(context.Background(), bareLabelRetract(), 2, 3, "retract")
	if err == nil || !strings.Contains(err.Error(), "connection reset") || !strings.Contains(err.Error(), "2/3") {
		t.Fatalf("executeDrainLoop() error = %v, want the drain error with statement 2/3 context", err)
	}
}

// TestExecuteDrainLoopDrainsWhenProbeFails keeps the ProbeExecutor fail-safe
// contract (go/internal/storage/cypher/writer.go): a failed probe means
// "unknown", never "zero rows", so the drain runs unconditionally and a
// probe-only failure cannot fail a projection whose delete would succeed.
func TestExecuteDrainLoopDrainsWhenProbeFails(t *testing.T) {
	t.Parallel()

	reader := &probeReader{probeErr: errors.New("nornicdb: probe timed out"), drained: []int64{4, 0}}
	executor := PhaseGroupExecutor{DrainReader: reader}
	if err := executor.executeDrainLoop(context.Background(), bareLabelRetract(), 1, 1, "retract"); err != nil {
		t.Fatalf("executeDrainLoop() error = %v, want nil (probe failure must fall through to the drain)", err)
	}
	if got, want := reader.kinds(), []string{"probe", "drain", "drain"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("call sequence = %v, want %v", got, want)
	}
}

// TestExecuteDrainLoopProbedEnforcesSafetyCap proves the probed loop keeps
// the drain node ceiling.
func TestExecuteDrainLoopProbedEnforcesSafetyCap(t *testing.T) {
	t.Parallel()

	drained := make([]int64, 10)
	for i := range drained {
		drained[i] = 1
	}
	reader := &probeReader{probeIDs: []string{fmt.Sprintf("4:a:%d", 1)}, drained: drained}
	// 5,000,000/2,500,000 + 2 = 4 iterations allowed.
	executor := PhaseGroupExecutor{RetractBatchSize: 2_500_000, DrainReader: reader}

	err := executor.executeDrainLoop(context.Background(), bareLabelRetract(), 1, 1, "retract")
	if err == nil || !strings.Contains(err.Error(), "safety cap exceeded after 4 iterations") {
		t.Fatalf("executeDrainLoop() error = %v, want the drain safety cap error", err)
	}
}

// TestExecuteDrainLoopProbedRecordsDriftRetractions proves the probed path
// still feeds eshu_dp_reconciliation_drift_retractions_total with the summed
// node and relationship deletes.
func TestExecuteDrainLoopProbedRecordsDriftRetractions(t *testing.T) {
	t.Parallel()

	meterReader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(meterReader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	reader := &probeReader{probeIDs: []string{"4:a:1"}, drained: []int64{3, 1, 0}}
	executor := PhaseGroupExecutor{DrainReader: reader, Instruments: instruments}
	stmt := bareLabelRetract()
	stmt.Parameters[sourcecypher.StatementMetadataReconciliationDriftKey] = true
	stmt.Parameters[sourcecypher.StatementMetadataPhaseKey] = "retract"

	if err := executor.executeDrainLoop(context.Background(), stmt, 1, 1, "retract"); err != nil {
		t.Fatalf("executeDrainLoop() error = %v, want nil", err)
	}
	var rm metricdata.ResourceMetrics
	if err := meterReader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	got := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_reconciliation_drift_retractions_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s data = %T, want Sum[int64]", m.Name, m.Data)
			}
			for _, point := range sum.DataPoints {
				kind, _ := point.Attributes.Value("kind")
				got[kind.AsString()] += point.Value
			}
		}
	}
	if got["node"] != 4 || got["edge"] != 8 {
		t.Fatalf("drift retractions = %v, want node=4 edge=8", got)
	}
}

// TestExecuteDrainLoopKeepsUnprobedDrainForAnchoredRetract proves
// relationship-anchored retracts keep the one-statement drain with no probe.
func TestExecuteDrainLoopKeepsUnprobedDrainForAnchoredRetract(t *testing.T) {
	t.Parallel()

	reader := &probeReader{}
	executor := PhaseGroupExecutor{DrainReader: reader}
	stmt := sourcecypher.Statement{
		Operation: sourcecypher.OperationCanonicalRetract,
		Cypher: "MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)\n" +
			"WHERE f.repo_id = $repo_id AND f.evidence_source = 'projector/canonical' AND f.generation_id <> $generation_id\n" +
			"DETACH DELETE f",
		Parameters: map[string]any{"repo_id": "repo-1", "generation_id": "gen-2"},
		Drain:      true,
		DrainVar:   "f",
	}

	if err := executor.executeDrainLoop(context.Background(), stmt, 1, 1, "retract"); err != nil {
		t.Fatalf("executeDrainLoop() error = %v, want nil", err)
	}
	if got, want := reader.kinds(), []string{"drain"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("call sequence = %v, want %v", got, want)
	}
	if !strings.Contains(reader.calls[0].cypher, "WITH f LIMIT $__retract_batch") {
		t.Fatalf("anchored drain cypher changed:\n%s", reader.calls[0].cypher)
	}
}
