package javascript

import (
	"regexp"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

var javaScriptMethodKindRe = regexp.MustCompile(`^\s*(?:static\s+)?(get|set|async)\b`)

func javaScriptDocstring(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}

	lines := strings.Split(string(source), "\n")
	startRow := int(node.StartPosition().Row)
	if startRow <= 0 || startRow > len(lines) {
		return ""
	}

	commentLines := make([]string, 0)
	for index := startRow - 1; index >= 0; index-- {
		trimmed := strings.TrimSpace(lines[index])
		switch {
		case trimmed == "":
			if len(commentLines) == 0 {
				continue
			}
			return ""
		case strings.HasPrefix(trimmed, "/**") && strings.HasSuffix(trimmed, "*/"):
			return normalizeJavaScriptDocstring([]string{trimmed})
		case strings.HasPrefix(trimmed, "*/"):
			commentLines = append([]string{trimmed}, commentLines...)
		case len(commentLines) > 0:
			commentLines = append([]string{trimmed}, commentLines...)
			if strings.HasPrefix(trimmed, "/**") {
				return normalizeJavaScriptDocstring(commentLines)
			}
			if strings.HasPrefix(trimmed, "/*") {
				return ""
			}
		default:
			return ""
		}
	}

	return ""
}

func normalizeJavaScriptDocstring(lines []string) string {
	if len(lines) == 0 {
		return ""
	}

	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		trimmed = strings.TrimPrefix(trimmed, "/**")
		trimmed = strings.TrimPrefix(trimmed, "/*")
		trimmed = strings.TrimPrefix(trimmed, "*/")
		trimmed = strings.TrimSuffix(trimmed, "*/")
		trimmed = strings.TrimPrefix(trimmed, "*")
		trimmed = strings.TrimSpace(trimmed)
		if trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return strings.Join(parts, "\n")
}

func javaScriptFunctionKind(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}

	switch node.Kind() {
	case "function_declaration", "function_expression", "arrow_function":
		declaration := strings.TrimSpace(nodeText(node, source))
		if strings.HasPrefix(declaration, "function*") || strings.HasPrefix(declaration, "async function*") {
			return "generator"
		}
		if strings.HasPrefix(declaration, "async ") {
			return "async"
		}
		return ""
	case "generator_function_declaration", "generator_function":
		return "generator"
	case "method_definition", "method_signature":
		matches := javaScriptMethodKindRe.FindStringSubmatch(nodeText(node, source))
		if len(matches) == 2 {
			switch matches[1] {
			case "get":
				return "getter"
			case "set":
				return "setter"
			default:
				return matches[1]
			}
		}
		declaration := strings.TrimSpace(nodeText(node, source))
		for {
			switch {
			case strings.HasPrefix(declaration, "static "):
				declaration = strings.TrimSpace(strings.TrimPrefix(declaration, "static "))
			case strings.HasPrefix(declaration, "async "):
				declaration = strings.TrimSpace(strings.TrimPrefix(declaration, "async "))
			default:
				if strings.HasPrefix(declaration, "*") {
					return "generator"
				}
				return ""
			}
		}
	}

	return ""
}
