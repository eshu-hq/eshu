package haskell

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// haskellNamedChildren returns the named children of a node as a stable slice.
// A nil node yields nil so callers can range without a guard.
func haskellNamedChildren(node *tree_sitter.Node) []tree_sitter.Node {
	if node == nil {
		return nil
	}
	cursor := node.Walk()
	defer cursor.Close()
	return node.NamedChildren(cursor)
}

// haskellFirstChildOfKind returns the first named child of node matching kind,
// or nil when none is present.
func haskellFirstChildOfKind(node *tree_sitter.Node, kind string) *tree_sitter.Node {
	for _, child := range haskellNamedChildren(node) {
		child := child
		if child.Kind() == kind {
			return &child
		}
	}
	return nil
}

// haskellModuleName joins the module_id segments of a `module` node into a dotted
// module path such as Data.Text. The grammar models each path segment as its own
// module_id child, so the dotted name is rebuilt from the segments rather than
// read from a single token.
func haskellModuleName(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	segments := make([]string, 0)
	for _, child := range haskellNamedChildren(node) {
		child := child
		if child.Kind() == "module_id" {
			segments = append(segments, strings.TrimSpace(shared.NodeText(&child, source)))
		}
	}
	if len(segments) == 0 {
		return strings.TrimSpace(shared.NodeText(node, source))
	}
	return strings.Join(segments, ".")
}

// haskellExportNames returns the identifier names listed in a module header's
// export list, covering both variable exports (`main`) and type exports
// (`Worker(..)`). It mirrors the bounded export parsing the prior line scanner
// performed over the header text.
func haskellExportNames(header *tree_sitter.Node, source []byte) map[string]struct{} {
	exports := make(map[string]struct{})
	list := haskellFirstChildOfKind(header, "exports")
	if list == nil {
		return exports
	}
	for _, export := range haskellNamedChildren(list) {
		export := export
		if export.Kind() != "export" {
			continue
		}
		for _, child := range haskellNamedChildren(&export) {
			child := child
			switch child.Kind() {
			case "variable", "name", "constructor", "operator":
				name := strings.TrimSpace(shared.NodeText(&child, source))
				if name != "" {
					exports[name] = struct{}{}
				}
			}
		}
	}
	return exports
}

// haskellImportFields returns the dotted module name and alias for an `import`
// node. The alias is the trailing `as T` module, empty when absent.
func haskellImportFields(node *tree_sitter.Node, source []byte) (string, string) {
	moduleNode := node.ChildByFieldName("module")
	if moduleNode == nil {
		return "", ""
	}
	name := haskellModuleName(moduleNode, source)
	alias := ""
	if aliasNode := node.ChildByFieldName("alias"); aliasNode != nil {
		alias = haskellModuleName(aliasNode, source)
	}
	return name, alias
}

// haskellBindingName returns the bound variable name for a top-level `bind`,
// `function`, or `signature` node. The grammar exposes the name as a `variable`
// child under the `name` field.
func haskellBindingName(node *tree_sitter.Node, source []byte) string {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return ""
	}
	return strings.TrimSpace(shared.NodeText(nameNode, source))
}

// haskellTypeDeclaration returns the declared type name and its semantic kind
// (data, newtype, type) for a data_type, newtype, or type_synomym node.
func haskellTypeDeclaration(node *tree_sitter.Node, source []byte) (string, string, bool) {
	var kind string
	switch node.Kind() {
	case "data_type":
		kind = "data"
	case "newtype":
		kind = "newtype"
	case "type_synomym":
		kind = "type"
	default:
		return "", "", false
	}
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return "", "", false
	}
	return strings.TrimSpace(shared.NodeText(nameNode, source)), kind, true
}

// haskellClassOrInstanceName returns the head name of a `class` or `instance`
// node. A class uses its `name` field; an instance combines the class `name`
// with its `type_patterns` head so the context reads `Runner Worker`.
func haskellClassOrInstanceName(node *tree_sitter.Node, source []byte) string {
	nameNode := node.ChildByFieldName("name")
	name := ""
	if nameNode != nil {
		name = strings.TrimSpace(shared.NodeText(nameNode, source))
	}
	if node.Kind() != "instance" {
		return name
	}
	patterns := node.ChildByFieldName("patterns")
	if patterns == nil {
		return name
	}
	parts := []string{name}
	for _, child := range haskellNamedChildren(patterns) {
		child := child
		text := strings.TrimSpace(shared.NodeText(&child, source))
		if text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, " ")
}

// haskellTreeFunctionParameters collects the simple variable names bound by a
// function definition's `patterns` node. Constructor-wrapped patterns such as
// `(Just value)` contribute their inner variable so call evidence can suppress
// parameter references.
func haskellTreeFunctionParameters(node *tree_sitter.Node, source []byte) map[string]struct{} {
	params := make(map[string]struct{})
	patterns := node.ChildByFieldName("patterns")
	if patterns == nil {
		return params
	}
	for _, child := range haskellNamedChildren(patterns) {
		child := child
		collectHaskellTreePatternParameters(&child, source, params)
	}
	return params
}

// collectHaskellTreePatternParameters descends a pattern subtree, recording each
// lowercase variable name as a bound parameter.
func collectHaskellTreePatternParameters(node *tree_sitter.Node, source []byte, params map[string]struct{}) {
	if node == nil {
		return
	}
	if node.Kind() == "variable" || node.Kind() == "pat_name" {
		name := strings.TrimSpace(shared.NodeText(node, source))
		if haskellTreeParameterName(name) {
			params[name] = struct{}{}
			return
		}
	}
	for _, child := range haskellNamedChildren(node) {
		child := child
		collectHaskellTreePatternParameters(&child, source, params)
	}
}

// haskellTreeParameterName reports whether name is a simple lowercase parameter
// identifier rather than a keyword or compound pattern fragment.
func haskellTreeParameterName(name string) bool {
	if name == "" || haskellIsKeyword(name) || strings.ContainsAny(name, " \t\r\n()[]{}") {
		return false
	}
	return strings.ContainsAny(name[:1], "abcdefghijklmnopqrstuvwxyz_")
}
