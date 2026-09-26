// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package symbols

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// CallArgumentNodes returns the direct argument nodes of a call_expression
// node's argument_list child.
func CallArgumentNodes(node *tree_sitter.Node) []*tree_sitter.Node {
	args := make([]*tree_sitter.Node, 0)
	WalkDirectNamed(node, func(child *tree_sitter.Node) {
		if child.Kind() != "argument_list" {
			return
		}
		WalkDirectNamed(child, func(arg *tree_sitter.Node) {
			args = append(args, arg)
		})
	})
	return args
}

// CallIsFmtFormatting reports whether node calls one of the fmt package's
// string-formatting functions (Sprint, Sprintln, Sprintf, Fprint, Fprintln,
// Fprintf), resolved through importAliases.
func CallIsFmtFormatting(node *tree_sitter.Node, source []byte, importAliases map[string][]string) bool {
	functionName := QualifiedCallFunctionName(node, source, importAliases)
	switch functionName {
	case "fmt.sprint", "fmt.sprintln", "fmt.sprintf", "fmt.fprint", "fmt.fprintln", "fmt.fprintf":
		return true
	default:
		return false
	}
}

// FmtStringerFirstValueArgIndex returns the argument index of the first
// value (non-format-string, non-writer) argument for a known fmt formatting
// call, as classified by CallIsFmtFormatting.
func FmtStringerFirstValueArgIndex(node *tree_sitter.Node, source []byte, importAliases map[string][]string) int {
	functionName := QualifiedCallFunctionName(node, source, importAliases)
	switch functionName {
	case "fmt.sprint", "fmt.sprintln":
		return 0
	case "fmt.sprintf", "fmt.fprint", "fmt.fprintln":
		return 1
	case "fmt.fprintf":
		return 2
	default:
		return 0
	}
}

// QualifiedCallFunctionName returns the lower-cased "<import path>.<field>"
// name for a call_expression whose function is a selector_expression on an
// imported package alias, or "" when the call does not resolve that way.
func QualifiedCallFunctionName(node *tree_sitter.Node, source []byte, importAliases map[string][]string) string {
	functionNode := node.ChildByFieldName("function")
	if functionNode == nil || functionNode.Kind() != "selector_expression" {
		return ""
	}
	base, field, ok := SelectorBaseAndField(functionNode, source)
	if !ok {
		return ""
	}
	base = strings.TrimSpace(base)
	field = strings.TrimSpace(field)
	if base == "" || field == "" {
		return ""
	}
	// Resolve through the sorted path list so an alias bound by several import
	// paths always picks the same one (issue #6947).
	if paths := ImportPathsForAlias(base, importAliases); len(paths) > 0 {
		return strings.ToLower(paths[0] + "." + field)
	}
	return ""
}
