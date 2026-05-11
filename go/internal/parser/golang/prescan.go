package golang

import (
	"fmt"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// PreScan returns deterministic Go symbols used by collector import-map
// prescans. The pre-scan path runs once per file before the real parse, so it
// must stay cheap: collect function, struct, and interface names directly
// from the tree-sitter tree without running the semantic dead-code roots or
// other Parse-only payload work, which earlier doubled per-file parse cost on
// repo-scale dogfood inputs (see #161).
func PreScan(parser *tree_sitter.Parser, path string) ([]string, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, fmt.Errorf("parse go file %q: parser returned nil tree", path)
	}
	defer tree.Close()

	names := make([]string, 0, 32)
	shared.WalkNamed(tree.RootNode(), func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "function_declaration", "method_declaration":
			name := strings.TrimSpace(nodeText(node.ChildByFieldName("name"), source))
			if name != "" {
				names = append(names, name)
			}
		case "type_spec":
			typeNode := node.ChildByFieldName("type")
			if typeNode == nil {
				return
			}
			switch typeNode.Kind() {
			case "struct_type", "interface_type":
				name := strings.TrimSpace(nodeText(node.ChildByFieldName("name"), source))
				if name != "" {
					names = append(names, name)
				}
			}
		}
	})
	slices.Sort(names)
	return names, nil
}
