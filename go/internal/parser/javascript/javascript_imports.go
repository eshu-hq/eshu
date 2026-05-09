package javascript

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func javaScriptImportEntries(node *tree_sitter.Node, source []byte, lang string) []map[string]any {
	sourceNode := node.ChildByFieldName("source")
	moduleSource := strings.Trim(nodeText(sourceNode, source), `"'`)
	if strings.TrimSpace(moduleSource) == "" {
		return nil
	}

	importNode := node.ChildByFieldName("import")
	if importNode == nil {
		cursor := node.Walk()
		defer cursor.Close()
		for _, child := range node.NamedChildren(cursor) {
			child := child
			if child.Kind() == "string" {
				continue
			}
			importNode = &child
			break
		}
	}
	if importNode == nil {
		return []map[string]any{{
			"name":        moduleSource,
			"source":      moduleSource,
			"line_number": nodeLine(sourceNode),
			"lang":        lang,
		}}
	}

	items := make([]map[string]any, 0)
	cursor := importNode.Walk()
	defer cursor.Close()
	children := importNode.NamedChildren(cursor)
	if len(children) == 0 {
		children = []tree_sitter.Node{*importNode}
	}
	for _, child := range children {
		child := child
		switch child.Kind() {
		case "import_clause":
			clauseCursor := child.Walk()
			defer clauseCursor.Close()
			for _, clauseChild := range child.NamedChildren(clauseCursor) {
				clauseChild := clauseChild
				items = append(items, javaScriptImportEntriesFromClause(&clauseChild, moduleSource, source, lang)...)
			}
		case "identifier":
			items = append(items, javaScriptImportEntriesFromClause(&child, moduleSource, source, lang)...)
		case "namespace_import", "named_imports":
			items = append(items, javaScriptImportEntriesFromClause(&child, moduleSource, source, lang)...)
		}
	}
	if len(items) == 0 {
		items = append(items, map[string]any{
			"name":        moduleSource,
			"source":      moduleSource,
			"line_number": nodeLine(sourceNode),
			"lang":        lang,
		})
	}
	return items
}

func javaScriptImportEntriesFromClause(
	node *tree_sitter.Node,
	moduleSource string,
	source []byte,
	lang string,
) []map[string]any {
	if node == nil {
		return nil
	}

	switch node.Kind() {
	case "identifier":
		return []map[string]any{{
			"name":        "default",
			"source":      moduleSource,
			"alias":       nodeText(node, source),
			"line_number": nodeLine(node),
			"lang":        lang,
		}}
	case "namespace_import":
		alias := javaScriptNamespaceImportAlias(node, source)
		return []map[string]any{{
			"name":        "*",
			"source":      moduleSource,
			"alias":       alias,
			"line_number": nodeLine(node),
			"lang":        lang,
		}}
	case "named_imports":
		items := make([]map[string]any, 0)
		cursor := node.Walk()
		defer cursor.Close()
		for _, specifier := range node.NamedChildren(cursor) {
			specifier := specifier
			if specifier.Kind() != "import_specifier" {
				continue
			}
			nameNode := specifier.ChildByFieldName("name")
			aliasNode := specifier.ChildByFieldName("alias")
			items = append(items, map[string]any{
				"name":        nodeText(nameNode, source),
				"source":      moduleSource,
				"alias":       nodeText(aliasNode, source),
				"line_number": nodeLine(&specifier),
				"lang":        lang,
			})
		}
		return items
	default:
		return nil
	}
}

func javaScriptNamespaceImportAlias(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
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
