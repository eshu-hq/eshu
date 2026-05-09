package python

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	yamlparser "github.com/eshu-hq/eshu/go/internal/parser/yaml"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func readSource(path string) ([]byte, error) {
	return shared.ReadSource(path)
}

func basePayload(path string, lang string, isDependency bool) map[string]any {
	return shared.BasePayload(path, lang, isDependency)
}

func walkNamed(node *tree_sitter.Node, visit func(*tree_sitter.Node)) {
	shared.WalkNamed(node, visit)
}

func nodeText(node *tree_sitter.Node, source []byte) string {
	return shared.NodeText(node, source)
}

func nodeLine(node *tree_sitter.Node) int {
	return shared.NodeLine(node)
}

func nodeEndLine(node *tree_sitter.Node) int {
	return shared.NodeEndLine(node)
}

func appendBucket(payload map[string]any, key string, item map[string]any) {
	shared.AppendBucket(payload, key, item)
}

func firstNamedDescendant(node *tree_sitter.Node, kinds ...string) *tree_sitter.Node {
	var result *tree_sitter.Node
	walkNamed(node, func(child *tree_sitter.Node) {
		if result != nil {
			return
		}
		for _, kind := range kinds {
			if child.Kind() == kind {
				result = shared.CloneNode(child)
				return
			}
		}
	})
	return result
}

func decodeYAMLDocuments(source string) ([]any, error) {
	return yamlparser.DecodeDocuments(source)
}

func sanitizeYAMLTemplating(source string) string {
	return yamlparser.SanitizeTemplating(source)
}

func uniqueOrdered(matches [][]string, group int) []string {
	seen := make(map[string]struct{}, len(matches))
	values := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) <= group {
			continue
		}
		value := strings.TrimSpace(match[group])
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	return values
}

func routeEntry(method string, path string) map[string]string {
	return map[string]string{
		"method": strings.ToUpper(strings.TrimSpace(method)),
		"path":   strings.TrimSpace(path),
	}
}
