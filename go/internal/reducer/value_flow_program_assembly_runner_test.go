// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/summary"
)

func TestValueFlowProgramAssemblyRunnerAggregatesStatsWithoutWriting(t *testing.T) {
	caller := summary.NewFunctionID("repo-app", "example.com/app", "", "Handle")
	callee := summary.NewFunctionID("repo-lib", "example.com/lib", "", "Query")
	loader := &recordingValueFlowProgramAssemblyLoader{
		inputs: []ValueFlowProgramInput{{
			Summaries: map[summary.FunctionID]summary.Effects{
				caller: {ParamToCallArg: []summary.CallArgFlow{{Callee: callee, Param: 0, Arg: 1}}},
				callee: {ParamToSink: []summary.ParamSink{{Param: 1, SinkKind: "sql"}}},
			},
			CallEdges: []ValueFlowCallEdge{{
				CallerFunctionID: caller,
				CalleeFunctionID: callee,
			}},
		}},
	}
	runner := ValueFlowProgramAssemblyRunner{
		InputLoader: loader,
		Config:      ValueFlowProgramAssemblyRunnerConfig{BatchLimit: 7},
	}

	result, err := runner.ProcessOnce(context.Background())
	if err != nil {
		t.Fatalf("ProcessOnce() error = %v", err)
	}
	if result.InputsProcessed != 1 {
		t.Fatalf("InputsProcessed = %d, want 1", result.InputsProcessed)
	}
	if result.ProgramEdgeCount != 1 || result.SinkCount != 1 {
		t.Fatalf("result = %#v, want one program edge and one sink", result)
	}
	if got, want := loader.limit, 7; got != want {
		t.Fatalf("loader limit = %d, want %d", got, want)
	}
}

func TestValueFlowProgramAssemblyRunnerAggregatesMissingCalleeIdentity(t *testing.T) {
	caller := summary.NewFunctionID("repo-app", "example.com/app", "", "Handle")
	loader := &recordingValueFlowProgramAssemblyLoader{
		inputs: []ValueFlowProgramInput{{
			Summaries: map[summary.FunctionID]summary.Effects{
				caller: {},
			},
			CallEdges: []ValueFlowCallEdge{{
				CallerFunctionID: caller,
			}},
		}},
	}
	runner := ValueFlowProgramAssemblyRunner{InputLoader: loader}

	result, err := runner.ProcessOnce(context.Background())
	if err != nil {
		t.Fatalf("ProcessOnce() error = %v", err)
	}
	if result.SkippedMissingIdentity != 1 {
		t.Fatalf("SkippedMissingIdentity = %d, want 1", result.SkippedMissingIdentity)
	}
}

type recordingValueFlowProgramAssemblyLoader struct {
	limit  int
	inputs []ValueFlowProgramInput
}

func (f *recordingValueFlowProgramAssemblyLoader) LoadPendingValueFlowProgramInputs(
	_ context.Context,
	limit int,
) ([]ValueFlowProgramInput, error) {
	f.limit = limit
	return f.inputs, nil
}
