package php

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// phpClassTraitAdaptations returns the `insteadof` and `as` trait adaptation
// clauses declared inside a class's trait use blocks, in source order.
func phpClassTraitAdaptations(node *tree_sitter.Node, source []byte) []string {
	list := phpDeclarationList(node)
	if list == nil {
		return nil
	}
	var adaptations []string
	cursor := list.Walk()
	defer cursor.Close()
	for _, member := range list.NamedChildren(cursor) {
		member := member
		if member.Kind() != "use_declaration" {
			continue
		}
		adaptations = append(adaptations, phpUseListAdaptations(&member, source)...)
	}
	return dedupePHPNonEmptyStrings(adaptations)
}

// phpUseListAdaptations collects adaptation clause text from the use_list of a
// single trait use declaration.
func phpUseListAdaptations(node *tree_sitter.Node, source []byte) []string {
	var adaptations []string
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		if child.Kind() != "use_list" {
			continue
		}
		listCursor := child.Walk()
		for _, clause := range child.NamedChildren(listCursor) {
			clause := clause
			switch clause.Kind() {
			case "use_instead_of_clause", "use_as_clause":
				if text := phpNormalizeAdaptationText(shared.NodeText(&clause, source)); text != "" {
					adaptations = append(adaptations, text)
				}
			}
		}
		listCursor.Close()
	}
	return adaptations
}

// phpNormalizeAdaptationText collapses internal whitespace in an adaptation
// clause and strips the trailing semicolon to match the legacy contract.
func phpNormalizeAdaptationText(raw string) string {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.TrimSuffix(trimmed, ";")
	return strings.Join(strings.Fields(trimmed), " ")
}
