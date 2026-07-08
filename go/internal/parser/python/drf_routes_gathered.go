// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package python

import tree_sitter "github.com/tree-sitter/go-tree-sitter"

// --- Gathered-variant helpers for the DRF detector ---

// detectPythonDRFSemanticsGathered produces the same output as
// detectPythonDRFSemantics using pre-gathered nodes.
func detectPythonDRFSemanticsGathered(g pythonGatheredNodes, source []byte) map[string]any {
	entries := pythonDRFManualViewSetEntriesGathered(g, source)
	entries = append(entries, pythonDRFRouterEntriesGathered(g, source)...)
	return pythonRouteSemantics(entries)
}

func pythonDRFManualViewSetEntriesGathered(g pythonGatheredNodes, source []byte) []map[string]string {
	if len(pythonDjangoURLImportNamesGathered(g.imports, source)) == 0 {
		return nil
	}
	entries := make([]map[string]string, 0)
	for _, node := range g.calls {
		if pythonCallSimpleName(node.ChildByFieldName("function"), source) != "path" {
			continue
		}
		if !pythonCallInURLPatterns(node, source) {
			continue
		}
		routePath := pythonDjangoRoutePath(pythonPositionalArgument(node, 0), source)
		if routePath == "" {
			continue
		}
		view := pythonPositionalArgument(node, 1)
		if view == nil || !pythonViewTargetIsDRFActionMap(view, source) {
			continue
		}
		className, ok := pythonAsViewClassName(view, source)
		if !ok {
			continue
		}
		for _, methodAction := range pythonDRFAsViewActions(view, source) {
			entries = append(entries, routeEntry(methodAction.method, routePath, className+"."+methodAction.action))
		}
	}
	return entries
}

func pythonDRFRouterEntriesGathered(g pythonGatheredNodes, source []byte) []map[string]string {
	routerSymbols := pythonDRFRouterSymbolsGathered(g.assignments, source)
	if len(routerSymbols) == 0 {
		return nil
	}
	mountPrefixes := pythonDRFRouterMountPrefixesGathered(g.calls, source)
	rootMounts := pythonDRFRouterRootMountSymbolsGathered(g.assignments, source)
	classActions := pythonDRFActionsByClassGathered(g.functions, source)
	classMethods := pythonMethodsByClassGathered(g.functions, source, pythonDRFStandardActions)
	entries := make([]map[string]string, 0)
	for _, node := range g.calls {
		function := node.ChildByFieldName("function")
		if function == nil || function.Kind() != "attribute" ||
			nodeText(function.ChildByFieldName("attribute"), source) != "register" {
			continue
		}
		routerName := nodeText(function.ChildByFieldName("object"), source)
		if _, ok := routerSymbols[routerName]; !ok {
			continue
		}
		prefix, ok := pythonDRFRouterPrefix(pythonPositionalArgument(node, 0), source)
		className := pythonViewTargetName(pythonPositionalArgument(node, 1), source)
		if !ok || className == "" {
			continue
		}
		prefixes := make([]string, 0)
		if mounts := mountPrefixes[routerName]; len(mounts) > 0 {
			for _, mount := range mounts {
				prefixes = append(prefixes, pythonJoinRoutePath(mount, prefix))
			}
		} else if _, ok := rootMounts[routerName]; ok {
			prefixes = append(prefixes, prefix)
		} else {
			continue
		}
		for _, mountedPrefix := range prefixes {
			entries = append(entries, pythonDRFStandardRouterEntries(mountedPrefix, className, classMethods[className])...)
			entries = append(entries, pythonDRFExtraActionEntries(mountedPrefix, className, classActions[className])...)
		}
	}
	return entries
}

func pythonDRFRouterSymbolsGathered(gathered []*tree_sitter.Node, source []byte) map[string]struct{} {
	symbols := make(map[string]struct{})
	pythonWalkServerAssignmentsGathered(gathered, source, func(symbol string, _ *tree_sitter.Node, callee string) {
		switch callee {
		case "DefaultRouter", "SimpleRouter":
			symbols[symbol] = struct{}{}
		}
	})
	return symbols
}

func pythonDRFRouterMountPrefixesGathered(gathered []*tree_sitter.Node, source []byte) map[string][]string {
	prefixes := make(map[string][]string)
	for _, node := range gathered {
		if pythonCallSimpleName(node.ChildByFieldName("function"), source) != "path" {
			continue
		}
		if !pythonCallInURLPatterns(node, source) {
			continue
		}
		routePath := pythonDjangoRoutePath(pythonPositionalArgument(node, 0), source)
		if routePath == "" {
			continue
		}
		routerName := pythonDRFRouterIncludeSymbol(pythonPositionalArgument(node, 1), source)
		if routerName == "" {
			continue
		}
		prefixes[routerName] = appendUniqueString(prefixes[routerName], routePath)
	}
	return prefixes
}

func pythonDRFRouterRootMountSymbolsGathered(gathered []*tree_sitter.Node, source []byte) map[string]struct{} {
	symbols := make(map[string]struct{})
	for _, node := range gathered {
		left := node.ChildByFieldName("left")
		if nodeText(left, source) != "urlpatterns" {
			continue
		}
		right := node.ChildByFieldName("right")
		routerName := pythonDRFRouterURLAttributeSymbol(right, source)
		if routerName != "" {
			symbols[routerName] = struct{}{}
		}
	}
	return symbols
}

func pythonDRFActionsByClassGathered(gathered []*tree_sitter.Node, source []byte) map[string][]pythonDRFAction {
	byClass := make(map[string][]pythonDRFAction)
	for _, node := range gathered {
		className := pythonEnclosingClassName(node, source)
		methodName := nodeText(node.ChildByFieldName("name"), source)
		if className == "" || methodName == "" {
			continue
		}
		for _, decorator := range pythonActionDecorators(node, source) {
			action, ok := pythonDRFActionFromDecorator(methodName, decorator, source)
			if !ok {
				continue
			}
			byClass[className] = append(byClass[className], action)
		}
	}
	return byClass
}
