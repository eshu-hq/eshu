package jsdataflow

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/cfg"
	"github.com/eshu-hq/eshu/go/internal/parser/taint"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	ts "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

// parseFirstFunction parses TypeScript src and returns the first function
// declaration node, the source, and its lowered CFG.
func parseFirstFunction(t *testing.T, src string) (*tree_sitter.Node, []byte, cfg.Function) {
	t.Helper()
	parser := tree_sitter.NewParser()
	t.Cleanup(parser.Close)
	if err := parser.SetLanguage(tree_sitter.NewLanguage(ts.LanguageTypescript())); err != nil {
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
		if n.Kind() == "function_declaration" {
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
		t.Fatalf("no function declaration in fixture")
	}
	return fnNode, source, LowerFunction(fnNode, source, cfg.DefaultLimits())
}

func taintedCount(res taint.Result, kind taint.Kind) int {
	n := 0
	for _, f := range res.Findings {
		if f.Kind == taint.FindingTainted && f.SinkKind == kind {
			n++
		}
	}
	return n
}

// TestTSSourceToSQLSink proves a request parameter reaching a db.query call is
// reported as a TAINTED sql finding.
func TestTSSourceToSQLSink(t *testing.T) {
	t.Parallel()

	node, source, fn := parseFirstFunction(t, "function handler(req) {\n"+
		"\tconst q = req.body;\n"+
		"\tdb.query(q);\n"+
		"}")
	facts := TaintFacts(node, source, fn)
	res := taint.Analyze(fn, facts, taint.DefaultLimits())
	if taintedCount(res, "sql") != 1 {
		t.Fatalf("want 1 TAINTED sql finding, got %+v", res.Findings)
	}
}

// TestTSWrongKindSanitizer proves an html escaper does not suppress a sql sink
// (the kind-set model end-to-end through the TS catalog).
func TestTSWrongKindSanitizer(t *testing.T) {
	t.Parallel()

	node, source, fn := parseFirstFunction(t, "function handler(req) {\n"+
		"\tconst raw = req.body;\n"+
		"\tconst safe = escape(raw);\n"+
		"\tdb.query(safe);\n"+
		"}")
	facts := TaintFacts(node, source, fn)
	res := taint.Analyze(fn, facts, taint.DefaultLimits())
	if taintedCount(res, "sql") != 1 {
		t.Fatalf("html escaper must not suppress a sql sink; want 1 TAINTED sql, got %+v", res.Findings)
	}
}

// TestTSNonRequestParamIsNotSource proves a parameter not in the request-name
// convention is not marked as a taint source, so no false finding is produced.
func TestTSNonRequestParamIsNotSource(t *testing.T) {
	t.Parallel()

	node, source, fn := parseFirstFunction(t, "function handler(value) {\n"+
		"\tdb.query(value);\n"+
		"}")
	facts := TaintFacts(node, source, fn)
	res := taint.Analyze(fn, facts, taint.DefaultLimits())
	if len(res.Findings) != 0 {
		t.Fatalf("non-request param must not be a source; got %+v", res.Findings)
	}
}

// TestTSSinkInNestedClosureNotAttributed proves a sink inside a nested closure is
// not attributed to the enclosing function (the request source there does not
// reach it via the outer function's facts).
func TestTSSinkInNestedClosureNotAttributed(t *testing.T) {
	t.Parallel()

	node, source, fn := parseFirstFunction(t, "function handler(req) {\n"+
		"\tconst q = req.body;\n"+
		"\tconst f = () => { db.query(other); };\n"+
		"\treturn q;\n"+
		"}")
	facts := TaintFacts(node, source, fn)
	if len(facts.Sinks) != 0 {
		t.Fatalf("sink inside nested closure must not be attributed to the outer function; got %+v", facts.Sinks)
	}
}

// TestTSConditionalSanitizerNotMarked proves a sanitizer call inside a
// conditional (cond ? raw : escape(raw)) does not mark the whole binding as
// sanitized, because the other branch is unsanitized — marking it would wrongly
// suppress a real finding.
func TestTSConditionalSanitizerNotMarked(t *testing.T) {
	t.Parallel()

	node, source, fn := parseFirstFunction(t, "function handler(req) {\n"+
		"\tconst safe = cond ? req.body : escape(req.body);\n"+
		"\tdb.query(safe);\n"+
		"}")
	facts := TaintFacts(node, source, fn)
	if len(facts.Sanitizers) != 0 {
		t.Fatalf("conditional sanitizer must not mark the binding; got %+v", facts.Sanitizers)
	}
}
