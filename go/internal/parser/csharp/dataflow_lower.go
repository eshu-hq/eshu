// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package csharp

import (
	"github.com/eshu-hq/eshu/go/internal/parser/cfg"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// csharpLowerFunction lowers a C# method/constructor/local-function declaration
// into a control-flow graph. Parameters seed the entry block so model-binding
// sources have a definition site to anchor taint propagation.
func csharpLowerFunction(node *tree_sitter.Node, source []byte, limits cfg.Limits) cfg.Function {
	builder := cfg.NewBuilder(limits)
	lowerer := &csharpLowerer{builder: builder, source: source}

	entry := builder.AddBlock()
	builder.SetEntry(entry)
	if params := csharpDataflowParamNames(node, source); len(params) > 0 {
		builder.AddStmt(entry, shared.NodeLine(node), params, nil)
	}
	if body := node.ChildByFieldName("body"); body != nil {
		lowerer.lowerStmt(body, entry)
	}
	return builder.Build()
}

// csharpLowerer accumulates basic blocks while walking C# statement nodes.
type csharpLowerer struct {
	builder *cfg.Builder
	source  []byte
}

// lowerStmtList lowers each named child statement in sequence, stopping when a
// child makes the remainder unreachable.
func (l *csharpLowerer) lowerStmtList(blockNode *tree_sitter.Node, cur cfg.BlockID) (cfg.BlockID, bool) {
	cursor := blockNode.Walk()
	defer cursor.Close()
	for _, child := range blockNode.NamedChildren(cursor) {
		child := child
		next, reachable := l.lowerStmt(&child, cur)
		cur = next
		if !reachable {
			return cur, false
		}
	}
	return cur, true
}

// lowerStmt lowers one C# statement node, returning the continuation block and
// whether control can fall through to it.
func (l *csharpLowerer) lowerStmt(node *tree_sitter.Node, cur cfg.BlockID) (cfg.BlockID, bool) {
	switch node.Kind() {
	case "block":
		return l.lowerStmtList(node, cur)
	case "if_statement":
		return l.lowerIf(node, cur)
	case "for_statement", "for_each_statement", "foreach_statement", "while_statement", "do_statement":
		return l.lowerLoop(node, cur)
	case "try_statement":
		return l.lowerTry(node, cur)
	case "return_statement", "throw_statement":
		l.addUses(cur, node)
		return cur, false
	case "local_declaration_statement":
		l.lowerDeclaration(node, cur)
		return cur, true
	case "expression_statement":
		l.lowerExpressionStatement(node, cur)
		return cur, true
	default:
		l.addUses(cur, node)
		return cur, true
	}
}

// lowerDeclaration records each declarator's defined name and initializer uses.
func (l *csharpLowerer) lowerDeclaration(node *tree_sitter.Node, cur cfg.BlockID) {
	walkInDirectVariableDeclarations(node, func(declarator *tree_sitter.Node) {
		name := declarator.ChildByFieldName("name")
		if name == nil || name.Kind() != "identifier" {
			return
		}
		var uses []string
		if value := csharpDeclaratorValue(declarator); value != nil {
			uses = csharpExprUses(value, l.source)
		}
		l.addStmt(cur, shared.NodeLine(declarator), []string{shared.NodeText(name, l.source)}, uses)
	})
}

// csharpDeclaratorValue returns the initializer expression of a
// variable_declarator, or nil when the declarator has no initializer. The name
// identifier is the declarator's `name` field; the initializer is the first
// remaining named child (the C# grammar emits it as a direct sibling or an
// equals_value_clause wrapper).
func csharpDeclaratorValue(declarator *tree_sitter.Node) *tree_sitter.Node {
	nameNode := declarator.ChildByFieldName("name")
	cursor := declarator.Walk()
	defer cursor.Close()
	for _, child := range declarator.NamedChildren(cursor) {
		child := child
		if nameNode != nil && child.Equals(*nameNode) {
			continue
		}
		if child.Kind() == "equals_value_clause" {
			return csharpFirstNamedChild(&child)
		}
		return shared.CloneNode(&child)
	}
	return nil
}

// walkInDirectVariableDeclarations invokes visit for each variable_declarator
// nested under a local_declaration_statement's variable_declaration.
func walkInDirectVariableDeclarations(node *tree_sitter.Node, visit func(*tree_sitter.Node)) {
	walkDirectNamed(node, func(child *tree_sitter.Node) {
		if child.Kind() != "variable_declaration" {
			return
		}
		walkDirectNamed(child, func(declarator *tree_sitter.Node) {
			if declarator.Kind() == "variable_declarator" {
				visit(declarator)
			}
		})
	})
}

// lowerExpressionStatement lowers the single expression wrapped by an
// expression_statement.
func (l *csharpLowerer) lowerExpressionStatement(node *tree_sitter.Node, cur cfg.BlockID) {
	expr := csharpFirstNamedChild(node)
	if expr == nil {
		return
	}
	l.lowerInlineExpr(expr, cur)
}

// lowerInlineExpr records def/use facts for an assignment or update expression,
// or plain uses for any other expression.
func (l *csharpLowerer) lowerInlineExpr(expr *tree_sitter.Node, block cfg.BlockID) {
	switch expr.Kind() {
	case "assignment_expression", "postfix_unary_expression", "prefix_unary_expression":
		defs, uses := csharpAssignDefsUses(expr, l.source)
		l.addStmt(block, shared.NodeLine(expr), defs, uses)
	default:
		l.addUses(block, expr)
	}
}

// lowerIf lowers an if/else into branch blocks joined at a merge block.
func (l *csharpLowerer) lowerIf(node *tree_sitter.Node, cur cfg.BlockID) (cfg.BlockID, bool) {
	if cond := node.ChildByFieldName("condition"); cond != nil {
		l.addUses(cur, cond)
	}
	merge := l.builder.AddBlock()
	mergeReachable := false

	thenBlk := l.builder.AddBlock()
	l.builder.AddEdge(cur, thenBlk)
	thenReach := true
	if cons := node.ChildByFieldName("consequence"); cons != nil {
		thenBlk, thenReach = l.lowerStmt(cons, thenBlk)
	}
	if thenReach {
		l.builder.AddEdge(thenBlk, merge)
		mergeReachable = true
	}

	if alt := node.ChildByFieldName("alternative"); alt != nil {
		elseBlk := l.builder.AddBlock()
		l.builder.AddEdge(cur, elseBlk)
		elseBlk, elseReach := l.lowerStmt(alt, elseBlk)
		if elseReach {
			l.builder.AddEdge(elseBlk, merge)
			mergeReachable = true
		}
	} else {
		l.builder.AddEdge(cur, merge)
		mergeReachable = true
	}
	return merge, mergeReachable
}

// lowerLoop lowers a for/foreach/while/do loop with a header, body, and exit so
// the body's writes flow back through the header.
func (l *csharpLowerer) lowerLoop(node *tree_sitter.Node, cur cfg.BlockID) (cfg.BlockID, bool) {
	header := l.builder.AddBlock()
	l.builder.AddEdge(cur, header)
	l.addUses(header, node)
	body := l.builder.AddBlock()
	l.builder.AddEdge(header, body)
	bodyReach := true
	if b := node.ChildByFieldName("body"); b != nil {
		body, bodyReach = l.lowerStmt(b, body)
	}
	if bodyReach {
		l.builder.AddEdge(body, header)
	}
	exit := l.builder.AddBlock()
	l.builder.AddEdge(header, exit)
	return exit, true
}

// lowerTry lowers try/catch/finally, joining the try body and each catch body at
// a merge block before threading an optional finally block.
func (l *csharpLowerer) lowerTry(node *tree_sitter.Node, cur cfg.BlockID) (cfg.BlockID, bool) {
	merge := l.builder.AddBlock()
	reachable := false

	if body := node.ChildByFieldName("body"); body != nil {
		tryBlock := l.builder.AddBlock()
		l.builder.AddEdge(cur, tryBlock)
		end, ok := l.lowerStmt(body, tryBlock)
		if ok {
			l.builder.AddEdge(end, merge)
			reachable = true
		}
	}

	walkDirectNamed(node, func(child *tree_sitter.Node) {
		if child.Kind() != "catch_clause" {
			return
		}
		catchBlock := l.builder.AddBlock()
		l.builder.AddEdge(cur, catchBlock)
		if name := csharpCatchName(child, l.source); name != "" {
			l.addStmt(catchBlock, shared.NodeLine(child), []string{name}, nil)
		}
		if body := child.ChildByFieldName("body"); body != nil {
			end, ok := l.lowerStmt(body, catchBlock)
			if ok {
				l.builder.AddEdge(end, merge)
				reachable = true
			}
		}
	})

	if !reachable {
		return merge, false
	}
	if finallyBody := csharpFinallyBody(node); finallyBody != nil {
		finallyBlock := l.builder.AddBlock()
		l.builder.AddEdge(merge, finallyBlock)
		return l.lowerStmt(finallyBody, finallyBlock)
	}
	return merge, true
}

// csharpCatchName returns the exception variable bound by a catch clause, or
// empty when the clause omits a declaration.
func csharpCatchName(clause *tree_sitter.Node, source []byte) string {
	var name string
	walkDirectNamed(clause, func(child *tree_sitter.Node) {
		if child.Kind() != "catch_declaration" || name != "" {
			return
		}
		if nameNode := child.ChildByFieldName("name"); nameNode != nil {
			name = shared.NodeText(nameNode, source)
		}
	})
	return name
}

// csharpFinallyBody returns the block lowered for a finally clause, or nil.
func csharpFinallyBody(node *tree_sitter.Node) *tree_sitter.Node {
	var body *tree_sitter.Node
	walkDirectNamed(node, func(child *tree_sitter.Node) {
		if child.Kind() != "finally_clause" || body != nil {
			return
		}
		walkDirectNamed(child, func(inner *tree_sitter.Node) {
			if body == nil && inner.Kind() == "block" {
				body = shared.CloneNode(inner)
			}
		})
	})
	return body
}

// addStmt records a CFG statement when it carries any def or use.
func (l *csharpLowerer) addStmt(block cfg.BlockID, line int, defs, uses []string) {
	if len(defs) == 0 && len(uses) == 0 {
		return
	}
	l.builder.AddStmt(block, line, defs, uses)
}

// addUses records the identifier uses of an arbitrary expression node.
func (l *csharpLowerer) addUses(block cfg.BlockID, node *tree_sitter.Node) {
	l.addStmt(block, shared.NodeLine(node), nil, csharpExprUses(node, l.source))
}
