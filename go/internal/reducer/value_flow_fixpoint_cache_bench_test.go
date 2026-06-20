package reducer

import (
	"context"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/interproc"
	"github.com/eshu-hq/eshu/go/internal/parser/summary"
	"github.com/eshu-hq/eshu/go/internal/parser/valueflow"
)

func BenchmarkValueFlowFixpointFull(b *testing.B) {
	program, versions := benchmarkValueFlowProgram(100, 100)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = interproc.SolvePartitioned(program, interproc.DefaultLimits())
	}
	_ = versions
}

func BenchmarkValueFlowFixpointIncrementalCached(b *testing.B) {
	program, versions := benchmarkValueFlowProgram(100, 100)
	cache := NewValueFlowFixpointCache()
	SolveValueFlowProgramIncremental(program, versions, cache, interproc.DefaultLimits())

	changed := make(map[summary.FunctionID]string, len(versions))
	for id, version := range versions {
		changed[id] = version
	}
	changedID := summary.NewFunctionID("repo-099", "pkg", "", "step-00")

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		changed[changedID] = fmt.Sprintf("v%d", i+2)
		_, stats := SolveValueFlowProgramIncremental(program, changed, cache, interproc.DefaultLimits())
		if stats.RecomputedComponents != 1 {
			b.Fatalf("RecomputedComponents = %d, want 1", stats.RecomputedComponents)
		}
	}
}

func BenchmarkValueFlowSnapshotFullAssemblySolve(b *testing.B) {
	effects, sources, versions := benchmarkValueFlowSnapshot(100, 100)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		program := valueflow.BuildProgram(effects, sources, nil)
		_ = interproc.SolvePartitioned(program, interproc.DefaultLimits())
	}
	_ = versions
}

func BenchmarkValueFlowSnapshotDurableRestartCached(b *testing.B) {
	effects, sources, versions := benchmarkValueFlowSnapshot(100, 100)
	store := newMemoryValueFlowFixpointComponentStore()
	_, _, err := SolveValueFlowSnapshotIncrementalDurable(
		context.Background(),
		effects,
		versions,
		sources,
		nil,
		NewValueFlowFixpointCache(),
		store,
		interproc.DefaultLimits(),
	)
	if err != nil {
		b.Fatalf("warm snapshot solve error = %v", err)
	}

	changedID := summary.NewFunctionID("repo-099", "pkg", "", "step-00")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		changed := make(map[summary.FunctionID]string, len(versions))
		for id, version := range versions {
			changed[id] = version
		}
		changed[changedID] = fmt.Sprintf("v%d", i+2)
		_, stats, err := SolveValueFlowSnapshotIncrementalDurable(
			context.Background(),
			effects,
			changed,
			sources,
			nil,
			NewValueFlowFixpointCache(),
			store,
			interproc.DefaultLimits(),
		)
		if err != nil {
			b.Fatalf("snapshot solve error = %v", err)
		}
		if stats.AssembledComponents != 1 || stats.RecomputedComponents != 1 {
			b.Fatalf("stats = %+v, want one assembled and recomputed component", stats)
		}
		if stats.DurableReused != 99 {
			b.Fatalf("DurableReused = %d, want 99", stats.DurableReused)
		}
	}
}

func benchmarkValueFlowProgram(components int, chainLength int) (interproc.Program, map[summary.FunctionID]string) {
	program := interproc.Program{
		Edges:   make([]interproc.Edge, 0, components*(chainLength-1)),
		Sources: make([]interproc.Source, 0, components),
		Sinks:   make([]interproc.Sink, 0, components),
	}
	versions := make(map[summary.FunctionID]string, components*chainLength)
	for i := 0; i < components; i++ {
		var previous summary.FunctionID
		for step := 0; step < chainLength; step++ {
			current := summary.NewFunctionID(fmt.Sprintf("repo-%03d", i), "pkg", "", fmt.Sprintf("step-%02d", step))
			if step == 0 {
				program.Sources = append(program.Sources, interproc.Source{Port: valueFlowParamPort(current, 0), Kind: "http_request"})
			} else {
				program.Edges = append(program.Edges, interproc.Edge{From: valueFlowParamPort(previous, 0), To: valueFlowParamPort(current, 0)})
			}
			if step == chainLength-1 {
				program.Sinks = append(program.Sinks, interproc.Sink{Port: valueFlowParamPort(current, 0), Kind: "sql"})
			}
			versions[current] = "v1"
			previous = current
		}
	}
	return program, versions
}

func benchmarkValueFlowSnapshot(
	components int,
	chainLength int,
) (map[summary.FunctionID]summary.Effects, []interproc.Source, map[summary.FunctionID]string) {
	effects := make(map[summary.FunctionID]summary.Effects, components*chainLength)
	sources := make([]interproc.Source, 0, components)
	versions := make(map[summary.FunctionID]string, components*chainLength)
	for i := 0; i < components; i++ {
		for step := 0; step < chainLength; step++ {
			current := summary.NewFunctionID(fmt.Sprintf("repo-%03d", i), "pkg", "", fmt.Sprintf("step-%02d", step))
			fx := summary.Effects{}
			switch step {
			case 0:
				sources = append(sources, interproc.Source{Port: valueFlowParamPort(current, 0), Kind: "http_request"})
				if chainLength == 1 {
					fx.ParamToSink = []summary.ParamSink{{Param: 0, SinkKind: "sql"}}
				} else {
					next := summary.NewFunctionID(fmt.Sprintf("repo-%03d", i), "pkg", "", fmt.Sprintf("step-%02d", step+1))
					fx.ParamToCallArg = []summary.CallArgFlow{{Callee: next, Param: 0, Arg: 0}}
				}
			case chainLength - 1:
				fx.ParamToSink = []summary.ParamSink{{Param: 0, SinkKind: "sql"}}
			default:
				next := summary.NewFunctionID(fmt.Sprintf("repo-%03d", i), "pkg", "", fmt.Sprintf("step-%02d", step+1))
				fx.ParamToCallArg = []summary.CallArgFlow{{Callee: next, Param: 0, Arg: 0}}
			}
			effects[current] = fx
			versions[current] = "v1"
		}
	}
	return effects, sources, versions
}
