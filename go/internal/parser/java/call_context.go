package java

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// javaClassContextChain returns nearest-to-outermost Java type names for
// unqualified calls that may resolve against either an inner or enclosing type.
func javaClassContextChain(node *tree_sitter.Node, source []byte) []string {
	if node == nil {
		return nil
	}
	var contexts []string
	for current := node.Parent(); current != nil; current = current.Parent() {
		switch current.Kind() {
		case "class_declaration", "interface_declaration", "enum_declaration", "record_declaration":
			name := strings.TrimSpace(nodeText(current.ChildByFieldName("name"), source))
			if name != "" {
				contexts = append(contexts, name)
			}
		}
	}
	return contexts
}

// javaEnhancedForTypedName extracts the loop variable and element type from
// Java enhanced-for headers so receiver calls inside the loop stay typed.
func javaEnhancedForTypedName(node *tree_sitter.Node, source []byte) (javaTypedName, bool) {
	if node == nil || node.Kind() != "enhanced_for_statement" {
		return javaTypedName{}, false
	}
	raw := strings.TrimSpace(nodeText(node, source))
	open := strings.Index(raw, "(")
	colon := strings.Index(raw, ":")
	if open < 0 || colon <= open {
		return javaTypedName{}, false
	}
	declaration := strings.TrimSpace(raw[open+1 : colon])
	fields := strings.Fields(declaration)
	if len(fields) < 2 {
		return javaTypedName{}, false
	}
	name := strings.Trim(strings.TrimSpace(fields[len(fields)-1]), "[]")
	typeName := javaTypeLeafName(strings.Join(javaDropJavaDeclarationModifiers(fields[:len(fields)-1]), " "))
	if name == "" || typeName == "" {
		return javaTypedName{}, false
	}
	return javaTypedName{name: name, typeName: typeName, line: nodeLine(node)}, true
}

// javaExplicitOuterThisField splits receivers such as
// BootZipCopyAction.this.layerResolver into the named outer class and field.
func javaExplicitOuterThisField(receiver string) (string, string, bool) {
	receiver = strings.TrimSpace(receiver)
	if receiver == "" || strings.ContainsAny(receiver, "()[") {
		return "", "", false
	}
	parts := strings.Split(receiver, ".")
	if len(parts) != 3 || parts[1] != "this" {
		return "", "", false
	}
	className := strings.TrimSpace(parts[0])
	fieldName := strings.TrimSpace(parts[2])
	return className, fieldName, className != "" && fieldName != ""
}

func javaDropJavaDeclarationModifiers(fields []string) []string {
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		trimmed := strings.TrimSpace(field)
		if trimmed == "" || trimmed == "final" || strings.HasPrefix(trimmed, "@") {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}
