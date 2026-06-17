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

	root, source := parseRoot(t, "function handler(req, db) {\n"+
		"\tquery(db, req);\n"+
		"}\n"+
		"function query(db, q) {\n"+
		"\tdb.query(q);\n"+
		"}\n")
	findings := InterprocFindings(root, source, "")

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

// TestTSInterprocNoFalseEdgeFromMethodCall proves a method call (conn.query) whose
// final name matches a local function (query) does not resolve to that local
// function, so no false cross-function finding is produced from the caller.
func TestTSInterprocNoFalseEdgeFromMethodCall(t *testing.T) {
	t.Parallel()

	root, source := parseRoot(t, "function query(req) {\n"+
		"\tdb.query(req);\n"+
		"}\n"+
		"function handler(req) {\n"+
		"\tconn.query(req);\n"+
		"}\n")
	findings := InterprocFindings(root, source, "")
	for _, f := range findings {
		if strings.Contains(string(f.SourceFunc), "handler") && f.SourceFunc != f.SinkFunc {
			t.Fatalf("false cross-function finding from handler via method call conn.query: %+v", f)
		}
	}
}
