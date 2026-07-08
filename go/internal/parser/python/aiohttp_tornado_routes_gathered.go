// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package python

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// --- Gathered-variant framework detectors (aiohttp, Tornado) ---

// detectPythonAioHTTPSemanticsGathered produces the same output as
// detectPythonAioHTTPSemantics using pre-gathered nodes instead of
// separate full-tree walks.
func detectPythonAioHTTPSemanticsGathered(g pythonGatheredNodes, source []byte) map[string]any {
	webSymbols := pythonAioHTTPWebSymbolsGathered(g.imports, source)
	routeTableSymbols, appSymbols := pythonAioHTTPSymbolsGathered(g.assignments, source, webSymbols)
	paramAppSymbols := pythonAioHTTPParamAppSymbolsGathered(g.functions, source)
	if len(webSymbols) == 0 && len(paramAppSymbols) == 0 {
		return nil
	}
	if len(routeTableSymbols) == 0 && len(appSymbols) == 0 && len(paramAppSymbols) == 0 {
		return nil
	}
	entries := pythonAioHTTPRouteTableEntriesGathered(g.decorators, source, routeTableSymbols)
	entries = append(entries, pythonAioHTTPApplicationRouteEntriesGathered(g.calls, source, appSymbols, webSymbols, pythonModuleFunctionNamesGathered(g.functions, source), paramAppSymbols)...)
	return pythonRouteSemantics(entries)
}

// detectPythonTornadoSemanticsGathered produces the same output as
// detectPythonTornadoSemantics using pre-gathered nodes instead of
// separate full-tree walks.
func detectPythonTornadoSemanticsGathered(g pythonGatheredNodes, source []byte) map[string]any {
	imports := pythonTornadoImportSymbolsGathered(g.imports, source)
	if len(imports.moduleObjects) == 0 && len(imports.applicationConstructors) == 0 {
		return nil
	}
	classMethods := pythonHTTPMethodsByClassGathered(g.functions, source)
	entries := pythonTornadoApplicationEntriesGathered(g.calls, source, classMethods, imports)
	return pythonRouteSemantics(entries)
}

func pythonAioHTTPWebSymbolsGathered(gathered []*tree_sitter.Node, source []byte) map[string]struct{} {
	symbols := make(map[string]struct{})
	pythonWalkImportStatementsGathered(gathered, source, func(statement string) {
		switch {
		case strings.HasPrefix(statement, "from aiohttp import "):
			importClause := strings.TrimSpace(strings.TrimPrefix(statement, "from aiohttp import "))
			for _, clause := range pythonSplitImportClauses(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(importClause, "("), ")"))) {
				name, alias := pythonSplitImportAlias(clause)
				if name != "web" {
					continue
				}
				if alias == "" {
					alias = name
				}
				symbols[alias] = struct{}{}
			}
		case strings.HasPrefix(statement, "import "):
			for _, clause := range pythonSplitImportClauses(strings.TrimSpace(strings.TrimPrefix(statement, "import "))) {
				modulePath, alias := pythonSplitImportAlias(clause)
				if modulePath != "aiohttp.web" {
					continue
				}
				if alias == "" {
					symbols[modulePath] = struct{}{}
					continue
				}
				symbols[alias] = struct{}{}
			}
		}
	})
	return symbols
}

func pythonAioHTTPSymbolsGathered(
	gathered []*tree_sitter.Node,
	source []byte,
	webSymbols map[string]struct{},
) (map[string]struct{}, map[string]struct{}) {
	routeTableSymbols := make(map[string]struct{})
	appSymbols := make(map[string]struct{})
	pythonWalkServerAssignmentsGathered(gathered, source, func(symbol string, call *tree_sitter.Node, callee string) {
		if pythonInsidePythonDefinition(call) {
			return
		}
		if !pythonCallTargetsAnyObjectAttribute(call.ChildByFieldName("function"), source, webSymbols, callee) {
			return
		}
		switch callee {
		case "RouteTableDef":
			routeTableSymbols[symbol] = struct{}{}
		case "Application":
			appSymbols[symbol] = struct{}{}
		}
	})
	return routeTableSymbols, appSymbols
}

func pythonAioHTTPRouteTableEntriesGathered(
	gathered []*tree_sitter.Node,
	source []byte,
	routeTableSymbols map[string]struct{},
) []map[string]string {
	if len(routeTableSymbols) == 0 {
		return nil
	}
	entries := make([]map[string]string, 0)
	pythonWalkRouteDecoratorsGathered(gathered, source, func(decorator *tree_sitter.Node, symbol string, attribute string, call *tree_sitter.Node) {
		if _, ok := routeTableSymbols[symbol]; !ok {
			return
		}
		method, path := pythonAioHTTPDecoratorMethodPath(attribute, call, source)
		handler := pythonDecoratorHandlerName(decorator, source)
		if method == "" || path == "" || handler == "" {
			return
		}
		entries = append(entries, routeEntry(method, path, handler))
	})
	return entries
}

func pythonAioHTTPApplicationRouteEntriesGathered(
	gathered []*tree_sitter.Node,
	source []byte,
	appSymbols map[string]struct{},
	webSymbols map[string]struct{},
	functionNames map[string]struct{},
	paramAppSymbols map[uintptr]map[string]struct{},
) []map[string]string {
	if len(appSymbols) == 0 && len(paramAppSymbols) == 0 {
		return nil
	}
	entries := make([]map[string]string, 0)
	for _, node := range gathered {
		funcDef := pythonEnclosingFunctionDef(node)
		if funcDef != nil {
			paramApps, ok := paramAppSymbols[funcDef.Id()]
			if !ok {
				continue
			}
			effectiveSymbols := make(map[string]struct{}, len(appSymbols)+len(paramApps))
			for k := range appSymbols {
				effectiveSymbols[k] = struct{}{}
			}
			for k := range paramApps {
				effectiveSymbols[k] = struct{}{}
			}
			method, path, handler, ok := pythonAioHTTPParamApplicationRouteEntry(node, source, effectiveSymbols)
			if ok {
				entries = append(entries, routeEntry(method, path, handler))
				continue
			}
			entries = append(entries, pythonAioHTTPAddRoutesEntries(node, source, effectiveSymbols, webSymbols, functionNames)...)
			continue
		}
		if pythonInsidePythonDefinition(node) {
			continue
		}
		method, path, handler, ok := pythonAioHTTPApplicationRouteEntry(node, source, appSymbols, functionNames)
		if ok {
			entries = append(entries, routeEntry(method, path, handler))
			continue
		}
		entries = append(entries, pythonAioHTTPAddRoutesEntries(node, source, appSymbols, webSymbols, functionNames)...)
	}
	return entries
}

func pythonTornadoImportSymbolsGathered(gathered []*tree_sitter.Node, source []byte) pythonTornadoImports {
	imports := pythonTornadoImports{
		moduleObjects:           make(map[string]struct{}),
		applicationConstructors: make(map[string]struct{}),
		urlSpecConstructors:     make(map[string]struct{}),
	}
	pythonWalkImportStatementsGathered(gathered, source, func(statement string) {
		switch {
		case strings.HasPrefix(statement, "import "):
			for _, clause := range pythonSplitImportClauses(strings.TrimSpace(strings.TrimPrefix(statement, "import "))) {
				modulePath, alias := pythonSplitImportAlias(clause)
				if modulePath != "tornado.web" {
					continue
				}
				if alias == "" {
					imports.moduleObjects[modulePath] = struct{}{}
					continue
				}
				imports.moduleObjects[alias] = struct{}{}
			}
		case strings.HasPrefix(statement, "from tornado.web import "):
			importClause := strings.TrimSpace(strings.TrimPrefix(statement, "from tornado.web import "))
			for _, clause := range pythonSplitImportClauses(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(importClause, "("), ")"))) {
				name, alias := pythonSplitImportAlias(clause)
				if alias == "" {
					alias = name
				}
				switch name {
				case "Application":
					imports.applicationConstructors[alias] = struct{}{}
				case "URLSpec", "url":
					imports.urlSpecConstructors[alias] = struct{}{}
				}
			}
		}
	})
	return imports
}

func pythonTornadoApplicationEntriesGathered(
	gathered []*tree_sitter.Node,
	source []byte,
	classMethods map[string][]string,
	imports pythonTornadoImports,
) []map[string]string {
	entries := make([]map[string]string, 0)
	for _, node := range gathered {
		if !pythonIsTornadoApplicationCall(node, source, imports) {
			continue
		}
		if pythonInsidePythonDefinition(node) {
			continue
		}
		routeList := pythonPositionalArgument(node, 0)
		if routeList == nil || routeList.Kind() != "list" {
			continue
		}
		cursor := routeList.Walk()
		defer cursor.Close()
		for _, child := range routeList.NamedChildren(cursor) {
			child := child
			path, handlerClass := pythonTornadoURLSpec(&child, source, imports)
			if path == "" || handlerClass == "" || len(classMethods[handlerClass]) == 0 {
				continue
			}
			for _, method := range classMethods[handlerClass] {
				entries = append(entries, routeEntry(method, path, handlerClass+"."+strings.ToLower(method)))
			}
		}
	}
	return entries
}

// pythonAioHTTPParamAppSymbolsGathered mirrors pythonAioHTTPParamAppSymbols
// but uses pre-gathered function_definition nodes instead of a full-tree walk.
func pythonAioHTTPParamAppSymbolsGathered(functions []*tree_sitter.Node, source []byte) map[uintptr]map[string]struct{} {
	result := make(map[uintptr]map[string]struct{})

	for _, funcNode := range functions {
		params := pythonParameterNames(funcNode.ChildByFieldName("parameters"), source)
		if len(params) == 0 {
			continue
		}
		paramSet := make(map[string]struct{}, len(params))
		for _, p := range params {
			paramSet[p] = struct{}{}
		}
		body := funcNode.ChildByFieldName("body")
		if body == nil {
			continue
		}

		matched := make(map[string]struct{})
		walkNamed(body, func(child *tree_sitter.Node) {
			if child.Kind() != "call" {
				return
			}
			function := child.ChildByFieldName("function")
			if function == nil {
				return
			}
			if function.Kind() != "attribute" {
				return
			}
			attr := strings.TrimSpace(nodeText(function.ChildByFieldName("attribute"), source))

			// param.add_routes(...)
			if attr == "add_routes" {
				receiver := function.ChildByFieldName("object")
				if receiver != nil && receiver.Kind() == "identifier" {
					name := strings.TrimSpace(nodeText(receiver, source))
					if _, ok := paramSet[name]; ok {
						matched[name] = struct{}{}
					}
				}
				return
			}

			// param.router.add_<verb>(...) or param.router.add_route(...)
			_, isHTTPMethod := pythonAioHTTPRouteMethods[attr]
			if !isHTTPMethod && attr != "add_route" {
				return
			}
			object := function.ChildByFieldName("object")
			if object == nil || object.Kind() != "attribute" ||
				strings.TrimSpace(nodeText(object.ChildByFieldName("attribute"), source)) != "router" {
				return
			}
			receiver := object.ChildByFieldName("object")
			if receiver == nil || receiver.Kind() != "identifier" {
				return
			}
			name := strings.TrimSpace(nodeText(receiver, source))
			if _, ok := paramSet[name]; ok {
				matched[name] = struct{}{}
			}
		})

		if len(matched) > 0 {
			result[funcNode.Id()] = matched
		}
	}

	return result
}
