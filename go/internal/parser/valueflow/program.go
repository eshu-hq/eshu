// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package valueflow

import (
	"sort"

	"github.com/eshu-hq/eshu/go/internal/parser/interproc"
	"github.com/eshu-hq/eshu/go/internal/parser/summary"
)

// BuildProgram assembles an interprocedural port graph from per-function summary
// effects plus the externally-known sources and sinks (for example an HTTP
// request parameter as a source, or a correlated cloud fact as a sink). A
// parameter that flows to a callee argument becomes a cross-function edge; a
// parameter that flows to a sink becomes a sink at that parameter port; a
// parameter that flows to the return becomes an edge to the return port. The
// result is deterministic regardless of map iteration order.
func BuildProgram(summaries map[summary.FunctionID]summary.Effects, sources []interproc.Source, sinks []interproc.Sink) interproc.Program {
	program := interproc.Program{
		Sources: append([]interproc.Source(nil), sources...),
		Sinks:   append([]interproc.Sink(nil), sinks...),
	}

	ids := make([]summary.FunctionID, 0, len(summaries))
	for id := range summaries {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	for _, id := range ids {
		effects := summaries[id]
		for _, flow := range effects.ParamToCallArg {
			program.Edges = append(program.Edges, interproc.Edge{
				From: paramPort(id, flow.Param),
				To:   paramPort(flow.Callee, flow.Arg),
			})
		}
		for _, ps := range effects.ParamToSink {
			program.Sinks = append(program.Sinks, interproc.Sink{
				Port: paramPort(id, ps.Param),
				Kind: ps.SinkKind,
			})
		}
		for _, param := range effects.ParamToReturn {
			program.Edges = append(program.Edges, interproc.Edge{
				From: paramPort(id, param),
				To:   returnPort(id),
			})
		}
	}
	return program
}

// paramPort builds the interproc port for a function parameter.
func paramPort(id summary.FunctionID, index int) interproc.Port {
	return interproc.Port{
		Func: interproc.FunctionID(id),
		Slot: interproc.Slot{Kind: interproc.SlotParam, Index: index},
	}
}

// returnPort builds the interproc port for a function's return value.
func returnPort(id summary.FunctionID) interproc.Port {
	return interproc.Port{
		Func: interproc.FunctionID(id),
		Slot: interproc.Slot{Kind: interproc.SlotReturn},
	}
}
