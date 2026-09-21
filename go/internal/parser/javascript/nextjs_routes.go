// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript

import (
	"path/filepath"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func nextJSRouteSegmentsForModule(path string, moduleKind string) []string {
	if moduleKind == "pages_api" {
		return nextJSPagesAPIRouteSegments(path)
	}
	return nextJSRouteSegments(path)
}

func javaScriptIsNextJSPagesAPIPath(path string) bool {
	relative := filepath.ToSlash(path)
	if !strings.Contains(relative, "/pages/api/") && !strings.HasPrefix(relative, "pages/api/") {
		return false
	}
	switch strings.ToLower(filepath.Ext(relative)) {
	case ".js", ".jsx", ".ts", ".tsx":
		return true
	default:
		return false
	}
}

func nextJSPagesAPIRouteSegments(path string) []string {
	relative := filepath.ToSlash(path)
	const marker = "/pages/api/"
	if idx := strings.Index(relative, marker); idx >= 0 {
		relative = relative[idx+len(marker):]
	} else {
		relative = strings.TrimPrefix(relative, "pages/api/")
	}
	withoutExt := strings.TrimSuffix(relative, filepath.Ext(relative))
	parts := strings.Split(withoutExt, "/")
	segments := []string{"api"}
	for idx, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || (part == "index" && idx == len(parts)-1) {
			continue
		}
		segments = append(segments, part)
	}
	return segments
}

func javaScriptNextJSAppRouteEntries(
	root *tree_sitter.Node,
	source []byte,
	routeSegments []string,
) []map[string]string {
	routePath := nextJSRoutePathFromSegments(routeSegments)
	if routePath == "" {
		return nil
	}
	handlers := javaScriptNextJSRouteHandlerExports(root, source)
	entries := make([]map[string]string, 0, len(handlers))
	for _, handler := range handlers {
		entries = append(entries, routeEntry(handler, routePath, handler))
	}
	return entries
}

func javaScriptNextJSPagesAPIRouteEntries(
	root *tree_sitter.Node,
	source []byte,
	routeSegments []string,
) []map[string]string {
	routePath := nextJSRoutePathFromSegments(routeSegments)
	if routePath == "" {
		return nil
	}
	handler := javaScriptNextJSDefaultHandlerExport(root, source)
	if handler == "" {
		return nil
	}
	return []map[string]string{routeEntry("ANY", routePath, handler)}
}

func nextJSRoutePathFromSegments(segments []string) string {
	pathSegments := make([]string, 0, len(segments))
	for _, segment := range segments {
		segment = strings.TrimSpace(segment)
		if segment == "" || (strings.HasPrefix(segment, "(") && strings.HasSuffix(segment, ")")) {
			continue
		}
		if strings.HasPrefix(segment, "@") ||
			strings.HasPrefix(segment, "_") ||
			strings.HasPrefix(segment, "(.)") ||
			strings.HasPrefix(segment, "(..)") ||
			strings.HasPrefix(segment, "(...)") {
			return ""
		}
		pathSegments = append(pathSegments, segment)
	}
	if len(pathSegments) == 0 {
		return "/"
	}
	return "/" + strings.Join(pathSegments, "/")
}

func routeEntryMethods(entries []map[string]string) []string {
	methods := make([]string, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		method := strings.TrimSpace(entry["method"])
		if method == "" {
			continue
		}
		if _, ok := seen[method]; ok {
			continue
		}
		seen[method] = struct{}{}
		methods = append(methods, method)
	}
	return methods
}

func javaScriptNextJSRouteHandlerExports(root *tree_sitter.Node, source []byte) []string {
	handlers := make([]string, 0, 4)
	seen := map[string]struct{}{}
	if root == nil {
		return handlers
	}
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "export_statement" {
			return
		}
		for _, handler := range javaScriptHTTPHandlersFromExport(node, source) {
			if _, ok := seen[handler]; ok {
				continue
			}
			seen[handler] = struct{}{}
			handlers = append(handlers, handler)
		}
	})
	return handlers
}

func javaScriptHTTPHandlersFromExport(node *tree_sitter.Node, source []byte) []string {
	declaration := node.ChildByFieldName("declaration")
	if declaration == nil {
		return nil
	}
	switch declaration.Kind() {
	case "function_declaration", "generator_function_declaration":
		return javaScriptHTTPHandlerName(declaration.ChildByFieldName("name"), source)
	case "lexical_declaration", "variable_declaration":
		return javaScriptHTTPHandlerNamesFromVariableDeclaration(declaration, source)
	default:
		return nil
	}
}

func javaScriptHTTPHandlerName(nameNode *tree_sitter.Node, source []byte) []string {
	name := strings.TrimSpace(nodeText(nameNode, source))
	verb := strings.ToUpper(name)
	if _, ok := javaScriptHTTPMethodVerbs[verb]; !ok {
		return nil
	}
	return []string{verb}
}

func javaScriptHTTPHandlerNamesFromVariableDeclaration(node *tree_sitter.Node, source []byte) []string {
	cursor := node.Walk()
	children := node.NamedChildren(cursor)
	cursor.Close()
	handlers := make([]string, 0, len(children))
	for i := range children {
		child := children[i]
		if child.Kind() != "variable_declarator" || !isJavaScriptFunctionValue(child.ChildByFieldName("value")) {
			continue
		}
		handlers = append(handlers, javaScriptHTTPHandlerName(child.ChildByFieldName("name"), source)...)
	}
	return handlers
}

func javaScriptNextJSDefaultHandlerExport(root *tree_sitter.Node, source []byte) string {
	localFunctions := javaScriptLocalFunctionBindings(root, source)
	handler := ""
	walkNamed(root, func(node *tree_sitter.Node) {
		if handler != "" || node.Kind() != "export_statement" || !javaScriptExportStatementIsDefault(node, source) {
			return
		}
		declaration := node.ChildByFieldName("declaration")
		if declaration == nil {
			return
		}
		switch declaration.Kind() {
		case "function_declaration", "generator_function_declaration":
			handler = strings.TrimSpace(nodeText(declaration.ChildByFieldName("name"), source))
		case "identifier":
			name := strings.TrimSpace(nodeText(declaration, source))
			if localFunctions[name] {
				handler = name
			}
		}
	})
	return handler
}

func javaScriptExportStatementIsDefault(node *tree_sitter.Node, source []byte) bool {
	text := strings.TrimSpace(nodeText(node, source))
	return strings.HasPrefix(text, "export default")
}

func javaScriptLocalFunctionBindings(root *tree_sitter.Node, source []byte) map[string]bool {
	bindings := map[string]bool{}
	if root == nil {
		return bindings
	}
	walkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "function_declaration", "generator_function_declaration":
			name := strings.TrimSpace(nodeText(node.ChildByFieldName("name"), source))
			if name != "" {
				bindings[name] = true
			}
		case "variable_declarator":
			if isJavaScriptFunctionValue(node.ChildByFieldName("value")) {
				name := strings.TrimSpace(nodeText(node.ChildByFieldName("name"), source))
				if name != "" {
					bindings[name] = true
				}
			}
		}
	})
	return bindings
}
