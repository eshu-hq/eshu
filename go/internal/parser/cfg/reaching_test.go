// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cfg

import (
	"fmt"
	"reflect"
	"sort"
	"testing"
)

// defUseKey renders a def->use edge as a stable string for set comparison.
func defUseKey(du DefUse) string {
	return fmt.Sprintf("%s:def=%d:use=%d", du.Binding, du.DefStmt, du.UseStmt)
}

func defUseSet(fn Function) []string {
	out := make([]string, 0, len(fn.DefUses))
	for _, du := range fn.DefUses {
		out = append(out, defUseKey(du))
	}
	sort.Strings(out)
	return out
}

// TestReachingDefinitionsStraightLine proves a later definition kills the
// earlier one on a straight-line path, so a use sees only the most recent def.
func TestReachingDefinitionsStraightLine(t *testing.T) {
	t.Parallel()

	b := NewBuilder(DefaultLimits())
	blk := b.AddBlock()
	sParam := b.AddStmt(blk, 1, []string{"x"}, nil)         // func param x
	sAssign := b.AddStmt(blk, 2, []string{"x"}, nil)        // x = 1
	sUse := b.AddStmt(blk, 3, []string{"y"}, []string{"x"}) // y = x

	fn := b.Build()

	want := []string{
		defUseKey(DefUse{Binding: "x", DefStmt: int(sAssign), UseStmt: int(sUse)}),
	}
	if got := defUseSet(fn); !reflect.DeepEqual(got, want) {
		t.Fatalf("def->use = %v, want %v (param def at %d must be killed by %d)",
			got, want, sParam, sAssign)
	}
}

// TestReachingDefinitionsBranchMerge proves both branch definitions reach a use
// at the merge while the pre-branch definition is killed on every path.
func TestReachingDefinitionsBranchMerge(t *testing.T) {
	t.Parallel()

	b := NewBuilder(DefaultLimits())
	entry := b.AddBlock()
	bTrue := b.AddBlock()
	bFalse := b.AddBlock()
	merge := b.AddBlock()

	sPre := b.AddStmt(entry, 1, []string{"x"}, nil)    // x = 0
	sTrue := b.AddStmt(bTrue, 2, []string{"x"}, nil)   // x = 1
	sFalse := b.AddStmt(bFalse, 3, []string{"x"}, nil) // x = 2
	sUse := b.AddStmt(merge, 4, nil, []string{"x"})    // return x

	b.AddEdge(entry, bTrue)
	b.AddEdge(entry, bFalse)
	b.AddEdge(bTrue, merge)
	b.AddEdge(bFalse, merge)

	fn := b.Build()

	want := []string{
		defUseKey(DefUse{Binding: "x", DefStmt: int(sTrue), UseStmt: int(sUse)}),
		defUseKey(DefUse{Binding: "x", DefStmt: int(sFalse), UseStmt: int(sUse)}),
	}
	sort.Strings(want)
	if got := defUseSet(fn); !reflect.DeepEqual(got, want) {
		t.Fatalf("def->use = %v, want %v (pre-branch def at %d must be killed)",
			got, want, sPre)
	}
}

// TestReachingDefinitionsLoopBackEdge proves a loop back-edge lets both the
// pre-loop definition (first iteration) and the in-loop definition (later
// iterations) reach a use at the loop head.
func TestReachingDefinitionsLoopBackEdge(t *testing.T) {
	t.Parallel()

	b := NewBuilder(DefaultLimits())
	pre := b.AddBlock()
	loop := b.AddBlock()

	sInit := b.AddStmt(pre, 1, []string{"x"}, nil)           // x = 0
	sUse := b.AddStmt(loop, 2, nil, []string{"x"})           // read x
	sInc := b.AddStmt(loop, 3, []string{"x"}, []string{"x"}) // x = x + 1

	b.AddEdge(pre, loop)
	b.AddEdge(loop, loop) // back-edge

	fn := b.Build()

	// The use at sUse sees the pre-loop init on entry and the increment via the
	// back-edge. The use inside sInc (x + 1) sees the same two reaching defs.
	want := []string{
		defUseKey(DefUse{Binding: "x", DefStmt: int(sInit), UseStmt: int(sUse)}),
		defUseKey(DefUse{Binding: "x", DefStmt: int(sInc), UseStmt: int(sUse)}),
		defUseKey(DefUse{Binding: "x", DefStmt: int(sInit), UseStmt: int(sInc)}),
		defUseKey(DefUse{Binding: "x", DefStmt: int(sInc), UseStmt: int(sInc)}),
	}
	sort.Strings(want)
	if got := defUseSet(fn); !reflect.DeepEqual(got, want) {
		t.Fatalf("def->use = %v, want %v", got, want)
	}
}

// TestBuildDeterministicOrder proves def->use edges are emitted in a stable
// sorted order so the facts hash identically across runs.
func TestBuildDeterministicOrder(t *testing.T) {
	t.Parallel()

	build := func() Function {
		b := NewBuilder(DefaultLimits())
		entry := b.AddBlock()
		bTrue := b.AddBlock()
		bFalse := b.AddBlock()
		merge := b.AddBlock()
		b.AddStmt(entry, 1, []string{"x"}, nil)
		b.AddStmt(bTrue, 2, []string{"x"}, nil)
		b.AddStmt(bFalse, 3, []string{"x"}, nil)
		b.AddStmt(merge, 4, nil, []string{"x"})
		b.AddEdge(entry, bTrue)
		b.AddEdge(entry, bFalse)
		b.AddEdge(bTrue, merge)
		b.AddEdge(bFalse, merge)
		return b.Build()
	}

	first := build()
	for i := 0; i < 5; i++ {
		if got := build(); !reflect.DeepEqual(got, first) {
			t.Fatalf("Build is not deterministic on run %d", i)
		}
	}
	// Confirm the slice itself is sorted by (UseStmt, DefStmt, Binding).
	if !sort.SliceIsSorted(first.DefUses, func(i, j int) bool {
		return defUseLess(first.DefUses[i], first.DefUses[j])
	}) {
		t.Fatalf("def->use edges are not emitted in sorted order: %+v", first.DefUses)
	}
}

// BenchmarkBuildReaching measures Build on a chain of branch-merge diamonds, a
// shape that exercises the worklist fixpoint across many blocks. It exists to
// give the dataflow pass a measured baseline; the pass is opt-in and bounded.
func BenchmarkBuildReaching(b *testing.B) {
	const diamonds = 64
	build := func() Function {
		bld := NewBuilder(DefaultLimits())
		cur := bld.AddBlock()
		bld.AddStmt(cur, 1, []string{"x"}, nil)
		for d := 0; d < diamonds; d++ {
			t := bld.AddBlock()
			f := bld.AddBlock()
			m := bld.AddBlock()
			bld.AddStmt(t, d*4+2, []string{"x"}, []string{"x"})
			bld.AddStmt(f, d*4+3, []string{"x"}, []string{"x"})
			bld.AddStmt(m, d*4+4, nil, []string{"x"})
			bld.AddEdge(cur, t)
			bld.AddEdge(cur, f)
			bld.AddEdge(t, m)
			bld.AddEdge(f, m)
			cur = m
		}
		return bld.Build()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = build()
	}
}

// TestDefUseEdgeOverflowCounted proves edge emission stops at the cap and counts
// the dropped edges instead of dropping them silently.
func TestDefUseEdgeOverflowCounted(t *testing.T) {
	t.Parallel()

	b := NewBuilder(Limits{MaxDefUseEdges: 1})
	blk := b.AddBlock()
	b.AddStmt(blk, 1, []string{"x"}, nil)
	b.AddStmt(blk, 2, nil, []string{"x"})
	b.AddStmt(blk, 3, nil, []string{"x"})
	b.AddStmt(blk, 4, nil, []string{"x"})

	fn := b.Build()
	if len(fn.DefUses) != 1 {
		t.Fatalf("emitted %d edges, want 1 (cap)", len(fn.DefUses))
	}
	if fn.Overflow.DefUseEdges != 2 {
		t.Fatalf("overflow count = %d, want 2 dropped edges", fn.Overflow.DefUseEdges)
	}
}
