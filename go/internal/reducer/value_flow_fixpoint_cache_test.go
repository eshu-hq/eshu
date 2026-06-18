package reducer

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/interproc"
	"github.com/eshu-hq/eshu/go/internal/parser/summary"
)

func TestValueFlowFixpointCacheRecomputesOnlyChangedComponent(t *testing.T) {
	t.Parallel()

	leftSource := summary.NewFunctionID("repo-a", "pkg", "", "leftSource")
	leftSink := summary.NewFunctionID("repo-a", "pkg", "", "leftSink")
	rightSource := summary.NewFunctionID("repo-b", "pkg", "", "rightSource")
	rightSink := summary.NewFunctionID("repo-b", "pkg", "", "rightSink")
	program := twoComponentValueFlowProgram(leftSource, leftSink, rightSource, rightSink)
	cache := NewValueFlowFixpointCache()

	_, first := SolveValueFlowProgramIncremental(program, valueFlowVersions("v1", leftSource, leftSink, rightSource, rightSink), cache, interproc.DefaultLimits())
	if first.RecomputedComponents != 2 || first.ReusedComponents != 0 {
		t.Fatalf("first stats = %+v, want two recomputed components", first)
	}

	_, second := SolveValueFlowProgramIncremental(program, map[summary.FunctionID]string{
		leftSource:  "v1",
		leftSink:    "v1",
		rightSource: "v2",
		rightSink:   "v1",
	}, cache, interproc.DefaultLimits())
	if second.RecomputedComponents != 1 || second.ReusedComponents != 1 {
		t.Fatalf("second stats = %+v, want one recomputed and one reused component", second)
	}
}

func TestValueFlowFixpointCacheMatchesFullSolve(t *testing.T) {
	t.Parallel()

	leftSource := summary.NewFunctionID("repo-a", "pkg", "", "leftSource")
	leftSink := summary.NewFunctionID("repo-a", "pkg", "", "leftSink")
	rightSource := summary.NewFunctionID("repo-b", "pkg", "", "rightSource")
	rightSink := summary.NewFunctionID("repo-b", "pkg", "", "rightSink")
	program := twoComponentValueFlowProgram(leftSource, leftSink, rightSource, rightSink)
	versions := valueFlowVersions("v1", leftSource, leftSink, rightSource, rightSink)

	for _, limits := range []interproc.Limits{
		interproc.DefaultLimits(),
		{MaxFindings: 1},
	} {
		got, stats := SolveValueFlowProgramIncremental(program, versions, NewValueFlowFixpointCache(), limits)
		want := interproc.SolvePartitioned(program, limits)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("incremental result with limits %+v = %+v, want full solve %+v", limits, got, want)
		}
		if stats.ComponentCount != 2 || stats.RecomputedComponents != 2 {
			t.Fatalf("stats with limits %+v = %+v, want two components recomputed", limits, stats)
		}
	}
}

func twoComponentValueFlowProgram(
	leftSource summary.FunctionID,
	leftSink summary.FunctionID,
	rightSource summary.FunctionID,
	rightSink summary.FunctionID,
) interproc.Program {
	return interproc.Program{
		Edges: []interproc.Edge{
			{From: valueFlowParamPort(leftSource, 0), To: valueFlowParamPort(leftSink, 0)},
			{From: valueFlowParamPort(rightSource, 0), To: valueFlowParamPort(rightSink, 0)},
		},
		Sources: []interproc.Source{
			{Port: valueFlowParamPort(leftSource, 0), Kind: "http_request"},
			{Port: valueFlowParamPort(rightSource, 0), Kind: "http_request"},
		},
		Sinks: []interproc.Sink{
			{Port: valueFlowParamPort(leftSink, 0), Kind: "sql"},
			{Port: valueFlowParamPort(rightSink, 0), Kind: "command"},
		},
	}
}

func valueFlowVersions(version string, ids ...summary.FunctionID) map[summary.FunctionID]string {
	versions := make(map[summary.FunctionID]string, len(ids))
	for _, id := range ids {
		versions[id] = version
	}
	return versions
}

func valueFlowParamPort(id summary.FunctionID, index int) interproc.Port {
	return interproc.Port{Func: interproc.FunctionID(id), Slot: interproc.Slot{Kind: interproc.SlotParam, Index: index}}
}
