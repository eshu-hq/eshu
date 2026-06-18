package reducer

import (
	"context"
	"fmt"
	"log/slog"
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

// ValueFlowFixpointEvidenceLoader composes durable function summaries, source
// ports, and graph ids into the existing code_interproc_evidence reducer input.
type ValueFlowFixpointEvidenceLoader struct {
	SummarySnapshotLoader FunctionSummarySnapshotLoader
	SourceSnapshotLoader  FunctionSourceSnapshotLoader
	GraphIDSnapshotLoader FunctionGraphIDSnapshotLoader
	Logger                *slog.Logger
}

// LoadCodeInterprocEvidence solves the cross-repo value-flow Program assembled
// from persisted summaries and sources, then resolves finding endpoints through
// the durable FunctionID->graph uid map.
func (l ValueFlowFixpointEvidenceLoader) LoadCodeInterprocEvidence(
	ctx context.Context,
	scopeID string,
	generationID string,
) ([]CodeInterprocEvidenceInput, error) {
	effects, err := l.loadEffects(ctx, scopeID, generationID)
	if err != nil {
		return nil, err
	}
	sources, err := l.loadSources(ctx, scopeID, generationID)
	if err != nil {
		return nil, err
	}
	graphIDs, err := l.loadGraphIDs(ctx, scopeID, generationID)
	if err != nil {
		return nil, err
	}
	if len(effects) == 0 || len(sources) == 0 {
		return nil, nil
	}

	program := valueflow.BuildProgram(effects, sources, nil)
	result := interproc.SolvePartitioned(program, interproc.DefaultLimits())
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
		})
	}
	if l.Logger != nil {
		l.Logger.Info(
			"value-flow fixpoint evidence loaded",
			"scope_id", scopeID,
			"generation_id", generationID,
			"summary_count", len(effects),
			"source_count", len(sources),
			"finding_count", len(inputs),
			"overflow_count", result.Overflow,
			"unresolved_endpoint_count", unresolvedCodeInterprocEndpointCount(inputs),
		)
	}
	return inputs, nil
}

func (l ValueFlowFixpointEvidenceLoader) loadEffects(
	ctx context.Context,
	scopeID string,
	generationID string,
) (map[summary.FunctionID]summary.Effects, error) {
	effects := map[summary.FunctionID]summary.Effects{}
	if l.SummarySnapshotLoader != nil {
		snap, err := l.SummarySnapshotLoader.LoadSnapshot(ctx)
		if err != nil {
			return nil, fmt.Errorf("load durable function summaries: %w", err)
		}
		for _, fn := range snap.Functions {
			if fn.ID == "" {
				continue
			}
			effects[fn.ID] = fn.Effects
		}
	}
	return effects, nil
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

func functionName(id summary.FunctionID) string {
	parts := strings.Split(string(id), "\x1f")
	if len(parts) == 4 && strings.TrimSpace(parts[3]) != "" {
		return parts[3]
	}
	return string(id)
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

// ProjectValueFlowFixpointEvidence retracts and rewrites only fixpoint-owned
// TAINT_FLOWS_TO edges for the current scope.
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
	if err := p.Writer.RetractCodeInterprocEvidence(ctx, []string{scopeID}, generationID, codeInterprocFixpointEvidenceSource); err != nil {
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
