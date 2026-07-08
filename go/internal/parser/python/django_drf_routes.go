// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package python

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

var pythonHTTPRouteMethods = map[string]struct{}{
	"get":     {},
	"post":    {},
	"put":     {},
	"patch":   {},
	"delete":  {},
	"options": {},
	"head":    {},
	"trace":   {},
}

func detectPythonDjangoSemantics(root *tree_sitter.Node, source []byte) map[string]any {
	classMethods := pythonHTTPMethodsByClass(root, source)
	entries := pythonDjangoURLPatternEntries(root, source, classMethods)
	return pythonRouteSemantics(entries)
}

func pythonRouteSemantics(entries []map[string]string) map[string]any {
	if len(entries) == 0 {
		return nil
	}
	methods := make([]string, 0, len(entries))
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		methods = appendUniqueString(methods, entry["method"])
		paths = appendUniqueString(paths, entry["path"])
	}
	return map[string]any{
		"route_methods": methods,
		"route_paths":   paths,
		"route_entries": entries,
	}
}

func pythonDjangoURLPatternEntries(
	root *tree_sitter.Node,
	source []byte,
	classMethods map[string][]string,
) []map[string]string {
	if !pythonHasDjangoURLImport(root, source) {
		return nil
	}
	functionNames := pythonModuleFunctionNames(root, source)
	entries := make([]map[string]string, 0)
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "call" {
			return
		}
		callName := pythonCallSimpleName(node.ChildByFieldName("function"), source)
		if callName != "path" && callName != "url" && callName != "re_path" {
			return
		}
		if !pythonCallInURLPatterns(node, source) {
			return
		}
		routePath := pythonDjangoURLPatternPath(node, callName, source)
		if routePath == "" {
			return
		}
		view := pythonPositionalArgument(node, 1)
		if view == nil || pythonViewTargetIsInclude(view, source) || pythonViewTargetIsDRFActionMap(view, source) {
			return
		}
		entries = append(entries, pythonDjangoViewEntries(routePath, view, source, classMethods, functionNames)...)
	})
	return entries
}

func pythonDjangoViewEntries(
	routePath string,
	view *tree_sitter.Node,
	source []byte,
	classMethods map[string][]string,
	functionNames map[string]struct{},
) []map[string]string {
	switch view.Kind() {
	case "identifier":
		handler := pythonViewTargetName(view, source)
		if handler == "" {
			return nil
		}
		if _, ok := functionNames[handler]; !ok {
			return []map[string]string{routeEntry("ANY", routePath, "")}
		}
		return []map[string]string{routeEntry("ANY", routePath, handler)}
	case "attribute":
		return []map[string]string{routeEntry("ANY", routePath, "")}
	case "call":
		if pythonImportedAsViewCall(view, source) {
			if pythonPositionalArgument(view, 0) != nil {
				return nil
			}
			return []map[string]string{routeEntry("ANY", routePath, "")}
		}
		className, ok := pythonAsViewClassName(view, source)
		if !ok {
			return nil
		}
		if pythonPositionalArgument(view, 0) != nil {
			return nil
		}
		methods := classMethods[className]
		if len(methods) == 0 {
			// Class is not defined in this module (imported from
			// another file). Emit an ANY-method entry with no handler
			// so the route is still counted, matching the
			// imported-class convention.
			return []map[string]string{routeEntry("ANY", routePath, "")}
		}
		entries := make([]map[string]string, 0, len(methods))
		for _, method := range methods {
			entries = append(entries, routeEntry(method, routePath, className+"."+strings.ToLower(method)))
		}
		return entries
	default:
		return nil
	}
}

func pythonModuleFunctionNames(root *tree_sitter.Node, source []byte) map[string]struct{} {
	names := make(map[string]struct{})
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "function_definition" || !pythonModuleLevelDefinition(node) {
			return
		}
		name := strings.TrimSpace(nodeText(node.ChildByFieldName("name"), source))
		if name != "" {
			names[name] = struct{}{}
		}
	})
	return names
}

func pythonModuleLevelDefinition(node *tree_sitter.Node) bool {
	parent := node.Parent()
	if parent != nil && parent.Kind() == "decorated_definition" {
		parent = parent.Parent()
	}
	return parent != nil && parent.Kind() == "module"
}

func pythonHTTPMethodsByClass(root *tree_sitter.Node, source []byte) map[string][]string {
	return pythonMethodsByClass(root, source, pythonHTTPRouteMethods)
}

func pythonMethodsByClass(
	root *tree_sitter.Node,
	source []byte,
	allowed map[string]struct{},
) map[string][]string {
	byClass := make(map[string][]string)
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "function_definition" {
			return
		}
		className := pythonEnclosingClassName(node, source)
		methodName := strings.TrimSpace(nodeText(node.ChildByFieldName("name"), source))
		if className == "" || methodName == "" {
			return
		}
		if _, ok := allowed[strings.ToLower(methodName)]; !ok {
			return
		}
		byClass[className] = appendUniqueString(byClass[className], strings.ToUpper(methodName))
	})
	return byClass
}

func pythonViewTargetIsInclude(view *tree_sitter.Node, source []byte) bool {
	return view.Kind() == "call" && pythonCallSimpleName(view.ChildByFieldName("function"), source) == "include"
}

func pythonViewTargetIsDRFActionMap(view *tree_sitter.Node, source []byte) bool {
	if view == nil || view.Kind() != "call" {
		return false
	}
	_, ok := pythonAsViewClassName(view, source)
	return ok && pythonPositionalArgument(view, 0) != nil && pythonPositionalArgument(view, 0).Kind() == "dictionary"
}

func pythonAsViewClassName(call *tree_sitter.Node, source []byte) (string, bool) {
	function := call.ChildByFieldName("function")
	if function == nil || function.Kind() != "attribute" ||
		strings.TrimSpace(nodeText(function.ChildByFieldName("attribute"), source)) != "as_view" {
		return "", false
	}
	object := function.ChildByFieldName("object")
	if object == nil || object.Kind() != "identifier" {
		return "", false
	}
	className := pythonViewTargetName(object, source)
	return className, className != ""
}

func pythonImportedAsViewCall(call *tree_sitter.Node, source []byte) bool {
	function := call.ChildByFieldName("function")
	if function == nil || function.Kind() != "attribute" ||
		strings.TrimSpace(nodeText(function.ChildByFieldName("attribute"), source)) != "as_view" {
		return false
	}
	object := function.ChildByFieldName("object")
	return object != nil && object.Kind() == "attribute"
}

func pythonViewTargetName(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	switch node.Kind() {
	case "identifier":
		return strings.TrimSpace(nodeText(node, source))
	case "attribute":
		return strings.TrimSpace(nodeText(node.ChildByFieldName("attribute"), source))
	default:
		return ""
	}
}

// pythonDjangoURLPatternPath extracts a normalized route path from the first
// positional argument of a path(), url(), or re_path() call. For path() the
// argument is a literal route string. For url() and re_path() the argument is
// a regex pattern whose anchors (^, $) and optional trailing-slash quantifier
// (/? at end) are stripped before normalization. pythonEnsureRoutePath handles
// the empty-string-to-"/" conversion for all call forms.
func pythonDjangoURLPatternPath(call *tree_sitter.Node, callName string, source []byte) string {
	arg := pythonPositionalArgument(call, 0)
	if arg == nil || arg.Kind() != "string" {
		return ""
	}
	raw := pythonStringLiteralValue(arg, source)
	if callName == "url" || callName == "re_path" {
		raw = strings.TrimPrefix(raw, "^")
		raw = strings.TrimSuffix(raw, "$")
		// Strip the `?` from an optional trailing slash group (/? → /).
		if strings.HasSuffix(raw, "/?") {
			raw = raw[:len(raw)-1]
		}
	}
	return pythonEnsureRoutePath(raw)
}

func pythonDjangoRoutePath(node *tree_sitter.Node, source []byte) string {
	if node == nil || node.Kind() != "string" {
		return ""
	}
	return pythonEnsureRoutePath(pythonStringLiteralValue(node, source))
}

func pythonDRFRouterPrefix(node *tree_sitter.Node, source []byte) (string, bool) {
	if node == nil || node.Kind() != "string" {
		return "", false
	}
	return pythonStringLiteralValue(node, source), true
}

func pythonEnsureRoutePath(path string) string {
	rawPath := path
	path = strings.TrimSpace(path)
	if path == "" {
		if rawPath == "" {
			return "/"
		}
		return ""
	}
	if strings.Trim(path, "/") == "" {
		return "/"
	}
	if strings.HasPrefix(path, "/") {
		return path
	}
	return "/" + path
}

func pythonJoinRoutePath(base string, segment string) string {
	base = pythonEnsureRoutePath(base)
	segment = strings.Trim(strings.TrimSpace(segment), "/")
	if base == "" || segment == "" {
		return base
	}
	return strings.TrimRight(base, "/") + "/" + segment + "/"
}

func pythonEnsureTrailingRouteSlash(path string) string {
	path = pythonEnsureRoutePath(path)
	if path == "" || path == "/" || strings.HasSuffix(path, "/") {
		return path
	}
	return path + "/"
}

func pythonPositionalArgument(call *tree_sitter.Node, index int) *tree_sitter.Node {
	if call == nil || index < 0 {
		return nil
	}
	arguments := call.ChildByFieldName("arguments")
	if arguments == nil {
		return nil
	}
	seen := 0
	cursor := arguments.Walk()
	defer cursor.Close()
	for _, child := range arguments.NamedChildren(cursor) {
		child := child
		if child.Kind() == "keyword_argument" {
			continue
		}
		if seen == index {
			return &child
		}
		seen++
	}
	return nil
}

// pythonHasDjangoURLImport returns true when the module contains an
// import-from statement that loads a URL dispatcher from either
// django.conf.urls (legacy) or django.urls (modern). It does not require
// a specific imported name (url, path, re_path, include, etc.) because a
// Django URLconf module may import only the names it needs.
func pythonHasDjangoURLImport(root *tree_sitter.Node, source []byte) bool {
	hasImport := false
	walkNamed(root, func(node *tree_sitter.Node) {
		if hasImport || node.Kind() != "import_from_statement" {
			return
		}
		text := nodeText(node, source)
		hasImport = strings.Contains(text, "django.conf.urls") || strings.Contains(text, "django.urls")
	})
	return hasImport
}

func pythonCallInURLPatterns(call *tree_sitter.Node, source []byte) bool {
	for parent := call.Parent(); parent != nil; parent = parent.Parent() {
		if parent.Kind() != "assignment" {
			continue
		}
		left := parent.ChildByFieldName("left")
		return strings.TrimSpace(nodeText(left, source)) == "urlpatterns"
	}
	return false
}

// --- Gathered-variant helpers for the Django/DRF detectors ---

// pythonModuleFunctionNamesGathered mirrors pythonModuleFunctionNames
// but iterates a pre-gathered function_definition slice.
func pythonModuleFunctionNamesGathered(gathered []*tree_sitter.Node, source []byte) map[string]struct{} {
	names := make(map[string]struct{})
	for _, node := range gathered {
		if !pythonModuleLevelDefinition(node) {
			continue
		}
		name := nodeText(node.ChildByFieldName("name"), source)
		if name != "" {
			names[name] = struct{}{}
		}
	}
	return names
}

// pythonHTTPMethodsByClassGathered mirrors pythonHTTPMethodsByClass
// but iterates a pre-gathered function_definition slice.
func pythonHTTPMethodsByClassGathered(gathered []*tree_sitter.Node, source []byte) map[string][]string {
	return pythonMethodsByClassGathered(gathered, source, pythonHTTPRouteMethods)
}

// pythonMethodsByClassGathered mirrors pythonMethodsByClass
// but iterates a pre-gathered function_definition slice.
func pythonMethodsByClassGathered(
	gathered []*tree_sitter.Node,
	source []byte,
	allowed map[string]struct{},
) map[string][]string {
	byClass := make(map[string][]string)
	for _, node := range gathered {
		className := pythonEnclosingClassName(node, source)
		methodName := nodeText(node.ChildByFieldName("name"), source)
		if className == "" || methodName == "" {
			continue
		}
		if _, ok := allowed[strings.ToLower(methodName)]; !ok {
			continue
		}
		byClass[className] = appendUniqueString(byClass[className], strings.ToUpper(methodName))
	}
	return byClass
}

// pythonHasDjangoURLImportGathered mirrors pythonHasDjangoURLImport
// but iterates a pre-gathered import slice. It only considers
// import_from_statement nodes, matching the original helper's predicate
// (an import like `import django.urls as path` must not match).
func pythonHasDjangoURLImportGathered(gathered []*tree_sitter.Node, source []byte) bool {
	for _, node := range gathered {
		if node.Kind() != "import_from_statement" {
			continue
		}
		text := nodeText(node, source)
		if strings.Contains(text, "django.conf.urls") || strings.Contains(text, "django.urls") {
			return true
		}
	}
	return false
}

// detectPythonDjangoSemanticsGathered produces the same output as
// detectPythonDjangoSemantics using pre-gathered nodes.
func detectPythonDjangoSemanticsGathered(g pythonGatheredNodes, source []byte) map[string]any {
	classMethods := pythonHTTPMethodsByClassGathered(g.functions, source)
	entries := pythonDjangoURLPatternEntriesGathered(g, source, classMethods)
	return pythonRouteSemantics(entries)
}

// pythonDjangoURLPatternEntriesGathered mirrors pythonDjangoURLPatternEntries
// using pre-gathered nodes.
func pythonDjangoURLPatternEntriesGathered(
	g pythonGatheredNodes,
	source []byte,
	classMethods map[string][]string,
) []map[string]string {
	if !pythonHasDjangoURLImportGathered(g.imports, source) {
		return nil
	}
	functionNames := pythonModuleFunctionNamesGathered(g.functions, source)
	entries := make([]map[string]string, 0)
	for _, node := range g.calls {
		callName := pythonCallSimpleName(node.ChildByFieldName("function"), source)
		if callName != "path" && callName != "url" && callName != "re_path" {
			continue
		}
		if !pythonCallInURLPatterns(node, source) {
			continue
		}
		routePath := pythonDjangoURLPatternPath(node, callName, source)
		if routePath == "" {
			continue
		}
		view := pythonPositionalArgument(node, 1)
		if view == nil || pythonViewTargetIsInclude(view, source) || pythonViewTargetIsDRFActionMap(view, source) {
			continue
		}
		entries = append(entries, pythonDjangoViewEntries(routePath, view, source, classMethods, functionNames)...)
	}
	return entries
}
