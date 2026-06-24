// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package java

import (
	"github.com/eshu-hq/eshu/go/internal/parser/cfg"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func javaDataflowParamNames(node *tree_sitter.Node, source []byte) []string {
	params := node.ChildByFieldName("parameters")
	if params == nil {
		return nil
	}
	var names []string
	walkDirectNamed(params, func(child *tree_sitter.Node) {
		if child.Kind() != "formal_parameter" && child.Kind() != "spread_parameter" {
			return
		}
		if name := javaParameterName(child, source); name != "" {
			names = append(names, name)
		}
	})
	return names
}

func javaAssignDefsUses(node *tree_sitter.Node, source []byte) (defs, uses []string) {
	switch node.Kind() {
	case "assignment_expression":
		left := node.ChildByFieldName("left")
		if left != nil && left.Kind() == "identifier" {
			defs = append(defs, nodeText(left, source))
		} else if left != nil {
			uses = append(uses, javaExprUses(left, source)...)
		}
		if right := node.ChildByFieldName("right"); right != nil {
			uses = append(uses, javaExprUses(right, source)...)
		}
	case "update_expression":
		if arg := node.ChildByFieldName("argument"); arg != nil && arg.Kind() == "identifier" {
			name := nodeText(arg, source)
			defs = append(defs, name)
			uses = append(uses, name)
		}
	}
	return defs, uses
}

func javaExprUses(node *tree_sitter.Node, source []byte) []string {
	if node == nil {
		return nil
	}
	var uses []string
	var visit func(*tree_sitter.Node)
	visit = func(current *tree_sitter.Node) {
		if current == nil || javaIsNestedFunction(current.Kind()) {
			return
		}
		if current.Kind() == "identifier" {
			if name := nodeText(current, source); name != "" {
				uses = append(uses, name)
			}
			return
		}
		cursor := current.Walk()
		defer cursor.Close()
		for _, child := range current.NamedChildren(cursor) {
			child := child
			visit(&child)
		}
	}
	visit(node)
	return uses
}

func javaIsNestedFunction(kind string) bool {
	return kind == "method_declaration" || kind == "constructor_declaration" || kind == "lambda_expression"
}

type javaLineIndex struct {
	defByLine map[int]map[string]int
	useByLine map[int]int
}

func newJavaLineIndex(fn cfg.Function) *javaLineIndex {
	index := &javaLineIndex{defByLine: map[int]map[string]int{}, useByLine: map[int]int{}}
	for _, block := range fn.Blocks {
		for _, stmt := range block.Stmts {
			for _, def := range stmt.Defs {
				byBinding := index.defByLine[stmt.Line]
				if byBinding == nil {
					byBinding = map[string]int{}
					index.defByLine[stmt.Line] = byBinding
				}
				if _, exists := byBinding[def]; !exists {
					byBinding[def] = stmt.ID
				}
			}
			if len(stmt.Uses) > 0 {
				if _, exists := index.useByLine[stmt.Line]; !exists {
					index.useByLine[stmt.Line] = stmt.ID
				}
			}
		}
	}
	return index
}

func (l *javaLineIndex) defStmt(line int, binding string) (int, bool) {
	byBinding, ok := l.defByLine[line]
	if !ok {
		return 0, false
	}
	stmtID, ok := byBinding[binding]
	return stmtID, ok
}

func (l *javaLineIndex) useStmt(line int) (int, bool) {
	stmtID, ok := l.useByLine[line]
	return stmtID, ok
}
