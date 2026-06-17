package jsdataflow

import (
	"github.com/eshu-hq/eshu/go/internal/parser/cfg"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// LowerFunction lowers one TS/JS function, method, or arrow function body into a
// control-flow graph and resolves reaching definitions. Parameters are modeled as
// definitions in the entry block so value flow from a parameter into the body is
// captured.
func LowerFunction(node *tree_sitter.Node, source []byte, limits cfg.Limits) cfg.Function {
	builder := cfg.NewBuilder(limits)
	lowerer := &lowerer{builder: builder, source: source}

	entry := builder.AddBlock()
	builder.SetEntry(entry)
	if params := paramNames(node, source); len(params) > 0 {
		builder.AddStmt(entry, nodeLine(node), params, nil)
	}
	if body := node.ChildByFieldName("body"); body != nil {
		lowerer.lowerStmt(body, entry)
	}
	return builder.Build()
}

type lowerer struct {
	builder *cfg.Builder
	source  []byte
}

// lowerStmtList lowers each statement in a block in source order.
func (l *lowerer) lowerStmtList(blockNode *tree_sitter.Node, cur cfg.BlockID) cfg.BlockID {
	cursor := blockNode.Walk()
	defer cursor.Close()
	for _, child := range blockNode.NamedChildren(cursor) {
		child := child
		cur = l.lowerStmt(&child, cur)
	}
	return cur
}

// lowerStmt lowers a single statement and returns the block control reaches after
// it.
func (l *lowerer) lowerStmt(node *tree_sitter.Node, cur cfg.BlockID) cfg.BlockID {
	switch node.Kind() {
	case "statement_block":
		return l.lowerStmtList(node, cur)
	case "else_clause":
		// The if_statement's alternative is an else_clause wrapper; descend into
		// the statement it holds so the else body is lowered, not flattened.
		return l.lowerStmtList(node, cur)
	case "if_statement":
		return l.lowerIf(node, cur)
	case "for_statement":
		return l.lowerFor(node, cur)
	case "for_in_statement":
		return l.lowerForIn(node, cur)
	case "while_statement":
		return l.lowerWhile(node, cur)
	case "lexical_declaration", "variable_declaration":
		l.lowerDeclaration(node, cur)
		return cur
	case "expression_statement":
		l.lowerExpressionStatement(node, cur)
		return cur
	case "assignment_expression", "augmented_assignment_expression", "update_expression":
		// A bare assignment in statement position (for example a C-style for-loop
		// initializer `for (i = 0; ...)`) must record its def, not just uses.
		l.lowerInlineExpr(node, cur)
		return cur
	default:
		l.addUses(cur, node)
		return cur
	}
}

// lowerDeclaration lowers a let/const/var declaration, one statement per
// declarator.
func (l *lowerer) lowerDeclaration(node *tree_sitter.Node, cur cfg.BlockID) {
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		if child.Kind() != "variable_declarator" {
			continue
		}
		name := child.ChildByFieldName("name")
		if name == nil || name.Kind() != "identifier" {
			continue
		}
		var uses []string
		if value := child.ChildByFieldName("value"); value != nil {
			uses = exprUses(value, l.source)
		}
		l.addStmt(cur, nodeLine(&child), []string{nodeText(name, l.source)}, uses)
	}
}

// lowerExpressionStatement lowers an assignment or a bare expression.
func (l *lowerer) lowerExpressionStatement(node *tree_sitter.Node, cur cfg.BlockID) {
	cursor := node.Walk()
	defer cursor.Close()
	children := node.NamedChildren(cursor)
	if len(children) == 0 {
		return
	}
	expr := children[0]
	l.lowerInlineExpr(&expr, cur)
}

// lowerInlineExpr lowers a bare expression (an expression statement's child or a
// for-loop increment): an assignment, augmented assignment, or update records
// its defs and uses; anything else records its uses only.
func (l *lowerer) lowerInlineExpr(expr *tree_sitter.Node, block cfg.BlockID) {
	switch expr.Kind() {
	case "assignment_expression", "augmented_assignment_expression", "update_expression":
		defs, uses := assignDefsUses(expr, l.source)
		l.addStmt(block, nodeLine(expr), defs, uses)
	default:
		l.addUses(block, expr)
	}
}

// lowerIf lowers an if/else.
func (l *lowerer) lowerIf(node *tree_sitter.Node, cur cfg.BlockID) cfg.BlockID {
	if cond := node.ChildByFieldName("condition"); cond != nil {
		l.addUses(cur, cond)
	}
	merge := l.builder.AddBlock()

	thenBlk := l.builder.AddBlock()
	l.builder.AddEdge(cur, thenBlk)
	if cons := node.ChildByFieldName("consequence"); cons != nil {
		thenBlk = l.lowerStmt(cons, thenBlk)
	}
	l.builder.AddEdge(thenBlk, merge)

	if alt := node.ChildByFieldName("alternative"); alt != nil {
		elseBlk := l.builder.AddBlock()
		l.builder.AddEdge(cur, elseBlk)
		elseBlk = l.lowerStmt(alt, elseBlk)
		l.builder.AddEdge(elseBlk, merge)
	} else {
		l.builder.AddEdge(cur, merge)
	}
	return merge
}

// lowerFor lowers a C-style for loop with a back-edge from the body to the head.
func (l *lowerer) lowerFor(node *tree_sitter.Node, cur cfg.BlockID) cfg.BlockID {
	if init := node.ChildByFieldName("initializer"); init != nil {
		cur = l.lowerStmt(init, cur)
	}
	header := l.builder.AddBlock()
	l.builder.AddEdge(cur, header)
	if cond := node.ChildByFieldName("condition"); cond != nil {
		l.addUses(header, cond)
	}
	body := l.builder.AddBlock()
	l.builder.AddEdge(header, body)
	if b := node.ChildByFieldName("body"); b != nil {
		body = l.lowerStmt(b, body)
	}
	if inc := node.ChildByFieldName("increment"); inc != nil {
		l.lowerInlineExpr(inc, body)
	}
	l.builder.AddEdge(body, header)
	exit := l.builder.AddBlock()
	l.builder.AddEdge(header, exit)
	return exit
}

// lowerForIn lowers a for-in/for-of loop: the left binding is defined each
// iteration from the right expression.
func (l *lowerer) lowerForIn(node *tree_sitter.Node, cur cfg.BlockID) cfg.BlockID {
	header := l.builder.AddBlock()
	l.builder.AddEdge(cur, header)
	var defs, uses []string
	if left := node.ChildByFieldName("left"); left != nil {
		defs = forInTargets(left, l.source)
	}
	if right := node.ChildByFieldName("right"); right != nil {
		uses = exprUses(right, l.source)
	}
	l.addStmt(header, nodeLine(node), defs, uses)
	body := l.builder.AddBlock()
	l.builder.AddEdge(header, body)
	if b := node.ChildByFieldName("body"); b != nil {
		body = l.lowerStmt(b, body)
	}
	l.builder.AddEdge(body, header)
	exit := l.builder.AddBlock()
	l.builder.AddEdge(header, exit)
	return exit
}

// lowerWhile lowers a while loop with a back-edge.
func (l *lowerer) lowerWhile(node *tree_sitter.Node, cur cfg.BlockID) cfg.BlockID {
	header := l.builder.AddBlock()
	l.builder.AddEdge(cur, header)
	if cond := node.ChildByFieldName("condition"); cond != nil {
		l.addUses(header, cond)
	}
	body := l.builder.AddBlock()
	l.builder.AddEdge(header, body)
	if b := node.ChildByFieldName("body"); b != nil {
		body = l.lowerStmt(b, body)
	}
	l.builder.AddEdge(body, header)
	exit := l.builder.AddBlock()
	l.builder.AddEdge(header, exit)
	return exit
}

// addStmt records a statement on a block when it carries at least one binding.
func (l *lowerer) addStmt(block cfg.BlockID, line int, defs, uses []string) {
	if len(defs) == 0 && len(uses) == 0 {
		return
	}
	l.builder.AddStmt(block, line, defs, uses)
}

// addUses records the identifier uses of an expression-bearing node.
func (l *lowerer) addUses(block cfg.BlockID, node *tree_sitter.Node) {
	l.addStmt(block, nodeLine(node), nil, exprUses(node, l.source))
}

func nodeText(node *tree_sitter.Node, source []byte) string { return shared.NodeText(node, source) }
func nodeLine(node *tree_sitter.Node) int                   { return shared.NodeLine(node) }
