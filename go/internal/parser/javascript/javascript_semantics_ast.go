// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript

import (
	"regexp"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// javaScriptHTTPMethodVerbs is the ordered, canonical set of HTTP verbs that a
// Next.js route module or Express route call may declare.
var javaScriptHTTPMethodVerbs = map[string]struct{}{
	"GET":     {},
	"POST":    {},
	"PUT":     {},
	"PATCH":   {},
	"DELETE":  {},
	"HEAD":    {},
	"OPTIONS": {},
}

// javaScriptAWSClientServiceRe extracts the service slug from an @aws-sdk
// client-* package specifier. It is a within-string-content exception: it runs
// only against an import/require module-specifier string already isolated by the
// AST, never against raw source.
var javaScriptAWSClientServiceRe = regexp.MustCompile(`^@aws-sdk/client-([a-z0-9-]+)$`)

// javaScriptGCPServiceRe extracts the service slug from a @google-cloud/* package
// specifier. It is a within-string-content exception: it runs only against an
// import/require module-specifier string already isolated by the AST.
var javaScriptGCPServiceRe = regexp.MustCompile(`^@google-cloud/([a-z0-9-]+)$`)

// javaScriptRuntimeDirective returns "client" or "server" when the program opens
// with a "use client"/"use server" directive prologue, or "" otherwise. The
// directive is the first statement: an expression_statement wrapping a string
// literal whose normalized content is "use client" or "use server".
func javaScriptRuntimeDirective(root *tree_sitter.Node, source []byte) string {
	if root == nil {
		return ""
	}
	cursor := root.Walk()
	defer cursor.Close()
	for _, child := range root.NamedChildren(cursor) {
		child := child
		if child.Kind() == "comment" {
			continue
		}
		if child.Kind() != "expression_statement" {
			return ""
		}
		stringNode := javaScriptFirstStringChild(&child)
		if stringNode == nil {
			return ""
		}
		switch javaScriptNormalizeDirective(jsStringLiteralValue(stringNode, source)) {
		case "use client":
			return "client"
		case "use server":
			return "server"
		default:
			return ""
		}
	}
	return ""
}

// javaScriptNormalizeDirective collapses internal whitespace so a directive
// written with multiple spaces (use  client) still matches.
func javaScriptNormalizeDirective(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func javaScriptFirstStringChild(node *tree_sitter.Node) *tree_sitter.Node {
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		if child.Kind() == "string" {
			return &child
		}
	}
	return nil
}

// javaScriptHasMetadataConstExport reports whether the module exports a const
// named metadata: an export_statement whose declaration is a lexical_declaration
// with a variable_declarator named "metadata".
func javaScriptHasMetadataConstExport(root *tree_sitter.Node, source []byte) bool {
	if root == nil {
		return false
	}
	found := false
	walkNamed(root, func(node *tree_sitter.Node) {
		if found || node.Kind() != "export_statement" {
			return
		}
		declaration := node.ChildByFieldName("declaration")
		if declaration == nil || declaration.Kind() != "lexical_declaration" {
			return
		}
		cursor := declaration.Walk()
		defer cursor.Close()
		for _, child := range declaration.NamedChildren(cursor) {
			child := child
			if child.Kind() != "variable_declarator" {
				continue
			}
			if strings.TrimSpace(nodeText(child.ChildByFieldName("name"), source)) == "metadata" {
				found = true
				return
			}
		}
	})
	return found
}

// javaScriptHTTPVerbExports returns the HTTP verbs exported as functions from a
// Next.js route module, deduplicated in source order. It walks export_statement
// nodes whose declaration is a function_declaration named after an HTTP verb.
func javaScriptHTTPVerbExports(root *tree_sitter.Node, source []byte) []string {
	verbs := make([]string, 0, 4)
	seen := make(map[string]struct{})
	if root == nil {
		return verbs
	}
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "export_statement" {
			return
		}
		declaration := node.ChildByFieldName("declaration")
		if declaration == nil || declaration.Kind() != "function_declaration" {
			return
		}
		name := strings.TrimSpace(nodeText(declaration.ChildByFieldName("name"), source))
		verb := strings.ToUpper(name)
		if _, ok := javaScriptHTTPMethodVerbs[verb]; !ok {
			return
		}
		if _, ok := seen[verb]; ok {
			return
		}
		seen[verb] = struct{}{}
		verbs = append(verbs, verb)
	})
	return verbs
}

// javaScriptExpressRoute is one Express route registration discovered in the AST.
type javaScriptExpressRoute struct {
	symbol  string
	method  string
	path    string
	handler string
}

// javaScriptExpressRouteCalls walks call_expression nodes that register an
// Express route: a member_expression callee whose object is an identifier and
// whose property is an HTTP verb, with a string-literal first argument as the
// path. The handler binds only when the call has exactly two arguments and the
// second is a bare identifier, matching the prior single-named-reference rule so
// inline callbacks and middleware chains stay unbound (#2721). Routes are
// returned in source order.
func javaScriptExpressRouteCalls(root *tree_sitter.Node, source []byte) []javaScriptExpressRoute {
	routes := make([]javaScriptExpressRoute, 0)
	if root == nil {
		return routes
	}
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "call_expression" {
			return
		}
		functionNode := node.ChildByFieldName("function")
		if functionNode == nil || functionNode.Kind() != "member_expression" {
			return
		}
		objectNode := functionNode.ChildByFieldName("object")
		propertyNode := functionNode.ChildByFieldName("property")
		if objectNode == nil || objectNode.Kind() != "identifier" || propertyNode == nil {
			return
		}
		method := strings.ToLower(strings.TrimSpace(nodeText(propertyNode, source)))
		if _, ok := javaScriptExpressRouteMethods[method]; !ok {
			return
		}
		argsNode := node.ChildByFieldName("arguments")
		if argsNode == nil {
			return
		}
		cursor := argsNode.Walk()
		args := argsNode.NamedChildren(cursor)
		cursor.Close()
		if len(args) == 0 || args[0].Kind() != "string" {
			return
		}
		path := jsStringLiteralValue(&args[0], source)
		if strings.TrimSpace(path) == "" {
			return
		}
		handler := ""
		if len(args) == 2 && args[1].Kind() == "identifier" {
			handler = strings.TrimSpace(nodeText(&args[1], source))
		}
		routes = append(routes, javaScriptExpressRoute{
			symbol:  strings.TrimSpace(nodeText(objectNode, source)),
			method:  strings.ToUpper(method),
			path:    path,
			handler: handler,
		})
	})
	return routes
}

// javaScriptContainsJSXReturn reports whether a function node returns JSX: a
// return_statement whose value is a JSX element/fragment, or a concise arrow
// body that is itself JSX. This replaces a text-level return-of-JSX scan with an
// AST check.
func javaScriptContainsJSXReturn(node *tree_sitter.Node) bool {
	if node == nil {
		return false
	}
	found := false
	walkNamed(node, func(child *tree_sitter.Node) {
		if found {
			return
		}
		switch child.Kind() {
		case "return_statement":
			if javaScriptIsJSXNode(javaScriptFirstNamedChild(child)) {
				found = true
			}
		case "arrow_function":
			if javaScriptIsJSXNode(child.ChildByFieldName("body")) {
				found = true
			}
		}
	})
	return found
}

// javaScriptFirstNamedChild returns the first named child of node, or nil.
func javaScriptFirstNamedChild(node *tree_sitter.Node) *tree_sitter.Node {
	if node == nil {
		return nil
	}
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		return &child
	}
	return nil
}

func javaScriptIsJSXNode(node *tree_sitter.Node) bool {
	if node == nil {
		return false
	}
	switch node.Kind() {
	case "jsx_element", "jsx_self_closing_element", "jsx_fragment":
		return true
	case "parenthesized_expression":
		cursor := node.Walk()
		defer cursor.Close()
		for _, child := range node.NamedChildren(cursor) {
			child := child
			if javaScriptIsJSXNode(&child) {
				return true
			}
		}
	}
	return false
}

// javaScriptImportModuleSpecifiers returns the module-specifier string value of
// every import_statement and require call in source order. The values feed the
// AWS/GCP within-string slug extraction, keeping module detection on the AST
// while the residual slug match runs only against the isolated specifier.
func javaScriptImportModuleSpecifiers(root *tree_sitter.Node, source []byte) []string {
	specifiers := make([]string, 0)
	if root == nil {
		return specifiers
	}
	walkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "import_statement":
			if sourceNode := node.ChildByFieldName("source"); sourceNode != nil && sourceNode.Kind() == "string" {
				specifiers = append(specifiers, jsStringLiteralValue(sourceNode, source))
			}
		case "call_expression":
			functionNode := node.ChildByFieldName("function")
			if functionNode == nil {
				return
			}
			// Cover both require("...") and dynamic import("..."). The grammar
			// emits the dynamic-import callee as an "import" node, while require
			// is a plain identifier. The prior raw-source regex matched the
			// specifier string in either form, so both must be collected.
			if functionNode.Kind() != "import" && strings.TrimSpace(nodeText(functionNode, source)) != "require" {
				return
			}
			argsNode := node.ChildByFieldName("arguments")
			if argsNode == nil {
				return
			}
			cursor := argsNode.Walk()
			args := argsNode.NamedChildren(cursor)
			cursor.Close()
			if len(args) == 1 && args[0].Kind() == "string" {
				specifiers = append(specifiers, jsStringLiteralValue(&args[0], source))
			}
		}
	})
	return specifiers
}

// javaScriptClientSymbolNames returns the unique, source-order names of
// XxxClient symbols constructed via new expressions in the file. Covers:
//   - new SSMClient(...)        → identifier child of new_expression → "SSMClient"
//   - new vision.ImageAnnotatorClient() → member_expression property  → "ImageAnnotatorClient"
//
// Matching is structural (AST node kinds), not regex over raw source, so
// comments and string literals are never visited. This intentionally narrows
// the prior raw-source regex, which also matched the XxxClient token in import
// bindings, type annotations, and comments; those non-construction occurrences
// were false positives and are no longer reported.
func javaScriptClientSymbolNames(root *tree_sitter.Node, source []byte) []string {
	symbols := make([]string, 0)
	seen := make(map[string]struct{})
	if root == nil {
		return symbols
	}
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "new_expression" {
			return
		}
		constructor := node.ChildByFieldName("constructor")
		if constructor == nil {
			return
		}
		var name string
		switch constructor.Kind() {
		case "identifier":
			name = strings.TrimSpace(nodeText(constructor, source))
		case "member_expression":
			prop := constructor.ChildByFieldName("property")
			if prop != nil {
				name = strings.TrimSpace(nodeText(prop, source))
			}
		}
		if !isClientSymbolName(name) {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		symbols = append(symbols, name)
	})
	return symbols
}

// isClientSymbolName reports whether name matches the XxxClient pattern:
// starts with an upper-case letter and ends with "Client".
func isClientSymbolName(name string) bool {
	if len(name) < 7 { // shortest valid: "AClient" = 7 chars
		return false
	}
	runes := []rune(name)
	if runes[0] < 'A' || runes[0] > 'Z' {
		return false
	}
	return strings.HasSuffix(name, "Client")
}

// javaScriptHookCallNames returns the unique, source-order names of React hook
// calls. A hook call is a call_expression whose callee resolves to a hook name
// (use[A-Z][A-Za-z0-9_]*) either as a bare identifier (useState(...)) or as the
// property of a member-expression callee (React.useState(...)). The member case
// preserves the legitimate matches the prior raw-source regex produced while the
// structural walk still ignores hook-shaped tokens inside comments and string
// literals, which the regex matched in error.
func javaScriptHookCallNames(root *tree_sitter.Node, source []byte) []string {
	names := make([]string, 0)
	seen := make(map[string]struct{})
	if root == nil {
		return names
	}
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "call_expression" {
			return
		}
		fn := node.ChildByFieldName("function")
		if fn == nil {
			return
		}
		var nameNode *tree_sitter.Node
		switch fn.Kind() {
		case "identifier":
			nameNode = fn
		case "member_expression":
			nameNode = fn.ChildByFieldName("property")
		}
		if nameNode == nil {
			return
		}
		name := strings.TrimSpace(nodeText(nameNode, source))
		if !isHookName(name) {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		names = append(names, name)
	})
	return names
}

// isHookName reports whether name follows the React hook convention:
// starts with "use" followed by an upper-case letter.
func isHookName(name string) bool {
	if len(name) < 4 {
		return false
	}
	if name[:3] != "use" {
		return false
	}
	r := rune(name[3])
	return r >= 'A' && r <= 'Z'
}

// javaScriptImportServiceSlugs collects the trailing slug matched by pattern from
// each import/require module specifier, deduplicated in source order. The slug
// regex is a within-string-content exception applied to the isolated specifier.
func javaScriptImportServiceSlugs(root *tree_sitter.Node, source []byte, pattern *regexp.Regexp) []string {
	slugs := make([]string, 0)
	seen := make(map[string]struct{})
	for _, specifier := range javaScriptImportModuleSpecifiers(root, source) {
		match := pattern.FindStringSubmatch(strings.TrimSpace(specifier))
		if len(match) != 2 {
			continue
		}
		slug := strings.TrimSpace(match[1])
		if slug == "" {
			continue
		}
		if _, ok := seen[slug]; ok {
			continue
		}
		seen[slug] = struct{}{}
		slugs = append(slugs, slug)
	}
	return slugs
}
