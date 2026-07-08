// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript

import (
	"sort"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// jsChildProcessCalls is the set of Node.js child_process APIs that spawn an
// operating-system process. A call to any of these is reported as an embedded
// shell command.
var jsChildProcessCalls = map[string]struct{}{
	"exec":         {},
	"execSync":     {},
	"execFile":     {},
	"execFileSync": {},
	"spawn":        {},
	"spawnSync":    {},
	"fork":         {},
}

type jsEmbeddedShellCommand struct {
	functionName       string
	functionLineNumber int
	lineNumber         int
	api                string
	language           string
}

type jsShellImports struct {
	moduleAliases map[string]struct{}
	directCalls   map[string]string
}

// jsShellFunction is one enclosing named function: a function_declaration or a
// named function_expression whose body may contain child_process calls.
type jsShellFunction struct {
	name       string
	lineNumber int
	body       *tree_sitter.Node
}

func embeddedShellCommandPayloads(root *tree_sitter.Node, source []byte, language string) []map[string]any {
	commands := embeddedShellCommands(root, source, language)
	if len(commands) == 0 {
		return []map[string]any{}
	}
	payload := make([]map[string]any, 0, len(commands))
	for _, command := range commands {
		payload = append(payload, map[string]any{
			"function_name":        command.functionName,
			"function_line_number": command.functionLineNumber,
			"line_number":          command.lineNumber,
			"api":                  command.api,
			"language":             command.language,
		})
	}
	return payload
}

func embeddedShellCommands(root *tree_sitter.Node, source []byte, language string) []jsEmbeddedShellCommand {
	if root == nil {
		return nil
	}
	imports, functions := jsShellRootScan(root, source)
	if len(imports.moduleAliases) == 0 && len(imports.directCalls) == 0 {
		return nil
	}

	var commands []jsEmbeddedShellCommand
	for _, function := range functions {
		walkNamed(function.body, func(node *tree_sitter.Node) {
			if node.Kind() != "call_expression" {
				return
			}
			api, alias := jsChildProcessCallAPI(node, source, imports)
			if api == "" {
				return
			}
			if jsIdentifierShadowedBeforeNode(function.body, node, alias, source) {
				return
			}
			commands = append(commands, jsEmbeddedShellCommand{
				functionName:       function.name,
				functionLineNumber: function.lineNumber,
				lineNumber:         nodeLine(node),
				api:                api,
				language:           language,
			})
		})
	}
	sort.Slice(commands, func(i, j int) bool {
		if commands[i].lineNumber != commands[j].lineNumber {
			return commands[i].lineNumber < commands[j].lineNumber
		}
		return commands[i].api < commands[j].api
	})
	return commands
}

// jsChildProcessCallAPI classifies a call_expression as an embedded shell
// command and returns its "child_process.<api>" label plus the alias whose
// in-function shadowing must be checked. It returns an empty api when the call
// is not a child_process invocation. Member calls (alias.exec(...)) bind on a
// module alias; bare calls (exec(...)) bind on a destructured/named import.
func jsChildProcessCallAPI(node *tree_sitter.Node, source []byte, imports jsShellImports) (string, string) {
	functionNode := node.ChildByFieldName("function")
	if functionNode == nil {
		return "", ""
	}
	switch functionNode.Kind() {
	case "member_expression":
		objectNode := functionNode.ChildByFieldName("object")
		propertyNode := functionNode.ChildByFieldName("property")
		if objectNode == nil || objectNode.Kind() != "identifier" || propertyNode == nil {
			return "", ""
		}
		alias := strings.TrimSpace(nodeText(objectNode, source))
		if _, ok := imports.moduleAliases[alias]; !ok {
			return "", ""
		}
		call := strings.TrimSpace(nodeText(propertyNode, source))
		if _, ok := jsChildProcessCalls[call]; !ok {
			return "", ""
		}
		return "child_process." + call, alias
	case "identifier":
		local := strings.TrimSpace(nodeText(functionNode, source))
		if api, ok := imports.directCalls[local]; ok {
			return api, local
		}
	}
	return "", ""
}

// jsShellRootScan collects child_process import/require bindings and every
// named enclosing function in a single root traversal. The two results are
// independent -- import bindings never gate which functions are collected,
// and function collection never reads the bindings -- so combining them into
// one walkNamed pass over root produces the identical imports and functions
// values that two separate full-tree walks (jsShellImportAliases and
// jsShellEnclosingFunctions historically) would, while visiting the tree
// once instead of twice.
//
// Import bindings come from import_statement and require variable_declarator
// nodes. Module aliases come from default/namespace imports and
// `const alias = require("child_process")`; direct-call bindings come from
// named imports and `const { exec } = require("child_process")`, restricted
// to the known process-spawning APIs. The "node:" specifier prefix is
// accepted for both forms.
//
// Functions are every named function whose body could host a child_process
// call: function_declaration and named function_expression nodes. A call
// nested inside multiple such functions is attributed to each enclosing
// function, matching the per-function scan semantics relied upon by callers.
func jsShellRootScan(root *tree_sitter.Node, source []byte) (jsShellImports, []jsShellFunction) {
	imports := jsShellImports{moduleAliases: map[string]struct{}{}, directCalls: map[string]string{}}
	functions := make([]jsShellFunction, 0)
	walkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "import_statement":
			jsCollectShellImportStatement(node, source, &imports)
		case "variable_declarator":
			jsCollectShellRequireDeclarator(node, source, &imports)
		case "function_declaration", "function_expression":
			nameNode := node.ChildByFieldName("name")
			if nameNode == nil {
				return
			}
			name := strings.TrimSpace(nodeText(nameNode, source))
			if name == "" {
				return
			}
			body := node.ChildByFieldName("body")
			if body == nil {
				return
			}
			functions = append(functions, jsShellFunction{
				name:       name,
				lineNumber: nodeLine(node),
				body:       body,
			})
		}
	})
	return imports, functions
}

func jsCollectShellImportStatement(node *tree_sitter.Node, source []byte, imports *jsShellImports) {
	sourceNode := node.ChildByFieldName("source")
	if !jsIsChildProcessSpecifier(sourceNode, source) {
		return
	}
	walkNamed(node, func(child *tree_sitter.Node) {
		switch child.Kind() {
		case "namespace_import":
			if alias := javaScriptNamespaceImportAlias(child, source); alias != "" {
				imports.moduleAliases[alias] = struct{}{}
			}
		case "import_clause":
			// A bare default import is an identifier child of the clause.
			cursor := child.Walk()
			for _, clauseChild := range child.NamedChildren(cursor) {
				clauseChild := clauseChild
				if clauseChild.Kind() == "identifier" {
					if alias := strings.TrimSpace(nodeText(&clauseChild, source)); alias != "" {
						imports.moduleAliases[alias] = struct{}{}
					}
				}
			}
			cursor.Close()
		case "import_specifier":
			imported := strings.TrimSpace(nodeText(child.ChildByFieldName("name"), source))
			local := imported
			if aliasNode := child.ChildByFieldName("alias"); aliasNode != nil {
				if aliasText := strings.TrimSpace(nodeText(aliasNode, source)); aliasText != "" {
					local = aliasText
				}
			}
			if _, ok := jsChildProcessCalls[imported]; ok && local != "" {
				imports.directCalls[local] = "child_process." + imported
			}
		}
	})
}

func jsCollectShellRequireDeclarator(node *tree_sitter.Node, source []byte, imports *jsShellImports) {
	valueNode := node.ChildByFieldName("value")
	if !jsIsChildProcessRequireCall(valueNode, source) {
		return
	}
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	switch nameNode.Kind() {
	case "identifier":
		if alias := strings.TrimSpace(nodeText(nameNode, source)); alias != "" {
			imports.moduleAliases[alias] = struct{}{}
		}
	case "object_pattern":
		cursor := nameNode.Walk()
		for _, child := range nameNode.NamedChildren(cursor) {
			child := child
			imported, local := jsObjectPatternBinding(&child, source)
			if imported == "" || local == "" {
				continue
			}
			if _, ok := jsChildProcessCalls[imported]; ok {
				imports.directCalls[local] = "child_process." + imported
			}
		}
		cursor.Close()
	}
}

// jsObjectPatternBinding returns the imported name and local binding name for one
// destructuring element of a require object_pattern. A shorthand binding
// ({ exec }) maps a name to itself; a renamed binding ({ exec: run }) maps the
// source property to its local alias.
func jsObjectPatternBinding(node *tree_sitter.Node, source []byte) (string, string) {
	if node == nil {
		return "", ""
	}
	switch node.Kind() {
	case "shorthand_property_identifier_pattern":
		name := strings.TrimSpace(nodeText(node, source))
		return name, name
	case "pair_pattern":
		keyNode := node.ChildByFieldName("key")
		valueNode := node.ChildByFieldName("value")
		imported := strings.TrimSpace(nodeText(keyNode, source))
		local := strings.TrimSpace(nodeText(valueNode, source))
		return imported, local
	}
	return "", ""
}

// jsIsChildProcessRequireCall reports whether node is a require("child_process")
// call_expression, accepting the optional "node:" specifier prefix.
func jsIsChildProcessRequireCall(node *tree_sitter.Node, source []byte) bool {
	if node == nil || node.Kind() != "call_expression" {
		return false
	}
	functionNode := node.ChildByFieldName("function")
	if functionNode == nil || strings.TrimSpace(nodeText(functionNode, source)) != "require" {
		return false
	}
	argumentsNode := node.ChildByFieldName("arguments")
	if argumentsNode == nil {
		return false
	}
	cursor := argumentsNode.Walk()
	defer cursor.Close()
	argChildren := argumentsNode.NamedChildren(cursor)
	if len(argChildren) != 1 {
		return false
	}
	specifier := argChildren[0]
	return jsIsChildProcessSpecifier(&specifier, source)
}

// jsIsChildProcessSpecifier reports whether a string-literal node resolves to the
// child_process module, accepting the optional "node:" prefix. The string node's
// fragment child holds the literal text without surrounding quotes.
func jsIsChildProcessSpecifier(node *tree_sitter.Node, source []byte) bool {
	if node == nil || node.Kind() != "string" {
		return false
	}
	value := strings.TrimSpace(jsStringLiteralValue(node, source))
	return value == "child_process" || value == "node:child_process"
}

// jsStringLiteralValue returns the unquoted content of a string node by reading
// its string_fragment child, falling back to trimming the quote bytes.
func jsStringLiteralValue(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		if child.Kind() == "string_fragment" {
			return nodeText(&child, source)
		}
	}
	text := strings.TrimSpace(nodeText(node, source))
	if unquoted, ok := trimJavaScriptQuotes(text); ok {
		return unquoted
	}
	return text
}

// jsIdentifierShadowedBeforeNode reports whether identifier is re-bound inside
// body before call, mirroring the prior "local re-binding before the call site"
// guard: an assignment_expression or a declarator targeting identifier that ends
// at or before the call's start byte shadows the imported binding.
func jsIdentifierShadowedBeforeNode(body *tree_sitter.Node, call *tree_sitter.Node, identifier string, source []byte) bool {
	if body == nil || call == nil || identifier == "" {
		return false
	}
	callStart := call.StartByte()
	shadowed := false
	walkNamed(body, func(node *tree_sitter.Node) {
		if shadowed || node.StartByte() >= callStart {
			return
		}
		switch node.Kind() {
		case "assignment_expression":
			leftNode := node.ChildByFieldName("left")
			if leftNode != nil && leftNode.Kind() == "identifier" &&
				strings.TrimSpace(nodeText(leftNode, source)) == identifier {
				shadowed = true
			}
		case "variable_declarator":
			nameNode := node.ChildByFieldName("name")
			if nameNode != nil && nameNode.Kind() == "identifier" &&
				strings.TrimSpace(nodeText(nameNode, source)) == identifier {
				shadowed = true
			}
		}
	})
	return shadowed
}
