// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/interproc"
	"github.com/eshu-hq/eshu/go/internal/parser/summary"
)

func TestBuildValueFlowProgramFiltersSummaryCallFlowsThroughActiveCalls(t *testing.T) {
	caller := summary.NewFunctionID("repo-app", "example.com/app", "", "Handle")
	callee := summary.NewFunctionID("repo-lib", "example.com/lib", "", "Query")
	unconfirmed := summary.NewFunctionID("repo-lib", "example.com/lib", "", "Unused")

	program, stats := BuildValueFlowProgram(ValueFlowProgramInput{
		Summaries: map[summary.FunctionID]summary.Effects{
			caller: {
				ParamToCallArg: []summary.CallArgFlow{
					{Callee: callee, Param: 0, Arg: 1},
					{Callee: unconfirmed, Param: 0, Arg: 0},
				},
			},
			callee: {
				ParamToSink: []summary.ParamSink{{Param: 1, SinkKind: "sql"}},
			},
			unconfirmed: {
				ParamToSink: []summary.ParamSink{{Param: 0, SinkKind: "exec"}},
			},
		},
		CallEdges: []ValueFlowCallEdge{{
			CallerFunctionID: caller,
			CalleeFunctionID: callee,
		}},
	})

	wantEdge := interproc.Edge{
		From: interproc.Port{Func: interproc.FunctionID(caller), Slot: interproc.Slot{Kind: interproc.SlotParam, Index: 0}},
		To:   interproc.Port{Func: interproc.FunctionID(callee), Slot: interproc.Slot{Kind: interproc.SlotParam, Index: 1}},
	}
	if len(program.Edges) != 1 || program.Edges[0] != wantEdge {
		t.Fatalf("program.Edges = %#v, want %#v", program.Edges, []interproc.Edge{wantEdge})
	}
	if len(program.Sinks) != 1 || program.Sinks[0].Kind != "sql" {
		t.Fatalf("program.Sinks = %#v, want one sql sink", program.Sinks)
	}
	if stats.SummaryCount != 2 {
		t.Fatalf("SummaryCount = %d, want 2", stats.SummaryCount)
	}
	if stats.CallEdgeCount != 1 {
		t.Fatalf("CallEdgeCount = %d, want 1", stats.CallEdgeCount)
	}
	if stats.SkippedUnconfirmedCallFlow != 1 {
		t.Fatalf("SkippedUnconfirmedCallFlow = %d, want 1", stats.SkippedUnconfirmedCallFlow)
	}
}

func TestBuildValueFlowProgramCountsMissingSummary(t *testing.T) {
	caller := summary.NewFunctionID("repo-app", "example.com/app", "", "Handle")
	callee := summary.NewFunctionID("repo-lib", "example.com/lib", "", "Query")

	_, stats := BuildValueFlowProgram(ValueFlowProgramInput{
		Summaries: map[summary.FunctionID]summary.Effects{
			caller: {
				ParamToCallArg: []summary.CallArgFlow{{Callee: callee, Param: 0, Arg: 0}},
			},
		},
		CallEdges: []ValueFlowCallEdge{{
			CallerFunctionID: caller,
			CalleeFunctionID: callee,
		}},
	})

	if stats.SkippedMissingSummary != 1 {
		t.Fatalf("SkippedMissingSummary = %d, want 1", stats.SkippedMissingSummary)
	}
}
