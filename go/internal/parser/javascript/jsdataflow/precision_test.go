package jsdataflow

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/taint"
)

// TestLowerFieldWriteDefinesAccessPath proves a member-expression assignment
// target defines its field-sensitive access path, so a later read of the same
// path is reached by it. Whole-binding lowering dropped the def entirely, a
// false negative for taint that flows through a struct/object field.
func TestLowerFieldWriteDefinesAccessPath(t *testing.T) {
	t.Parallel()

	src := "function f(p) {\n" +
		"\tobj.data = p;\n" +
		"\tuse(obj.data);\n" +
		"}"
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)
	if !contains(got, "obj.data:2->3") {
		t.Fatalf("field write did not define obj.data; got %v", got)
	}
	if !contains(got, "p:1->2") {
		t.Fatalf("param p did not reach the field write; got %v", got)
	}
}

// TestLowerSelectorReadsAreFieldSensitive proves distinct fields are distinct
// bindings: a read of obj.a is reached by the write to obj.a, not by the write
// to obj.b.
func TestLowerSelectorReadsAreFieldSensitive(t *testing.T) {
	t.Parallel()

	src := "function f(src, clean) {\n" +
		"\tobj.a = src;\n" +
		"\tobj.b = clean;\n" +
		"\tuse(obj.a);\n" +
		"}"
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)
	if !contains(got, "obj.a:2->4") {
		t.Fatalf("read of obj.a not reached by its write; got %v", got)
	}
	if contains(got, "obj.b:3->4") {
		t.Fatalf("read of obj.a wrongly reached by write to obj.b (field-insensitive); got %v", got)
	}
}

// TestLowerContainerElementWholeContainerApproximation proves an indexed
// assignment and read lower to the explicitly labeled whole-container
// approximation m[*], so element flow is captured without inventing per-key
// precision.
func TestLowerContainerElementWholeContainerApproximation(t *testing.T) {
	t.Parallel()

	src := "function f(p, k) {\n" +
		"\tm[k] = p;\n" +
		"\tuse(m[k]);\n" +
		"}"
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)
	if !contains(got, "m[*]:2->3") {
		t.Fatalf("container element flow not captured as m[*]; got %v", got)
	}
}

// TestLowerAliasContainerElementNormalizesWrite proves a container write through
// a reference alias normalizes to the original container before the [*] marker is
// applied, so it reaches a read through the original binding.
func TestLowerAliasContainerElementNormalizesWrite(t *testing.T) {
	t.Parallel()

	src := "function f(p, k) {\n" +
		"\tlet a = m;\n" +
		"\ta[k] = p;\n" +
		"\tuse(m[k]);\n" +
		"}"
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)
	if !contains(got, "m[*]:3->4") {
		t.Fatalf("alias container write did not normalize to m[*]; got %v", got)
	}
}

// TestLowerReferenceAliasNormalizesFieldWrite proves a field write through a
// reference alias (let a = obj; a.data = p) normalizes to the aliased object, so
// a later read of obj.data is reached by it.
func TestLowerReferenceAliasNormalizesFieldWrite(t *testing.T) {
	t.Parallel()

	src := "function f(p) {\n" +
		"\tlet a = obj;\n" +
		"\ta.data = p;\n" +
		"\tuse(obj.data);\n" +
		"}"
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)
	if !contains(got, "obj.data:3->4") {
		t.Fatalf("alias field write did not normalize to obj.data; got %v", got)
	}
}

// TestLowerAliasChainNormalizesFieldWrite proves a multi-hop reference alias
// chain (b = a = obj) resolves a field write through the chain to the root object.
func TestLowerAliasChainNormalizesFieldWrite(t *testing.T) {
	t.Parallel()

	src := "function f(p) {\n" +
		"\tlet a = obj;\n" +
		"\tlet b = a;\n" +
		"\tb.data = p;\n" +
		"\tuse(obj.data);\n" +
		"}"
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)
	if !contains(got, "obj.data:4->5") {
		t.Fatalf("alias chain did not normalize b.data to obj.data; got %v", got)
	}
}

// TestLowerAliasConflictingAcrossBranchesIsDropped proves an alias that differs
// between the then and fall-through paths is dropped at the merge, so a later
// field write keeps its own (unresolved) identity rather than normalizing to one
// branch's target.
func TestLowerAliasConflictingAcrossBranchesIsDropped(t *testing.T) {
	t.Parallel()

	src := "function f(p, cond) {\n" +
		"\tlet a = obj;\n" +
		"\tif (cond) { a = other; }\n" +
		"\ta.data = p;\n" +
		"\tuse(a.data);\n" +
		"}"
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)
	if !contains(got, "a.data:4->5") {
		t.Fatalf("conflicting alias should leave a.data unresolved; got %v", got)
	}
	if contains(got, "obj.data:4->5") {
		t.Fatalf("then-branch-only alias obj leaked past the merge; got %v", got)
	}
}

// TestLowerLoopBodyAliasInvalidationDropsPostLoopAlias proves a loop body that
// may reassign an alias prevents post-loop field writes from being normalized to
// the pre-loop object. The loop may run, so carrying only the zero-iteration
// alias would invent a false edge.
func TestLowerLoopBodyAliasInvalidationDropsPostLoopAlias(t *testing.T) {
	t.Parallel()

	src := "function f(p, cond) {\n" +
		"\tlet a = obj;\n" +
		"\twhile (cond) { a = other; }\n" +
		"\ta.data = p;\n" +
		"\tuse(a.data);\n" +
		"\tuse(obj.data);\n" +
		"}"
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)
	if !contains(got, "a.data:4->5") {
		t.Fatalf("post-loop write should stay on a.data after alias invalidation; got %v", got)
	}
	if contains(got, "obj.data:4->6") {
		t.Fatalf("loop-mutated alias wrongly normalized post-loop write to obj.data; got %v", got)
	}
}

// TestLowerAccessPathTruncationIsLabeled proves an access path deeper than
// MaxAccessPathParts truncates to a "*"-suffixed prefix on both the write and
// the read (so they still match) and counts an overflow rather than dropping it
// silently.
func TestLowerAccessPathTruncationIsLabeled(t *testing.T) {
	t.Parallel()

	src := "function f(p) {\n" +
		"\ta.b.c.d.e = p;\n" +
		"\tuse(a.b.c.d.e);\n" +
		"}"
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)
	if !contains(got, "a.b.c.d.*:2->3") {
		t.Fatalf("deep access path not truncated to a.b.c.d.*; got %v", got)
	}
	if fn.Overflow.AccessPaths == 0 {
		t.Fatalf("access-path truncation not counted in Overflow.AccessPaths")
	}
}

// TestLowerClosureCaptureUsesOuterDefinition proves a variable captured by an
// invoked callback is attributed to the enclosing function, so the outer
// definition reaches the capture site.
func TestLowerClosureCaptureUsesOuterDefinition(t *testing.T) {
	t.Parallel()

	src := "function f(p) {\n" +
		"\tlet v = p;\n" +
		"\tdoThing(() => sink(v));\n" +
		"}"
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)
	if !contains(got, "v:2->3") {
		t.Fatalf("captured variable v not attributed to enclosing function; got %v", got)
	}
}

// TestLowerClosureInnerShadowIsNotCaptured proves a closure-local redefinition
// shadows the outer variable, so the outer definition is not treated as captured.
func TestLowerClosureInnerShadowIsNotCaptured(t *testing.T) {
	t.Parallel()

	src := "function f(p) {\n" +
		"\tlet v = p;\n" +
		"\tdoThing(() => { let v = other; sink(v); });\n" +
		"}"
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)
	if contains(got, "v:2->3") {
		t.Fatalf("shadowed inner v wrongly captured the outer v; got %v", got)
	}
}

// TestTSFieldSensitiveTaintReachesSink proves taint flowing through an object
// field (a whole-binding false negative) is now reported as TAINTED.
func TestTSFieldSensitiveTaintReachesSink(t *testing.T) {
	t.Parallel()

	node, source, fn := parseFirstFunction(t, "import type { Request } from 'express';\n"+
		"function handler(req: Request) {\n"+
		"\tlet obj = {};\n"+
		"\tobj.data = req.body;\n"+
		"\tdb.query(obj.data);\n"+
		"}")
	facts := TaintFacts(node, source, fn)
	res := taint.Analyze(fn, facts, taint.DefaultLimits())
	if taintedCount(res, "sql") != 1 {
		t.Fatalf("want 1 TAINTED sql finding through obj.data, got %+v", res.Findings)
	}
}

// TestTSContainerElementTaintReachesSink proves taint flowing through a
// container element is reported as TAINTED.
func TestTSContainerElementTaintReachesSink(t *testing.T) {
	t.Parallel()

	node, source, fn := parseFirstFunction(t, "import type { Request } from 'express';\n"+
		"function handler(req: Request, key) {\n"+
		"\tlet m = {};\n"+
		"\tm[key] = req.body;\n"+
		"\tdb.query(m[key]);\n"+
		"}")
	facts := TaintFacts(node, source, fn)
	res := taint.Analyze(fn, facts, taint.DefaultLimits())
	if taintedCount(res, "sql") != 1 {
		t.Fatalf("want 1 TAINTED sql finding through m[*], got %+v", res.Findings)
	}
}

// TestTSAliasContainerElementTaintReachesSink proves taint written through a
// reference alias into a container element is read through the original
// container's [*] path.
func TestTSAliasContainerElementTaintReachesSink(t *testing.T) {
	t.Parallel()

	node, source, fn := parseFirstFunction(t, "import type { Request } from 'express';\n"+
		"function handler(req: Request, key) {\n"+
		"\tlet m = {};\n"+
		"\tlet a = m;\n"+
		"\ta[key] = req.body;\n"+
		"\tdb.query(m[key]);\n"+
		"}")
	facts := TaintFacts(node, source, fn)
	res := taint.Analyze(fn, facts, taint.DefaultLimits())
	if taintedCount(res, "sql") != 1 {
		t.Fatalf("want 1 TAINTED sql finding through alias m[*], got %+v", res.Findings)
	}
}

// TestTSAliasFieldTaintReachesSink proves taint written through a reference
// alias and read through the original object is reported as TAINTED.
func TestTSAliasFieldTaintReachesSink(t *testing.T) {
	t.Parallel()

	node, source, fn := parseFirstFunction(t, "import type { Request } from 'express';\n"+
		"function handler(req: Request) {\n"+
		"\tlet obj = {};\n"+
		"\tlet a = obj;\n"+
		"\ta.data = req.body;\n"+
		"\tdb.query(obj.data);\n"+
		"}")
	facts := TaintFacts(node, source, fn)
	res := taint.Analyze(fn, facts, taint.DefaultLimits())
	if taintedCount(res, "sql") != 1 {
		t.Fatalf("want 1 TAINTED sql finding through alias obj.data, got %+v", res.Findings)
	}
}

// TestLowerForOfTargetAliasDoesNotLeak proves a reference alias on a name that a
// for-of loop rebinds each iteration does not survive the loop. After the loop
// the name may hold a loop element, not the pre-loop object, so a field write
// through it must NOT normalize to the pre-loop object (a false edge).
func TestLowerForOfTargetAliasDoesNotLeak(t *testing.T) {
	t.Parallel()

	src := "function f(p) {\n" +
		"\tlet a = obj;\n" +
		"\tfor (a of items) {}\n" +
		"\ta.x = p;\n" +
		"\tuse(obj.x);\n" +
		"}"
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)
	if contains(got, "obj.x:4->5") {
		t.Fatalf("for-of target alias leaked: a.x wrongly normalized to obj.x; got %v", got)
	}
}

// TestLowerForBodyReassignAliasDoesNotLeak proves an alias reassigned inside a
// C-style loop body does not get restored to its pre-loop target after the loop.
func TestLowerForBodyReassignAliasDoesNotLeak(t *testing.T) {
	t.Parallel()

	src := "function f(p, n) {\n" +
		"\tlet a = obj;\n" +
		"\tfor (let i = 0; i < n; i = i + 1) {\n" +
		"\t\ta = other;\n" +
		"\t}\n" +
		"\ta.x = p;\n" +
		"\tuse(obj.x);\n" +
		"}"
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)
	if contains(got, "obj.x:6->7") {
		t.Fatalf("loop-body reassigned alias leaked: a.x wrongly normalized to obj.x; got %v", got)
	}
}
