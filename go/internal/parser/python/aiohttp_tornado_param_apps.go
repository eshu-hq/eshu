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

// pythonModuleImportedNames collects every module-level imported alias that is
// in scope at the top of the file. It handles `import foo`, `import foo as bar`,
// `from module import name`, and `from module import name as alias`. Wildcard
// imports (`from module import *`) are intentionally ignored.
func pythonModuleImportedNames(root *tree_sitter.Node, source []byte) map[string]struct{} {
	names := make(map[string]struct{})
	pythonWalkImportStatements(root, source, func(statement string) {
		statement = strings.Join(strings.Fields(strings.TrimSpace(statement)), " ")
		switch {
		case strings.HasPrefix(statement, "import "):
			for _, clause := range pythonSplitImportClauses(strings.TrimSpace(strings.TrimPrefix(statement, "import "))) {
				_, alias := pythonSplitImportAlias(clause)
				if alias != "" {
					names[alias] = struct{}{}
					continue
				}
				// Import without `as`: use the first dot-segment as the local name.
				if localName := pythonImportLocalAlias(clause); localName != "" {
					names[localName] = struct{}{}
				}
			}
		case strings.HasPrefix(statement, "from "):
			rest := strings.TrimSpace(strings.TrimPrefix(statement, "from "))
			importIndex := strings.Index(rest, " import ")
			if importIndex == -1 {
				return
			}
			importClause := strings.TrimSpace(rest[importIndex+len(" import "):])
			importClause = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(importClause, "("), ")"))
			for _, clause := range pythonSplitImportClauses(importClause) {
				if strings.TrimSpace(clause) == "*" {
					continue // wildcard imports are unresolved
				}
				name, alias := pythonSplitImportAlias(clause)
				if alias != "" {
					names[alias] = struct{}{}
					continue
				}
				if name != "" {
					names[name] = struct{}{}
				}
			}
		}
	})
	return names
}

// pythonIsProvenParamAppHandler reports whether handler is a proven handler
// function identifier for a param-app aiohttp route. A handler is proven when it
// is either a module-level imported name or a module-level function definition
// that is not shadowed by a local variable assignment in the enclosing function
// body before the call.
func pythonIsProvenParamAppHandler(
	handler string,
	functionNames map[string]struct{},
	importedNames map[string]struct{},
	funcDef *tree_sitter.Node,
	call *tree_sitter.Node,
	source []byte,
) bool {
	if handler == "" {
		return false
	}
	_, isModuleFn := functionNames[handler]
	_, isImported := importedNames[handler]
	if !isModuleFn && !isImported {
		return false
	}
	// Check for shadowing by a local assignment in the enclosing function body
	// before the call.
	if funcDef != nil && isShadowedInFuncBody(funcDef, handler, call, source) {
		return false
	}
	return true
}

// isShadowedInFuncBody reports whether the function body contains an assignment
// to name (as a bare left-hand identifier) at a source position before call.
func isShadowedInFuncBody(funcDef *tree_sitter.Node, name string, call *tree_sitter.Node, source []byte) bool {
	body := funcDef.ChildByFieldName("body")
	if body == nil {
		return false
	}
	callStart := call.StartByte()
	shadowed := false
	walkNamed(body, func(node *tree_sitter.Node) {
		if shadowed {
			return
		}
		if node.Kind() != "assignment" {
			return
		}
		if node.StartByte() >= callStart {
			return
		}
		left := node.ChildByFieldName("left")
		if left == nil || left.Kind() != "identifier" {
			return
		}
		if strings.TrimSpace(nodeText(left, source)) == name {
			shadowed = true
		}
	})
	return shadowed
}
