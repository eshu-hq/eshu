// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package python

import (
	"sort"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

var pythonSubprocessCalls = map[string]struct{}{
	"Popen":        {},
	"run":          {},
	"call":         {},
	"check_call":   {},
	"check_output": {},
}

type embeddedShellCommand struct {
	functionName       string
	functionLineNumber int
	lineNumber         int
	api                string
	language           string
}

type pythonShellImports struct {
	moduleAliases map[string]string
	directCalls   map[string]string
}

func embeddedShellCommandPayloads(root *tree_sitter.Node, source []byte) []map[string]any {
	commands := embeddedShellCommands(root, source)
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

// embeddedShellCommands reports subprocess/os shell entry points reached from a
// function body. Calls are resolved from the tree-sitter call nodes: the called
// API is matched against the file's subprocess/os import aliases, the owning
// function is the outermost enclosing function_definition (module-level calls
// are not reported), and a call is dropped when its alias was reassigned earlier
// in that function. Results are ordered by line number then API for a stable
// payload.
func embeddedShellCommands(root *tree_sitter.Node, source []byte) []embeddedShellCommand {
	if root == nil {
		return nil
	}
	imports := pythonShellImportAliases(root, source)
	if len(imports.moduleAliases) == 0 && len(imports.directCalls) == 0 {
		return nil
	}

	var commands []embeddedShellCommand
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "call" {
			return
		}
		api, aliasName, ok := imports.resolveCall(node, source)
		if !ok {
			return
		}
		function := pythonOutermostFunction(node)
		if function == nil {
			return
		}
		callLine := nodeLine(node)
		if pythonAliasReassignedBefore(function, source, aliasName, callLine) {
			return
		}
		commands = append(commands, embeddedShellCommand{
			functionName:       strings.TrimSpace(nodeText(function.ChildByFieldName("name"), source)),
			functionLineNumber: nodeLine(function),
			lineNumber:         callLine,
			api:                api,
			language:           "python",
		})
	})

	sort.Slice(commands, func(i, j int) bool {
		if commands[i].lineNumber != commands[j].lineNumber {
			return commands[i].lineNumber < commands[j].lineNumber
		}
		return commands[i].api < commands[j].api
	})
	return commands
}

// resolveCall maps a call node to its shell API and the alias identifier that
// must not be shadowed. A direct call (from-import) keys on the function
// identifier; an attribute call keys on a bare module-alias object.
func (imports pythonShellImports) resolveCall(call *tree_sitter.Node, source []byte) (string, string, bool) {
	function := call.ChildByFieldName("function")
	if function == nil {
		return "", "", false
	}
	switch function.Kind() {
	case "identifier":
		name := nodeText(function, source)
		if api, ok := imports.directCalls[name]; ok {
			return api, name, true
		}
	case "attribute":
		object := function.ChildByFieldName("object")
		if object == nil || object.Kind() != "identifier" {
			return "", "", false
		}
		alias := nodeText(object, source)
		module, ok := imports.moduleAliases[alias]
		if !ok {
			return "", "", false
		}
		attribute := nodeText(function.ChildByFieldName("attribute"), source)
		switch module {
		case "subprocess":
			if _, ok := pythonSubprocessCalls[attribute]; ok {
				return "subprocess." + attribute, alias, true
			}
		case "os":
			if attribute == "system" {
				return "os.system", alias, true
			}
		}
	}
	return "", "", false
}

// pythonOutermostFunction returns the highest enclosing function_definition, or
// nil when the node sits at module scope. Using the outermost function keeps the
// owning-function attribution stable for calls nested inside inner functions.
func pythonOutermostFunction(node *tree_sitter.Node) *tree_sitter.Node {
	var outermost *tree_sitter.Node
	for current := node.Parent(); current != nil; current = current.Parent() {
		if current.Kind() == "function_definition" {
			outermost = current
		}
	}
	return outermost
}

// pythonAliasReassignedBefore reports whether a plain assignment binds alias to a
// new value on a line before callLine within scope. Annotated assignments are
// ignored so the check matches a `name =` rebind rather than a `name: T`
// declaration.
func pythonAliasReassignedBefore(scope *tree_sitter.Node, source []byte, alias string, callLine int) bool {
	if alias == "" {
		return false
	}
	reassigned := false
	walkNamed(scope, func(node *tree_sitter.Node) {
		if reassigned || node.Kind() != "assignment" {
			return
		}
		if node.ChildByFieldName("type") != nil {
			return
		}
		left := node.ChildByFieldName("left")
		if left == nil || left.Kind() != "identifier" {
			return
		}
		if nodeText(left, source) != alias {
			return
		}
		if nodeLine(node) < callLine {
			reassigned = true
		}
	})
	return reassigned
}

// pythonShellImportAliases collects subprocess/os module aliases and the
// from-import local names that resolve to a shell API.
func pythonShellImportAliases(root *tree_sitter.Node, source []byte) pythonShellImports {
	imports := pythonShellImports{moduleAliases: map[string]string{}, directCalls: map[string]string{}}
	walkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "import_statement":
			collectPythonModuleAliases(node, source, imports.moduleAliases)
		case "import_from_statement":
			collectPythonDirectImports(node, source, imports.directCalls)
		}
	})
	return imports
}

func collectPythonModuleAliases(node *tree_sitter.Node, source []byte, out map[string]string) {
	for i := uint(0); i < node.ChildCount(); i++ {
		if node.FieldNameForChild(uint32(i)) != "name" {
			continue
		}
		child := node.Child(i)
		var moduleNode *tree_sitter.Node
		alias := ""
		switch child.Kind() {
		case "dotted_name":
			moduleNode = child
		case "aliased_import":
			moduleNode = child.ChildByFieldName("name")
			alias = strings.TrimSpace(nodeText(child.ChildByFieldName("alias"), source))
		default:
			continue
		}
		module := strings.TrimSpace(nodeText(moduleNode, source))
		if module != "subprocess" && module != "os" {
			continue
		}
		if alias == "" {
			alias = module
		}
		out[alias] = module
	}
}

func collectPythonDirectImports(node *tree_sitter.Node, source []byte, out map[string]string) {
	module := strings.TrimSpace(nodeText(node.ChildByFieldName("module_name"), source))
	if module != "subprocess" && module != "os" {
		return
	}
	for i := uint(0); i < node.ChildCount(); i++ {
		if node.FieldNameForChild(uint32(i)) != "name" {
			continue
		}
		child := node.Child(i)
		local := ""
		imported := ""
		switch child.Kind() {
		case "dotted_name":
			imported = strings.TrimSpace(nodeText(child, source))
			local = imported
		case "aliased_import":
			imported = strings.TrimSpace(nodeText(child.ChildByFieldName("name"), source))
			local = strings.TrimSpace(nodeText(child.ChildByFieldName("alias"), source))
		default:
			continue
		}
		if local == "" || imported == "" {
			continue
		}
		switch module {
		case "subprocess":
			if _, ok := pythonSubprocessCalls[imported]; ok {
				out[local] = "subprocess." + imported
			}
		case "os":
			if imported == "system" {
				out[local] = "os.system"
			}
		}
	}
}
