// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript

import (
	"path/filepath"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func maybeAppendJavaScriptComponent(
	payload map[string]any,
	node *tree_sitter.Node,
	nameNode *tree_sitter.Node,
	source []byte,
	outputLanguage string,
	reactAliases map[string]string,
) {
	name := strings.TrimSpace(nodeText(nameNode, source))
	if !isPascalIdentifier(name) {
		return
	}
	if !javaScriptLooksLikeComponent(node, source, outputLanguage) {
		return
	}
	item := map[string]any{
		"name":        name,
		"line_number": nodeLine(nameNode),
		"end_line":    nodeEndLine(node),
		"lang":        outputLanguage,
	}
	if outputLanguage == "tsx" && javaScriptContainsJSXFragmentShorthand(node) {
		item["jsx_fragment_shorthand"] = true
	}
	if outputLanguage == "tsx" {
		if wrapperKind := javaScriptComponentWrapperKind(node, source, reactAliases); wrapperKind != "" {
			item["component_wrapper_kind"] = wrapperKind
		}
	}
	appendBucket(payload, "components", item)
}

func javaScriptComponentWrapperKind(node *tree_sitter.Node, source []byte, reactAliases map[string]string) string {
	if node == nil {
		return ""
	}
	switch node.Kind() {
	case "parenthesized_expression":
		cursor := node.Walk()
		children := node.NamedChildren(cursor)
		cursor.Close()
		for i := range children {
			if wrapper := javaScriptComponentWrapperKind(&children[i], source, reactAliases); wrapper != "" {
				return wrapper
			}
		}
	case "call_expression":
		functionNode := node.ChildByFieldName("function")
		name := javaScriptNormalizeReactAlias(strings.TrimSpace(javaScriptCallName(functionNode, source)), reactAliases)
		switch name {
		case "memo", "forwardRef", "lazy":
			return name
		}
	}
	return ""
}

func javaScriptComponentTypeAssertion(node *tree_sitter.Node, source []byte, reactAliases map[string]string) string {
	if node == nil {
		return ""
	}
	switch node.Kind() {
	case "type_annotation":
		if typeNode := node.ChildByFieldName("type"); typeNode != nil {
			if typeName := javaScriptAssertionTypeName(typeNode, source); typeName != "" {
				return typeName
			}
		}
		cursor := node.Walk()
		children := node.NamedChildren(cursor)
		cursor.Close()
		for i := range children {
			child := children[i]
			if typeName := javaScriptAssertionTypeName(&child, source); typeName != "" {
				return javaScriptNormalizeReactAlias(typeName, reactAliases)
			}
		}
	case "as_expression", "type_assertion":
		if typeName := javaScriptAssertionTypeName(node.ChildByFieldName("type"), source); typeName != "" {
			return javaScriptNormalizeReactAlias(typeName, reactAliases)
		}
		cursor := node.Walk()
		children := node.NamedChildren(cursor)
		cursor.Close()
		if len(children) >= 2 {
			return javaScriptNormalizeReactAlias(javaScriptAssertionTypeName(&children[1], source), reactAliases)
		}
	case "parenthesized_expression":
		cursor := node.Walk()
		children := node.NamedChildren(cursor)
		cursor.Close()
		for i := range children {
			child := children[i]
			if typeName := javaScriptComponentTypeAssertion(&child, source, reactAliases); typeName != "" {
				return typeName
			}
		}
	}
	return ""
}

func javaScriptNormalizeReactAlias(name string, reactAliases map[string]string) string {
	name = strings.TrimSpace(name)
	if name == "" || len(reactAliases) == 0 {
		return name
	}
	if normalized, ok := reactAliases[name]; ok && normalized != "" {
		return normalized
	}
	return name
}

func javaScriptReactAliases(root *tree_sitter.Node, source []byte, outputLanguage string) map[string]string {
	if root == nil || outputLanguage != "tsx" {
		return nil
	}

	reactAliases := map[string]string{}
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "import_statement" {
			return
		}
		for _, item := range javaScriptImportEntries(node, source, outputLanguage) {
			sourceName, _ := item["source"].(string)
			if sourceName != "react" {
				continue
			}
			alias, _ := item["alias"].(string)
			if alias == "" {
				continue
			}
			name, _ := item["name"].(string)
			if name == "" || name == "*" || name == "default" {
				continue
			}
			switch name {
			case "ComponentType", "FC", "FunctionComponent", "memo", "forwardRef", "lazy":
				reactAliases[alias] = name
			}
		}
	})
	if len(reactAliases) == 0 {
		return nil
	}
	return reactAliases
}

func javaScriptLooksLikeComponent(node *tree_sitter.Node, source []byte, outputLanguage string) bool {
	if outputLanguage == "tsx" {
		return true
	}
	text := nodeText(node, source)
	return strings.Contains(text, "React.Component") ||
		strings.Contains(text, "React.PureComponent") ||
		strings.Contains(text, "useState(") ||
		strings.Contains(text, "useEffect(") ||
		strings.Contains(text, "useMemo(") ||
		javaScriptContainsJSXReturn(node)
}

func buildJavaScriptFrameworkSemantics(path string, root *tree_sitter.Node, source []byte, payload map[string]any) map[string]any {
	semantics := map[string]any{
		"frameworks": []string{},
	}
	frameworks := make([]string, 0, 6)

	if nextjs, ok := detectNextJSSemantics(path, root, source); ok {
		frameworks = append(frameworks, "nextjs")
		semantics["nextjs"] = nextjs
	}
	if express, ok := detectExpressSemantics(root, source); ok {
		frameworks = append(frameworks, "express")
		semantics["express"] = express
	}
	if aws, ok := detectAWSSemantics(root, source); ok {
		frameworks = append(frameworks, "aws")
		semantics["aws"] = aws
	}
	if gcp, ok := detectGCPSemantics(root, source); ok {
		frameworks = append(frameworks, "gcp")
		semantics["gcp"] = gcp
	}
	if react, ok := detectReactSemantics(path, root, source, payload); ok {
		frameworks = append(frameworks, "react")
		semantics["react"] = react
	}
	if hapi, ok := detectHapiSemantics(root, source); ok {
		frameworks = append(frameworks, "hapi")
		semantics["hapi"] = hapi
	}

	semantics["frameworks"] = frameworks
	return semantics
}

func detectNextJSSemantics(path string, root *tree_sitter.Node, source []byte) (map[string]any, bool) {
	text := string(source)
	moduleKind := ""
	switch filepath.Base(path) {
	case "route.ts", "route.tsx", "route.js", "route.jsx":
		moduleKind = "route"
	case "page.tsx", "page.jsx", "page.ts", "page.js":
		moduleKind = "page"
	case "layout.tsx", "layout.jsx", "layout.ts", "layout.js":
		moduleKind = "layout"
	}
	if moduleKind == "" && javaScriptIsNextJSPagesAPIPath(path) {
		moduleKind = "pages_api"
	}
	if moduleKind == "" {
		return nil, false
	}

	routeSegments := nextJSRouteSegmentsForModule(path, moduleKind)
	metadataExports := "none"
	if strings.Contains(text, "generateMetadata") {
		metadataExports = "dynamic"
	} else if javaScriptHasMetadataConstExport(root, source) {
		metadataExports = "static"
	}

	runtimeBoundary := "server"
	if directive := javaScriptRuntimeDirective(root, source); directive != "" {
		runtimeBoundary = directive
	}

	nextjs := map[string]any{
		"module_kind":      moduleKind,
		"metadata_exports": metadataExports,
		"route_segments":   routeSegments,
		"runtime_boundary": runtimeBoundary,
	}
	switch moduleKind {
	case "route":
		entries := javaScriptNextJSAppRouteEntries(root, source, routeSegments)
		nextjs["route_verbs"] = routeEntryMethods(entries)
		if len(entries) > 0 {
			nextjs["route_entries"] = entries
		}
		nextjs["request_response_apis"] = nextJSRequestResponseAPIs(text)
	case "pages_api":
		entries := javaScriptNextJSPagesAPIRouteEntries(root, source, routeSegments)
		nextjs["route_verbs"] = routeEntryMethods(entries)
		if len(entries) > 0 {
			nextjs["route_entries"] = entries
		}
	}
	return nextjs, true
}

func javaScriptHasExpressImport(source string) bool {
	return strings.Contains(source, `require("express")`) ||
		strings.Contains(source, `require('express')`) ||
		strings.Contains(source, `from "express"`) ||
		strings.Contains(source, `from 'express'`)
}

func detectExpressSemantics(root *tree_sitter.Node, source []byte) (map[string]any, bool) {
	if !javaScriptHasExpressImport(string(source)) {
		return nil, false
	}
	routes := javaScriptExpressRouteCalls(root, source)
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
		// A route registered exactly once has an unambiguous handler. A route
		// registered more than once (e.g. an inline and a named callback, or two
		// routers) is ambiguous about which handler serves it, so it stays
		// unbound rather than attach a handler to the wrong entry (#2721).
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

func detectHapiSemantics(root *tree_sitter.Node, source []byte) (map[string]any, bool) {
	text := string(source)
	if strings.Contains(text, "server.inject(") {
		return nil, false
	}
	if !javaScriptHasHapiRouteSignal(text) {
		return nil, false
	}
	entries := javaScriptHapiRouteEntries(root, source)
	methods := make([]string, 0, len(entries))
	paths := make([]string, 0, len(entries))
	seenMethods := make(map[string]struct{})
	seenPaths := make(map[string]struct{})
	for _, entry := range entries {
		method := entry["method"]
		if _, ok := seenMethods[method]; !ok && method != "" {
			seenMethods[method] = struct{}{}
			methods = append(methods, method)
		}
		path := entry["path"]
		if _, ok := seenPaths[path]; !ok && path != "" {
			seenPaths[path] = struct{}{}
			paths = append(paths, path)
		}
	}
	if len(methods) == 0 || len(paths) == 0 {
		return nil, false
	}
	return map[string]any{
		"route_methods":  methods,
		"route_paths":    paths,
		"route_entries":  entries,
		"server_symbols": []string{},
	}, true
}

// javaScriptHasHapiRouteSignal keeps generic config objects with method/path
// fields from being classified as Hapi routes unless the file shows Hapi usage.
func javaScriptHasHapiRouteSignal(source string) bool {
	return strings.Contains(source, "server.route(") ||
		strings.Contains(source, `require("@hapi/hapi")`) ||
		strings.Contains(source, `require('@hapi/hapi')`) ||
		strings.Contains(source, `require("hapi")`) ||
		strings.Contains(source, `require('hapi')`) ||
		strings.Contains(source, `from "@hapi/hapi"`) ||
		strings.Contains(source, `from '@hapi/hapi'`)
}

// routeEntry is the parser-owned wire shape consumed by query read models. The
// handler symbol is included only when an exact route↔handler binding was
// observed; an empty handler is omitted so consumers never read a fabricated
// binding for an inline or middleware-wrapped route (#2721).
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

// expressRouteKey identifies an Express route by its normalized method and path
// so a route's handler binding can be matched back to its entry and duplicate
// registrations can be counted.
func expressRouteKey(method string, path string) string {
	return strings.ToUpper(strings.TrimSpace(method)) + " " + strings.TrimSpace(path)
}
