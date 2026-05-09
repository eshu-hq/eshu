package golang

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// ImportedInterfaceParamMethods returns same-file Go function signatures that
// accept known imported interfaces. The parent parser groups these rows by
// package directory before feeding them into per-file parse options.
func ImportedInterfaceParamMethods(
	parser *tree_sitter.Parser,
	path string,
) (shared.GoImportedInterfaceParamMethods, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, fmt.Errorf("parse go file %q: parser returned nil tree", path)
	}
	defer tree.Close()

	return goFunctionParamImportedInterfaceMethods(tree.RootNode(), source), nil
}
