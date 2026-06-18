package jsdataflow

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

type jsFrameworkRequestEvidence struct {
	typeKinds      map[string]string
	namespaceMods  map[string]string
	frameworkTypes map[string]string
}

func newJSFrameworkRequestEvidence() jsFrameworkRequestEvidence {
	frameworkTypes := map[string]string{}
	for _, spec := range jsSourceTypeSpecs {
		if len(spec.Modules) == 0 {
			continue
		}
		for _, module := range spec.Modules {
			frameworkTypes[jsImportKey(module, spec.TypeName)] = spec.Kind
		}
	}
	return jsFrameworkRequestEvidence{
		typeKinds:      map[string]string{},
		namespaceMods:  map[string]string{},
		frameworkTypes: frameworkTypes,
	}
}

func (e jsFrameworkRequestEvidence) TypeKind(typeName string) string {
	return e.typeKinds[strings.TrimSpace(typeName)]
}

func (e jsFrameworkRequestEvidence) NamespaceTypeKind(typeName string) (string, bool) {
	namespace, member, ok := strings.Cut(strings.TrimSpace(typeName), ".")
	if !ok || namespace == "" || member == "" {
		return "", false
	}
	module := e.namespaceMods[namespace]
	if module == "" {
		return "", false
	}
	kind := e.frameworkTypes[jsImportKey(module, member)]
	return kind, kind != ""
}

func jsFrameworkRequestImports(funcNode *tree_sitter.Node, source []byte) jsFrameworkRequestEvidence {
	evidence := newJSFrameworkRequestEvidence()
	root := treeRoot(funcNode)
	if root == nil {
		return evidence
	}
	var walk func(*tree_sitter.Node)
	walk = func(node *tree_sitter.Node) {
		if node == nil {
			return
		}
		if node.Kind() == "import_statement" {
			evidence.addImportStatement(node, source)
			return
		}
		if isNestedFunction(node.Kind()) {
			return
		}
		cursor := node.Walk()
		defer cursor.Close()
		for _, child := range node.NamedChildren(cursor) {
			child := child
			walk(&child)
		}
	}
	walk(root)
	return evidence
}

func (e jsFrameworkRequestEvidence) addImportStatement(node *tree_sitter.Node, source []byte) {
	moduleNode := node.ChildByFieldName("source")
	module := strings.Trim(nodeText(moduleNode, source), `"'`)
	if !jsHasFrameworkModule(module) {
		return
	}
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		e.addImportClause(&child, source, module)
	}
}

func (e jsFrameworkRequestEvidence) addImportClause(node *tree_sitter.Node, source []byte, module string) {
	if node == nil {
		return
	}
	switch node.Kind() {
	case "import_specifier":
		imported := strings.TrimSpace(nodeText(node.ChildByFieldName("name"), source))
		alias := strings.TrimSpace(nodeText(node.ChildByFieldName("alias"), source))
		if alias == "" {
			alias = imported
		}
		if kind := e.frameworkTypes[jsImportKey(module, imported)]; kind != "" {
			e.typeKinds[alias] = kind
		}
	case "namespace_import":
		alias := jsNamespaceAlias(node, source)
		if alias != "" {
			e.namespaceMods[alias] = module
		}
	case "identifier":
		alias := strings.TrimSpace(nodeText(node, source))
		if alias != "" {
			e.namespaceMods[alias] = module
		}
	default:
		cursor := node.Walk()
		defer cursor.Close()
		for _, child := range node.NamedChildren(cursor) {
			child := child
			e.addImportClause(&child, source, module)
		}
	}
}

func jsNamespaceAlias(node *tree_sitter.Node, source []byte) string {
	if aliasNode := node.ChildByFieldName("name"); aliasNode != nil {
		if alias := strings.TrimSpace(nodeText(aliasNode, source)); alias != "" {
			return alias
		}
	}
	text := strings.TrimSpace(nodeText(node, source))
	parts := strings.Fields(text)
	if len(parts) >= 3 && parts[0] == "*" && parts[1] == "as" {
		return parts[2]
	}
	return ""
}

func jsHasFrameworkModule(module string) bool {
	for key := range newJSFrameworkRequestEvidence().frameworkTypes {
		if strings.HasPrefix(key, module+"\x00") {
			return true
		}
	}
	return false
}

func jsImportKey(module string, typeName string) string {
	return strings.TrimSpace(module) + "\x00" + strings.TrimSpace(typeName)
}

func treeRoot(node *tree_sitter.Node) *tree_sitter.Node {
	for current := node; current != nil; current = current.Parent() {
		if current.Parent() == nil {
			return current
		}
	}
	return nil
}
