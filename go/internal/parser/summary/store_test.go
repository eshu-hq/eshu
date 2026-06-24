// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary

import (
	"reflect"
	"strconv"
	"testing"
)

func id(name string) FunctionID { return FunctionID(name) }

// calls builds Effects for a function that calls the given callees and flows its
// first parameter to the return.
func calls(callees ...FunctionID) Effects {
	var flows []CallArgFlow
	for _, c := range callees {
		flows = append(flows, CallArgFlow{Callee: c, Param: 0, Arg: 0})
	}
	return Effects{ParamToReturn: []int{0}, ParamToCallArg: flows}
}

// chainFixture builds A->B->C and an independent D->E.
func chainFixture() map[FunctionID]Effects {
	return map[FunctionID]Effects{
		id("A"): calls(id("B")),
		id("B"): calls(id("C")),
		id("C"): {ParamToReturn: []int{0}},
		id("D"): calls(id("E")),
		id("E"): {ParamToReturn: []int{0}},
	}
}

// TestUpsertComputesAllVersionsFirstTime proves the first Upsert recomputes every
// function and assigns a version to each.
func TestUpsertComputesAllVersionsFirstTime(t *testing.T) {
	t.Parallel()

	s := NewStore()
	recomputed := s.Upsert(chainFixture())

	want := []FunctionID{id("A"), id("B"), id("C"), id("D"), id("E")}
	if !reflect.DeepEqual(recomputed, want) {
		t.Fatalf("recomputed = %v, want %v", recomputed, want)
	}
	for _, fn := range want {
		if _, ok := s.Version(fn); !ok {
			t.Fatalf("no version for %s", fn)
		}
	}
}

// TestReUpsertSameEffectsRecomputesNothing proves an idempotent Upsert (same
// effects) recomputes nothing.
func TestReUpsertSameEffectsRecomputesNothing(t *testing.T) {
	t.Parallel()

	s := NewStore()
	s.Upsert(chainFixture())
	recomputed := s.Upsert(chainFixture())
	if len(recomputed) != 0 {
		t.Fatalf("re-upsert recomputed %v, want none", recomputed)
	}
}

// TestIncrementalRecomposeOnlyAffected is the core delta proof: changing one
// callee recomputes only that function and its transitive callers, not unrelated
// functions.
func TestIncrementalRecomposeOnlyAffected(t *testing.T) {
	t.Parallel()

	s := NewStore()
	s.Upsert(chainFixture())

	// Change C's own facts (add a param->sink flow).
	changed := map[FunctionID]Effects{
		id("C"): {ParamToReturn: []int{0}, ParamToSink: []ParamSink{{Param: 0, SinkKind: "sql"}}},
	}
	recomputed := s.Upsert(changed)

	want := []FunctionID{id("A"), id("B"), id("C")}
	if !reflect.DeepEqual(recomputed, want) {
		t.Fatalf("recomputed = %v, want %v (D and E must be untouched)", recomputed, want)
	}
}

// TestVersionsPropagateThroughCallers proves the changed callee's new version
// changes its callers' versions but leaves independent functions stable.
func TestVersionsPropagateThroughCallers(t *testing.T) {
	t.Parallel()

	s := NewStore()
	s.Upsert(chainFixture())
	before := map[FunctionID]string{}
	for _, fn := range s.IDs() {
		before[fn], _ = s.Version(fn)
	}

	s.Upsert(map[FunctionID]Effects{
		id("C"): {ParamToReturn: []int{0}, ParamToSink: []ParamSink{{Param: 0, SinkKind: "sql"}}},
	})

	for _, fn := range []FunctionID{id("A"), id("B"), id("C")} {
		now, _ := s.Version(fn)
		if now == before[fn] {
			t.Fatalf("%s version did not change after C changed", fn)
		}
	}
	for _, fn := range []FunctionID{id("D"), id("E")} {
		now, _ := s.Version(fn)
		if now != before[fn] {
			t.Fatalf("%s version changed but should be independent of C", fn)
		}
	}
}

// TestRecursionTerminates proves the version fixpoint terminates on a cyclic call
// graph (mutual recursion) and assigns every function a version.
func TestRecursionTerminates(t *testing.T) {
	t.Parallel()

	s := NewStore()
	recomputed := s.Upsert(map[FunctionID]Effects{
		id("A"): calls(id("B")),
		id("B"): calls(id("A")), // cycle
		id("C"): calls(id("A")),
	})
	if len(recomputed) != 3 {
		t.Fatalf("recomputed %d, want 3", len(recomputed))
	}
	for _, fn := range []FunctionID{id("A"), id("B"), id("C")} {
		if _, ok := s.Version(fn); !ok {
			t.Fatalf("no version for %s in cycle", fn)
		}
	}
}

// TestCalleeSetShrinkDropsReverseEdge proves that when a function stops calling a
// callee, the reverse edge is removed so later changes to that callee no longer
// recompute the former caller.
func TestCalleeSetShrinkDropsReverseEdge(t *testing.T) {
	t.Parallel()

	s := NewStore()
	s.Upsert(map[FunctionID]Effects{
		id("A"): calls(id("B"), id("C")),
		id("B"): {ParamToReturn: []int{0}},
		id("C"): {ParamToReturn: []int{0}},
	})

	// A drops its call to C.
	s.Upsert(map[FunctionID]Effects{id("A"): calls(id("B"))})

	// Changing C must no longer recompute A.
	recomputed := s.Upsert(map[FunctionID]Effects{
		id("C"): {ParamToReturn: []int{0}, ParamToSink: []ParamSink{{Param: 0, SinkKind: "sql"}}},
	})
	for _, fn := range recomputed {
		if fn == id("A") {
			t.Fatalf("A recomputed after it stopped calling C; recomputed=%v", recomputed)
		}
	}
	if len(recomputed) != 1 || recomputed[0] != id("C") {
		t.Fatalf("recomputed = %v, want [C] only", recomputed)
	}
}

// TestDeepChainTerminates proves a deep linear call chain condenses without a
// stack overflow (the iterative Tarjan path).
func TestDeepChainTerminates(t *testing.T) {
	t.Parallel()

	const depth = 20000
	updates := map[FunctionID]Effects{}
	for i := 0; i < depth; i++ {
		self := FunctionID("f" + strconv.Itoa(i))
		if i+1 < depth {
			updates[self] = calls(FunctionID("f" + strconv.Itoa(i+1)))
		} else {
			updates[self] = Effects{ParamToReturn: []int{0}}
		}
	}
	s := NewStore()
	if got := len(s.Upsert(updates)); got != depth {
		t.Fatalf("recomputed %d, want %d", got, depth)
	}
}

// TestIdentityStableAcrossGenerations proves NewFunctionID is generation
// independent: identical durable attributes yield identical IDs.
func TestIdentityStableAcrossGenerations(t *testing.T) {
	t.Parallel()

	a := NewFunctionID("repo", "pkg/handlers", "Server", "Handle")
	b := NewFunctionID("repo", "pkg/handlers", "Server", "Handle")
	if a != b {
		t.Fatalf("identity not stable: %q != %q", a, b)
	}
	if a == NewFunctionID("repo", "pkg/handlers", "Server", "Other") {
		t.Fatalf("distinct functions collided")
	}
}
