// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package golang

import (
	"strconv"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func goHTTPFrameworkSemantics(
	root *tree_sitter.Node,
	source []byte,
	importAliases map[string][]string,
) (map[string]any, bool) {
	if root == nil {
		return nil, false
	}
	serveMuxVars := goHTTPServeMuxVars(root, source, importAliases)
	entries := make([]map[string]string, 0)
	routeMethods := make([]string, 0)
	routePaths := make([]string, 0)

	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "call_expression" {
			return
		}
		entry, ok := goHTTPRouteEntry(node, source, importAliases, serveMuxVars)
		if !ok {
			return
		}
		entries = append(entries, entry)
		routeMethods = appendUniqueImportAlias(routeMethods, entry["method"])
		routePaths = appendUniqueImportAlias(routePaths, entry["path"])
	})
	if len(entries) == 0 {
		return nil, false
	}

	return map[string]any{
		"frameworks": []string{"net_http"},
		"net_http": map[string]any{
			"route_methods": routeMethods,
			"route_paths":   routePaths,
			"route_entries": entries,
		},
	}, true
}

func goHTTPRouteEntry(
	node *tree_sitter.Node,
	source []byte,
	importAliases map[string][]string,
	serveMuxVars map[string]struct{},
) (map[string]string, bool) {
	functionNode := node.ChildByFieldName("function")
	base, field, ok := goSelectorBaseAndField(functionNode, source)
	if !ok {
		return nil, false
	}

	base = strings.ToLower(base)
	field = strings.ToLower(field)
	if field != "handlefunc" && field != "handle" {
		return nil, false
	}

	if !goHTTPRegistrationBaseKnown(base, importAliases, serveMuxVars) {
		return nil, false
	}

	argsNode := node.ChildByFieldName("arguments")
	if argsNode == nil {
		return nil, false
	}
	args := argsNode.NamedChildren(argsNode.Walk())
	if len(args) < 2 {
		return nil, false
	}

	pattern, ok := goStringLiteralValue(&args[0], source)
	if !ok {
		return nil, false
	}
	method, routePath, ok := goHTTPRoutePattern(pattern)
	if !ok {
		return nil, false
	}

	handlerName := ""
	switch field {
	case "handlefunc":
		if args[1].Kind() == "identifier" {
			handlerName = strings.TrimSpace(nodeText(&args[1], source))
		}
	case "handle":
		handlerName = goHTTPHandlerWrapperTarget(&args[1], source, importAliases)
	}
	if handlerName == "" {
		return nil, false
	}

	return map[string]string{
		"method":  method,
		"path":    routePath,
		"handler": handlerName,
	}, true
}

func goHTTPRegistrationBaseKnown(
	base string,
	importAliases map[string][]string,
	serveMuxVars map[string]struct{},
) bool {
	for _, alias := range goAliasesForImportPath(importAliases, "net/http") {
		if strings.ToLower(alias) == base {
			return true
		}
	}
	_, ok := serveMuxVars[base]
	return ok
}

func goStringLiteralValue(node *tree_sitter.Node, source []byte) (string, bool) {
	if node == nil {
		return "", false
	}
	switch node.Kind() {
	case "interpreted_string_literal", "raw_string_literal":
	default:
		return "", false
	}
	value, err := strconv.Unquote(strings.TrimSpace(nodeText(node, source)))
	if err != nil {
		return "", false
	}
	value = strings.TrimSpace(value)
	return value, value != ""
}

func goHTTPRoutePattern(pattern string) (string, string, bool) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return "", "", false
	}
	fields := strings.Fields(pattern)
	if len(fields) == 2 && goKnownHTTPMethod(fields[0]) {
		routePath := strings.TrimSpace(fields[1])
		return strings.ToUpper(fields[0]), routePath, routePath != ""
	}
	return "ANY", pattern, true
}

func goKnownHTTPMethod(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case "CONNECT", "DELETE", "GET", "HEAD", "OPTIONS", "PATCH", "POST", "PUT", "TRACE":
		return true
	default:
		return false
	}
}
