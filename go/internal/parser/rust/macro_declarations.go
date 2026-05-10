package rust

import (
	"regexp"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

var (
	rustMacroModDeclarationPattern = regexp.MustCompile(`(?m)\b(?:pub(?:\([^)]*\))?\s+)?mod\s+([A-Za-z_][A-Za-z0-9_]*)\s*;`)
	rustMacroUseDeclarationPattern = regexp.MustCompile(`(?m)\b(?:pub(?:\([^)]*\))?\s+)?use\s+([^;]+);`)
)

func appendRustMacroDeclarations(payload map[string]any, node *tree_sitter.Node, source []byte) {
	body, ok := rustMacroInvocationBody(shared.NodeText(node, source))
	if !ok {
		return
	}
	appendRustMacroModules(payload, node, body)
	appendRustMacroImports(payload, node, body)
}

func rustMacroInvocationBody(text string) (string, bool) {
	bang := strings.Index(text, "!")
	if bang < 0 || bang+1 >= len(text) {
		return "", false
	}
	return rustMacroBody(text[bang+1:])
}

func appendRustMacroModules(payload map[string]any, node *tree_sitter.Node, body string) {
	for _, match := range rustMacroModDeclarationPattern.FindAllStringSubmatch(body, -1) {
		if len(match) < 2 {
			continue
		}
		name := strings.TrimSpace(match[1])
		if name == "" {
			continue
		}
		item := map[string]any{
			"name":          name,
			"line_number":   shared.NodeLine(node),
			"end_line":      shared.NodeEndLine(node),
			"module_kind":   "declaration",
			"module_origin": "macro_invocation",
			"lang":          "rust",
		}
		if candidates := rustModuleDeclaredPathCandidates(name, "declaration"); len(candidates) > 0 {
			item["declared_path_candidates"] = candidates
		}
		shared.AppendBucket(payload, "modules", item)
	}
}

func appendRustMacroImports(payload map[string]any, node *tree_sitter.Node, body string) {
	for _, match := range rustMacroUseDeclarationPattern.FindAllStringSubmatch(body, -1) {
		if len(match) < 2 {
			continue
		}
		raw := strings.TrimSpace(match[0])
		visibility := rustVisibility(raw)
		importText := rustStripVisibility(raw, visibility)
		importText = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(importText, "use "), ";"))
		for _, entry := range rustImportEntries(importText) {
			item := map[string]any{
				"name":             entry.name,
				"source":           entry.name,
				"alias":            entry.alias,
				"full_import_name": raw,
				"import_type":      entry.importType,
				"import_origin":    "macro_invocation",
				"line_number":      shared.NodeLine(node),
				"lang":             "rust",
			}
			if visibility != "" {
				item["visibility"] = visibility
			}
			shared.AppendBucket(payload, "imports", item)
		}
	}
}
