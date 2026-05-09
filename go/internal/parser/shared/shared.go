package shared

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// GoImportedInterfaceParamMethods maps lower-case function names to the
// imported interface methods required by each parameter index.
type GoImportedInterfaceParamMethods map[string]map[int][]string

// GoPackageImportedInterfaceParamMethods maps absolute Go package directories
// to same-package imported interface parameter contracts.
type GoPackageImportedInterfaceParamMethods map[string]GoImportedInterfaceParamMethods

// Options configures one parser execution.
type Options struct {
	IndexSource                     bool
	VariableScope                   string
	GoImportedInterfaceParamMethods GoImportedInterfaceParamMethods
}

// NormalizedVariableScope returns the canonical scope used by language
// adapters that can choose between module-level and full local-variable output.
func (o Options) NormalizedVariableScope() string {
	scope := strings.TrimSpace(strings.ToLower(o.VariableScope))
	if scope == "all" {
		return "all"
	}
	return "module"
}

// BasePayload returns the common parser payload fields and empty buckets shared
// by source-language adapters.
func BasePayload(path string, lang string, isDependency bool) map[string]any {
	return map[string]any{
		"path":           path,
		"lang":           lang,
		"is_dependency":  isDependency,
		"functions":      []map[string]any{},
		"classes":        []map[string]any{},
		"variables":      []map[string]any{},
		"imports":        []map[string]any{},
		"function_calls": []map[string]any{},
	}
}

// ReadSource reads one parser input file and wraps the path into read errors.
func ReadSource(path string) ([]byte, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read source %q: %w", path, err)
	}
	return body, nil
}

// WalkNamed visits a node and every named descendant in source order.
func WalkNamed(node *tree_sitter.Node, visit func(*tree_sitter.Node)) {
	if node == nil {
		return
	}

	visit(node)

	cursor := node.Walk()
	defer cursor.Close()

	for _, child := range node.NamedChildren(cursor) {
		child := child
		WalkNamed(&child, visit)
	}
}

// NodeText returns the source slice covered by a tree-sitter node.
func NodeText(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	return node.Utf8Text(source)
}

// NodeLine returns the 1-based start line for a tree-sitter node.
func NodeLine(node *tree_sitter.Node) int {
	if node == nil {
		return 1
	}
	return int(node.StartPosition().Row) + 1
}

// NodeEndLine returns the 1-based end line for a tree-sitter node.
func NodeEndLine(node *tree_sitter.Node) int {
	if node == nil {
		return 1
	}
	return int(node.EndPosition().Row) + 1
}

// CloneNode returns a stable pointer copy for callers that need to keep a node
// after cursor iteration advances.
func CloneNode(node *tree_sitter.Node) *tree_sitter.Node {
	if node == nil {
		return nil
	}
	cloned := *node
	return &cloned
}

// AppendBucket appends one row to a parser payload bucket.
func AppendBucket(payload map[string]any, key string, item map[string]any) {
	items, _ := payload[key].([]map[string]any)
	payload[key] = append(items, item)
}

// SortNamedBucket sorts a payload bucket by its string name field.
func SortNamedBucket(payload map[string]any, key string) {
	items, _ := payload[key].([]map[string]any)
	SortNamedMaps(items)
	payload[key] = items
}

// SortNamedMaps sorts parser payload rows by their string name field.
func SortNamedMaps(values []map[string]any) {
	slices.SortFunc(values, func(left, right map[string]any) int {
		if delta := IntValue(left["line_number"]) - IntValue(right["line_number"]); delta != 0 {
			return delta
		}
		leftName, _ := left["name"].(string)
		rightName, _ := right["name"].(string)
		return strings.Compare(leftName, rightName)
	})
}

// CollectBucketNames returns cleaned non-empty name values from parser payload
// buckets in caller-provided bucket order.
func CollectBucketNames(payload map[string]any, keys ...string) []string {
	var names []string
	for _, key := range keys {
		items, _ := payload[key].([]map[string]any)
		for _, item := range items {
			name, _ := item["name"].(string)
			if strings.TrimSpace(name) != "" {
				names = append(names, filepath.Clean(name))
			}
		}
	}
	return names
}

// IntValue converts common JSON and parser numeric values to int.
func IntValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case float32:
		return int(typed)
	default:
		return 0
	}
}

// LastPathSegment returns the final non-empty segment split by separator.
func LastPathSegment(name string, separator string) string {
	parts := strings.Split(strings.TrimSpace(name), separator)
	for i := len(parts) - 1; i >= 0; i-- {
		if segment := strings.TrimSpace(parts[i]); segment != "" {
			return segment
		}
	}
	return strings.TrimSpace(name)
}

// DedupeNonEmptyStrings returns sorted unique non-empty strings.
func DedupeNonEmptyStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		seen[value] = struct{}{}
	}
	result := make([]string, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	slices.Sort(result)
	return result
}
