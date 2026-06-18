package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/interproc"
	"github.com/eshu-hq/eshu/go/internal/parser/summary"
	"github.com/eshu-hq/eshu/go/internal/parser/valueflow"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/truth"
)

// codeValueFlowFixpointSource is the evidence-source tag for cross-repo
// value-flow edges, distinct from the per-file interproc evidence source so the
// two are retracted independently.
const codeValueFlowFixpointSource = "reducer/code-value-flow"

func codeValueFlowFixpointDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainCodeValueFlowFixpoint,
		Summary: "compose persisted value-flow summaries across repos and project cross-repo TAINT_FLOWS_TO evidence",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "code_value_flow_fixpoint",
			SourceLayers: []truth.Layer{
				truth.LayerSourceDeclaration,
			},
		},
	}
}

// CodeValueFlowSummaryReader loads the global, cross-repo function summaries.
type CodeValueFlowSummaryReader interface {
	LoadSnapshot(ctx context.Context) (summary.Snapshot, error)
}

// CodeValueFlowSourceReader loads the global, cross-repo param-level taint sources.
type CodeValueFlowSourceReader interface {
	LoadSources(ctx context.Context) ([]interproc.Source, error)
}

// CodeValueFlowGraphIDReader loads the global FunctionID->graph-uid map.
type CodeValueFlowGraphIDReader interface {
	LoadGraphIDs(ctx context.Context) (map[summary.FunctionID]string, error)
}

// CodeValueFlowFixpointHandler composes the persisted value-flow summaries across
// every repo into one interprocedural program, solves the partitioned fixpoint,
// and projects the cross-repo findings as TAINT_FLOWS_TO evidence edges. A source
// in service A reaching a sink in library B via the deployed call graph becomes a
// single edge. The projection is idempotent: it retracts this source's prior
// edges for the scope and re-MERGEs by evidence uid, so re-running a generation
// converges. Findings whose endpoints have no resolved graph uid are dropped (no
// edge to a phantom node).
type CodeValueFlowFixpointHandler struct {
	SummaryReader CodeValueFlowSummaryReader
	SourceReader  CodeValueFlowSourceReader
	GraphIDReader CodeValueFlowGraphIDReader
	Writer        CodeInterprocEvidenceWriter
	Instruments   *telemetry.Instruments
}

// Handle runs one cross-repo value-flow fixpoint intent.
func (h CodeValueFlowFixpointHandler) Handle(ctx context.Context, intent Intent) (Result, error) {
	if intent.Domain != DomainCodeValueFlowFixpoint {
		return Result{}, fmt.Errorf("code value flow fixpoint handler does not accept domain %q", intent.Domain)
	}
	if h.SummaryReader == nil || h.SourceReader == nil || h.GraphIDReader == nil || h.Writer == nil {
		return Result{}, fmt.Errorf("code value flow fixpoint readers and writer are required")
	}

	snap, err := h.SummaryReader.LoadSnapshot(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("load value-flow summaries: %w", err)
	}
	sources, err := h.SourceReader.LoadSources(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("load value-flow sources: %w", err)
	}
	graphIDs, err := h.GraphIDReader.LoadGraphIDs(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("load value-flow graph ids: %w", err)
	}

	effects := make(map[summary.FunctionID]summary.Effects, len(snap.Functions))
	for _, fn := range snap.Functions {
		effects[fn.ID] = fn.Effects
	}

	program := valueflow.BuildProgram(effects, sources, nil)
	result := interproc.SolvePartitioned(program, interproc.DefaultLimits())

	inputs := make([]CodeInterprocEvidenceInput, 0, len(result.Findings))
	for _, finding := range result.Findings {
		sourceUID := graphIDs[summary.FunctionID(finding.SourceFunc)]
		sinkUID := graphIDs[summary.FunctionID(finding.SinkFunc)]
		if sourceUID == "" || sinkUID == "" {
			continue
		}
		inputs = append(inputs, CodeInterprocEvidenceInput{
			SourceFunctionUID:  sourceUID,
			SinkFunctionUID:    sinkUID,
			SourceFunctionName: functionIDLeafName(finding.SourceFunc),
			SinkFunctionName:   functionIDLeafName(finding.SinkFunc),
			SinkKind:           string(finding.SinkKind),
			SourceKind:         finding.SourceKind,
			Confidence:         finding.Confidence,
			Cloud:              finding.Cloud,
		})
	}
	rows := ExtractCodeInterprocEvidenceRows(inputs)

	if err := h.Writer.RetractCodeInterprocEvidence(ctx, []string{intent.ScopeID}, intent.GenerationID, codeValueFlowFixpointSource); err != nil {
		return Result{}, fmt.Errorf("retract cross-repo value-flow evidence: %w", err)
	}
	if len(rows) > 0 {
		if err := h.Writer.WriteCodeInterprocEvidence(ctx, rows, intent.ScopeID, intent.GenerationID, codeValueFlowFixpointSource); err != nil {
			return Result{}, fmt.Errorf("write cross-repo value-flow evidence: %w", err)
		}
	}

	slog.Info(
		"code value flow fixpoint completed",
		"scope_id", intent.ScopeID,
		"generation_id", intent.GenerationID,
		"function_count", len(effects),
		"source_count", len(sources),
		"finding_count", len(result.Findings),
		"edge_count", len(rows),
	)

	return Result{
		IntentID:        intent.IntentID,
		Domain:          DomainCodeValueFlowFixpoint,
		Status:          ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf("projected %d cross-repo TAINT_FLOWS_TO edge(s) from %d finding(s)", len(rows), len(result.Findings)),
		CanonicalWrites: len(rows),
	}, nil
}

// functionIDLeafName returns the name component of a FunctionID
// (repo\x1fpkg\x1freceiver\x1fname): the substring after the last separator.
func functionIDLeafName(id interproc.FunctionID) string {
	raw := string(id)
	if idx := strings.LastIndexByte(raw, '\x1f'); idx >= 0 {
		return raw[idx+1:]
	}
	return raw
}
