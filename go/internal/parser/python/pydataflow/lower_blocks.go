// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pydataflow

import (
	"github.com/eshu-hq/eshu/go/internal/parser/cfg"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// lowerWith lowers a with statement: the with-clause evaluates its context
// managers (uses) and binds any `as` aliases (defs) on the header line, then the
// body always runs in sequence. Lowering the body precisely (rather than
// collapsing the whole statement onto the header via the default path) gives a
// call inside the body its own CFG statement, so a sink there can be located by
// its own source line.
func (l *lowerer) lowerWith(node *tree_sitter.Node, cur cfg.BlockID) (cfg.BlockID, bool) {
	defs, uses := l.withClauseBindings(node)
	// An `as` target rebinds its name to the context-manager value, so any prior
	// reference alias on it is stale. The body runs in sequence, so aliases flow
	// through it as in straight-line code.
	l.dropAliases(defs)
	l.addStmt(cur, nodeLine(node), defs, uses)
	if body := node.ChildByFieldName("body"); body != nil {
		return l.lowerStmt(body, cur)
	}
	return cur, true
}

// withClauseBindings collects the defs (aliases) and uses (context managers) of
// every with_item in a with statement's with_clause.
func (l *lowerer) withClauseBindings(node *tree_sitter.Node) (defs, uses []string) {
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		if child.Kind() != "with_clause" {
			continue
		}
		// NamedChildren materializes the items, so the cursor can be closed before
		// the loop body runs — no open cursor is held across the per-item work.
		itemCursor := child.Walk()
		items := child.NamedChildren(itemCursor)
		itemCursor.Close()
		for _, item := range items {
			item := item
			value := item.ChildByFieldName("value")
			if value == nil {
				uses = append(uses, exprUses(&item, l.source)...)
				continue
			}
			d, u := asPatternDefsUses(value, l.source)
			defs = append(defs, d...)
			uses = append(uses, u...)
		}
	}
	return defs, uses
}

// lowerTry lowers a try statement conservatively. The try body runs from the
// current block; each except/else/finally handler branches from the pre-try
// state, an over-approximation that records the handlers' inner statements (so
// sinks resolve) without inventing a body-completed definition that reaches a
// handler — that would be a false reaching definition, hence a false edge. The
// under-modeled body-to-handler flow is a safe false negative.
func (l *lowerer) lowerTry(node *tree_sitter.Node, cur cfg.BlockID) (cfg.BlockID, bool) {
	// Body and every handler branch from the pre-try state, so each is lowered
	// with the pre-try alias map. After the try the reachable state is ambiguous
	// (body completed, or some handler ran), so aliases are cleared rather than
	// guessed — a safe false negative, never a stale alias that could mislabel.
	entryAliases := l.aliases.clone()
	merge := l.builder.AddBlock()
	reachMerge := false
	connect := func(end cfg.BlockID, reach bool) {
		if reach {
			l.builder.AddEdge(end, merge)
			reachMerge = true
		}
	}

	if body := node.ChildByFieldName("body"); body != nil {
		b := l.builder.AddBlock()
		l.builder.AddEdge(cur, b)
		l.aliases = entryAliases.clone()
		connect(l.lowerStmt(body, b))
	} else {
		l.builder.AddEdge(cur, merge)
		reachMerge = true
	}

	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		switch child.Kind() {
		case "except_clause", "except_group_clause":
			b := l.builder.AddBlock()
			l.builder.AddEdge(cur, b)
			l.aliases = entryAliases.clone()
			connect(l.lowerExcept(&child, b))
		case "else_clause", "finally_clause":
			if blk := firstBlock(&child); blk != nil {
				b := l.builder.AddBlock()
				l.builder.AddEdge(cur, b)
				l.aliases = entryAliases.clone()
				connect(l.lowerStmt(blk, b))
			}
		}
	}
	l.aliases = pyBindingAliases{}
	return merge, reachMerge
}

// lowerExcept lowers one except clause: the exception type is a use and an
// `as` alias is a def, recorded on the clause header line, then the handler body
// runs in sequence.
func (l *lowerer) lowerExcept(node *tree_sitter.Node, cur cfg.BlockID) (cfg.BlockID, bool) {
	cursor := node.Walk()
	defer cursor.Close()
	var body *tree_sitter.Node
	var defs, uses []string
	for _, child := range node.NamedChildren(cursor) {
		child := child
		switch child.Kind() {
		case "block":
			body = &child
		case "as_pattern":
			d, u := asPatternDefsUses(&child, l.source)
			defs = append(defs, d...)
			uses = append(uses, u...)
		default:
			uses = append(uses, exprUses(&child, l.source)...)
		}
	}
	// The `as` alias rebinds its name to the caught exception, so drop any prior
	// reference alias on it.
	l.dropAliases(defs)
	l.addStmt(cur, nodeLine(node), defs, uses)
	if body != nil {
		return l.lowerStmt(body, cur)
	}
	return cur, true
}

// firstBlock returns the first block named child of a node, or nil.
func firstBlock(node *tree_sitter.Node) *tree_sitter.Node {
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		if child.Kind() == "block" {
			child := child
			return &child
		}
	}
	return nil
}
