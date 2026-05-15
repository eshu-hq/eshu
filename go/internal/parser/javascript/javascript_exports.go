package javascript

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func javaScriptReExportEntries(
	node *tree_sitter.Node,
	source []byte,
	lang string,
) []map[string]any {
	if node == nil || node.Kind() != "export_statement" {
		return nil
	}

	sourceNode := node.ChildByFieldName("source")
	moduleSource := javaScriptReExportSource(node, source)
	if !strings.HasPrefix(strings.TrimSpace(moduleSource), ".") {
		return nil
	}

	fullImportName := strings.TrimSpace(nodeText(node, source))
	if javaScriptIsStarReExport(node, source) {
		return []map[string]any{javaScriptReExportEntry(
			"*",
			"*",
			moduleSource,
			fullImportName,
			nodeLine(sourceNode),
			lang,
		)}
	}

	specifiers := javaScriptReExportSpecifiers(node, source)
	items := make([]map[string]any, 0, len(specifiers))
	for _, specifier := range specifiers {
		items = append(items, javaScriptReExportEntry(
			specifier.exportedName,
			specifier.originalName,
			moduleSource,
			fullImportName,
			specifier.lineNumber,
			lang,
		))
	}
	return items
}

func javaScriptReExportSource(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	if sourceNode := node.ChildByFieldName("source"); sourceNode != nil {
		if moduleSource := strings.Trim(strings.TrimSpace(nodeText(sourceNode, source)), `"'`); moduleSource != "" {
			return moduleSource
		}
	}

	text := strings.TrimSpace(nodeText(node, source))
	before, rawSource, ok := strings.Cut(text, " from ")
	if !ok || !strings.HasPrefix(strings.TrimSpace(before), "export") {
		return ""
	}
	rawSource = strings.TrimSpace(strings.TrimSuffix(rawSource, ";"))
	return strings.Trim(rawSource, `"'`)
}

// javaScriptReExportSpecifier records one static export-clause mapping from a
// barrel's public name to the original symbol name in the target module.
type javaScriptReExportSpecifier struct {
	exportedName string
	originalName string
	lineNumber   int
}

func javaScriptReExportEntry(
	exportedName string,
	originalName string,
	moduleSource string,
	fullImportName string,
	lineNumber int,
	lang string,
) map[string]any {
	item := map[string]any{
		"name":             exportedName,
		"source":           moduleSource,
		"import_type":      "reexport",
		"full_import_name": fullImportName,
		"line_number":      lineNumber,
		"lang":             lang,
	}
	if originalName != "" {
		item["original_name"] = originalName
	}
	return item
}

func javaScriptIsStarReExport(node *tree_sitter.Node, source []byte) bool {
	if node == nil {
		return false
	}
	text := strings.TrimSpace(nodeText(node, source))
	if !strings.HasPrefix(text, "export") {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(text, "export")), "*")
}

func javaScriptReExportSpecifiers(node *tree_sitter.Node, source []byte) []javaScriptReExportSpecifier {
	specifiers := make([]javaScriptReExportSpecifier, 0)
	walkNamed(node, func(candidate *tree_sitter.Node) {
		if candidate.Kind() != "export_specifier" {
			return
		}
		nameNode := candidate.ChildByFieldName("name")
		aliasNode := candidate.ChildByFieldName("alias")
		originalName := strings.TrimSpace(nodeText(nameNode, source))
		exportedName := strings.TrimSpace(nodeText(aliasNode, source))
		if exportedName == "" {
			exportedName = originalName
		}
		if exportedName == "" || originalName == "" {
			return
		}
		specifiers = append(specifiers, javaScriptReExportSpecifier{
			exportedName: exportedName,
			originalName: originalName,
			lineNumber:   nodeLine(candidate),
		})
	})
	if len(specifiers) > 0 {
		return specifiers
	}
	return javaScriptReExportSpecifiersFromText(node, source)
}

func javaScriptReExportSpecifiersFromText(
	node *tree_sitter.Node,
	source []byte,
) []javaScriptReExportSpecifier {
	text := strings.TrimSpace(nodeText(node, source))
	start := strings.Index(text, "{")
	end := strings.Index(text, "}")
	if start < 0 || end <= start {
		return nil
	}

	parts := strings.Split(text[start+1:end], ",")
	specifiers := make([]javaScriptReExportSpecifier, 0, len(parts))
	for _, part := range parts {
		originalName, exportedName := javaScriptReExportSpecifierNames(part)
		if originalName == "" || exportedName == "" {
			continue
		}
		specifiers = append(specifiers, javaScriptReExportSpecifier{
			exportedName: exportedName,
			originalName: originalName,
			lineNumber:   nodeLine(node),
		})
	}
	return specifiers
}

func javaScriptReExportSpecifierNames(raw string) (string, string) {
	part := strings.TrimSpace(strings.TrimPrefix(javaScriptExportSpecifierWithoutLineComments(raw), "type "))
	if part == "" || strings.Contains(part, "...") {
		return "", ""
	}

	fields := strings.Fields(part)
	switch len(fields) {
	case 1:
		return fields[0], fields[0]
	case 3:
		if fields[1] == "as" {
			return fields[0], fields[2]
		}
	}

	left, right, ok := strings.Cut(part, " as ")
	if !ok {
		return "", ""
	}
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" || right == "" {
		return "", ""
	}
	return left, right
}

func javaScriptExportSpecifierWithoutLineComments(raw string) string {
	segments := make([]string, 0, 1)
	for _, line := range strings.Split(raw, "\n") {
		beforeComment, _, _ := strings.Cut(line, "//")
		if trimmed := strings.TrimSpace(beforeComment); trimmed != "" {
			segments = append(segments, trimmed)
		}
	}
	return strings.TrimSpace(strings.Join(segments, " "))
}
