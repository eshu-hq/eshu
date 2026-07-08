// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// javaScriptCollectFastifyTypedParameterBase seeds fastifyBases from Fastify-
// typed parameters and from variable declarators whose declared type is a
// Fastify type and whose value is a function (the autoload/plugin pattern).
//
// It handles two patterns:
//
//  1. Explicitly-typed parameter:
//     async function plugin(fastify: FastifyInstance) { ... }
//     Detects the parameter's declared type leaf name is in
//     javaScriptFastifyTypeLeafNames and adds the parameter name to dst.
//
//  2. Variable-declarator type inference (autoload plugin):
//     const plugin: FastifyPluginAsyncTypebox = async (fastify) => { ... }
//     The parameter fastify has no explicit type (TS infers it). The
//     variable declarator carries the type, and the value is a function.
//     Extract the function's parameter names and add them to dst.
//
// Import gate: deliberately absent — these types are often imported from
// @fastify/type-provider-typebox or similar packages that do not match
// javaScriptHasFastifyImport. A false fastifyBase from a generic type
// sharing a Fastify leaf name would only produce a base entry, not a
// route; the route matching still requires .get()/.post() etc. on the
// resolved base, which is the real specificity gate.
//
// It is a no-op for any other node kind, so callers may invoke it on
// every visited node in a shared traversal without pre-filtering.
func javaScriptCollectFastifyTypedParameterBase(node *tree_sitter.Node, source []byte, dst map[string]struct{}) {
	switch node.Kind() {
	case "required_parameter", "optional_parameter", "formal_parameter":
		paramName := javaScriptTypedBindingName(node, source)
		typeName := javaScriptDeclaredTypeName(node, source)
		if paramName == "" || typeName == "" {
			return
		}
		if _, ok := javaScriptFastifyTypeLeafNames[typeName]; ok {
			javaScriptAddName(dst, paramName)
		}
	case "variable_declarator":
		typeName := javaScriptDeclaredTypeName(node, source)
		if typeName == "" {
			return
		}
		if _, ok := javaScriptFastifyTypeLeafNames[typeName]; !ok {
			return
		}
		valueNode := node.ChildByFieldName("value")
		if valueNode == nil {
			return
		}
		javaScriptCollectFunctionParameterNames(valueNode, source, dst)
	}
}

// javaScriptCollectFunctionParameterNames adds the name of each parameter in
// a function, arrow function, or generator function node to dst.
func javaScriptCollectFunctionParameterNames(node *tree_sitter.Node, source []byte, dst map[string]struct{}) {
	switch node.Kind() {
	case "function_expression", "arrow_function", "generator_function":
	default:
		return
	}
	cursor := node.Walk()
	children := node.NamedChildren(cursor)
	cursor.Close()
	for i := range children {
		child := children[i]
		if child.Kind() == "formal_parameters" {
			javaScriptCollectFormalParameterNames(&child, source, dst)
			return
		}
	}
}

// javaScriptCollectFormalParameterNames adds the name of each
// required_parameter or optional_parameter to dst. Uses name extraction
// that works for both typed parameters (fastify: FastifyInstance) and
// untyped/inferred parameters (fastify) by trying ChildByFieldName("pattern")
// first, then falling back to javaScriptTypedBindingName.
func javaScriptCollectFormalParameterNames(node *tree_sitter.Node, source []byte, dst map[string]struct{}) {
	cursor := node.Walk()
	children := node.NamedChildren(cursor)
	cursor.Close()
	for i := range children {
		child := children[i]
		switch child.Kind() {
		case "required_parameter", "optional_parameter":
			name := javaScriptFormalParameterName(&child, source)
			if name != "" {
				javaScriptAddName(dst, name)
			}
		}
	}
}

// javaScriptFormalParameterName returns the parameter name for a
// required_parameter or optional_parameter node. It tries the pattern
// field first (which works for both typed and untyped parameters),
// then falls back to javaScriptTypedBindingName for edge cases.
func javaScriptFormalParameterName(node *tree_sitter.Node, source []byte) string {
	// Tree-sitter uses field "pattern" for parameter names.
	if patternNode := node.ChildByFieldName("pattern"); patternNode != nil {
		if name := strings.TrimSpace(nodeText(patternNode, source)); name != "" {
			return name
		}
	}
	// Fall back to typed-binding name extraction (handles ": Type" notation).
	return javaScriptTypedBindingName(node, source)
}
