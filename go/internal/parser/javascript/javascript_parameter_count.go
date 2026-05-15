package javascript

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func javaScriptParameterCount(parametersNode *tree_sitter.Node, source []byte) int {
	if parametersNode == nil {
		return 0
	}
	text := strings.TrimSpace(nodeText(parametersNode, source))
	text = strings.TrimPrefix(strings.TrimSuffix(text, ")"), "(")
	if strings.TrimSpace(text) == "" {
		return 0
	}
	return len(splitJavaScriptTopLevelParameters(text))
}

func splitJavaScriptTopLevelParameters(text string) []string {
	var parts []string
	start := 0
	depth := 0
	for index, char := range text {
		switch char {
		case '(', '[', '{', '<':
			depth++
		case ')', ']', '}', '>':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				parts = appendNonEmptyJavaScriptParameter(parts, text[start:index])
				start = index + len(string(char))
			}
		}
	}
	return appendNonEmptyJavaScriptParameter(parts, text[start:])
}

func appendNonEmptyJavaScriptParameter(parts []string, value string) []string {
	if strings.TrimSpace(value) == "" {
		return parts
	}
	return append(parts, value)
}
