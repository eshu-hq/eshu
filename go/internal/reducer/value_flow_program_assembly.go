// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"

	"github.com/eshu-hq/eshu/go/internal/parser/interproc"
	"github.com/eshu-hq/eshu/go/internal/parser/summary"
	"github.com/eshu-hq/eshu/go/internal/parser/valueflow"
)

// ValueFlowProgramInput is the bounded in-memory snapshot used to assemble an
// interprocedural value-flow Program from active CALLS evidence and persisted
// function summaries.
type ValueFlowProgramInput struct {
	ScopeID      string
	GenerationID string
	RepositoryID string
	SourceRunID  string
	Summaries    map[summary.FunctionID]summary.Effects
	CallEdges    []ValueFlowCallEdge
	Sources      []interproc.Source
	Sinks        []interproc.Sink
	// SkippedMissingIdentity carries loader-side skips where a completed CALLS
	// endpoint could not be mapped to a durable summary.FunctionID.
	SkippedMissingIdentity int
}

// ValueFlowCallEdge is one active code-call edge with both graph entity IDs and
// resolved summary identities. The graph IDs are diagnostic; Program assembly
// uses the summary identities.
type ValueFlowCallEdge struct {
	CallerEntityID   string
	CalleeEntityID   string
	CallerFunctionID summary.FunctionID
	CalleeFunctionID summary.FunctionID
	RelationshipType string
	ResolutionMethod string
}

// ValueFlowProgramAssemblyStats reports bounded assembly outcomes.
type ValueFlowProgramAssemblyStats struct {
	SummaryCount                 int
	CallEdgeCount                int
	ProgramEdgeCount             int
	SourceCount                  int
	SinkCount                    int
	SkippedMissingSummary        int
	SkippedUnconfirmedCallFlow   int
	SkippedCallEdgeMissingID     int
	SkippedCallEdgeMissingCallee int
}

// BuildValueFlowProgram assembles a deterministic interproc.Program from
// summaries whose call flows are confirmed by active CALLS edges. It does not
// run the solver or write graph evidence.
func BuildValueFlowProgram(input ValueFlowProgramInput) (interproc.Program, ValueFlowProgramAssemblyStats) {
	activeCalls := make(map[valueFlowFunctionPair]struct{}, len(input.CallEdges))
	referenced := make(map[summary.FunctionID]struct{}, len(input.CallEdges)*2)
	stats := ValueFlowProgramAssemblyStats{
		SourceCount:              len(input.Sources),
		SkippedCallEdgeMissingID: input.SkippedMissingIdentity,
	}
	for _, edge := range input.CallEdges {
		if edge.CallerFunctionID == "" {
			stats.SkippedCallEdgeMissingID++
			continue
		}
		if edge.CalleeFunctionID == "" {
			stats.SkippedCallEdgeMissingCallee++
			continue
		}
		pair := valueFlowFunctionPair{caller: edge.CallerFunctionID, callee: edge.CalleeFunctionID}
		activeCalls[pair] = struct{}{}
		referenced[edge.CallerFunctionID] = struct{}{}
		referenced[edge.CalleeFunctionID] = struct{}{}
		stats.CallEdgeCount++
	}

	filtered := make(map[summary.FunctionID]summary.Effects, len(referenced))
	missingSummaries := make(map[summary.FunctionID]struct{})
	for _, id := range sortedValueFlowFunctionIDs(referenced) {
		if _, ok := input.Summaries[id]; !ok {
			missingSummaries[id] = struct{}{}
			stats.SkippedMissingSummary++
		}
	}
	for _, id := range sortedValueFlowFunctionIDs(referenced) {
		effects, ok := input.Summaries[id]
		if !ok {
			continue
		}
		stats.SummaryCount++
		filtered[id] = filterValueFlowEffects(id, effects, input.Summaries, activeCalls, missingSummaries, &stats)
	}

	program := valueflow.BuildProgram(filtered, input.Sources, input.Sinks)
	stats.ProgramEdgeCount = len(program.Edges)
	stats.SinkCount = len(program.Sinks)
	return program, stats
}

type valueFlowFunctionPair struct {
	caller summary.FunctionID
	callee summary.FunctionID
}

func filterValueFlowEffects(
	id summary.FunctionID,
	effects summary.Effects,
	summaries map[summary.FunctionID]summary.Effects,
	activeCalls map[valueFlowFunctionPair]struct{},
	missingSummaries map[summary.FunctionID]struct{},
	stats *ValueFlowProgramAssemblyStats,
) summary.Effects {
	filtered := effects
	filtered.ParamToCallArg = nil
	for _, flow := range effects.ParamToCallArg {
		if _, ok := activeCalls[valueFlowFunctionPair{caller: id, callee: flow.Callee}]; !ok {
			stats.SkippedUnconfirmedCallFlow++
			continue
		}
		if _, ok := summaries[flow.Callee]; !ok {
			if _, alreadyCounted := missingSummaries[flow.Callee]; !alreadyCounted {
				missingSummaries[flow.Callee] = struct{}{}
				stats.SkippedMissingSummary++
			}
			continue
		}
		filtered.ParamToCallArg = append(filtered.ParamToCallArg, flow)
	}
	return filtered
}

func sortedValueFlowFunctionIDs(ids map[summary.FunctionID]struct{}) []summary.FunctionID {
	out := make([]summary.FunctionID, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
