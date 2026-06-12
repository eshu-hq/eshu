package java

import (
	"slices"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func javaNamedTypeRelationships(node *tree_sitter.Node, source []byte) ([]string, []string) {
	if node == nil {
		return nil, nil
	}
	bases, implemented := javaNamedTypeRelationshipNodes(node, source)
	if len(bases) > 0 || len(implemented) > 0 {
		return bases, implemented
	}

	header := javaTypeHeader(nodeText(node, source))
	kind := node.Kind()
	switch kind {
	case "class_declaration", "record_declaration":
		return javaExtendsTypes(header), javaImplementsTypes(header)
	case "interface_declaration":
		return javaExtendsTypes(header), nil
	default:
		return nil, nil
	}
}

func javaNamedTypeRelationshipNodes(node *tree_sitter.Node, source []byte) ([]string, []string) {
	var bases []string
	var implemented []string
	walkDirectNamed(node, func(child *tree_sitter.Node) {
		switch child.Kind() {
		case "superclass":
			bases = append(bases, javaTypeList(javaStripRelationshipKeyword(nodeText(child, source)))...)
		case "super_interfaces":
			implemented = append(implemented, javaTypeList(javaStripRelationshipKeyword(nodeText(child, source)))...)
		case "extends_interfaces":
			bases = append(bases, javaTypeList(javaStripRelationshipKeyword(nodeText(child, source)))...)
		}
	})
	return dedupeSortedJavaStrings(bases), dedupeSortedJavaStrings(implemented)
}

func javaStripRelationshipKeyword(raw string) string {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) == 0 {
		return ""
	}
	switch fields[0] {
	case "extends", "implements":
		return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), fields[0]))
	default:
		return strings.TrimSpace(raw)
	}
}

func javaTypeHeader(raw string) string {
	header := strings.TrimSpace(raw)
	if index := strings.Index(header, "{"); index >= 0 {
		header = header[:index]
	}
	return strings.TrimSpace(header)
}

func javaExtendsTypes(header string) []string {
	tail, ok := javaKeywordTail(header, "extends")
	if !ok {
		return nil
	}
	tail = javaTrimBeforeKeyword(tail, "implements")
	tail = javaTrimBeforeKeyword(tail, "permits")
	return javaTypeList(tail)
}

func javaImplementsTypes(header string) []string {
	tail, ok := javaKeywordTail(header, "implements")
	if !ok {
		return nil
	}
	tail = javaTrimBeforeKeyword(tail, "permits")
	return javaTypeList(tail)
}

func javaKeywordTail(header string, keyword string) (string, bool) {
	fields := strings.Fields(header)
	offset := 0
	for _, field := range fields {
		cleaned := strings.Trim(field, "{}();")
		if cleaned == keyword {
			index := strings.Index(header[offset:], field)
			if index < 0 {
				return "", false
			}
			start := offset + index + len(field)
			return strings.TrimSpace(header[start:]), true
		}
		offset += len(field) + 1
	}
	return "", false
}

func javaTrimBeforeKeyword(value string, keyword string) string {
	marker := " " + keyword + " "
	if index := strings.Index(value, marker); index >= 0 {
		return strings.TrimSpace(value[:index])
	}
	return strings.TrimSpace(value)
}

func javaTypeList(raw string) []string {
	parts := splitJavaTypeList(raw)
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		name := javaRelationshipTypeName(part)
		if name != "" {
			values = append(values, name)
		}
	}
	return dedupeSortedJavaStrings(values)
}

func splitJavaTypeList(raw string) []string {
	parts := make([]string, 0, 2)
	var current strings.Builder
	genericDepth := 0
	for _, r := range raw {
		switch r {
		case '<':
			genericDepth++
			current.WriteRune(r)
		case '>':
			if genericDepth > 0 {
				genericDepth--
			}
			current.WriteRune(r)
		case ',':
			if genericDepth == 0 {
				parts = append(parts, strings.TrimSpace(current.String()))
				current.Reset()
				continue
			}
			current.WriteRune(r)
		default:
			current.WriteRune(r)
		}
	}
	if tail := strings.TrimSpace(current.String()); tail != "" {
		parts = append(parts, tail)
	}
	return parts
}

func javaRelationshipTypeName(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if index := strings.Index(value, "<"); index >= 0 {
		value = value[:index]
	}
	value = strings.TrimSpace(strings.TrimSuffix(value, "[]"))
	if index := strings.LastIndex(value, "."); index >= 0 {
		value = value[index+1:]
	}
	return strings.TrimSpace(value)
}

func dedupeSortedJavaStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			seen[value] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	slices.Sort(out)
	return out
}
