package cfg

import (
	"reflect"
	"testing"
)

// TestControlDependenceIfThenBranch proves a branch predicate controls the
// then-only block but not the post-if merge block.
func TestControlDependenceIfThenBranch(t *testing.T) {
	t.Parallel()

	b := NewBuilder(DefaultLimits())
	entry := b.AddBlock()
	thenBlock := b.AddBlock()
	merge := b.AddBlock()
	guard := b.AddGuardStmt(entry, 10, []string{"allowed"}, "allowed")
	b.AddStmt(thenBlock, 11, nil, []string{"secret"})
	b.AddStmt(merge, 12, nil, []string{"public"})
	b.AddGuardedEdge(entry, thenBlock, "allowed")
	b.AddGuardedEdge(entry, merge, "!(allowed)")
	b.AddEdge(thenBlock, merge)

	fn := b.Build()
	if len(fn.ControlDependencies) != 1 {
		t.Fatalf("control dependencies = %+v, want one then-branch dependency", fn.ControlDependencies)
	}
	dep := fn.ControlDependencies[0]
	if dep.GuardStmt != int(guard) || dep.GuardLine != 10 || dep.Guard != "allowed" {
		t.Fatalf("guard not carried from predicate statement: %+v", dep)
	}
	if dep.DependentBlock != int(thenBlock) {
		t.Fatalf("dependent block = %d, want then block %d", dep.DependentBlock, thenBlock)
	}
}

func controlDepsByBlock(fn Function) map[int][]string {
	out := map[int][]string{}
	for _, dep := range fn.ControlDependencies {
		out[dep.DependentBlock] = append(out[dep.DependentBlock], dep.Guard)
	}
	return out
}

// TestControlDependenceIfElseBranches proves mutually exclusive branch bodies
// carry branch-polarized predicate provenance while the post-if merge does not.
func TestControlDependenceIfElseBranches(t *testing.T) {
	t.Parallel()

	b := NewBuilder(DefaultLimits())
	entry := b.AddBlock()
	thenBlock := b.AddBlock()
	elseBlock := b.AddBlock()
	merge := b.AddBlock()
	b.AddGuardStmt(entry, 10, []string{"allowed"}, "allowed")
	b.AddStmt(thenBlock, 11, nil, []string{"a"})
	b.AddStmt(elseBlock, 12, nil, []string{"b"})
	b.AddStmt(merge, 13, nil, []string{"c"})
	b.AddGuardedEdge(entry, thenBlock, "allowed")
	b.AddGuardedEdge(entry, elseBlock, "!(allowed)")
	b.AddEdge(thenBlock, merge)
	b.AddEdge(elseBlock, merge)

	got := controlDepsByBlock(b.Build())
	want := map[int][]string{
		int(thenBlock): {"allowed"},
		int(elseBlock): {"!(allowed)"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("control deps = %+v, want %+v", got, want)
	}
}

// TestControlDependenceEarlyReturnUsesSyntheticExit proves a fallthrough block
// is still control-dependent when the alternate branch exits the function.
func TestControlDependenceEarlyReturnUsesSyntheticExit(t *testing.T) {
	t.Parallel()

	b := NewBuilder(DefaultLimits())
	entry := b.AddBlock()
	returnBlock := b.AddBlock()
	fallthroughBlock := b.AddBlock()
	b.AddGuardStmt(entry, 10, []string{"blocked"}, "blocked")
	b.AddStmt(returnBlock, 11, nil, []string{"err"})
	b.AddStmt(fallthroughBlock, 12, nil, []string{"secret"})
	b.AddGuardedEdge(entry, returnBlock, "blocked")
	b.AddGuardedEdge(entry, fallthroughBlock, "!(blocked)")

	got := controlDepsByBlock(b.Build())
	want := map[int][]string{
		int(returnBlock):      {"blocked"},
		int(fallthroughBlock): {"!(blocked)"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("control deps = %+v, want %+v", got, want)
	}
}

// TestControlDependenceLoopCondition proves a loop condition controls the body
// but not the post-loop exit block.
func TestControlDependenceLoopCondition(t *testing.T) {
	t.Parallel()

	b := NewBuilder(DefaultLimits())
	pre := b.AddBlock()
	header := b.AddBlock()
	body := b.AddBlock()
	exit := b.AddBlock()
	b.AddStmt(pre, 1, []string{"i"}, nil)
	b.AddGuardStmt(header, 2, []string{"i"}, "i < n")
	b.AddStmt(body, 3, []string{"i"}, []string{"i"})
	b.AddStmt(exit, 4, nil, []string{"i"})
	b.AddEdge(pre, header)
	b.AddGuardedEdge(header, body, "i < n")
	b.AddGuardedEdge(header, exit, "!(i < n)")
	b.AddEdge(body, header)

	got := controlDepsByBlock(b.Build())
	want := map[int][]string{int(body): {"i < n"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("control deps = %+v, want %+v", got, want)
	}
}

// TestControlDependenceOverflowCounted proves dependency emission is capped and
// records dropped entries deterministically.
func TestControlDependenceOverflowCounted(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	limits.MaxControlDependencies = 1
	b := NewBuilder(limits)
	entry := b.AddBlock()
	thenBlock := b.AddBlock()
	elseBlock := b.AddBlock()
	merge := b.AddBlock()
	b.AddGuardStmt(entry, 10, []string{"allowed"}, "allowed")
	b.AddStmt(thenBlock, 11, nil, []string{"a"})
	b.AddStmt(elseBlock, 12, nil, []string{"b"})
	b.AddStmt(merge, 13, nil, []string{"c"})
	b.AddGuardedEdge(entry, thenBlock, "allowed")
	b.AddGuardedEdge(entry, elseBlock, "!(allowed)")
	b.AddEdge(thenBlock, merge)
	b.AddEdge(elseBlock, merge)

	fn := b.Build()
	if len(fn.ControlDependencies) != 1 {
		t.Fatalf("emitted %d deps, want cap of 1: %+v", len(fn.ControlDependencies), fn.ControlDependencies)
	}
	if fn.Overflow.ControlDependencies != 1 {
		t.Fatalf("control dep overflow = %d, want 1", fn.Overflow.ControlDependencies)
	}
}
