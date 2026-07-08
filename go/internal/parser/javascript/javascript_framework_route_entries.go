// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript

import (
	"sort"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func detectKoaSemantics(root *tree_sitter.Node, source []byte) (map[string]any, bool) {
	bases := javaScriptKoaRegistrationBases(root, source, string(source))
	if len(bases) == 0 {
		return nil, false
	}
	entries := javaScriptKoaRouteEntries(root, source, bases)
	if len(entries) == 0 {
		return nil, false
	}
	return javaScriptFrameworkRouteSemantics(entries, javaScriptSortedNameSet(bases)), true
}

func detectFastifySemantics(root *tree_sitter.Node, source []byte, fastifyBases map[string]struct{}) (map[string]any, bool) {
	if len(fastifyBases) == 0 {
		return nil, false
	}
	entries := javaScriptFastifyRouteEntries(root, source, fastifyBases)
	if len(entries) == 0 {
		return nil, false
	}
	return javaScriptFrameworkRouteSemantics(entries, javaScriptSortedNameSet(fastifyBases)), true
}

func detectNestJSSemantics(root *tree_sitter.Node, source []byte, parents *javaScriptParentLookup) (map[string]any, bool) {
	if !javaScriptHasNestJSCommonImport(string(source)) {
		return nil, false
	}
	entries := javaScriptNestJSRouteEntries(root, source, parents)
	if len(entries) == 0 {
		return nil, false
	}
	return javaScriptFrameworkRouteSemantics(entries, nil), true
}

func javaScriptKoaRouteEntries(
	root *tree_sitter.Node,
	source []byte,
	bases map[string]struct{},
) []map[string]string {
	entries := make([]map[string]string, 0)
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "call_expression" {
			return
		}
		base, property, ok := javaScriptMemberBaseAndProperty(node.ChildByFieldName("function"), source)
		if !ok || !javaScriptNameSetContains(bases, base) {
			return
		}
		method := strings.ToLower(property)
		if _, ok := javaScriptKoaRouteMethods[method]; !ok || method == "use" {
			return
		}
		args := javaScriptCallArguments(node)
		path, handler, ok := javaScriptKoaRoutePathAndHandler(args, source)
		if !ok {
			return
		}
		entries = append(entries, routeEntry(method, path, handler))
	})
	return entries
}

func javaScriptKoaRoutePathAndHandler(args []tree_sitter.Node, source []byte) (string, string, bool) {
	path, ok := javaScriptRoutePathArg(args, 0, source)
	if !ok {
		return "", "", false
	}
	handlerIndex := 1
	if !strings.HasPrefix(path, "/") {
		namedPath, namedPathOK := javaScriptRoutePathArg(args, 1, source)
		if !namedPathOK || !strings.HasPrefix(namedPath, "/") {
			return "", "", false
		}
		path = namedPath
		handlerIndex = 2
	}
	handler := ""
	if len(args) == handlerIndex+1 {
		handler = javaScriptIdentifierName(&args[handlerIndex], source)
	}
	return path, handler, true
}

func javaScriptFastifyRouteEntries(
	root *tree_sitter.Node,
	source []byte,
	bases map[string]struct{},
) []map[string]string {
	entries := make([]map[string]string, 0)
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "call_expression" {
			return
		}
		base, property, ok := javaScriptMemberBaseAndProperty(node.ChildByFieldName("function"), source)
		if !ok || !javaScriptNameSetContains(bases, base) {
			return
		}
		method := strings.ToLower(property)
		if _, ok := javaScriptFastifyRouteMethods[method]; !ok {
			return
		}
		args := javaScriptCallArguments(node)
		if method == "route" {
			entries = append(entries, javaScriptFastifyRouteObjectEntries(args, source)...)
			return
		}
		path, ok := javaScriptRoutePathArg(args, 0, source)
		if !ok || !strings.HasPrefix(path, "/") {
			return
		}
		handler := ""
		switch len(args) {
		case 2:
			handler = javaScriptIdentifierName(&args[1], source)
		case 3:
			handler = javaScriptIdentifierName(&args[2], source)
		}
		entries = append(entries, routeEntry(method, path, handler))
	})
	return entries
}

func javaScriptFastifyRouteObjectEntries(args []tree_sitter.Node, source []byte) []map[string]string {
	entries := make([]map[string]string, 0, 1)
	for i := range args {
		if args[i].Kind() != "object" {
			continue
		}
		method, path, handler, ok := javaScriptFastifyRouteObjectEntry(&args[i], source)
		if !ok {
			continue
		}
		entries = append(entries, routeEntry(method, path, handler))
	}
	return entries
}

func javaScriptFastifyRouteObjectEntry(
	object *tree_sitter.Node,
	source []byte,
) (string, string, string, bool) {
	method := ""
	path := ""
	handler := ""
	cursor := object.Walk()
	defer cursor.Close()
	for _, child := range object.NamedChildren(cursor) {
		child := child
		if child.Kind() != "pair" {
			continue
		}
		key := javaScriptHapiPairKey(&child, source)
		valueNode := child.ChildByFieldName("value")
		switch key {
		case "method":
			if valueNode != nil && valueNode.Kind() == "string" {
				method = jsStringLiteralValue(valueNode, source)
			}
		case "url", "path":
			if valueNode != nil && valueNode.Kind() == "string" {
				path = jsStringLiteralValue(valueNode, source)
			}
		case "handler":
			handler = javaScriptIdentifierName(valueNode, source)
		}
	}
	method = strings.ToLower(strings.TrimSpace(method))
	if _, ok := javaScriptFastifyRouteMethods[method]; !ok || method == "route" {
		return "", "", "", false
	}
	return method, path, handler, strings.HasPrefix(path, "/")
}

func javaScriptNestJSRouteEntries(
	root *tree_sitter.Node,
	source []byte,
	parents *javaScriptParentLookup,
) []map[string]string {
	entries := make([]map[string]string, 0)
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "method_definition" {
			return
		}
		classNode := javaScriptEnclosingClassNode(node, parents)
		prefix, ok := javaScriptNestJSControllerPrefix(classNode, source, parents)
		if !ok {
			return
		}
		method, routePath, ok := javaScriptNestJSMethodRoute(node, source, parents)
		if !ok {
			return
		}
		handler := javaScriptIdentifierName(node.ChildByFieldName("name"), source)
		if handler == "" {
			return
		}
		entries = append(entries, routeEntry(method, joinNestJSPaths(prefix, routePath), handler))
	})
	return entries
}

func javaScriptNestJSControllerPrefix(
	classNode *tree_sitter.Node,
	source []byte,
	parents *javaScriptParentLookup,
) (string, bool) {
	if classNode == nil {
		return "", false
	}
	for _, decorator := range javaScriptNestJSRouteDecorators(classNode, source, parents) {
		name, value, ok := javaScriptDecoratorNameAndStringArg(decorator)
		if !ok || strings.ToLower(name) != "controller" {
			continue
		}
		return value, true
	}
	return "", false
}

func javaScriptNestJSMethodRoute(
	methodNode *tree_sitter.Node,
	source []byte,
	parents *javaScriptParentLookup,
) (string, string, bool) {
	for _, decorator := range javaScriptNestJSRouteDecorators(methodNode, source, parents) {
		name, value, ok := javaScriptDecoratorNameAndStringArg(decorator)
		if !ok {
			continue
		}
		lower := strings.ToLower(name)
		if _, ok := javaScriptNestJSRouteDecoratorNames()[lower]; !ok {
			continue
		}
		if lower == "all" {
			return "ANY", value, true
		}
		return lower, value, true
	}
	return "", "", false
}

func javaScriptDecoratorNameAndStringArg(decorator string) (string, string, bool) {
	decorator = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(decorator), "@"))
	if decorator == "" {
		return "", "", false
	}
	open := strings.Index(decorator, "(")
	if open < 0 {
		return strings.TrimSpace(decorator), "", true
	}
	name := strings.TrimSpace(decorator[:open])
	close := strings.LastIndex(decorator, ")")
	if close < open {
		return "", "", false
	}
	arg := strings.TrimSpace(decorator[open+1 : close])
	if arg == "" {
		return name, "", true
	}
	if value, ok := trimJavaScriptQuotes(arg); ok {
		return name, value, true
	}
	return "", "", false
}

func javaScriptRoutePathArg(args []tree_sitter.Node, index int, source []byte) (string, bool) {
	if len(args) <= index || args[index].Kind() != "string" {
		return "", false
	}
	path := strings.TrimSpace(jsStringLiteralValue(&args[index], source))
	return path, path != ""
}

func javaScriptFrameworkRouteSemantics(entries []map[string]string, serverSymbols []string) map[string]any {
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
	return map[string]any{
		"route_methods":  methods,
		"route_paths":    paths,
		"route_entries":  entries,
		"server_symbols": serverSymbols,
	}
}

func javaScriptSortedNameSet(names map[string]struct{}) []string {
	out := make([]string, 0, len(names))
	for name := range names {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func joinNestJSPaths(prefix string, routePath string) string {
	segments := []string{}
	for _, part := range []string{prefix, routePath} {
		part = strings.Trim(strings.TrimSpace(part), "/")
		if part != "" {
			segments = append(segments, part)
		}
	}
	if len(segments) == 0 {
		return "/"
	}
	return "/" + strings.Join(segments, "/")
}
