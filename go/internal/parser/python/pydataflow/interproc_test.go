package pydataflow

import (
	"strings"
	"testing"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	py "github.com/tree-sitter/tree-sitter-python/bindings/go"
)

// parsePyRoot parses Python src and returns the root node and source, keeping the
// parser and tree alive for the test's lifetime.
func parsePyRoot(t *testing.T, src string) (*tree_sitter.Node, []byte) {
	t.Helper()
	parser := tree_sitter.NewParser()
	t.Cleanup(parser.Close)
	if err := parser.SetLanguage(tree_sitter.NewLanguage(py.Language())); err != nil {
		t.Fatalf("set language: %v", err)
	}
	source := []byte(src)
	tree := parser.Parse(source, nil)
	t.Cleanup(tree.Close)
	return tree.RootNode(), source
}

// TestPyInterprocFindingAcrossFunctions proves the value-flow engine detects an
// interprocedural taint flow in real Python: a request parameter in view is
// passed to query, whose parameter reaches a cursor.execute SQL sink.
func TestPyInterprocFindingAcrossFunctions(t *testing.T) {
	t.Parallel()

	root, source := parsePyRoot(t, "def view(request: Request, db):\n"+
		"    query(db, request)\n"+
		"def query(db, q):\n"+
		"    cursor.execute(q)\n")
	findings := InterprocFindings(root, source, "repo-alpha", "")

	found := false
	for _, f := range findings {
		if strings.Contains(string(f.SourceFunc), "view") &&
			strings.Contains(string(f.SinkFunc), "query") && f.SinkKind == "sql" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an interprocedural view->query sql finding, got %+v", findings)
	}
}

// TestPyInterprocNoFalseEdgeFromMethodCall proves a method call (conn.query) whose
// final name matches a local function (query) does not resolve to that local
// function, so no false cross-function finding is produced from the caller.
func TestPyInterprocNoFalseEdgeFromMethodCall(t *testing.T) {
	t.Parallel()

	root, source := parsePyRoot(t, "def query(request: Request):\n"+
		"    cursor.execute(request)\n"+
		"def view(request: Request):\n"+
		"    conn.query(request)\n")
	findings := InterprocFindings(root, source, "repo-alpha", "")
	for _, f := range findings {
		if strings.Contains(string(f.SourceFunc), "view") && f.SourceFunc != f.SinkFunc {
			t.Fatalf("false cross-function finding from view via method call conn.query: %+v", f)
		}
	}
}

// TestPyInterprocNoEdgeToNestedFunction proves a function nested inside another is
// lexically private: an unrelated top-level caller that uses the same name is not
// wired to it, so no false cross-function finding is produced.
func TestPyInterprocNoEdgeToNestedFunction(t *testing.T) {
	t.Parallel()

	root, source := parsePyRoot(t, "def outer():\n"+
		"    def query(db, q):\n"+
		"        cursor.execute(q)\n"+
		"def view(request: Request, db):\n"+
		"    query(db, request)\n")
	findings := InterprocFindings(root, source, "repo-alpha", "")
	for _, f := range findings {
		if strings.Contains(string(f.SourceFunc), "view") && f.SourceFunc != f.SinkFunc {
			t.Fatalf("view must not resolve to outer's private nested query: %+v", f)
		}
	}
}

// TestPyInterprocNoEdgeToClassMethod proves a class method is not a module-level
// function: an unrelated top-level caller using the same bare name is not wired
// to the method, so no false cross-function finding is produced. The argument is
// positioned so it would align with the method's sink parameter if the method
// were wrongly resolved.
func TestPyInterprocNoEdgeToClassMethod(t *testing.T) {
	t.Parallel()

	root, source := parsePyRoot(t, "class C:\n"+
		"    def query(self, q):\n"+
		"        cursor.execute(q)\n"+
		"def view(request: Request):\n"+
		"    query(other, request)\n")
	findings := InterprocFindings(root, source, "repo-alpha", "")
	for _, f := range findings {
		if strings.Contains(string(f.SourceFunc), "view") && f.SourceFunc != f.SinkFunc {
			t.Fatalf("view must not resolve to class C's method query: %+v", f)
		}
	}
}

// TestPyInterprocMultiArgSameBinding proves that when one tainted binding is
// passed to more than one argument of a call, every argument position is kept:
// the callee's sink is on the FIRST parameter, so dropping all but the last
// argument slot would miss the flow.
func TestPyInterprocMultiArgSameBinding(t *testing.T) {
	t.Parallel()

	root, source := parsePyRoot(t, "def view(request: Request):\n"+
		"    sink2(request, request)\n"+
		"def sink2(a, b):\n"+
		"    cursor.execute(a)\n")
	findings := InterprocFindings(root, source, "repo-alpha", "")

	found := false
	for _, f := range findings {
		if strings.Contains(string(f.SourceFunc), "view") &&
			strings.Contains(string(f.SinkFunc), "sink2") && f.SinkKind == "sql" {
			found = true
		}
	}
	if !found {
		t.Fatalf("flow into the first argument position must survive; got %+v", findings)
	}
}
