// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kotlin

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// handleFunctionDeclaration emits one function or secondary-constructor row and
// recurses into the body under the function scope.
func (w *astWalker) handleFunctionDeclaration(node *tree_sitter.Node, f frame) {
	if node.Kind() == "secondary_constructor" {
		w.handleSecondaryConstructor(node, f)
		return
	}

	name := strings.TrimSpace(shared.NodeText(node.ChildByFieldName("name"), w.source))
	if name == "" {
		w.walkChildren(node, f)
		return
	}

	line := shared.NodeLine(node)
	item := map[string]any{
		"name":                  name,
		"line_number":           line,
		"end_line":              shared.NodeEndLine(node),
		"lang":                  "kotlin",
		"decorators":            []string{},
		"cyclomatic_complexity": kotlinCyclomaticComplexity(node, w.source),
	}
	if w.functionIsSuspend(node) {
		item["suspend"] = true
	}

	classContext := f.classContext
	if extensionReceiver := w.extensionReceiver(node); extensionReceiver != "" {
		item["extension_receiver"] = extensionReceiver
		if classContext == "" {
			classContext = extensionReceiver
		}
	}
	if classContext != "" {
		item["class_context"] = classContext
	}

	annotations := w.annotations(node)
	scopeKind := ""
	if f.classContext != "" {
		scopeKind = w.scopeKindFor(f.classContext)
	}
	if rootKinds := kotlinFunctionDeadCodeRootKinds(
		w.functionIsOverride(node),
		w.functionFirstParameterType(node),
		annotations,
		name,
		f.classContext,
		scopeKind,
		w.interfaceMethods,
		w.classInterfaces,
	); len(rootKinds) > 0 {
		item["dead_code_root_kinds"] = rootKinds
	}

	if w.indexSource {
		item["source"] = w.firstLineText(node)
	}
	shared.AppendBucket(w.payload, "functions", item)

	w.walkChildren(node, f.withFunction(name))
}

// handleSecondaryConstructor emits one secondary-constructor row.
func (w *astWalker) handleSecondaryConstructor(node *tree_sitter.Node, f frame) {
	item := map[string]any{
		"name":                  "constructor",
		"line_number":           shared.NodeLine(node),
		"end_line":              shared.NodeEndLine(node),
		"constructor_kind":      "secondary",
		"lang":                  "kotlin",
		"decorators":            []string{},
		"cyclomatic_complexity": kotlinCyclomaticComplexity(node, w.source),
	}
	if f.classContext != "" {
		item["class_context"] = f.classContext
	}
	item["dead_code_root_kinds"] = kotlinConstructorDeadCodeRootKinds()
	if w.indexSource {
		item["source"] = w.firstLineText(node)
	}
	shared.AppendBucket(w.payload, "functions", item)

	w.walkChildren(node, f.withFunction("constructor"))
}

// scopeKindFor reports whether the enclosing type name is an interface or a
// class, used by dead-code-root classification.
func (w *astWalker) scopeKindFor(typeName string) string {
	if _, ok := w.interfaceMethods[typeName]; ok {
		return "interface"
	}
	if _, ok := w.classInterfaces[typeName]; ok {
		return "class"
	}
	if _, ok := w.classTypeParameters[typeName]; ok {
		return "class"
	}
	if _, ok := w.classPropertyTypes[typeName]; ok {
		return "class"
	}
	if _, ok := w.knownTypeNames[typeName]; ok {
		return "class"
	}
	return ""
}

// functionIsSuspend reports whether a function declaration carries the suspend
// modifier.
func (w *astWalker) functionIsSuspend(node *tree_sitter.Node) bool {
	modifiers := w.childByKind(node, "modifiers")
	if modifiers == nil {
		return false
	}
	found := false
	shared.WalkNamed(modifiers, func(child *tree_sitter.Node) {
		if child.Kind() == "function_modifier" && strings.TrimSpace(shared.NodeText(child, w.source)) == "suspend" {
			found = true
		}
	})
	return found
}

// functionIsOverride reports whether a function declaration carries the
// override member modifier.
func (w *astWalker) functionIsOverride(node *tree_sitter.Node) bool {
	modifiers := w.childByKind(node, "modifiers")
	if modifiers == nil {
		return false
	}
	found := false
	shared.WalkNamed(modifiers, func(child *tree_sitter.Node) {
		if child.Kind() == "member_modifier" && strings.TrimSpace(shared.NodeText(child, w.source)) == "override" {
			found = true
		}
	})
	return found
}

// extensionReceiver returns the receiver type name of an extension function, or
// "" for a non-extension function. The grammar places a user_type before the
// name field for `fun Receiver.method()`.
func (w *astWalker) extensionReceiver(node *tree_sitter.Node) string {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return ""
	}
	nameStart := nameNode.StartByte()
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		if child.Kind() == "user_type" && child.StartByte() < nameStart {
			return kotlinBaseTypeName(shared.NodeText(child, w.source))
		}
	}
	return ""
}

// functionFirstParameterType reports whether a function has exactly one
// parameter, returning a non-empty marker so Gradle task-setter detection can
// run without re-parsing the declaration text.
func (w *astWalker) functionFirstParameterType(node *tree_sitter.Node) bool {
	params := w.childByKind(node, "function_value_parameters")
	if params == nil {
		return false
	}
	count := 0
	cursor := params.Walk()
	defer cursor.Close()
	for _, child := range params.NamedChildren(cursor) {
		if child.Kind() == "parameter" {
			count++
		}
	}
	return count == 1
}

// annotations returns the short annotation names attached to a declaration
// node's modifiers list.
func (w *astWalker) annotations(node *tree_sitter.Node) []string {
	modifiers := w.childByKind(node, "modifiers")
	if modifiers == nil {
		return nil
	}
	var names []string
	cursor := modifiers.Walk()
	defer cursor.Close()
	for _, child := range modifiers.NamedChildren(cursor) {
		child := child
		if child.Kind() != "annotation" {
			continue
		}
		userType := w.annotationUserType(&child)
		if userType == nil {
			continue
		}
		if short := kotlinShortName(shared.NodeText(userType, w.source)); short != "" {
			names = append(names, short)
		}
	}
	return names
}

// annotationUserType returns the user_type naming an annotation, whether it is
// a bare `@Name` (direct user_type child) or a `@Name(args)` form (wrapped in a
// constructor_invocation).
func (w *astWalker) annotationUserType(annotation *tree_sitter.Node) *tree_sitter.Node {
	if userType := w.childByKind(annotation, "user_type"); userType != nil {
		return userType
	}
	if invocation := w.childByKind(annotation, "constructor_invocation"); invocation != nil {
		return w.childByKind(invocation, "user_type")
	}
	return nil
}

// declaredTypeParameters returns the class/interface type parameter names.
func (w *astWalker) declaredTypeParameters(node *tree_sitter.Node) []string {
	params := w.childByKind(node, "type_parameters")
	if params == nil {
		return nil
	}
	var names []string
	cursor := params.Walk()
	defer cursor.Close()
	for _, child := range params.NamedChildren(cursor) {
		child := child
		if child.Kind() != "type_parameter" {
			continue
		}
		identifier := w.childByKind(&child, "identifier")
		if identifier == nil {
			continue
		}
		if name := strings.TrimSpace(shared.NodeText(identifier, w.source)); name != "" {
			names = append(names, name)
		}
	}
	return names
}

// implementedTypes returns the names listed in a class delegation_specifiers
// node (the `: A, B` supertype list).
func (w *astWalker) implementedTypes(node *tree_sitter.Node) []string {
	specifiers := w.childByKind(node, "delegation_specifiers")
	if specifiers == nil {
		return nil
	}
	var names []string
	cursor := specifiers.Walk()
	defer cursor.Close()
	for _, child := range specifiers.NamedChildren(cursor) {
		child := child
		if child.Kind() != "delegation_specifier" {
			continue
		}
		userType := w.childByKind(&child, "user_type")
		if userType == nil {
			if invocation := w.childByKind(&child, "constructor_invocation"); invocation != nil {
				userType = w.childByKind(invocation, "user_type")
			}
		}
		if userType == nil {
			continue
		}
		if name := kotlinShortName(kotlinBaseTypeName(shared.NodeText(userType, w.source))); name != "" {
			names = append(names, name)
		}
	}
	return names
}

// collectInterfaceMethods records the method names declared on an interface.
func (w *astWalker) collectInterfaceMethods(name string, node *tree_sitter.Node) {
	body := w.childByKind(node, "class_body")
	if body == nil {
		return
	}
	cursor := body.Walk()
	defer cursor.Close()
	for _, child := range body.NamedChildren(cursor) {
		child := child
		if child.Kind() != "function_declaration" {
			continue
		}
		methodName := strings.TrimSpace(shared.NodeText(child.ChildByFieldName("name"), w.source))
		if methodName == "" {
			continue
		}
		if _, ok := w.interfaceMethods[name]; !ok {
			w.interfaceMethods[name] = make(map[string]struct{})
		}
		w.interfaceMethods[name][methodName] = struct{}{}
	}
}

// firstLineText returns the full physical source line that a node starts on,
// including original indentation. This matches the previous IndexSource
// behavior that stored the raw declaration line.
func (w *astWalker) firstLineText(node *tree_sitter.Node) string {
	row := int(node.StartPosition().Row)
	lineStart := 0
	current := 0
	for i := 0; i < len(w.source); i++ {
		if w.source[i] != '\n' {
			continue
		}
		if current == row {
			return string(w.source[lineStart:i])
		}
		current++
		lineStart = i + 1
	}
	if current == row {
		return string(w.source[lineStart:])
	}
	return ""
}
