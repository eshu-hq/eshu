// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package python

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

var pythonDRFStandardActions = map[string]struct{}{
	"retrieve":       {},
	"update":         {},
	"partial_update": {},
	"create":         {},
	"list":           {},
	"destroy":        {},
}

type pythonDRFAction struct {
	name    string
	methods []string
	detail  bool
	path    string
}

func detectPythonDRFSemantics(root *tree_sitter.Node, source []byte) map[string]any {
	entries := pythonDRFManualViewSetEntries(root, source)
	entries = append(entries, pythonDRFRouterEntries(root, source)...)
	return pythonRouteSemantics(entries)
}

func pythonDRFManualViewSetEntries(root *tree_sitter.Node, source []byte) []map[string]string {
	if !pythonHasDjangoPathImport(root, source) {
		return nil
	}
	entries := make([]map[string]string, 0)
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "call" || pythonCallSimpleName(node.ChildByFieldName("function"), source) != "path" {
			return
		}
		if !pythonCallInURLPatterns(node, source) {
			return
		}
		routePath := pythonDjangoRoutePath(pythonPositionalArgument(node, 0), source)
		if routePath == "" {
			return
		}
		view := pythonPositionalArgument(node, 1)
		if view == nil || !pythonViewTargetIsDRFActionMap(view, source) {
			return
		}
		className, ok := pythonAsViewClassName(view, source)
		if !ok {
			return
		}
		for _, methodAction := range pythonDRFAsViewActions(view, source) {
			entries = append(entries, routeEntry(methodAction.method, routePath, className+"."+methodAction.action))
		}
	})
	return entries
}

func pythonDRFRouterEntries(root *tree_sitter.Node, source []byte) []map[string]string {
	routerSymbols := pythonDRFRouterSymbols(root, source)
	if len(routerSymbols) == 0 {
		return nil
	}
	mountPrefixes := pythonDRFRouterMountPrefixes(root, source)
	rootMounts := pythonDRFRouterRootMountSymbols(root, source)
	classActions := pythonDRFActionsByClass(root, source)
	classMethods := pythonMethodsByClass(root, source, pythonDRFStandardActions)
	entries := make([]map[string]string, 0)
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "call" {
			return
		}
		function := node.ChildByFieldName("function")
		if function == nil || function.Kind() != "attribute" ||
			strings.TrimSpace(nodeText(function.ChildByFieldName("attribute"), source)) != "register" {
			return
		}
		routerName := strings.TrimSpace(nodeText(function.ChildByFieldName("object"), source))
		if _, ok := routerSymbols[routerName]; !ok {
			return
		}
		prefix, ok := pythonDRFRouterPrefix(pythonPositionalArgument(node, 0), source)
		className := pythonViewTargetName(pythonPositionalArgument(node, 1), source)
		if !ok || className == "" {
			return
		}
		prefixes := make([]string, 0)
		if mounts := mountPrefixes[routerName]; len(mounts) > 0 {
			for _, mount := range mounts {
				prefixes = append(prefixes, pythonJoinRoutePath(mount, prefix))
			}
		} else if _, ok := rootMounts[routerName]; ok {
			prefixes = append(prefixes, prefix)
		} else {
			return
		}
		for _, mountedPrefix := range prefixes {
			entries = append(entries, pythonDRFStandardRouterEntries(mountedPrefix, className, classMethods[className])...)
			entries = append(entries, pythonDRFExtraActionEntries(mountedPrefix, className, classActions[className])...)
		}
	})
	return entries
}

func pythonDRFStandardRouterEntries(prefix string, className string, methods []string) []map[string]string {
	collectionPath := pythonEnsureTrailingRouteSlash(prefix)
	detailPath := pythonJoinRoutePath(collectionPath, "{lookup}")
	actions := []struct {
		method string
		name   string
		path   string
	}{
		{"GET", "list", collectionPath},
		{"POST", "create", collectionPath},
		{"GET", "retrieve", detailPath},
		{"PUT", "update", detailPath},
		{"PATCH", "partial_update", detailPath},
		{"DELETE", "destroy", detailPath},
	}
	available := make(map[string]struct{}, len(methods))
	for _, method := range methods {
		available[strings.ToLower(method)] = struct{}{}
	}
	entries := make([]map[string]string, 0, len(actions))
	for _, action := range actions {
		if _, ok := available[action.name]; !ok {
			continue
		}
		entries = append(entries, routeEntry(action.method, action.path, className+"."+action.name))
	}
	return entries
}

func pythonDRFExtraActionEntries(prefix string, className string, actions []pythonDRFAction) []map[string]string {
	entries := make([]map[string]string, 0)
	collectionPath := pythonEnsureTrailingRouteSlash(prefix)
	detailPath := pythonJoinRoutePath(collectionPath, "{lookup}")
	for _, action := range actions {
		basePath := collectionPath
		if action.detail {
			basePath = detailPath
		}
		actionPath := pythonJoinRoutePath(basePath, action.path)
		for _, method := range action.methods {
			entries = append(entries, routeEntry(method, actionPath, className+"."+action.name))
		}
	}
	return entries
}

func pythonDRFRouterMountPrefixes(root *tree_sitter.Node, source []byte) map[string][]string {
	prefixes := make(map[string][]string)
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "call" || pythonCallSimpleName(node.ChildByFieldName("function"), source) != "path" {
			return
		}
		if !pythonCallInURLPatterns(node, source) {
			return
		}
		routePath := pythonDjangoRoutePath(pythonPositionalArgument(node, 0), source)
		if routePath == "" {
			return
		}
		routerName := pythonDRFRouterIncludeSymbol(pythonPositionalArgument(node, 1), source)
		if routerName == "" {
			return
		}
		prefixes[routerName] = appendUniqueString(prefixes[routerName], routePath)
	})
	return prefixes
}

func pythonDRFRouterRootMountSymbols(root *tree_sitter.Node, source []byte) map[string]struct{} {
	symbols := make(map[string]struct{})
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "assignment" {
			return
		}
		left := node.ChildByFieldName("left")
		if strings.TrimSpace(nodeText(left, source)) != "urlpatterns" {
			return
		}
		right := node.ChildByFieldName("right")
		routerName := pythonDRFRouterURLAttributeSymbol(right, source)
		if routerName != "" {
			symbols[routerName] = struct{}{}
		}
	})
	return symbols
}

func pythonDRFRouterIncludeSymbol(view *tree_sitter.Node, source []byte) string {
	if view == nil || view.Kind() != "call" || pythonCallSimpleName(view.ChildByFieldName("function"), source) != "include" {
		return ""
	}
	return pythonDRFRouterURLAttributeSymbol(pythonPositionalArgument(view, 0), source)
}

func pythonDRFRouterURLAttributeSymbol(argument *tree_sitter.Node, source []byte) string {
	if argument == nil || argument.Kind() != "attribute" ||
		strings.TrimSpace(nodeText(argument.ChildByFieldName("attribute"), source)) != "urls" {
		return ""
	}
	object := argument.ChildByFieldName("object")
	if object == nil || object.Kind() != "identifier" {
		return ""
	}
	return strings.TrimSpace(nodeText(object, source))
}

func pythonDRFActionsByClass(root *tree_sitter.Node, source []byte) map[string][]pythonDRFAction {
	byClass := make(map[string][]pythonDRFAction)
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "function_definition" {
			return
		}
		className := pythonEnclosingClassName(node, source)
		methodName := strings.TrimSpace(nodeText(node.ChildByFieldName("name"), source))
		if className == "" || methodName == "" {
			return
		}
		for _, decorator := range pythonActionDecorators(node, source) {
			action, ok := pythonDRFActionFromDecorator(methodName, decorator, source)
			if !ok {
				continue
			}
			byClass[className] = append(byClass[className], action)
		}
	})
	return byClass
}

func pythonDRFActionFromDecorator(
	methodName string,
	decorator *tree_sitter.Node,
	source []byte,
) (pythonDRFAction, bool) {
	detail, ok := pythonDRFActionDetail(decorator, source)
	if !ok {
		return pythonDRFAction{}, false
	}
	methods, ok := pythonDRFActionMethods(decorator, source)
	if !ok {
		return pythonDRFAction{}, false
	}
	path, ok := pythonDRFActionPath(methodName, decorator, source)
	if !ok {
		return pythonDRFAction{}, false
	}
	return pythonDRFAction{
		name:    methodName,
		methods: methods,
		detail:  detail,
		path:    path,
	}, true
}

func pythonActionDecorators(functionDefinition *tree_sitter.Node, source []byte) []*tree_sitter.Node {
	parent := functionDefinition.Parent()
	if parent == nil || parent.Kind() != "decorated_definition" {
		return nil
	}
	decorators := make([]*tree_sitter.Node, 0)
	cursor := parent.Walk()
	defer cursor.Close()
	for _, child := range parent.NamedChildren(cursor) {
		child := child
		if child.Kind() != "decorator" {
			continue
		}
		call := pythonDecoratorCall(&child)
		if call == nil || pythonCallSimpleName(call.ChildByFieldName("function"), source) != "action" {
			continue
		}
		decorators = append(decorators, call)
	}
	return decorators
}

func pythonDRFRouterSymbols(root *tree_sitter.Node, source []byte) map[string]struct{} {
	symbols := make(map[string]struct{})
	pythonWalkServerAssignments(root, source, func(symbol string, _ *tree_sitter.Node, callee string) {
		switch callee {
		case "DefaultRouter", "SimpleRouter":
			symbols[symbol] = struct{}{}
		}
	})
	return symbols
}

type pythonDRFMethodAction struct {
	method string
	action string
}

func pythonDRFAsViewActions(call *tree_sitter.Node, source []byte) []pythonDRFMethodAction {
	dict := pythonPositionalArgument(call, 0)
	if dict == nil || dict.Kind() != "dictionary" {
		return nil
	}
	actions := make([]pythonDRFMethodAction, 0)
	cursor := dict.Walk()
	defer cursor.Close()
	for _, child := range dict.NamedChildren(cursor) {
		child := child
		if child.Kind() != "pair" {
			return nil
		}
		key := child.ChildByFieldName("key")
		value := child.ChildByFieldName("value")
		if key == nil || value == nil || key.Kind() != "string" || value.Kind() != "string" {
			return nil
		}
		method := strings.ToLower(pythonStringLiteralValue(key, source))
		action := pythonStringLiteralValue(value, source)
		if method == "" || action == "" {
			continue
		}
		if _, ok := pythonHTTPRouteMethods[method]; !ok {
			return nil
		}
		actions = append(actions, pythonDRFMethodAction{method: strings.ToUpper(method), action: action})
	}
	return actions
}

func pythonDRFActionMethods(call *tree_sitter.Node, source []byte) ([]string, bool) {
	value := pythonKeywordArgumentValue(call, "methods", source)
	if value == nil {
		return []string{"GET"}, true
	}
	if value.Kind() != "list" {
		return nil, false
	}
	methods := make([]string, 0)
	cursor := value.Walk()
	defer cursor.Close()
	for _, child := range value.NamedChildren(cursor) {
		child := child
		if child.Kind() != "string" {
			return nil, false
		}
		method := strings.ToLower(pythonStringLiteralValue(&child, source))
		if method == "" {
			return nil, false
		}
		if _, ok := pythonHTTPRouteMethods[method]; !ok {
			return nil, false
		}
		methods = appendUniqueString(methods, strings.ToUpper(method))
	}
	if len(methods) == 0 {
		return nil, false
	}
	return methods, true
}

func pythonDRFActionDetail(call *tree_sitter.Node, source []byte) (bool, bool) {
	value := pythonKeywordArgumentValue(call, "detail", source)
	if value == nil {
		return false, false
	}
	text := strings.TrimSpace(nodeText(value, source))
	if strings.EqualFold(text, "true") {
		return true, true
	}
	if strings.EqualFold(text, "false") {
		return false, true
	}
	return false, false
}

func pythonDRFActionPath(methodName string, call *tree_sitter.Node, source []byte) (string, bool) {
	value := pythonKeywordArgumentValue(call, "url_path", source)
	if value == nil {
		return strings.ReplaceAll(methodName, "_", "-"), true
	}
	if value.Kind() != "string" {
		return "", false
	}
	path := pythonStringLiteralValue(value, source)
	if path == "" {
		return "", false
	}
	return path, true
}
