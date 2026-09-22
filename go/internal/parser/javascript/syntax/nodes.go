// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package syntax

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func IsFunctionValue(node *tree_sitter.Node) bool {
	if node == nil {
		return false
	}
	switch node.Kind() {
	case "function_expression", "arrow_function", "generator_function", "generator_function_declaration":
		return true
	default:
		return false
	}
}

func InsideFunction(node *tree_sitter.Node, parents *ParentLookup) bool {
	for current := parents.Parent(node); current != nil; current = parents.Parent(current) {
		switch current.Kind() {
		case "function_declaration", "function_expression", "arrow_function", "method_definition":
			return true
		}
	}
	return false
}

func Decorators(node *tree_sitter.Node, source []byte, parents *ParentLookup) []string {
	decorators := make([]string, 0)
	for current := node; current != nil; current = parents.Parent(current) {
		cursor := current.Walk()
		for _, child := range current.NamedChildren(cursor) {
			child := child
			if child.Kind() != "decorator" {
				continue
			}
			decorator := strings.TrimSpace(shared.NodeText(&child, source))
			if decorator == "" {
				continue
			}
			decorators = append(decorators, decorator)
		}
		cursor.Close()
		if current.Kind() == "decorated_definition" {
			return decorators
		}
		if parents.Parent(current) == nil || parents.Parent(current).Kind() != "decorated_definition" {
			break
		}
	}
	return decorators
}

func CallName(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	switch node.Kind() {
	case "parenthesized_expression":
		cursor := node.Walk()
		children := node.NamedChildren(cursor)
		cursor.Close()
		for i := range children {
			if name := CallName(&children[i], source); name != "" {
				return name
			}
		}
	case "identifier":
		return shared.NodeText(node, source)
	case "member_expression":
		property := node.ChildByFieldName("property")
		return shared.NodeText(property, source)
	default:
		return ""
	}
	return ""
}

func CallFullName(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	return strings.TrimSpace(shared.NodeText(node, source))
}

func JSXComponentName(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return ""
	}

	switch nameNode.Kind() {
	case "identifier", "property_identifier", "jsx_identifier", "type_identifier":
		return strings.TrimSpace(shared.NodeText(nameNode, source))
	case "member_expression", "nested_identifier":
		propertyNode := nameNode.ChildByFieldName("property")
		if propertyNode != nil {
			return strings.TrimSpace(shared.NodeText(propertyNode, source))
		}
		text := strings.TrimSpace(shared.NodeText(nameNode, source))
		if text == "" {
			return ""
		}
		parts := strings.Split(text, ".")
		return strings.TrimSpace(parts[len(parts)-1])
	default:
		return ""
	}
}

func NodeContainsKind(node *tree_sitter.Node, kind string) bool {
	if node == nil {
		return false
	}
	if node.Kind() == kind {
		return true
	}
	cursor := node.Walk()
	children := node.NamedChildren(cursor)
	cursor.Close()
	for i := range children {
		child := children[i]
		if NodeContainsKind(&child, kind) {
			return true
		}
	}
	return false
}

func NodeSameRange(left *tree_sitter.Node, right *tree_sitter.Node) bool {
	return left != nil && right != nil && left.StartByte() == right.StartByte() && left.EndByte() == right.EndByte()
}

func HasExpressImport(source string) bool {
	return strings.Contains(source, `require("express")`) ||
		strings.Contains(source, `require('express')`) ||
		strings.Contains(source, `from "express"`) ||
		strings.Contains(source, `from 'express'`)
}
