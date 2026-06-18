package jsdataflow

import (
	"strings"
	"testing"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	ts "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

func parseRoot(t *testing.T, src string) (*tree_sitter.Node, []byte) {
	t.Helper()
	parser := tree_sitter.NewParser()
	t.Cleanup(parser.Close)
	if err := parser.SetLanguage(tree_sitter.NewLanguage(ts.LanguageTypescript())); err != nil {
		t.Fatalf("set language: %v", err)
	}
	source := []byte(src)
	tree := parser.Parse(source, nil)
	t.Cleanup(tree.Close)
	root := tree.RootNode()
	return root, source
}

// TestTSInterprocFindingAcrossFunctions proves the value-flow engine detects an
// interprocedural taint flow in real TS: a request parameter in handler is passed
// to query, whose parameter reaches a db.query SQL sink.
func TestTSInterprocFindingAcrossFunctions(t *testing.T) {
	t.Parallel()

	root, source := parseRoot(t, "function handler(req: Request, db) {\n"+
		"\tquery(db, req);\n"+
		"}\n"+
		"function query(db, q) {\n"+
		"\tdb.query(q);\n"+
		"}\n")
	findings := InterprocFindings(root, source, "repo-alpha", "")

	found := false
	for _, f := range findings {
		if strings.Contains(string(f.SourceFunc), "handler") &&
			strings.Contains(string(f.SinkFunc), "query") && f.SinkKind == "sql" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an interprocedural handler->query sql finding, got %+v", findings)
	}
}

// TestTSInterprocNoEdgeToNestedFunction proves a function nested inside another
// is lexically private: an unrelated top-level caller that happens to use the
// same name is not wired to it, so no false cross-function finding is produced.
func TestTSInterprocNoEdgeToNestedFunction(t *testing.T) {
	t.Parallel()

	root, source := parseRoot(t, "function outer() {\n"+
		"\tfunction query(db, q) {\n"+
		"\t\tdb.query(q);\n"+
		"\t}\n"+
		"}\n"+
		"function handler(req: Request, db) {\n"+
		"\tquery(db, req);\n"+
		"}\n")
	findings := InterprocFindings(root, source, "repo-alpha", "")
	for _, f := range findings {
		if strings.Contains(string(f.SourceFunc), "handler") && f.SourceFunc != f.SinkFunc {
			t.Fatalf("handler must not resolve to outer's private nested query: %+v", f)
		}
	}
}

// TestTSInterprocMultiArgSameBinding proves that when one tainted binding is
// passed to more than one argument of a call, every argument position is kept:
// the callee's sink is on the FIRST parameter, so dropping all but the last
// argument slot would miss the flow.
func TestTSInterprocMultiArgSameBinding(t *testing.T) {
	t.Parallel()

	root, source := parseRoot(t, "function handler(req: Request) {\n"+
		"\tsink2(req, req);\n"+
		"}\n"+
		"function sink2(a, b) {\n"+
		"\tdb.query(a);\n"+
		"}\n")
	findings := InterprocFindings(root, source, "repo-alpha", "")

	found := false
	for _, f := range findings {
		if strings.Contains(string(f.SourceFunc), "handler") &&
			strings.Contains(string(f.SinkFunc), "sink2") && f.SinkKind == "sql" {
			found = true
		}
	}
	if !found {
		t.Fatalf("flow into the first argument position must survive; got %+v", findings)
	}
}

// TestTSInterprocNoFalseEdgeFromMethodCall proves a method call (conn.query) whose
// final name matches a local function (query) does not resolve to that local
// function, so no false cross-function finding is produced from the caller.
func TestTSInterprocNoFalseEdgeFromMethodCall(t *testing.T) {
	t.Parallel()

	root, source := parseRoot(t, "function query(req: Request) {\n"+
		"\tdb.query(req);\n"+
		"}\n"+
		"function handler(req: Request) {\n"+
		"\tconn.query(req);\n"+
		"}\n")
	findings := InterprocFindings(root, source, "repo-alpha", "")
	for _, f := range findings {
		if strings.Contains(string(f.SourceFunc), "handler") && f.SourceFunc != f.SinkFunc {
			t.Fatalf("false cross-function finding from handler via method call conn.query: %+v", f)
		}
	}
}
