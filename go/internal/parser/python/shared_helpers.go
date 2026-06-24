// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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

// appendUniqueString appends value to values only when it is not already
// present, preserving first-seen order for deterministic parser payloads.
func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

// routeEntry is the parser-owned wire shape consumed by query read models. The
// handler symbol is included only when an exact route->handler binding was
// observed (a single named def following the route decorator); an empty handler
// is omitted so consumers never read a fabricated binding (#2788).
func routeEntry(method string, path string, handler string) map[string]string {
	entry := map[string]string{
		"method": strings.ToUpper(strings.TrimSpace(method)),
		"path":   strings.TrimSpace(path),
	}
	if handler = strings.TrimSpace(handler); handler != "" {
		entry["handler"] = handler
	}
	return entry
}
