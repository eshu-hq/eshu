package kotlin

import (
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// Parse extracts Kotlin declarations, imports, variables, calls, and
// receiver-type metadata from one source file by walking the tree-sitter AST.
func Parse(repoRoot, path string, isDependency bool, options shared.Options, parser *tree_sitter.Parser) (map[string]any, error) {
	return walkFile(repoRoot, path, isDependency, options, parser)
}
