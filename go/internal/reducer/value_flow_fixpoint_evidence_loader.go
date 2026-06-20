package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/interproc"
	"github.com/eshu-hq/eshu/go/internal/parser/summary"
	"github.com/eshu-hq/eshu/go/internal/parser/valueflow"
)

// FunctionSummarySnapshotLoader reloads durable value-flow summaries for the
// cross-repo fixpoint.
type FunctionSummarySnapshotLoader interface {
	LoadSnapshot(ctx context.Context) (summary.Snapshot, error)
}

// FunctionSourceSnapshotLoader reloads durable value-flow source ports for the
// cross-repo fixpoint.
type FunctionSourceSnapshotLoader interface {
	LoadSources(ctx context.Context) ([]interproc.Source, error)
}

// FunctionGraphIDSnapshotLoader reloads durable FunctionID->Function.uid
// mappings for TAINT_FLOWS_TO projection.
type FunctionGraphIDSnapshotLoader interface {
	LoadGraphIDs(ctx context.Context) (map[summary.FunctionID]string, error)
}

// ValueFlowCloudSinkTarget is a graph-backed cloud sink attached to a Function.
// The function-level bridge is expanded onto observed parameter ports before the
// interprocedural fixpoint runs so missing parameter evidence does not fabricate
// value-flow precision.
type ValueFlowCloudSinkTarget struct {
	FunctionID summary.FunctionID
	Kind       string
	Label      string
}

// FunctionCloudSinkTargetLoader reloads graph-backed cloud sink targets for the
// cross-repo fixpoint. The graphIDs map is the durable FunctionID->Function.uid
// snapshot and bounds graph-backed lookup to functions already known to the
// value-flow runtime.
type FunctionCloudSinkTargetLoader interface {
	LoadCloudSinkTargets(ctx context.Context, graphIDs map[summary.FunctionID]string) ([]ValueFlowCloudSinkTarget, error)
}

// ValueFlowFixpointEvidenceLoader composes durable function summaries, source
// ports, graph ids, and graph-backed cloud sink targets into the existing
// code_interproc_evidence reducer input.
type ValueFlowFixpointEvidenceLoader struct {
	SummarySnapshotLoader   FunctionSummarySnapshotLoader
	SourceSnapshotLoader    FunctionSourceSnapshotLoader
	GraphIDSnapshotLoader   FunctionGraphIDSnapshotLoader
	CloudSinkSnapshotLoader FunctionCloudSinkTargetLoader
	FixpointCache           *ValueFlowFixpointCache
	Logger                  *slog.Logger
}

// LoadCodeInterprocEvidence solves the cross-repo value-flow Program assembled
// from persisted summaries and sources, then resolves finding endpoints through
// the durable FunctionID->graph uid map.
func (l ValueFlowFixpointEvidenceLoader) LoadCodeInterprocEvidence(
	ctx context.Context,
	scopeID string,
	generationID string,
) ([]CodeInterprocEvidenceInput, error) {
	effects, versions, err := l.loadEffects(ctx, scopeID, generationID)
	if err != nil {
		return nil, err
	}
	sources, err := l.loadSources(ctx, scopeID, generationID)
	if err != nil {
		return nil, err
	}
	if len(sources) == 0 {
		return nil, nil
	}
	graphIDs, err := l.loadGraphIDs(ctx, scopeID, generationID)
	if err != nil {
		return nil, err
	}
	cloudSinks, err := l.loadCloudSinks(ctx, graphIDs, effects, sources)
	if err != nil {
		return nil, err
	}
	if len(effects) == 0 && len(cloudSinks) == 0 {
		return nil, nil
	}

	program := valueflow.BuildProgram(effects, sources, cloudSinks)
	result, cacheStats := SolveValueFlowProgramIncremental(program, versions, l.FixpointCache, interproc.DefaultLimits())
	inputs := make([]CodeInterprocEvidenceInput, 0, len(result.Findings))
	for _, finding := range result.Findings {
		sourceID := summary.FunctionID(finding.SourceFunc)
		sinkID := summary.FunctionID(finding.SinkFunc)
		inputs = append(inputs, CodeInterprocEvidenceInput{
			SourceFunctionUID:  graphIDs[sourceID],
			SinkFunctionUID:    graphIDs[sinkID],
			SourceFunctionName: functionName(sourceID),
			SinkFunctionName:   functionName(sinkID),
			SourceKind:         finding.SourceKind,
			SinkKind:           finding.SinkKind,
			Confidence:         finding.Confidence,
			Cloud:              finding.Cloud,
			WhyTrail:           valueFlowFindingWhyTrail(finding.Trail, graphIDs),
			WhyTrailTruncated:  finding.TrailTruncated,
		})
	}
	if l.Logger != nil {
		l.Logger.Info(
			"value-flow fixpoint evidence loaded",
			"scope_id", scopeID,
			"generation_id", generationID,
			"summary_count", len(effects),
			"source_count", len(sources),
			"cloud_sink_count", len(cloudSinks),
			"finding_count", len(inputs),
			"overflow_count", result.Overflow,
			"fixpoint_component_count", cacheStats.ComponentCount,
			"fixpoint_recomputed_components", cacheStats.RecomputedComponents,
			"fixpoint_reused_components", cacheStats.ReusedComponents,
			"unresolved_endpoint_count", unresolvedCodeInterprocEndpointCount(inputs),
		)
	}
	return inputs, nil
}

func (l ValueFlowFixpointEvidenceLoader) loadEffects(
	ctx context.Context,
	scopeID string,
	generationID string,
) (map[summary.FunctionID]summary.Effects, map[summary.FunctionID]string, error) {
	effects := map[summary.FunctionID]summary.Effects{}
	versions := map[summary.FunctionID]string{}
	if l.SummarySnapshotLoader != nil {
		snap, err := l.SummarySnapshotLoader.LoadSnapshot(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("load durable function summaries: %w", err)
		}
		for _, fn := range snap.Functions {
			if fn.ID == "" {
				continue
			}
			effects[fn.ID] = fn.Effects
			versions[fn.ID] = fn.Version
		}
	}
	return effects, versions, nil
}

func (l ValueFlowFixpointEvidenceLoader) loadSources(
	ctx context.Context,
	scopeID string,
	generationID string,
) ([]interproc.Source, error) {
	var sources []interproc.Source
	if l.SourceSnapshotLoader != nil {
		durable, err := l.SourceSnapshotLoader.LoadSources(ctx)
		if err != nil {
			return nil, fmt.Errorf("load durable function sources: %w", err)
		}
		sources = append(sources, durable...)
	}
	return sources, nil
}

func (l ValueFlowFixpointEvidenceLoader) loadGraphIDs(
	ctx context.Context,
	scopeID string,
	generationID string,
) (map[summary.FunctionID]string, error) {
	graphIDs := map[summary.FunctionID]string{}
	if l.GraphIDSnapshotLoader != nil {
		durable, err := l.GraphIDSnapshotLoader.LoadGraphIDs(ctx)
		if err != nil {
			return nil, fmt.Errorf("load durable function graph ids: %w", err)
		}
		for id, uid := range durable {
			if id == "" || strings.TrimSpace(uid) == "" {
				continue
			}
			graphIDs[id] = uid
		}
	}
	return graphIDs, nil
}

func (l ValueFlowFixpointEvidenceLoader) loadCloudSinks(
	ctx context.Context,
	graphIDs map[summary.FunctionID]string,
	effects map[summary.FunctionID]summary.Effects,
	sources []interproc.Source,
) ([]interproc.Sink, error) {
	if l.CloudSinkSnapshotLoader == nil {
		return nil, nil
	}
	targets, err := l.CloudSinkSnapshotLoader.LoadCloudSinkTargets(ctx, graphIDs)
	if err != nil {
		return nil, fmt.Errorf("load graph-backed cloud sinks: %w", err)
	}
	return expandCloudSinkTargets(targets, effects, sources), nil
}

func expandCloudSinkTargets(
	targets []ValueFlowCloudSinkTarget,
	effects map[summary.FunctionID]summary.Effects,
	sources []interproc.Source,
) []interproc.Sink {
	if len(targets) == 0 {
		return nil
	}
	portsByFunction := observedParamPorts(effects, sources)
	sinks := make([]interproc.Sink, 0, len(targets))
	for _, target := range targets {
		if target.FunctionID == "" || strings.TrimSpace(target.Kind) == "" {
			continue
		}
		params := portsByFunction[target.FunctionID]
		if len(params) == 0 {
			continue
		}
		for _, param := range params {
			sinks = append(sinks, interproc.Sink{
				Port: interproc.Port{
					Func: interproc.FunctionID(target.FunctionID),
					Slot: interproc.Slot{Kind: interproc.SlotParam, Index: param},
				},
				Kind:  target.Kind,
				Label: target.Label,
				Cloud: true,
			})
		}
	}
	return sinks
}

func observedParamPorts(
	effects map[summary.FunctionID]summary.Effects,
	sources []interproc.Source,
) map[summary.FunctionID][]int {
	seen := map[summary.FunctionID]map[int]struct{}{}
	add := func(id summary.FunctionID, param int) {
		if id == "" || param < 0 {
			return
		}
		params := seen[id]
		if params == nil {
			params = map[int]struct{}{}
			seen[id] = params
		}
		params[param] = struct{}{}
	}
	for id, fx := range effects {
		for _, param := range fx.ParamToReturn {
			add(id, param)
		}
		for _, sink := range fx.ParamToSink {
			add(id, sink.Param)
		}
		for _, flow := range fx.ParamToCallArg {
			add(id, flow.Param)
			add(flow.Callee, flow.Arg)
		}
	}
	for _, source := range sources {
		if source.Port.Slot.Kind == interproc.SlotParam {
			add(summary.FunctionID(source.Port.Func), source.Port.Slot.Index)
		}
	}

	out := make(map[summary.FunctionID][]int, len(seen))
	for id, params := range seen {
		values := make([]int, 0, len(params))
		for param := range params {
			values = append(values, param)
		}
		sort.Ints(values)
		out[id] = values
	}
	return out
}

func functionName(id summary.FunctionID) string {
	parts := strings.Split(string(id), "\x1f")
	if len(parts) == 4 && strings.TrimSpace(parts[3]) != "" {
		return parts[3]
	}
	return string(id)
}

func valueFlowFindingWhyTrail(trail []interproc.Port, graphIDs map[summary.FunctionID]string) []map[string]any {
	if len(trail) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(trail))
	for index, port := range trail {
		id := summary.FunctionID(port.Func)
		step := map[string]any{
			"role":          valueFlowTrailRole(index, len(trail)),
			"function_id":   string(id),
			"function_name": functionName(id),
			"slot_kind":     valueFlowTrailSlotKind(port.Slot.Kind),
		}
		if uid := graphIDs[id]; uid != "" {
			step["function_uid"] = uid
		}
		if port.Slot.Kind == interproc.SlotParam {
			step["slot_index"] = port.Slot.Index
		}
		if port.Slot.Name != "" {
			step["slot_name"] = port.Slot.Name
		}
		out = append(out, step)
	}
	return out
}

func valueFlowTrailRole(index, length int) string {
	switch index {
	case 0:
		return "source"
	default:
		if index == length-1 {
			return "sink"
		}
		return "intermediate"
	}
}

func valueFlowTrailSlotKind(kind interproc.SlotKind) string {
	switch kind {
	case interproc.SlotParam:
		return "param"
	case interproc.SlotReturn:
		return "return"
	case interproc.SlotNamed:
		return "named"
	default:
		return "unknown"
	}
}

// ValueFlowFixpointProjectionResult records the visible outcome of a post-summary
// fixpoint projection.
type ValueFlowFixpointProjectionResult struct {
	FindingCount            int
	GraphRows               int
	UnresolvedEndpointCount int
}

// ValueFlowFixpointEvidenceProjector writes summary-fixpoint findings as
// TAINT_FLOWS_TO evidence under a distinct evidence source and uid namespace.
type ValueFlowFixpointEvidenceProjector struct {
	Loader CodeInterprocEvidenceLoader
	Writer CodeInterprocEvidenceWriter
}

// ProjectValueFlowFixpointEvidence retracts and rewrites the full fixpoint-owned
// TAINT_FLOWS_TO evidence source. The solve reads global durable summary/source
// state, so retraction must match that global write contract rather than a
// triggering scope's last-stamped edge ownership.
func (p ValueFlowFixpointEvidenceProjector) ProjectValueFlowFixpointEvidence(
	ctx context.Context,
	scopeID string,
	generationID string,
) (ValueFlowFixpointProjectionResult, error) {
	if p.Loader == nil || p.Writer == nil {
		return ValueFlowFixpointProjectionResult{}, nil
	}
	inputs, err := p.Loader.LoadCodeInterprocEvidence(ctx, scopeID, generationID)
	if err != nil {
		return ValueFlowFixpointProjectionResult{}, err
	}
	rows := ExtractCodeInterprocFixpointEvidenceRows(inputs)
	if err := p.Writer.RetractCodeInterprocEvidenceSource(ctx, codeInterprocFixpointEvidenceSource); err != nil {
		return ValueFlowFixpointProjectionResult{}, fmt.Errorf("retract value-flow fixpoint evidence: %w", err)
	}
	if len(rows) > 0 {
		if err := p.Writer.WriteCodeInterprocEvidence(ctx, rows, scopeID, generationID, codeInterprocFixpointEvidenceSource); err != nil {
			return ValueFlowFixpointProjectionResult{}, fmt.Errorf("write value-flow fixpoint evidence: %w", err)
		}
	}
	return ValueFlowFixpointProjectionResult{
		FindingCount:            len(inputs),
		GraphRows:               len(rows),
		UnresolvedEndpointCount: unresolvedCodeInterprocEndpointCount(inputs),
	}, nil
}
