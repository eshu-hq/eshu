package pydataflow

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/cfg"
	"github.com/eshu-hq/eshu/go/internal/parser/taint"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	py "github.com/tree-sitter/tree-sitter-python/bindings/go"
)

// parseFirstPyFunction parses Python src and returns the first function
// definition node, the source, and its lowered CFG. The parser and tree are kept
// alive for the test's lifetime because the returned node points into the tree.
func parseFirstPyFunction(t *testing.T, src string) (*tree_sitter.Node, []byte, cfg.Function) {
	t.Helper()
	parser := tree_sitter.NewParser()
	t.Cleanup(parser.Close)
	if err := parser.SetLanguage(tree_sitter.NewLanguage(py.Language())); err != nil {
		t.Fatalf("set language: %v", err)
	}
	source := []byte(src)
	tree := parser.Parse(source, nil)
	t.Cleanup(tree.Close)

	var fnNode *tree_sitter.Node
	var walk func(*tree_sitter.Node)
	walk = func(n *tree_sitter.Node) {
		if fnNode != nil || n == nil {
			return
		}
		if n.Kind() == "function_definition" {
			captured := *n
			fnNode = &captured
			return
		}
		cursor := n.Walk()
		defer cursor.Close()
		for _, ch := range n.NamedChildren(cursor) {
			ch := ch
			walk(&ch)
		}
	}
	walk(tree.RootNode())
	if fnNode == nil {
		t.Fatalf("no function definition in fixture")
	}
	return fnNode, source, LowerFunction(fnNode, source, cfg.DefaultLimits())
}

func pyTaintedCount(res taint.Result, kind taint.Kind) int {
	n := 0
	for _, f := range res.Findings {
		if f.Kind == taint.FindingTainted && f.SinkKind == kind {
			n++
		}
	}
	return n
}

// TestPySourceToSQLSink proves a request parameter reaching cursor.execute is
// reported as a TAINTED sql finding.
func TestPyTypedSourceToSQLSink(t *testing.T) {
	t.Parallel()

	node, source, fn := parseFirstPyFunction(t, "from fastapi import Request\n\n"+
		"def view(request: Request):\n"+
		"    q = request.GET\n"+
		"    cursor.execute(q)\n")
	facts := TaintFacts(node, source, fn)
	res := taint.Analyze(fn, facts, taint.DefaultLimits())
	if pyTaintedCount(res, "sql") != 1 {
		t.Fatalf("want 1 TAINTED sql finding, got %+v", res.Findings)
	}
}

// TestPyAliasedFrameworkRequestImportIsSource proves framework request evidence
// follows the imported alias, not only the exported type name.
func TestPyAliasedFrameworkRequestImportIsSource(t *testing.T) {
	t.Parallel()

	node, source, fn := parseFirstPyFunction(t, "from fastapi import Request as FastAPIRequest\n\n"+
		"def view(request: FastAPIRequest):\n"+
		"    q = request.GET\n"+
		"    cursor.execute(q)\n")
	facts := TaintFacts(node, source, fn)
	res := taint.Analyze(fn, facts, taint.DefaultLimits())
	if pyTaintedCount(res, "sql") != 1 {
		t.Fatalf("want 1 TAINTED sql finding for aliased FastAPI Request, got %+v", res.Findings)
	}
}

// TestPyTypeCheckingFrameworkRequestImportIsSource proves typing-only framework
// imports still provide request source evidence for runtime annotations.
func TestPyTypeCheckingFrameworkRequestImportIsSource(t *testing.T) {
	t.Parallel()

	node, source, fn := parseFirstPyFunction(t, "from typing import TYPE_CHECKING\n\n"+
		"if TYPE_CHECKING:\n"+
		"    from fastapi import Request\n\n"+
		"def view(request: Request):\n"+
		"    q = request.GET\n"+
		"    cursor.execute(q)\n")
	facts := TaintFacts(node, source, fn)
	res := taint.Analyze(fn, facts, taint.DefaultLimits())
	if pyTaintedCount(res, "sql") != 1 {
		t.Fatalf("want 1 TAINTED sql finding for TYPE_CHECKING FastAPI Request, got %+v", res.Findings)
	}
}

// TestPyLocalRequestImportIsNotSource proves an unrelated type named Request is
// not framework request evidence.
func TestPyLocalRequestImportIsNotSource(t *testing.T) {
	t.Parallel()

	node, source, fn := parseFirstPyFunction(t, "from local_types import Request\n\n"+
		"def view(request: Request):\n"+
		"    cursor.execute(request.GET)\n")
	facts := TaintFacts(node, source, fn)
	res := taint.Analyze(fn, facts, taint.DefaultLimits())
	if len(res.Findings) != 0 {
		t.Fatalf("local Request import must not be a source; got %+v", res.Findings)
	}
}

// TestPySourceToCommandSink proves a request parameter reaching os.system is
// reported as a TAINTED command finding.
func TestPyTypedSourceToCommandSink(t *testing.T) {
	t.Parallel()

	node, source, fn := parseFirstPyFunction(t, "from fastapi import Request\n\n"+
		"def view(request: Request):\n"+
		"    cmd = request.GET\n"+
		"    os.system(cmd)\n")
	facts := TaintFacts(node, source, fn)
	res := taint.Analyze(fn, facts, taint.DefaultLimits())
	if pyTaintedCount(res, "command") != 1 {
		t.Fatalf("want 1 TAINTED command finding, got %+v", res.Findings)
	}
}

// TestPyUntypedRequestNameIsNotSource proves a parameter named like a request is
// not enough without framework/type evidence.
func TestPyUntypedRequestNameIsNotSource(t *testing.T) {
	t.Parallel()

	node, source, fn := parseFirstPyFunction(t, "def view(request):\n"+
		"    cursor.execute(request.GET)\n")
	facts := TaintFacts(node, source, fn)
	res := taint.Analyze(fn, facts, taint.DefaultLimits())
	if len(res.Findings) != 0 {
		t.Fatalf("untyped request-name parameter must not be a source; got %+v", res.Findings)
	}
}

// TestPyRequestPrefixTypeIsNotSource proves options/factory annotations that
// merely start with Request are not framework request evidence.
func TestPyRequestPrefixTypeIsNotSource(t *testing.T) {
	t.Parallel()

	node, source, fn := parseFirstPyFunction(t, "def view(opts: RequestFactory):\n"+
		"    cursor.execute(opts.query)\n")
	facts := TaintFacts(node, source, fn)
	res := taint.Analyze(fn, facts, taint.DefaultLimits())
	if len(res.Findings) != 0 {
		t.Fatalf("RequestFactory must not be a request source; got %+v", res.Findings)
	}
}

// TestPySameNamedCacheExecuteIsNotSink proves a same-named unrelated execute
// method is not treated as a SQL sink.
func TestPySameNamedCacheExecuteIsNotSink(t *testing.T) {
	t.Parallel()

	node, source, fn := parseFirstPyFunction(t, "from fastapi import Request\n\n"+
		"def view(request: Request):\n"+
		"    q = request.GET\n"+
		"    cache.execute(q)\n")
	facts := TaintFacts(node, source, fn)
	res := taint.Analyze(fn, facts, taint.DefaultLimits())
	if len(res.Findings) != 0 {
		t.Fatalf("cache.execute must not be a SQL sink; got %+v", res.Findings)
	}
}

func TestPyTaintCatalogVersionIsStable(t *testing.T) {
	t.Parallel()

	v1 := TaintCatalogVersion()
	v2 := TaintCatalogVersion()
	if v1 == "" || v1 != v2 {
		t.Fatalf("TaintCatalogVersion not stable: %q vs %q", v1, v2)
	}
	if len(v1) != 64 {
		t.Fatalf("TaintCatalogVersion length = %d, want sha256 hex length 64", len(v1))
	}
}

// TestPyWrongKindSanitizer proves an html escaper does not suppress a sql sink
// (the kind-set model end-to-end through the Python catalog).
func TestPyWrongKindSanitizer(t *testing.T) {
	t.Parallel()

	node, source, fn := parseFirstPyFunction(t, "from fastapi import Request\n\n"+
		"def view(request: Request):\n"+
		"    raw = request.GET\n"+
		"    safe = escape(raw)\n"+
		"    cursor.execute(safe)\n")
	facts := TaintFacts(node, source, fn)
	res := taint.Analyze(fn, facts, taint.DefaultLimits())
	if pyTaintedCount(res, "sql") != 1 {
		t.Fatalf("html escaper must not suppress a sql sink; want 1 TAINTED sql, got %+v", res.Findings)
	}
}

// TestPyNonRequestParamIsNotSource proves a parameter not in the request-name
// convention is not marked as a taint source, so no false finding is produced.
func TestPyNonRequestParamIsNotSource(t *testing.T) {
	t.Parallel()

	node, source, fn := parseFirstPyFunction(t, "def view(value):\n"+
		"    cursor.execute(value)\n")
	facts := TaintFacts(node, source, fn)
	res := taint.Analyze(fn, facts, taint.DefaultLimits())
	if len(res.Findings) != 0 {
		t.Fatalf("non-request param must not be a source; got %+v", res.Findings)
	}
}

// TestPySinkInNestedFunctionNotAttributed proves a sink inside a nested function
// is not attributed to the enclosing function.
func TestPySinkInNestedFunctionNotAttributed(t *testing.T) {
	t.Parallel()

	node, source, fn := parseFirstPyFunction(t, "def view(request: Request):\n"+
		"    q = request.GET\n"+
		"    def inner():\n"+
		"        cursor.execute(other)\n"+
		"    return q\n")
	facts := TaintFacts(node, source, fn)
	if len(facts.Sinks) != 0 {
		t.Fatalf("sink inside nested function must not be attributed to the outer function; got %+v", facts.Sinks)
	}
}

// TestPyWithBlockSinkResolved proves a sink call inside a `with` block (the
// common `with conn.cursor() as cursor:` pattern) is located and reported. The
// body must be lowered precisely so the call has its own CFG statement line.
func TestPyWithBlockSinkResolved(t *testing.T) {
	t.Parallel()

	node, source, fn := parseFirstPyFunction(t, "from fastapi import Request\n\n"+
		"def view(request: Request):\n"+
		"    q = request.GET\n"+
		"    with conn.cursor() as cursor:\n"+
		"        cursor.execute(q)\n")
	facts := TaintFacts(node, source, fn)
	res := taint.Analyze(fn, facts, taint.DefaultLimits())
	if pyTaintedCount(res, "sql") != 1 {
		t.Fatalf("sink inside a with block must be reported; want 1 TAINTED sql, got %+v", res.Findings)
	}
}

// TestPyTryBlockSinkResolved proves a sink call inside a `try` body is located
// and reported.
func TestPyTryBlockSinkResolved(t *testing.T) {
	t.Parallel()

	node, source, fn := parseFirstPyFunction(t, "from fastapi import Request\n\n"+
		"def view(request: Request):\n"+
		"    q = request.GET\n"+
		"    try:\n"+
		"        cursor.execute(q)\n"+
		"    except Exception as e:\n"+
		"        log(e)\n")
	facts := TaintFacts(node, source, fn)
	res := taint.Analyze(fn, facts, taint.DefaultLimits())
	if pyTaintedCount(res, "sql") != 1 {
		t.Fatalf("sink inside a try body must be reported; want 1 TAINTED sql, got %+v", res.Findings)
	}
}

// TestPyConditionalSanitizerNotMarked proves a sanitizer call inside a
// conditional expression (escape(raw) if cond else raw) does not mark the whole
// binding as sanitized, because the other branch is unsanitized — marking it
// would wrongly suppress a real finding.
func TestPyConditionalSanitizerNotMarked(t *testing.T) {
	t.Parallel()

	node, source, fn := parseFirstPyFunction(t, "def view(request):\n"+
		"    safe = escape(request.GET) if cond else request.GET\n"+
		"    cursor.execute(safe)\n")
	facts := TaintFacts(node, source, fn)
	if len(facts.Sanitizers) != 0 {
		t.Fatalf("conditional sanitizer must not mark the binding; got %+v", facts.Sanitizers)
	}
}
