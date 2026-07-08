// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// -- In-memory resolution entry points (no tree walks) --
//
// Each function below resolves framework route entries from call_expression
// or method_definition nodes that were gathered (cloned via shared.CloneNode)
// during the main declaration walk in Parse, eliminating the per-framework
// full-tree re-walks the pre-#4925 code performed.

// detectExpressSemanticsFromGathered resolves Express route semantics from
// call_expression nodes gathered during the main declaration walk.
func detectExpressSemanticsFromGathered(
	root *tree_sitter.Node,
	source []byte,
	gatheredCallExpressions []*tree_sitter.Node,
) (map[string]any, bool) {
	if !javaScriptHasExpressImport(string(source)) {
		return nil, false
	}
	routes := javaScriptExpressRouteEntriesFromGathered(gatheredCallExpressions, source)
	if len(routes) == 0 {
		return nil, false
	}

	routeRegistrations := make(map[string]int, len(routes))
	for _, route := range routes {
		routeRegistrations[expressRouteKey(route.method, route.path)]++
	}

	methods := make([]string, 0, len(routes))
	paths := make([]string, 0, len(routes))
	entries := make([]map[string]string, 0, len(routes))
	serverSymbols := make([]string, 0, len(routes))
	seenMethods := make(map[string]struct{})
	seenPaths := make(map[string]struct{})
	seenSymbols := make(map[string]struct{})
	for _, route := range routes {
		key := expressRouteKey(route.method, route.path)
		handler := ""
		if routeRegistrations[key] == 1 {
			handler = route.handler
		}
		entries = append(entries, routeEntry(route.method, route.path, handler))
		if _, ok := seenMethods[route.method]; !ok {
			seenMethods[route.method] = struct{}{}
			methods = append(methods, route.method)
		}
		if _, ok := seenPaths[route.path]; !ok {
			seenPaths[route.path] = struct{}{}
			paths = append(paths, route.path)
		}
		if _, ok := seenSymbols[route.symbol]; !ok {
			seenSymbols[route.symbol] = struct{}{}
			serverSymbols = append(serverSymbols, route.symbol)
		}
	}

	return map[string]any{
		"route_methods":  methods,
		"route_paths":    paths,
		"route_entries":  entries,
		"server_symbols": serverSymbols,
	}, true
}

// javaScriptExpressRouteEntriesFromGathered resolves Express route entries
// from pre-gathered call_expression nodes without a full-tree re-walk.
func javaScriptExpressRouteEntriesFromGathered(
	gathered []*tree_sitter.Node,
	source []byte,
) []javaScriptExpressRoute {
	routes := make([]javaScriptExpressRoute, 0)
	for _, node := range gathered {
		if node.Kind() != "call_expression" {
			continue
		}
		functionNode := node.ChildByFieldName("function")
		if functionNode == nil || functionNode.Kind() != "member_expression" {
			continue
		}
		objectNode := functionNode.ChildByFieldName("object")
		propertyNode := functionNode.ChildByFieldName("property")
		if objectNode == nil || objectNode.Kind() != "identifier" || propertyNode == nil {
			continue
		}
		method := strings.ToLower(strings.TrimSpace(nodeText(propertyNode, source)))
		if _, ok := javaScriptExpressRouteMethods[method]; !ok {
			continue
		}
		argsNode := node.ChildByFieldName("arguments")
		if argsNode == nil {
			continue
		}
		cursor := argsNode.Walk()
		args := argsNode.NamedChildren(cursor)
		cursor.Close()
		if len(args) == 0 || args[0].Kind() != "string" {
			continue
		}
		path := jsStringLiteralValue(&args[0], source)
		if strings.TrimSpace(path) == "" {
			continue
		}
		handler := ""
		if len(args) == 2 && args[1].Kind() == "identifier" {
			handler = strings.TrimSpace(nodeText(&args[1], source))
		}
		routes = append(routes, javaScriptExpressRoute{
			symbol:  strings.TrimSpace(nodeText(objectNode, source)),
			method:  strings.ToUpper(method),
			path:    path,
			handler: handler,
		})
	}
	return routes
}

// javaScriptKoaRouteEntriesFromGathered resolves Koa route entries from
// pre-gathered call_expression nodes without a full-tree re-walk.
func javaScriptKoaRouteEntriesFromGathered(
	gathered []*tree_sitter.Node,
	source []byte,
	bases map[string]struct{},
) []map[string]string {
	entries := make([]map[string]string, 0)
	for _, node := range gathered {
		if node.Kind() != "call_expression" {
			continue
		}
		base, property, ok := javaScriptMemberBaseAndProperty(node.ChildByFieldName("function"), source)
		if !ok || !javaScriptNameSetContains(bases, base) {
			continue
		}
		method := strings.ToLower(property)
		if _, ok := javaScriptKoaRouteMethods[method]; !ok || method == "use" {
			continue
		}
		args := javaScriptCallArguments(node)
		path, handler, ok := javaScriptKoaRoutePathAndHandler(args, source)
		if !ok {
			continue
		}
		entries = append(entries, routeEntry(method, path, handler))
	}
	return entries
}

// javaScriptFastifyRouteEntriesFromGathered resolves Fastify route entries
// from pre-gathered call_expression nodes without a full-tree re-walk.
func javaScriptFastifyRouteEntriesFromGathered(
	gathered []*tree_sitter.Node,
	source []byte,
	bases map[string]struct{},
) []map[string]string {
	entries := make([]map[string]string, 0)
	for _, node := range gathered {
		if node.Kind() != "call_expression" {
			continue
		}
		base, property, ok := javaScriptMemberBaseAndProperty(node.ChildByFieldName("function"), source)
		if !ok || !javaScriptNameSetContains(bases, base) {
			continue
		}
		method := strings.ToLower(property)
		if _, ok := javaScriptFastifyRouteMethods[method]; !ok {
			continue
		}
		args := javaScriptCallArguments(node)
		if method == "route" {
			entries = append(entries, javaScriptFastifyRouteObjectEntries(args, source)...)
			continue
		}
		path, ok := javaScriptRoutePathArg(args, 0, source)
		if !ok || !strings.HasPrefix(path, "/") {
			continue
		}
		handler := ""
		switch len(args) {
		case 2:
			handler = javaScriptIdentifierName(&args[1], source)
		case 3:
			handler = javaScriptIdentifierName(&args[2], source)
		}
		entries = append(entries, routeEntry(method, path, handler))
	}
	return entries
}

// javaScriptNestJSRouteEntriesFromGathered resolves NestJS route entries from
// pre-gathered method_definition nodes without a full-tree re-walk.
func javaScriptNestJSRouteEntriesFromGathered(
	gathered []*tree_sitter.Node,
	source []byte,
	parents *javaScriptParentLookup,
) []map[string]string {
	entries := make([]map[string]string, 0)
	for _, node := range gathered {
		if node.Kind() != "method_definition" {
			continue
		}
		classNode := javaScriptEnclosingClassNode(node, parents)
		prefix, ok := javaScriptNestJSControllerPrefix(classNode, source, parents)
		if !ok {
			continue
		}
		method, routePath, ok := javaScriptNestJSMethodRoute(node, source, parents)
		if !ok {
			continue
		}
		handler := javaScriptIdentifierName(node.ChildByFieldName("name"), source)
		if handler == "" {
			continue
		}
		entries = append(entries, routeEntry(method, joinNestJSPaths(prefix, routePath), handler))
	}
	return entries
}
