// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package python

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// pythonEnclosingFunctionDef returns the enclosing function_definition node, or
// nil when the node is inside a class_definition / lambda (which act as scope
// boundaries) or at module level.
func pythonEnclosingFunctionDef(node *tree_sitter.Node) *tree_sitter.Node {
	for current := node.Parent(); current != nil; current = current.Parent() {
		switch current.Kind() {
		case "function_definition":
			return current
		case "class_definition", "lambda":
			return nil
		}
	}
	return nil
}

// pythonAioHTTPParamAppSymbols walks function definitions and finds parameter
// names that are used as aiohttp application receivers within that function's
// body: the param must be the target of .router.add_<verb>(...) or
// .add_routes(...). The returned map keys are function_definition nodes, and
// the values are the set of param names that qualify as aiohttp app symbols
// scoped to that function body.
func pythonAioHTTPParamAppSymbols(root *tree_sitter.Node, source []byte) map[uintptr]map[string]struct{} {
	result := make(map[uintptr]map[string]struct{})

	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "function_definition" {
			return
		}
		params := pythonParameterNames(node.ChildByFieldName("parameters"), source)
		if len(params) == 0 {
			return
		}
		paramSet := make(map[string]struct{}, len(params))
		for _, p := range params {
			paramSet[p] = struct{}{}
		}
		body := node.ChildByFieldName("body")
		if body == nil {
			return
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
			result[node.Id()] = matched
		}
	})

	return result
}
