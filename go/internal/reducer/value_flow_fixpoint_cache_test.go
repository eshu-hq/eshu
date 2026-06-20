package reducer

import (
	"context"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/interproc"
	"github.com/eshu-hq/eshu/go/internal/parser/summary"
	"github.com/eshu-hq/eshu/go/internal/parser/valueflow"
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

func TestValueFlowFixpointDurableCacheSurvivesRestart(t *testing.T) {
	t.Parallel()

	leftSource := summary.NewFunctionID("repo-a", "pkg", "", "leftSource")
	leftSink := summary.NewFunctionID("repo-a", "pkg", "", "leftSink")
	rightSource := summary.NewFunctionID("repo-b", "pkg", "", "rightSource")
	rightSink := summary.NewFunctionID("repo-b", "pkg", "", "rightSink")
	program := twoComponentValueFlowProgram(leftSource, leftSink, rightSource, rightSink)
	versions := valueFlowVersions("v1", leftSource, leftSink, rightSource, rightSink)
	store := newMemoryValueFlowFixpointComponentStore()

	_, first, err := SolveValueFlowProgramIncrementalDurable(
		context.Background(),
		program,
		versions,
		NewValueFlowFixpointCache(),
		store,
		interproc.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("first durable solve error = %v", err)
	}
	if first.RecomputedComponents != 2 || first.ReusedComponents != 0 {
		t.Fatalf("first stats = %+v, want two recomputed components", first)
	}

	_, second, err := SolveValueFlowProgramIncrementalDurable(
		context.Background(),
		program,
		versions,
		NewValueFlowFixpointCache(),
		store,
		interproc.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("second durable solve error = %v", err)
	}
	if second.RecomputedComponents != 0 || second.ReusedComponents != 2 || second.DurableReused != 2 {
		t.Fatalf("second stats = %+v, want restart to reuse both components", second)
	}
}

func TestValueFlowFixpointDurableCacheInvalidatesChangedFunctionVersion(t *testing.T) {
	t.Parallel()

	leftSource := summary.NewFunctionID("repo-a", "pkg", "", "leftSource")
	leftSink := summary.NewFunctionID("repo-a", "pkg", "", "leftSink")
	rightSource := summary.NewFunctionID("repo-b", "pkg", "", "rightSource")
	rightSink := summary.NewFunctionID("repo-b", "pkg", "", "rightSink")
	program := twoComponentValueFlowProgram(leftSource, leftSink, rightSource, rightSink)
	store := newMemoryValueFlowFixpointComponentStore()

	_, _, err := SolveValueFlowProgramIncrementalDurable(
		context.Background(),
		program,
		valueFlowVersions("v1", leftSource, leftSink, rightSource, rightSink),
		NewValueFlowFixpointCache(),
		store,
		interproc.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("warm durable solve error = %v", err)
	}

	_, stats, err := SolveValueFlowProgramIncrementalDurable(
		context.Background(),
		program,
		map[summary.FunctionID]string{
			leftSource:  "v1",
			leftSink:    "v1",
			rightSource: "v2",
			rightSink:   "v1",
		},
		NewValueFlowFixpointCache(),
		store,
		interproc.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("changed-version durable solve error = %v", err)
	}
	if stats.RecomputedComponents != 1 || stats.ReusedComponents != 1 || stats.DurableReused != 1 {
		t.Fatalf("changed-version stats = %+v, want one recomputed and one reused component", stats)
	}
}

func TestValueFlowFixpointDurableCacheInvalidatesChangedComponentEdges(t *testing.T) {
	t.Parallel()

	source := summary.NewFunctionID("repo-a", "pkg", "", "source")
	sink := summary.NewFunctionID("repo-b", "pkg", "", "sink")
	versions := valueFlowVersions("v1", source, sink)
	store := newMemoryValueFlowFixpointComponentStore()

	_, _, err := SolveValueFlowProgramIncrementalDurable(
		context.Background(),
		interproc.Program{
			Edges:   []interproc.Edge{{From: valueFlowParamPort(source, 0), To: valueFlowParamPort(sink, 0)}},
			Sources: []interproc.Source{{Port: valueFlowParamPort(source, 0), Kind: "http_request"}},
			Sinks:   []interproc.Sink{{Port: valueFlowParamPort(sink, 0), Kind: "sql"}},
		},
		versions,
		NewValueFlowFixpointCache(),
		store,
		interproc.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("warm durable solve error = %v", err)
	}

	result, stats, err := SolveValueFlowProgramIncrementalDurable(
		context.Background(),
		interproc.Program{
			Edges:   []interproc.Edge{{From: valueFlowParamPort(sink, 0), To: valueFlowParamPort(source, 0)}},
			Sources: []interproc.Source{{Port: valueFlowParamPort(source, 0), Kind: "http_request"}},
			Sinks:   []interproc.Sink{{Port: valueFlowParamPort(sink, 0), Kind: "sql"}},
		},
		versions,
		NewValueFlowFixpointCache(),
		store,
		interproc.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("changed-edge durable solve error = %v", err)
	}
	if stats.RecomputedComponents != 1 || stats.ReusedComponents != 0 {
		t.Fatalf("changed-edge stats = %+v, want recompute instead of stale durable reuse", stats)
	}
	if len(result.Findings) != 0 {
		t.Fatalf("changed-edge result reused stale finding: %+v", result.Findings)
	}
}

func TestValueFlowFixpointSnapshotDurableCacheAssemblesOnlyChangedComponent(t *testing.T) {
	t.Parallel()

	leftSource := summary.NewFunctionID("repo-a", "pkg", "", "leftSource")
	leftSink := summary.NewFunctionID("repo-a", "pkg", "", "leftSink")
	rightSource := summary.NewFunctionID("repo-b", "pkg", "", "rightSource")
	rightSink := summary.NewFunctionID("repo-b", "pkg", "", "rightSink")
	effects := twoComponentValueFlowEffects(leftSource, leftSink, rightSource, rightSink)
	sources := []interproc.Source{
		{Port: valueFlowParamPort(leftSource, 0), Kind: "http_request"},
		{Port: valueFlowParamPort(rightSource, 0), Kind: "http_request"},
	}
	versions := valueFlowVersions("v1", leftSource, leftSink, rightSource, rightSink)
	store := newMemoryValueFlowFixpointComponentStore()

	_, first, err := SolveValueFlowSnapshotIncrementalDurable(
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
		t.Fatalf("first snapshot solve error = %v", err)
	}
	if first.AssembledComponents != 2 || first.RecomputedComponents != 2 {
		t.Fatalf("first stats = %+v, want two assembled and recomputed components", first)
	}

	changed := map[summary.FunctionID]string{
		leftSource:  "v1",
		leftSink:    "v1",
		rightSource: "v2",
		rightSink:   "v1",
	}
	got, second, err := SolveValueFlowSnapshotIncrementalDurable(
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
		t.Fatalf("changed snapshot solve error = %v", err)
	}
	if second.AssembledComponents != 1 || second.RecomputedComponents != 1 ||
		second.ReusedComponents != 1 || second.DurableReused != 1 {
		t.Fatalf("second stats = %+v, want only changed component assembled and solved", second)
	}
	want := interproc.SolvePartitioned(valueflow.BuildProgram(effects, sources, nil), interproc.DefaultLimits())
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("snapshot incremental result = %+v, want full solve %+v", got, want)
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

func twoComponentValueFlowEffects(
	leftSource summary.FunctionID,
	leftSink summary.FunctionID,
	rightSource summary.FunctionID,
	rightSink summary.FunctionID,
) map[summary.FunctionID]summary.Effects {
	return map[summary.FunctionID]summary.Effects{
		leftSource: {
			ParamToCallArg: []summary.CallArgFlow{{Callee: leftSink, Param: 0, Arg: 0}},
		},
		leftSink: {
			ParamToSink: []summary.ParamSink{{Param: 0, SinkKind: "sql"}},
		},
		rightSource: {
			ParamToCallArg: []summary.CallArgFlow{{Callee: rightSink, Param: 0, Arg: 0}},
		},
		rightSink: {
			ParamToSink: []summary.ParamSink{{Param: 0, SinkKind: "command"}},
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

type memoryValueFlowFixpointComponentStore struct {
	entries map[string]interproc.Result
}

func newMemoryValueFlowFixpointComponentStore() *memoryValueFlowFixpointComponentStore {
	return &memoryValueFlowFixpointComponentStore{entries: map[string]interproc.Result{}}
}

func (s *memoryValueFlowFixpointComponentStore) LoadValueFlowFixpointComponents(
	_ context.Context,
	keys []string,
) (map[string]interproc.Result, error) {
	out := make(map[string]interproc.Result, len(keys))
	for _, key := range keys {
		if result, ok := s.entries[key]; ok {
			out[key] = result
		}
	}
	return out, nil
}

func (s *memoryValueFlowFixpointComponentStore) StoreValueFlowFixpointComponents(
	_ context.Context,
	entries map[string]interproc.Result,
) error {
	for key, result := range entries {
		s.entries[key] = result
	}
	return nil
}
