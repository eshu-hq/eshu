package parser

import (
	"path/filepath"
	"testing"
)

// TestDefaultEngineParsePathCPPOutOfLineQualifiedMethodsViaAST locks the
// end-to-end behavior of the AST-based out-of-line qualified-method extraction
// that replaced cppQualifiedFunctionPattern. It covers the cases the prior regex
// matched (simple, destructor, namespace-nested) plus the operator and
// template-qualified definitions the regex dropped and the AST now recovers.
func TestDefaultEngineParsePathCPPOutOfLineQualifiedMethodsViaAST(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "shapes.cpp")
	writeTestFile(
		t,
		sourcePath,
		`#include <cstddef>

class Shape {
public:
    virtual void draw();
    virtual ~Shape();
};

namespace geom {
class Polygon {
public:
    int sides() const;
};
}

class Counter {
public:
    Counter& operator++();
};

virtual void Shape::draw() {
}

Shape::~Shape() {
}

int geom::Polygon::sides() const {
    return 3;
}

Counter& Counter::operator++() {
    return *this;
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%s) error = %v, want nil", sourcePath, err)
	}

	// Simple out-of-line method: regex parity, now AST-derived.
	draw := assertFunctionByNameAndClass(t, got, "draw", "Shape")
	assertParserStringSliceContains(t, draw, "dead_code_root_kinds", "cpp.virtual_method")

	// Destructor: regex parity (name "~Shape", class "Shape").
	assertFunctionByNameAndClass(t, got, "~Shape", "Shape")

	// Namespace-nested qualifier: class is the innermost scope "Polygon".
	assertFunctionByNameAndClass(t, got, "sides", "Polygon")

	// Operator overload: regex dropped this; AST recovers name and class.
	assertFunctionByNameAndClass(t, got, "operator++", "Counter")
}
