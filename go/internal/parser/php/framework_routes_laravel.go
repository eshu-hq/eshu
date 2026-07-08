// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package php

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// phpIsLaravelVerbCandidate reports whether a scoped_call_expression node has
// a method name matching one of the Laravel route-registration verbs. This is
// a cheap phase-1 filter; the full Laravel import gate and argument extraction
// happen in phpLaravelRoutes after phase 1 completes.
func phpIsLaravelVerbCandidate(node *tree_sitter.Node, source []byte) bool {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return false
	}
	methodName := strings.ToLower(strings.TrimSpace(shared.NodeText(nameNode, source)))
	switch methodName {
	case "get", "post", "put", "patch", "delete", "options", "any", "match":
		return true
	}
	return false
}

// phpLaravelRoutes resolves Laravel route entries from scoped_call_expression
// candidates gathered during the phase 1 declarations walk. Routes are only
// emitted when the scope resolves to the Illuminate\Support\Facades\Route
// facade — either through a use import, a fully-qualified scope, or the
// \Route global alias. The first string-literal argument is the route path;
// the second argument names the handler (string literal, array, or closure).
// For match() the first argument is an array of HTTP methods.
//
// Enclosing Route::group() prefixes are resolved by walking the AST parent
// chain and extracting the 'prefix' key from the first-arg array. A group
// with no 'prefix' key contributes no path segment (namespace/middleware-only
// groups are common). A non-literal prefix makes the path unknowable and
// causes the inner route to be skipped. Empty paths are never emitted.
func phpLaravelRoutes(candidates []*tree_sitter.Node, source []byte, payload map[string]any) []phpRoute {
	if !phpHasLaravelRouteImport(payload) {
		// Check if any candidate uses the \Route:: global alias or the
		// FQN \Illuminate\Support\Facades\Route:: — those do not need
		// an explicit use import. Bare Route:: in a namespace-less
		// file is also the global \Route alias when no import shadows it.
		foundLaravelScope := false
		for _, node := range candidates {
			if node.Kind() != "scoped_call_expression" {
				continue
			}
			scopeNode := node.ChildByFieldName("scope")
			if scopeNode == nil {
				continue
			}
			if phpIsLaravelRouteScopeText(strings.TrimSpace(shared.NodeText(scopeNode, source)), payload) {
				foundLaravelScope = true
				break
			}
		}
		if !foundLaravelScope {
			return nil
		}
	}

	routes := make([]phpRoute, 0)
	for _, node := range candidates {
		if node.Kind() != "scoped_call_expression" {
			continue
		}
		scopeNode := node.ChildByFieldName("scope")
		if scopeNode == nil {
			continue
		}
		scopeText := strings.TrimSpace(shared.NodeText(scopeNode, source))
		if !phpIsLaravelRouteScopeText(scopeText, payload) {
			continue
		}
		nameNode := node.ChildByFieldName("name")
		if nameNode == nil {
			continue
		}
		verb := strings.ToLower(strings.TrimSpace(shared.NodeText(nameNode, source)))
		args := phpCallArguments(node, source)

		// Resolve enclosing Route::group() prefixes.
		prefixes, prefixesOk := phpEnclosingLaravelGroupPrefixes(node, source)
		if !prefixesOk {
			continue
		}

		if verb == "match" {
			// match(['GET', 'POST'], '/path', handler)
			if len(args) < 2 {
				continue
			}
			methods := phpParseSlimMethodsArg(args[0])
			if len(methods) == 0 {
				continue
			}
			subpath, ok := phpExactStringLiteral(args[1])
			if !ok {
				continue
			}
			fullPath := phpLaravelRoutePath(prefixes, subpath)
			if fullPath == "" {
				continue
			}
			var handler string
			if len(args) >= 3 {
				handler = phpLaravelHandlerName(args[2])
			}
			for _, method := range methods {
				routes = append(routes, phpRoute{
					method:  method,
					path:    fullPath,
					handler: handler,
				})
			}
		} else if verb == "any" {
			// any('/path', handler) — all methods, emit as ANY
			if len(args) < 1 {
				continue
			}
			subpath, ok := phpExactStringLiteral(args[0])
			if !ok {
				continue
			}
			fullPath := phpLaravelRoutePath(prefixes, subpath)
			if fullPath == "" {
				continue
			}
			var handler string
			if len(args) >= 2 {
				handler = phpLaravelHandlerName(args[1])
			}
			routes = append(routes, phpRoute{
				method:  "ANY",
				path:    fullPath,
				handler: handler,
			})
		} else {
			// verb(path, handler)
			if len(args) < 1 {
				continue
			}
			subpath, ok := phpExactStringLiteral(args[0])
			if !ok {
				continue
			}
			fullPath := phpLaravelRoutePath(prefixes, subpath)
			if fullPath == "" {
				continue
			}
			var handler string
			if len(args) >= 2 {
				handler = phpLaravelHandlerName(args[1])
			}
			routes = append(routes, phpRoute{
				method:  strings.ToUpper(verb),
				path:    fullPath,
				handler: handler,
			})
		}
	}
	return routes
}

// phpHasLaravelRouteImport reports whether the file imports the Laravel
// Illuminate\Support\Facades\Route namespace, which gates Laravel route
// detection for bare Route:: calls resolved through the use import.
func phpHasLaravelRouteImport(payload map[string]any) bool {
	imports, _ := payload["imports"].([]map[string]any)
	for _, item := range imports {
		importName := strings.Trim(strings.TrimSpace(phpStringValue(item["name"])), `\`)
		if importName == "Illuminate\\Support\\Facades\\Route" {
			return true
		}
	}
	return false
}

// phpHasConflictingRouteImport reports whether the file has a use import whose
// last segment is "Route" but is NOT the Illuminate\Support\Facades\Route
// facade. When such an import exists, bare Route:: in the file is shadowed to
// point to the imported class, not the Laravel Route facade.
func phpHasConflictingRouteImport(payload map[string]any) bool {
	imports, _ := payload["imports"].([]map[string]any)
	for _, item := range imports {
		importName := strings.Trim(strings.TrimSpace(phpStringValue(item["name"])), `\`)
		if importName == "Illuminate\\Support\\Facades\\Route" {
			continue
		}
		if shared.LastPathSegment(importName, `\`) == "Route" {
			return true
		}
	}
	return false
}

// phpIsLaravelRouteScopeText reports whether a scope identifier text resolves
// to the Illuminate\Support\Facades\Route facade. It accepts:
//
//  1. Fully-qualified: \Illuminate\Support\Facades\Route (any leading
//     backslashes stripped).
//  2. Global alias: \Route.
//  3. Bare Route resolved through a use import for
//     Illuminate\Support\Facades\Route.
//  4. Bare Route in the global namespace (no namespace declaration) when no
//     import shadows it — this covers the common Laravel pattern where
//     routes/*.php files are loaded by the framework and Route resolves to
//     the registered \Route alias.
func phpIsLaravelRouteScopeText(scopeText string, payload map[string]any) bool {
	normalized := strings.Trim(scopeText, `\`)

	// FQN: \Illuminate\Support\Facades\Route
	if normalized == "Illuminate\\Support\\Facades\\Route" {
		return true
	}

	// \Route (global alias with explicit backslash) or bare Route
	if normalized == "Route" {
		// Case 1: Explicit use import resolves Route.
		if phpHasLaravelRouteImport(payload) {
			return true
		}
		// Case 2: Global namespace (no namespace declaration) and
		// no import shadows Route. This is the common pattern in
		// Laravel routes/*.php files where Route facade is available
		// through the framework's class alias.
		_, hasNS := payload["namespace"]
		if !hasNS && !phpHasConflictingRouteImport(payload) {
			return true
		}
	}

	return false
}

// phpLaravelHandlerName extracts a human-readable handler name from a Laravel
// route callable argument. String literals (e.g. 'Controller@method') are
// returned as-is; closures produce the empty string; arrays
// ([Controller::class, 'method']) are handled as best-effort.
func phpLaravelHandlerName(argText string) string {
	argText = strings.TrimSpace(argText)
	// [Controller::class, 'method'] — extract class short name and method.
	if strings.HasPrefix(argText, "[") && strings.HasSuffix(argText, "]") {
		return phpLaravelArrayHandler(argText)
	}
	if literal, ok := phpExactStringLiteral(argText); ok {
		return literal
	}
	return ""
}

// phpLaravelArrayHandler extracts a handler name from an array callable
// argument like [Controller::class, 'method']. Returns "Controller.method"
// on success, or the empty string when the array shape is unrecognized.
func phpLaravelArrayHandler(argText string) string {
	inner := strings.TrimSpace(argText[1 : len(argText)-1])
	parts := phpSplitTopLevel(inner)
	if len(parts) != 2 {
		return ""
	}
	classPart := strings.TrimSpace(parts[0])
	methodPart := strings.TrimSpace(parts[1])

	// Extract class name from ClassName::class
	classShort := ""
	if strings.HasSuffix(classPart, "::class") {
		classShort = shared.LastPathSegment(strings.TrimSuffix(classPart, "::class"), `\`)
	}

	methodLiteral, ok := phpExactStringLiteral(methodPart)
	if !ok {
		return ""
	}
	if classShort != "" {
		return classShort + "." + methodLiteral
	}
	return methodLiteral
}

// phpEnclosingLaravelGroupPrefixes walks up the AST parent chain from a route
// candidate node, collecting the 'prefix' key values from every enclosing
// scoped_call_expression whose method name is "group". Prefixes are returned
// outermost-first for concatenation. The second return value is false when any
// group's prefix value is not a string literal, meaning the accurate path
// cannot be determined and the route must be skipped. A group with no 'prefix'
// key contributes no path segment and is not an error.
func phpEnclosingLaravelGroupPrefixes(node *tree_sitter.Node, source []byte) ([]string, bool) {
	var prefixes []string
	for current := node.Parent(); current != nil; current = current.Parent() {
		if current.Kind() != "scoped_call_expression" {
			continue
		}
		nameNode := current.ChildByFieldName("name")
		if nameNode == nil || strings.ToLower(strings.TrimSpace(shared.NodeText(nameNode, source))) != "group" {
			continue
		}
		prefix, ok := phpLaravelGroupPrefix(current, source)
		if !ok {
			return nil, false
		}
		if prefix != "" {
			prefixes = append(prefixes, prefix)
		}
		// prefix == "" and ok == true means the group has no 'prefix'
		// key — a namespace/middleware-only group. Skip it silently.
	}
	// Reverse to get outermost-first order (parent walk yields innermost-first).
	for i, j := 0, len(prefixes)-1; i < j; i, j = i-1, j+1 {
		prefixes[i], prefixes[j] = prefixes[j], prefixes[i]
	}
	return prefixes, true
}

// phpLaravelGroupPrefix extracts the 'prefix' key value from the first
// argument of a Route::group(...) call. Returns:
//
//	("prefix_value", true)  — prefix found and is a string literal.
//	("", true)              — no 'prefix' key in the array (OK to proceed).
//	("", false)             — 'prefix' key found but value is not a literal.
func phpLaravelGroupPrefix(groupNode *tree_sitter.Node, source []byte) (string, bool) {
	args := phpCallArguments(groupNode, source)
	if len(args) < 1 {
		return "", true
	}
	return phpExtractArrayStringValue(args[0], "prefix")
}

// phpStripArrayBrackets strips the outer brackets from a PHP short [...]
// or long array(...) literal and returns the inner text. Returns the empty
// string when the text is not a recognized array literal.
func phpStripArrayBrackets(text string) string {
	if strings.HasPrefix(text, "[") && strings.HasSuffix(text, "]") {
		return strings.TrimSpace(text[1 : len(text)-1])
	}
	if strings.HasPrefix(text, "array(") && strings.HasSuffix(text, ")") {
		return strings.TrimSpace(text[len("array(") : len(text)-1])
	}
	return ""
}

// phpExtractArrayStringValue searches a PHP array literal text for a specific
// string key and returns its string-literal value. Returns:
//
//	("literal_value", true) — key found with a string-literal value.
//	("", true)              — key not found in the array.
//	("", false)             — key found but value is not a string literal.
func phpExtractArrayStringValue(arrayText, key string) (string, bool) {
	arrayText = strings.TrimSpace(arrayText)
	if len(arrayText) == 0 {
		return "", true
	}

	// Strip outer brackets: [...] or array(...)
	inner := phpStripArrayBrackets(arrayText)
	if inner == "" {
		return "", true
	}

	parts := phpSplitTopLevel(inner)
	for _, part := range parts {
		before, after, hasArrow := strings.Cut(part, "=>")
		if !hasArrow {
			continue
		}
		k := strings.TrimSpace(before)
		v := strings.TrimSpace(after)
		keyLiteral, ok := phpExactStringLiteral(k)
		if !ok || keyLiteral != key {
			continue
		}
		// Key matched — require the value to be a string literal.
		if literal, ok := phpExactStringLiteral(v); ok {
			return literal, true
		}
		return "", false
	}

	return "", true
}

// phpLaravelRoutePath concatenates enclosing group prefixes with the route's
// subpath. Empty paths are never emitted — an empty-subpath route with no
// enclosing group is skipped.
func phpLaravelRoutePath(prefixes []string, subpath string) string {
	parts := make([]string, 0, len(prefixes)+1)
	parts = append(parts, prefixes...)
	parts = append(parts, subpath)
	path := strings.Join(parts, "")
	if strings.TrimSpace(path) == "" {
		return ""
	}
	return path
}
