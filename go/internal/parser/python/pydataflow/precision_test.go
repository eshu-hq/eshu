// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pydataflow

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/taint"
)

// TestLowerAttributeWriteDefinesAccessPath proves an attribute assignment target
// defines its field-sensitive access path, so a later read of the same path is
// reached by it. Whole-binding lowering dropped the def entirely, a false
// negative for taint that flows through an object attribute.
func TestLowerAttributeWriteDefinesAccessPath(t *testing.T) {
	t.Parallel()

	src := "def f(p):\n" +
		"    obj.data = p\n" +
		"    use(obj.data)\n"
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)
	if !contains(got, "obj.data:2->3") {
		t.Fatalf("attribute write did not define obj.data; got %v", got)
	}
	if !contains(got, "p:1->2") {
		t.Fatalf("param p did not reach the attribute write; got %v", got)
	}
}

// TestLowerAttributeReadsAreFieldSensitive proves distinct attributes are
// distinct bindings.
func TestLowerAttributeReadsAreFieldSensitive(t *testing.T) {
	t.Parallel()

	src := "def f(src, clean):\n" +
		"    obj.a = src\n" +
		"    obj.b = clean\n" +
		"    use(obj.a)\n"
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)
	if !contains(got, "obj.a:2->4") {
		t.Fatalf("read of obj.a not reached by its write; got %v", got)
	}
	if contains(got, "obj.b:3->4") {
		t.Fatalf("read of obj.a wrongly reached by write to obj.b; got %v", got)
	}
}

// TestLowerSubscriptWholeContainerApproximation proves an indexed assignment and
// read lower to the explicitly labeled whole-container approximation d[*].
func TestLowerSubscriptWholeContainerApproximation(t *testing.T) {
	t.Parallel()

	src := "def f(p, k):\n" +
		"    d[k] = p\n" +
		"    use(d[k])\n"
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)
	if !contains(got, "d[*]:2->3") {
		t.Fatalf("container element flow not captured as d[*]; got %v", got)
	}
}

// TestLowerReferenceAliasNormalizesAttributeWrite proves an attribute write
// through a reference alias (a = obj; a.data = p) normalizes to the aliased
// object.
func TestLowerReferenceAliasNormalizesAttributeWrite(t *testing.T) {
	t.Parallel()

	src := "def f(p):\n" +
		"    a = obj\n" +
		"    a.data = p\n" +
		"    use(obj.data)\n"
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)
	if !contains(got, "obj.data:3->4") {
		t.Fatalf("alias attribute write did not normalize to obj.data; got %v", got)
	}
}

// TestLowerAliasChainNormalizesAttributeWrite proves a multi-hop alias chain
// resolves an attribute write to the root object.
func TestLowerAliasChainNormalizesAttributeWrite(t *testing.T) {
	t.Parallel()

	src := "def f(p):\n" +
		"    a = obj\n" +
		"    b = a\n" +
		"    b.data = p\n" +
		"    use(obj.data)\n"
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)
	if !contains(got, "obj.data:4->5") {
		t.Fatalf("alias chain did not normalize b.data to obj.data; got %v", got)
	}
}

// TestLowerNestedAttributeReadPreservesRootObjectUse proves a nested attribute
// read also records the root object. Whole-object definitions must still reach
// deep attribute reads when no intermediate object definition exists.
func TestLowerNestedAttributeReadPreservesRootObjectUse(t *testing.T) {
	t.Parallel()

	src := "def f(p):\n" +
		"    obj = build()\n" +
		"    use(obj.profile.name)\n"
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)
	if !contains(got, "obj:2->3") {
		t.Fatalf("nested attribute read did not preserve root object use; got %v", got)
	}
}

// TestLowerDestructuringAssignmentDropsAlias proves tuple/list assignment
// targets clear stale aliases for every identifier they rebind.
func TestLowerDestructuringAssignmentDropsAlias(t *testing.T) {
	t.Parallel()

	src := "def f(p, row):\n" +
		"    a = obj\n" +
		"    a, _ = row\n" +
		"    a.x = p\n" +
		"    use(obj.x)\n"
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)
	if contains(got, "obj.x:4->5") {
		t.Fatalf("destructuring rebind left stale alias a -> obj; got %v", got)
	}
}

// TestLowerStarredAssignmentBindsElements proves tuple/list unpacking with a
// starred target defines every bound name and reads the source container element
// approximation.
func TestLowerStarredAssignmentBindsElements(t *testing.T) {
	t.Parallel()

	src := "def f(p, k):\n" +
		"    row[k] = p\n" +
		"    first, *rest = row\n" +
		"    use(first)\n" +
		"    use(rest)\n"
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)
	if !contains(got, "row[*]:2->3") {
		t.Fatalf("starred unpacking did not read row[*]; got %v", got)
	}
	if !contains(got, "first:3->4") {
		t.Fatalf("starred unpacking did not define first; got %v", got)
	}
	if !contains(got, "rest:3->5") {
		t.Fatalf("starred unpacking did not define rest; got %v", got)
	}
}

// TestLowerForStarredTargetBindsElements proves for-loop unpacking with a
// starred target defines every bound name and reads the iterable element.
func TestLowerForStarredTargetBindsElements(t *testing.T) {
	t.Parallel()

	src := "def f(p, k):\n" +
		"    rows[k] = p\n" +
		"    for first, *rest in rows:\n" +
		"        use(first)\n" +
		"        use(rest)\n"
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)
	if !contains(got, "rows[*]:2->3") {
		t.Fatalf("for-unpacking did not read rows[*]; got %v", got)
	}
	if !contains(got, "first:3->4") {
		t.Fatalf("for-unpacking did not define first; got %v", got)
	}
	if !contains(got, "rest:3->5") {
		t.Fatalf("for-unpacking did not define rest; got %v", got)
	}
}

// TestLowerStarParametersDefineBindings proves Python *args and **kwargs
// parameters are entry definitions for their bound identifiers.
func TestLowerStarParametersDefineBindings(t *testing.T) {
	t.Parallel()

	src := "def f(*items, **kw):\n" +
		"    use(items)\n" +
		"    use(kw)\n"
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)
	if !contains(got, "items:1->2") {
		t.Fatalf("*items parameter did not define items; got %v", got)
	}
	if !contains(got, "kw:1->3") {
		t.Fatalf("**kw parameter did not define kw; got %v", got)
	}
}

// TestLowerAliasConflictingAcrossBranchesIsDropped proves an alias that differs
// between the then and fall-through paths is dropped at the merge.
func TestLowerAliasConflictingAcrossBranchesIsDropped(t *testing.T) {
	t.Parallel()

	src := "def f(p, cond):\n" +
		"    a = obj\n" +
		"    if cond:\n" +
		"        a = other\n" +
		"    a.data = p\n" +
		"    use(a.data)\n"
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)
	if !contains(got, "a.data:5->6") {
		t.Fatalf("conflicting alias should leave a.data unresolved; got %v", got)
	}
	if contains(got, "obj.data:5->6") {
		t.Fatalf("then-branch-only alias obj leaked past the merge; got %v", got)
	}
}

// TestLowerForLoopTargetAliasDoesNotLeak proves a reference alias on a name that
// the loop rebinds each iteration does not survive the loop. After the loop the
// name may hold a loop element, not the pre-loop object, so an attribute write
// through it must NOT normalize to the pre-loop object — that would be a false
// edge.
func TestLowerForLoopTargetAliasDoesNotLeak(t *testing.T) {
	t.Parallel()

	src := "def f(p):\n" +
		"    a = obj\n" +
		"    for a in items:\n" +
		"        a = obj\n" +
		"    a.x = p\n" +
		"    use(obj.x)\n"
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)
	if contains(got, "obj.x:5->6") {
		t.Fatalf("loop-target alias leaked: a.x wrongly normalized to obj.x; got %v", got)
	}
}

// TestLowerAccessPathTruncationIsLabeled proves an access path deeper than
// MaxAccessPathParts truncates to a "*"-suffixed prefix on both write and read
// and counts an overflow rather than dropping it silently.
func TestLowerAccessPathTruncationIsLabeled(t *testing.T) {
	t.Parallel()

	src := "def f(p):\n" +
		"    a.b.c.d.e = p\n" +
		"    use(a.b.c.d.e)\n"
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)
	if !contains(got, "a.b.c.d.*:2->3") {
		t.Fatalf("deep access path not truncated to a.b.c.d.*; got %v", got)
	}
	if fn.Overflow.AccessPaths == 0 {
		t.Fatalf("access-path truncation not counted in Overflow.AccessPaths")
	}
}

// TestLowerLambdaCaptureUsesOuterDefinition proves a variable captured by an
// invoked lambda is attributed to the enclosing function.
func TestLowerLambdaCaptureUsesOuterDefinition(t *testing.T) {
	t.Parallel()

	src := "def f(p):\n" +
		"    v = p\n" +
		"    do(lambda: sink(v))\n"
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)
	if !contains(got, "v:2->3") {
		t.Fatalf("captured variable v not attributed to enclosing function; got %v", got)
	}
}

// TestLowerLambdaParamShadowsOuter proves a lambda parameter shadows the outer
// variable, so the outer definition is not treated as captured.
func TestLowerLambdaParamShadowsOuter(t *testing.T) {
	t.Parallel()

	src := "def f(p):\n" +
		"    v = p\n" +
		"    do(lambda v: sink(v))\n"
	fn := lowerFirstFunction(t, src)
	got := defUseLines(fn)
	if contains(got, "v:2->3") {
		t.Fatalf("lambda parameter v wrongly captured the outer v; got %v", got)
	}
}

// TestPyFieldSensitiveTaintReachesSink proves taint flowing through an object
// attribute (a whole-binding false negative) is now reported as TAINTED.
func TestPyFieldSensitiveTaintReachesSink(t *testing.T) {
	t.Parallel()

	node, source, fn := parseFirstPyFunction(t, "from fastapi import Request\n\n"+
		"def view(request: Request):\n"+
		"    obj = {}\n"+
		"    obj.data = request.GET\n"+
		"    cursor.execute(obj.data)\n")
	facts := TaintFacts(node, source, fn)
	res := taint.Analyze(fn, facts, taint.DefaultLimits())
	if pyTaintedCount(res, "sql") != 1 {
		t.Fatalf("want 1 TAINTED sql finding through obj.data, got %+v", res.Findings)
	}
}

// TestPySubscriptTaintReachesSink proves taint flowing through a container
// element is reported as TAINTED.
func TestPySubscriptTaintReachesSink(t *testing.T) {
	t.Parallel()

	node, source, fn := parseFirstPyFunction(t, "from fastapi import Request\n\n"+
		"def view(request: Request, key):\n"+
		"    m = {}\n"+
		"    m[key] = request.GET\n"+
		"    cursor.execute(m[key])\n")
	facts := TaintFacts(node, source, fn)
	res := taint.Analyze(fn, facts, taint.DefaultLimits())
	if pyTaintedCount(res, "sql") != 1 {
		t.Fatalf("want 1 TAINTED sql finding through d[*], got %+v", res.Findings)
	}
}

// TestPyAliasAttributeTaintReachesSink proves taint written through a reference
// alias and read through the original object is reported as TAINTED.
func TestPyAliasAttributeTaintReachesSink(t *testing.T) {
	t.Parallel()

	node, source, fn := parseFirstPyFunction(t, "from fastapi import Request\n\n"+
		"def view(request: Request):\n"+
		"    obj = {}\n"+
		"    a = obj\n"+
		"    a.data = request.GET\n"+
		"    cursor.execute(obj.data)\n")
	facts := TaintFacts(node, source, fn)
	res := taint.Analyze(fn, facts, taint.DefaultLimits())
	if pyTaintedCount(res, "sql") != 1 {
		t.Fatalf("want 1 TAINTED sql finding through alias obj.data, got %+v", res.Findings)
	}
}
