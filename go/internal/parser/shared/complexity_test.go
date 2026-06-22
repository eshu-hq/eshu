package shared

import (
	"testing"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
)

func goComplexitySet() BranchNodeSet {
	return NewBranchNodeSet(
		[]string{"if_statement", "for_statement", "expression_case"},
		[]string{"function_declaration", "method_declaration", "func_literal"},
		[]string{"binary_expression"},
		[]string{"&&", "||"},
	)
}

func parseGoFunction(t *testing.T, source string) (*tree_sitter.Node, []byte) {
	t.Helper()
	language := tree_sitter.NewLanguage(tree_sitter_go.Language())
	parser := tree_sitter.NewParser()
	t.Cleanup(parser.Close)
	if err := parser.SetLanguage(language); err != nil {
		t.Fatalf("SetLanguage() error = %v", err)
	}
	bytes := []byte(source)
	tree := parser.Parse(bytes, nil)
	t.Cleanup(tree.Close)

	var found *tree_sitter.Node
	WalkNamed(tree.RootNode(), func(node *tree_sitter.Node) {
		if found == nil && node.Kind() == "function_declaration" {
			found = CloneNode(node)
		}
	})
	if found == nil {
		t.Fatalf("no function_declaration found in source")
	}
	return found, bytes
}

func TestCyclomaticComplexityNilNode(t *testing.T) {
	t.Parallel()
	if got := CyclomaticComplexity(nil, nil, goComplexitySet()); got != 0 {
		t.Fatalf("CyclomaticComplexity(nil) = %d, want 0", got)
	}
}

func TestCyclomaticComplexityStraightLineIsOne(t *testing.T) {
	t.Parallel()
	node, source := parseGoFunction(t, "package p\nfunc f(x int) int { return x + 1 }\n")
	if got := CyclomaticComplexity(node, source, goComplexitySet()); got != 1 {
		t.Fatalf("CyclomaticComplexity(straight line) = %d, want 1", got)
	}
}

func TestCyclomaticComplexityEmptySetIsOne(t *testing.T) {
	t.Parallel()
	node, source := parseGoFunction(t, "package p\nfunc f(x int) int { if x > 0 { return 1 }; return 0 }\n")
	if got := CyclomaticComplexity(node, source, BranchNodeSet{}); got != 1 {
		t.Fatalf("CyclomaticComplexity(empty set) = %d, want 1", got)
	}
}

func TestCyclomaticComplexityCountsBranchesAndBooleans(t *testing.T) {
	t.Parallel()
	// base 1 + if 1 + && 1 + for 1 = 4.
	node, source := parseGoFunction(t,
		"package p\nfunc f(x int) int {\n\tif x > 0 && x < 10 {\n\t\treturn 1\n\t}\n\tfor range []int{} {\n\t}\n\treturn 0\n}\n")
	if got := CyclomaticComplexity(node, source, goComplexitySet()); got != 4 {
		t.Fatalf("CyclomaticComplexity(branchy) = %d, want 4", got)
	}
}

func TestCyclomaticComplexityStopsAtNestedDefinition(t *testing.T) {
	t.Parallel()
	// Outer function is straight line; a nested closure has its own branch that
	// must not inflate the outer count.
	node, source := parseGoFunction(t,
		"package p\nfunc f() func() int {\n\treturn func() int {\n\t\tif true {\n\t\t\treturn 1\n\t\t}\n\t\treturn 0\n\t}\n}\n")
	if got := CyclomaticComplexity(node, source, goComplexitySet()); got != 1 {
		t.Fatalf("CyclomaticComplexity(nested closure) = %d, want 1", got)
	}
}
