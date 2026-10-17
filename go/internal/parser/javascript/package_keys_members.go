// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript

import (
	"bytes"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// This file stamps the #8280 static member keys on top of the #7601 package
// keys. A static method of an exported class carries export_name
// <Export>.<method>, where <Export> is the class's own export name ("default"
// only for the main or module entry file's default export). A consumer call
// carries the same key when the class is bound to a bare package import
// (`C.m(`, `ns.C.m(`, `D.m(` for a default import) and the closed import-name
// rule of package_keys_closed.go holds. Each key holds a '.', and a #7601
// export name is one identifier or "default", so a member key never names a
// top-level export: `D.m()` on a default import keys default.m, never m.
// Instance methods are not keyed: a parser cannot prove which object a
// receiver holds.

// staticMemberSeparator joins a class export name and a static method.
const staticMemberSeparator = "."

// stampMethod adds the producer keys to item when node is a public, named
// method of a class that the stamper keys as a package export. A nil item is
// ignored.
func (s packageExportStamper) stampMethod(item map[string]any, node *tree_sitter.Node) {
	if item == nil || s.producerName == "" || node == nil {
		return
	}
	name, _ := item["name"].(string)
	exportName := s.methodExportName(node, name)
	if exportName == "" {
		return
	}
	item["package_id"] = s.producerName
	item["export_name"] = exportName
}

// methodExportName returns the member key export name of a method_definition,
// or "" when another package cannot call it through the class: the
// constructor, a get or set accessor, a computed, string, numeric, or #private
// name, a TypeScript private or protected method, a method of an object
// literal or a class expression, and any method of a class that is not a
// top-level package export.
func (s packageExportStamper) methodExportName(node *tree_sitter.Node, name string) string {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil || nameNode.Kind() != "property_identifier" || !isJSIdentifier(name) || name == "constructor" {
		return ""
	}
	static := false
	for index := uint(0); index < node.ChildCount(); index++ {
		child := node.Child(index)
		if child == nil {
			continue
		}
		switch child.Kind() {
		case "get", "set":
			if !child.IsNamed() {
				return ""
			}
		case "static":
			static = static || !child.IsNamed()
		case "accessibility_modifier":
			if !methodAccessibilityIsPublic(child) {
				return ""
			}
		}
	}
	body := s.parents.Parent(node)
	if body == nil || body.Kind() != "class_body" {
		return ""
	}
	class := s.parents.Parent(body)
	if class == nil || (class.Kind() != "class_declaration" && class.Kind() != "abstract_class_declaration") {
		return ""
	}
	className := strings.TrimSpace(shared.NodeText(class.ChildByFieldName("name"), s.source))
	classExport := s.exportName(class, className)
	if classExport == "" {
		return ""
	}
	if _, shadowed := s.shadowedMembers(class, body)[name]; shadowed {
		return ""
	}
	if !static {
		return ""
	}
	return classExport + staticMemberSeparator + name
}

// methodAccessibilityIsPublic reports whether a TypeScript accessibility
// modifier is `public`.
func methodAccessibilityIsPublic(modifier *tree_sitter.Node) bool {
	for index := uint(0); index < modifier.ChildCount(); index++ {
		if child := modifier.Child(index); child != nil && child.Kind() == "public" {
			return true
		}
	}
	return false
}

// memberCallTarget maps a call's full name to the import binding that owns it
// and the export name its key names. It covers the #7601 shapes (`local(` and
// `ns.member(`, unchanged) and the #8280 static shapes (`C.m(` and `D.m(` on a
// named or default import, `ns.C.m(` on a namespace import), which apply to
// function_call rows only. A member call on any other receiver is not keyed.
func memberCallTarget(
	fullName string,
	name string,
	callKind string,
	bindings map[string]packageImportBinding,
) (localName string, exportName string) {
	if fullName == name {
		if binding, ok := bindings[name]; ok && binding.exportName != "" {
			return name, binding.exportName
		}
		return "", ""
	}
	parts := strings.Split(fullName, ".")
	if len(parts) < 2 || len(parts) > 3 || parts[len(parts)-1] != name {
		return "", ""
	}
	for _, part := range parts {
		if !isJSIdentifier(part) {
			return "", ""
		}
	}
	binding, bound := bindings[parts[0]]
	if !bound {
		return "", ""
	}
	if len(parts) == 2 {
		switch {
		case binding.exportName == "":
			return parts[0], name
		case callKind == "function_call":
			return parts[0], binding.exportName + staticMemberSeparator + name
		}
		return "", ""
	}
	if callKind == "function_call" && binding.exportName == "" {
		return parts[0], parts[1] + staticMemberSeparator + name
	}
	return "", ""
}

// shadowedMembers returns, memoized per class, the member names that a class
// field, a TypeScript parameter property, or a `this.<name> =` assignment
// anywhere in the class body also defines. Such a property replaces the
// method on the instance (or, for a static field or a `this` write in a static
// member, on the class), so a call by that name may never reach the method.
// The set is not split by static and instance: a name defined both ways keys
// neither method, which only costs recall.
func (s packageExportStamper) shadowedMembers(class *tree_sitter.Node, body *tree_sitter.Node) map[string]struct{} {
	if cached, ok := s.shadowed[class.Id()]; ok {
		return cached
	}
	names := map[string]struct{}{}
	cursor := body.Walk()
	defer cursor.Close()
	if cursor.GotoFirstChild() {
		for {
			node := cursor.Node()
			if node.IsNamed() {
				s.recordShadow(node, names)
			}
			if node.IsNamed() && cursor.GotoFirstChild() {
				continue
			}
			done := false
			for !cursor.GotoNextSibling() {
				if !cursor.GotoParent() || cursor.Node().Id() == body.Id() {
					done = true
					break
				}
			}
			if done {
				break
			}
		}
	}
	s.shadowed[class.Id()] = names
	return names
}

// recordShadow adds the member name node defines, if it is a field, a
// parameter property, or a `this.<name>` write.
func (s packageExportStamper) recordShadow(node *tree_sitter.Node, names map[string]struct{}) {
	switch node.Kind() {
	case "public_field_definition":
		s.addShadow(node.ChildByFieldName("name"), names)
	case "field_definition":
		s.addShadow(node.ChildByFieldName("property"), names)
	case "assignment_expression", "augmented_assignment_expression":
		left := node.ChildByFieldName("left")
		if left == nil || left.Kind() != "member_expression" {
			return
		}
		if object := left.ChildByFieldName("object"); object != nil && object.Kind() == "this" {
			s.addShadow(left.ChildByFieldName("property"), names)
		}
	case "required_parameter", "optional_parameter":
		for index := uint(0); index < node.ChildCount(); index++ {
			child := node.Child(index)
			if child != nil && (child.Kind() == "accessibility_modifier" || child.Kind() == "readonly" || child.Kind() == "override_modifier") {
				s.addShadow(node.ChildByFieldName("pattern"), names)
				return
			}
		}
	}
}

func (s packageExportStamper) addShadow(nameNode *tree_sitter.Node, names map[string]struct{}) {
	if nameNode == nil {
		return
	}
	if name := strings.TrimSpace(shared.NodeText(nameNode, s.source)); name != "" {
		names[name] = struct{}{}
	}
}

// hasWithStatement reports whether the file holds a sloppy-mode `with`
// statement. Inside `with (o)` a name may resolve to a property of o, so no
// call in such a file is keyed (review F8). It checks each `with` token
// against the AST, so the word in an identifier, a string, or a comment does
// not count.
func hasWithStatement(root *tree_sitter.Node, source []byte) bool {
	needle := []byte("with")
	for offset := 0; ; {
		index := bytes.Index(source[offset:], needle)
		if index < 0 {
			return false
		}
		start := offset + index
		offset = start + len(needle)
		if start > 0 && isJSIdentifierByte(source[start-1]) {
			continue
		}
		if offset < len(source) && isJSIdentifierByte(source[offset]) {
			continue
		}
		node := root.DescendantForByteRange(uint(start), uint(offset))
		if node == nil || node.Kind() != "with" {
			continue
		}
		if parent := node.Parent(); parent != nil && parent.Kind() == "with_statement" {
			return true
		}
	}
}
