package reducer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/exposure"
	"github.com/eshu-hq/eshu/go/internal/parser/interproc"
	"github.com/eshu-hq/eshu/go/internal/parser/summary"
)

type stubFunctionSummarySnapshotLoader struct {
	snapshot summary.Snapshot
}

func (l stubFunctionSummarySnapshotLoader) LoadSnapshot(context.Context) (summary.Snapshot, error) {
	return l.snapshot, nil
}

type stubFunctionSourceSnapshotLoader struct {
	sources []interproc.Source
}

func (l stubFunctionSourceSnapshotLoader) LoadSources(context.Context) ([]interproc.Source, error) {
	return l.sources, nil
}

type stubFunctionGraphIDSnapshotLoader struct {
	ids map[summary.FunctionID]string
}

func (l stubFunctionGraphIDSnapshotLoader) LoadGraphIDs(context.Context) (map[summary.FunctionID]string, error) {
	return l.ids, nil
}

type stubFunctionCloudSinkTargetLoader struct {
	targets []ValueFlowCloudSinkTarget
	ids     map[summary.FunctionID]string
}

func (l *stubFunctionCloudSinkTargetLoader) LoadCloudSinkTargets(
	_ context.Context,
	ids map[summary.FunctionID]string,
) ([]ValueFlowCloudSinkTarget, error) {
	l.ids = ids
	return l.targets, nil
}

func summarySnapshotFromEffects(effects map[summary.FunctionID]summary.Effects) summary.Snapshot {
	functions := make([]summary.SnapshotFunction, 0, len(effects))
	for id, fx := range effects {
		functions = append(functions, summary.SnapshotFunction{
			ID:      id,
			Effects: fx,
			Version: "version-" + functionName(id),
		})
	}
	return summary.Snapshot{Functions: functions}
}

func crossRepoFixpointEffects() (summary.FunctionID, summary.FunctionID, map[summary.FunctionID]summary.Effects) {
	sourceFn := summary.NewFunctionID("repo-a", "pkg", "", "handle")
	sinkFn := summary.NewFunctionID("repo-b", "pkg", "", "query")
	return sourceFn, sinkFn, map[summary.FunctionID]summary.Effects{
		sourceFn: {
			ParamToCallArg: []summary.CallArgFlow{{Callee: sinkFn, Param: 0, Arg: 0}},
		},
		sinkFn: {
			ParamToSink: []summary.ParamSink{{Param: 0, SinkKind: "sql"}},
		},
	}
}

func httpRequestSource(id summary.FunctionID) interproc.Source {
	return interproc.Source{
		Port: interproc.Port{
			Func: interproc.FunctionID(id),
			Slot: interproc.Slot{Kind: interproc.SlotParam, Index: 0},
		},
		Kind: "http_request",
	}
}

// TestValueFlowFixpointEvidenceLoaderProjectsCloudSinks proves graph-backed
// cloud sinks are loaded after FunctionID graph ids and participate in the same
// partitioned fixpoint as summary-derived sinks.
func TestValueFlowFixpointEvidenceLoaderProjectsCloudSinks(t *testing.T) {
	t.Parallel()

	sourceFn := summary.NewFunctionID("repo-a", "pkg", "", "handle")
	sinkFn := summary.NewFunctionID("repo-b", "pkg", "", "cloudAction")
	effects := map[summary.FunctionID]summary.Effects{
		sourceFn: {
			ParamToCallArg: []summary.CallArgFlow{{Callee: sinkFn, Param: 0, Arg: 0}},
		},
		sinkFn: {
			ParamToReturn: []int{0},
		},
	}
	cloudTargets := &stubFunctionCloudSinkTargetLoader{targets: []ValueFlowCloudSinkTarget{{
		FunctionID: sinkFn,
		Kind:       string(exposure.SinkIAMPrivilegedAction),
		Label:      "IAM role assumption",
	}}}
	loader := ValueFlowFixpointEvidenceLoader{
		SummarySnapshotLoader:   stubFunctionSummarySnapshotLoader{snapshot: summarySnapshotFromEffects(effects)},
		SourceSnapshotLoader:    stubFunctionSourceSnapshotLoader{sources: []interproc.Source{httpRequestSource(sourceFn)}},
		GraphIDSnapshotLoader:   stubFunctionGraphIDSnapshotLoader{ids: map[summary.FunctionID]string{sourceFn: "uid-source", sinkFn: "uid-cloud-fn"}},
		CloudSinkSnapshotLoader: cloudTargets,
	}

	inputs, err := loader.LoadCodeInterprocEvidence(context.Background(), "scope-1", "gen-1")
	if err != nil {
		t.Fatalf("LoadCodeInterprocEvidence returned error: %v", err)
	}
	if len(inputs) != 1 {
		t.Fatalf("inputs len = %d, want 1: %+v", len(inputs), inputs)
	}
	input := inputs[0]
	if !input.Cloud {
		t.Fatalf("cloud sink finding was not flagged cloud-backed: %+v", input)
	}
	if input.SourceFunctionUID != "uid-source" || input.SinkFunctionUID != "uid-cloud-fn" {
		t.Fatalf("finding endpoints not resolved through graph ids: %+v", input)
	}
	if input.SinkKind != string(exposure.SinkIAMPrivilegedAction) || input.SinkFunctionName != "cloudAction" {
		t.Fatalf("cloud sink metadata not preserved: %+v", input)
	}
	if cloudTargets.ids[sourceFn] != "uid-source" || cloudTargets.ids[sinkFn] != "uid-cloud-fn" {
		t.Fatalf("cloud sink loader did not receive graph ids: %+v", cloudTargets.ids)
	}
}

// TestValueFlowFixpointEvidenceLoaderSkipsCloudTargetsWithoutObservedPorts keeps
// function-level cloud bridge edges from fabricating value-flow precision when
// the summary store has no observed parameter port for that function.
func TestValueFlowFixpointEvidenceLoaderSkipsCloudTargetsWithoutObservedPorts(t *testing.T) {
	t.Parallel()

	sourceFn := summary.NewFunctionID("repo-a", "pkg", "", "handle")
	sinkFn := summary.NewFunctionID("repo-b", "pkg", "", "cloudAction")
	cloudTargets := &stubFunctionCloudSinkTargetLoader{targets: []ValueFlowCloudSinkTarget{{
		FunctionID: sinkFn,
		Kind:       string(exposure.SinkIAMPrivilegedAction),
		Label:      "IAM role assumption",
	}}}
	loader := ValueFlowFixpointEvidenceLoader{
		SummarySnapshotLoader:   stubFunctionSummarySnapshotLoader{snapshot: summarySnapshotFromEffects(map[summary.FunctionID]summary.Effects{})},
		SourceSnapshotLoader:    stubFunctionSourceSnapshotLoader{sources: []interproc.Source{httpRequestSource(sourceFn)}},
		GraphIDSnapshotLoader:   stubFunctionGraphIDSnapshotLoader{ids: map[summary.FunctionID]string{sourceFn: "uid-source", sinkFn: "uid-cloud-fn"}},
		CloudSinkSnapshotLoader: cloudTargets,
	}

	inputs, err := loader.LoadCodeInterprocEvidence(context.Background(), "scope-1", "gen-1")
	if err != nil {
		t.Fatalf("LoadCodeInterprocEvidence returned error: %v", err)
	}
	if len(inputs) != 0 {
		t.Fatalf("cloud target with no observed parameter port produced findings: %+v", inputs)
	}
}

// TestValueFlowFixpointEvidenceLoaderSkipsGraphWorkWithoutSources proves an
// empty source snapshot exits before graph id or graph-backed cloud sink reads.
func TestValueFlowFixpointEvidenceLoaderSkipsGraphWorkWithoutSources(t *testing.T) {
	t.Parallel()

	fn := summary.NewFunctionID("repo-a", "pkg", "", "handler")
	loader := ValueFlowFixpointEvidenceLoader{
		SummarySnapshotLoader: stubFunctionSummarySnapshotLoader{snapshot: summarySnapshotFromEffects(map[summary.FunctionID]summary.Effects{
			fn: {ParamToReturn: []int{0}},
		})},
		SourceSnapshotLoader: stubFunctionSourceSnapshotLoader{},
	}

	inputs, err := loader.LoadCodeInterprocEvidence(context.Background(), "scope-1", "gen-1")
	if err != nil {
		t.Fatalf("LoadCodeInterprocEvidence returned error: %v", err)
	}
	if len(inputs) != 0 {
		t.Fatalf("empty source snapshot produced findings: %+v", inputs)
	}
}

// TestValueFlowFixpointEvidenceLoaderProjectsDurableInputs proves durable
// summaries, sources, and graph ids are composed into reducer-ready
// TAINT_FLOWS_TO evidence.
func TestValueFlowFixpointEvidenceLoaderProjectsDurableInputs(t *testing.T) {
	t.Parallel()

	sourceFn, sinkFn, effects := crossRepoFixpointEffects()
	loader := ValueFlowFixpointEvidenceLoader{
		SummarySnapshotLoader: stubFunctionSummarySnapshotLoader{snapshot: summarySnapshotFromEffects(effects)},
		SourceSnapshotLoader:  stubFunctionSourceSnapshotLoader{sources: []interproc.Source{httpRequestSource(sourceFn)}},
		GraphIDSnapshotLoader: stubFunctionGraphIDSnapshotLoader{ids: map[summary.FunctionID]string{
			sourceFn: "uid-source",
			sinkFn:   "uid-sink",
		}},
	}

	inputs, err := loader.LoadCodeInterprocEvidence(context.Background(), "scope-1", "gen-1")
	if err != nil {
		t.Fatalf("LoadCodeInterprocEvidence returned error: %v", err)
	}
	if len(inputs) != 1 {
		t.Fatalf("inputs len = %d, want 1: %+v", len(inputs), inputs)
	}
	input := inputs[0]
	if input.SourceFunctionUID != "uid-source" || input.SinkFunctionUID != "uid-sink" {
		t.Fatalf("uids not resolved from graph map: %+v", input)
	}
	if input.SourceFunctionName != "handle" || input.SinkFunctionName != "query" {
		t.Fatalf("function names not derived from FunctionID: %+v", input)
	}
	if input.SourceKind != "http_request" || input.SinkKind != "sql" || input.Confidence <= 0 {
		t.Fatalf("finding metadata not preserved: %+v", input)
	}
	if len(input.WhyTrail) != 2 {
		t.Fatalf("why trail len = %d, want source and sink steps: %+v", len(input.WhyTrail), input.WhyTrail)
	}
	if input.WhyTrail[0]["role"] != "source" || input.WhyTrail[0]["function_uid"] != "uid-source" {
		t.Fatalf("source trail step not resolved: %+v", input.WhyTrail[0])
	}
	if input.WhyTrail[1]["role"] != "sink" || input.WhyTrail[1]["function_uid"] != "uid-sink" {
		t.Fatalf("sink trail step not resolved: %+v", input.WhyTrail[1])
	}
}

// TestValueFlowFixpointEvidenceLoaderSurfacesMissingGraphUIDs proves unresolved
// graph ids remain visible as skipped findings instead of fabricating edges.
func TestValueFlowFixpointEvidenceLoaderSurfacesMissingGraphUIDs(t *testing.T) {
	t.Parallel()

	sourceFn, sinkFn, effects := crossRepoFixpointEffects()
	loader := ValueFlowFixpointEvidenceLoader{
		SummarySnapshotLoader: stubFunctionSummarySnapshotLoader{snapshot: summarySnapshotFromEffects(effects)},
		SourceSnapshotLoader:  stubFunctionSourceSnapshotLoader{sources: []interproc.Source{httpRequestSource(sourceFn)}},
		GraphIDSnapshotLoader: stubFunctionGraphIDSnapshotLoader{ids: map[summary.FunctionID]string{
			sinkFn: "uid-sink",
		}},
	}

	inputs, err := loader.LoadCodeInterprocEvidence(context.Background(), "scope-1", "gen-1")
	if err != nil {
		t.Fatalf("LoadCodeInterprocEvidence returned error: %v", err)
	}
	if len(inputs) != 1 || inputs[0].SourceFunctionUID != "" || inputs[0].SinkFunctionUID != "uid-sink" {
		t.Fatalf("missing source uid not surfaced as unresolved input: %+v", inputs)
	}
	if rows := ExtractCodeInterprocEvidenceRows(inputs); len(rows) != 0 {
		t.Fatalf("unresolved finding projected %d graph rows, want 0", len(rows))
	}
}

// TestExtractCodeInterprocFixpointEvidenceRowsUsesSeparateUIDNamespace proves
// fixpoint rows cannot clobber direct fact rows in the graph writer's MERGE.
func TestExtractCodeInterprocFixpointEvidenceRowsUsesSeparateUIDNamespace(t *testing.T) {
	t.Parallel()

	input := sampleCodeInterprocInput()
	direct := ExtractCodeInterprocEvidenceRows([]CodeInterprocEvidenceInput{input})
	fixpoint := ExtractCodeInterprocFixpointEvidenceRows([]CodeInterprocEvidenceInput{input})
	if len(direct) != 1 || len(fixpoint) != 1 {
		t.Fatalf("rows missing: direct=%+v fixpoint=%+v", direct, fixpoint)
	}
	if direct[0]["uid"] == fixpoint[0]["uid"] {
		t.Fatalf("fixpoint row reused direct evidence uid %q", direct[0]["uid"])
	}
}

// TestValueFlowFixpointEvidenceProjectorRetractsGlobalFixpointEvidence proves
// summary-driven projection retracts the full fixpoint-owned evidence source
// before writing the global solve, rather than scope-stamping stale rows.
func TestValueFlowFixpointEvidenceProjectorRetractsGlobalFixpointEvidence(t *testing.T) {
	t.Parallel()

	writer := &recordingCodeInterprocEvidenceWriter{}
	projector := ValueFlowFixpointEvidenceProjector{
		Loader: stubCodeInterprocEvidenceLoader{inputs: []CodeInterprocEvidenceInput{sampleCodeInterprocInput()}},
		Writer: writer,
	}

	result, err := projector.ProjectValueFlowFixpointEvidence(context.Background(), "scope-1", "gen-1")
	if err != nil {
		t.Fatalf("ProjectValueFlowFixpointEvidence returned error: %v", err)
	}
	if writer.globalRetracts != 1 || writer.globalEvidence != codeInterprocFixpointEvidenceSource {
		t.Fatalf("global retract evidence = %q calls=%d, want fixpoint source", writer.globalEvidence, writer.globalRetracts)
	}
	if writer.retractCalls != 0 || len(writer.retractScopeIDs) != 0 {
		t.Fatalf("scoped retract used for global fixpoint solve: %+v", writer)
	}
	if writer.writeCalls != 1 || writer.writeEvidence != codeInterprocFixpointEvidenceSource {
		t.Fatalf("write evidence = %q calls=%d, want fixpoint source", writer.writeEvidence, writer.writeCalls)
	}
	if result.GraphRows != 1 || result.FindingCount != 1 || result.UnresolvedEndpointCount != 0 {
		t.Fatalf("result = %+v, want one written fixpoint row", result)
	}
}
