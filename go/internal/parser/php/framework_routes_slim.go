// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package php

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// phpIsSlimVerbCandidate reports whether a member_call_expression node has a
// method name matching one of the Slim route-registration verbs. This is a
// cheap phase-1 filter; the full Slim import gate and argument extraction
// happen in phpSlimRoutes after phase 1 completes.
func phpIsSlimVerbCandidate(node *tree_sitter.Node, source []byte) bool {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return false
	}
	methodName := strings.ToLower(strings.TrimSpace(shared.NodeText(nameNode, source)))
	switch methodName {
	case "get", "post", "put", "patch", "delete", "options", "any", "map":
		return true
	}
	return false
}

// phpSlimRoutes resolves Slim route entries from member_call_expression
// candidates gathered during the phase 1 declarations walk. Routes are only
// emitted when a Slim import (use Slim\...) is present in the file, which
// prevents false positives from non-Slim get/post/... calls. The first string-
// literal argument is the route path; the second argument names the handler
// (class::class, string literal, or closure — closures produce an empty
// handler). For the map() verb the first argument is an array of HTTP methods.
//
// Enclosing group() prefixes are resolved by walking the AST parent chain so
// inner routes registered under $app->group('/prefix', fn($group) =>
// $group->get('/sub', ...)) emit the concatenated path '/prefix/sub'. Nested
// groups are supported. A non-literal group prefix is treated as unresolvable
// and causes the inner route to be skipped — a wrong path is worse than a
// missing one. Empty paths (including empty-subpath routes with no enclosing
// group) are never emitted.
func phpSlimRoutes(candidates []*tree_sitter.Node, source []byte, payload map[string]any) []phpRoute {
	if !phpHasSlimImport(payload) {
		return nil
	}

	routes := make([]phpRoute, 0)
	for _, node := range candidates {
		if node.Kind() != "member_call_expression" {
			continue
		}
		nameNode := node.ChildByFieldName("name")
		if nameNode == nil {
			continue
		}
		verb := strings.ToLower(strings.TrimSpace(shared.NodeText(nameNode, source)))
		args := phpCallArguments(node, source)

		// Resolve enclosing group() prefixes. A non-literal prefix
		// makes the accurate path unknowable; skip rather than emit
		// a wrong one.
		prefixes, prefixesOk := phpEnclosingGroupPrefixes(node, source)
		if !prefixesOk {
			continue
		}

		if verb == "map" {
			// map(['GET', 'POST'], '/path', handler)
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
			fullPath := phpSlimRoutePath(prefixes, subpath)
			if fullPath == "" {
				continue
			}
			var handler string
			if len(args) >= 3 {
				handler = phpSlimHandlerName(args[2])
			}
			for _, method := range methods {
				routes = append(routes, phpRoute{
					method:  method,
					path:    fullPath,
					handler: handler,
				})
			}
		} else {
			// verb(path, handler)
			if len(args) < 1 {
				continue
			}
			subpath, ok := phpExactStringLiteral(args[0])
			if !ok {
				continue
			}
			fullPath := phpSlimRoutePath(prefixes, subpath)
			if fullPath == "" {
				continue
			}
			var handler string
			if len(args) >= 2 {
				handler = phpSlimHandlerName(args[1])
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

// phpHasSlimImport reports whether the file imports a Slim namespace, which
// gates Slim route detection to avoid false positives on unrelated ->get(),
// ->post(), etc. calls.
func phpHasSlimImport(payload map[string]any) bool {
	imports, _ := payload["imports"].([]map[string]any)
	for _, item := range imports {
		importName := strings.TrimSpace(phpStringValue(item["name"]))
		if strings.HasPrefix(importName, "Slim\\") || strings.HasPrefix(importName, "Slim/") {
			return true
		}
	}
	return false
}

// phpSlimHandlerName extracts a human-readable handler name from a Slim route
// callable argument. Class::class expressions produce the class short name;
// string literals are returned as-is; closures and other callables produce the
// empty string.
func phpSlimHandlerName(argText string) string {
	argText = strings.TrimSpace(argText)
	if strings.HasSuffix(argText, "::class") {
		return shared.LastPathSegment(strings.TrimSuffix(argText, "::class"), `\`)
	}
	if literal, ok := phpExactStringLiteral(argText); ok {
		return literal
	}
	return ""
}

// phpParseSlimMethodsArg extracts a list of HTTP methods from a Slim map() first
// argument, which may be a short array ['GET', 'POST'] or a long array(...)
// expression. It delegates to the shared phpExactHTTPMethods parser.
func phpParseSlimMethodsArg(argText string) []string {
	methods, _ := phpExactHTTPMethods(argText)
	return methods
}

// phpSlimRoutePath concatenates enclosing group prefixes with the route's
// subpath. Empty paths are never emitted — an empty-subpath route with no
// enclosing group is skipped.
func phpSlimRoutePath(prefixes []string, subpath string) string {
	parts := make([]string, 0, len(prefixes)+1)
	parts = append(parts, prefixes...)
	parts = append(parts, subpath)
	path := strings.Join(parts, "")
	if strings.TrimSpace(path) == "" {
		return ""
	}
	return path
}

// phpEnclosingGroupPrefixes walks up the AST parent chain from a route
// candidate node, collecting the literal prefix arguments of every enclosing
// member_call_expression whose method name is "group". Prefixes are returned
// outermost-first for concatenation. The second return value is false when any
// group's prefix is not a string literal, meaning the accurate path cannot be
// determined and the route must be skipped.
func phpEnclosingGroupPrefixes(node *tree_sitter.Node, source []byte) ([]string, bool) {
	var prefixes []string
	for current := node.Parent(); current != nil; current = current.Parent() {
		if current.Kind() != "member_call_expression" {
			continue
		}
		nameNode := current.ChildByFieldName("name")
		if nameNode == nil || strings.ToLower(strings.TrimSpace(shared.NodeText(nameNode, source))) != "group" {
			continue
		}
		args := phpCallArguments(current, source)
		if len(args) < 1 {
			continue
		}
		prefix, ok := phpExactStringLiteral(args[0])
		if !ok {
			return nil, false
		}
		prefixes = append(prefixes, prefix)
	}
	// Reverse to get outermost-first order (parent walk yields innermost-first).
	for i, j := 0, len(prefixes)-1; i < j; i, j = i+1, j-1 {
		prefixes[i], prefixes[j] = prefixes[j], prefixes[i]
	}
	return prefixes, true
}
