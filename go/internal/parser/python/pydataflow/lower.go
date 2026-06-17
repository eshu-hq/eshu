package pydataflow

import (
	"github.com/eshu-hq/eshu/go/internal/parser/cfg"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// LowerFunction lowers one Python function definition body into a control-flow
// graph and resolves reaching definitions. Parameters are modeled as definitions
// in the entry block so value flow from a parameter into the body is captured.
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
	case "block":
		return l.lowerStmtList(node, cur)
	case "if_statement":
		return l.lowerIf(node, cur)
	case "for_statement":
		return l.lowerFor(node, cur)
	case "while_statement":
		return l.lowerWhile(node, cur)
	case "expression_statement":
		l.lowerExpressionStatement(node, cur)
		return cur
	default:
		l.addUses(cur, node)
		return cur
	}
}

// lowerExpressionStatement lowers the assignment or bare expression an expression
// statement holds.
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

// lowerInlineExpr lowers a bare expression: an assignment or augmented assignment
// records its defs and uses; anything else records its uses only.
func (l *lowerer) lowerInlineExpr(expr *tree_sitter.Node, block cfg.BlockID) {
	switch expr.Kind() {
	case "assignment", "augmented_assignment":
		defs, uses := assignDefsUses(expr, l.source)
		l.addStmt(block, nodeLine(expr), defs, uses)
	default:
		l.addUses(block, expr)
	}
}

// lowerIf lowers a Python if/elif/else. A Python if has flat sibling
// `alternative` fields (zero or more elif_clause nodes, then an optional
// else_clause), not a nested chain. They are chained here so an elif's
// false path leads to the next alternative, not directly to the merge — branching
// every alternative from the if condition would let a pre-if definition leak
// through an elif's fall-through (a false reaching definition).
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

	cursor := node.Walk()
	defer cursor.Close()
	alternatives := node.ChildrenByFieldName("alternative", cursor)
	if len(alternatives) == 0 {
		l.builder.AddEdge(cur, merge)
		return merge
	}

	// chain is the block reached when every preceding condition was false.
	chain := l.builder.AddBlock()
	l.builder.AddEdge(cur, chain)
	chainOpen := true
	for _, alt := range alternatives {
		alt := alt
		switch alt.Kind() {
		case "elif_clause":
			if cond := alt.ChildByFieldName("condition"); cond != nil {
				l.addUses(chain, cond)
			}
			thenElif := l.builder.AddBlock()
			l.builder.AddEdge(chain, thenElif)
			if cons := alt.ChildByFieldName("consequence"); cons != nil {
				thenElif = l.lowerStmt(cons, thenElif)
			}
			l.builder.AddEdge(thenElif, merge)
			next := l.builder.AddBlock()
			l.builder.AddEdge(chain, next)
			chain = next
		case "else_clause":
			end := chain
			if body := alt.ChildByFieldName("body"); body != nil {
				end = l.lowerStmt(body, chain)
			}
			l.builder.AddEdge(end, merge)
			chainOpen = false
		}
	}
	if chainOpen {
		// No else: the last elif's false path falls through to the merge.
		l.builder.AddEdge(chain, merge)
	}
	return merge
}

// lowerFor lowers a for-in loop: the loop target is defined each iteration from
// the iterable, with a back-edge from the body to the header.
func (l *lowerer) lowerFor(node *tree_sitter.Node, cur cfg.BlockID) cfg.BlockID {
	header := l.builder.AddBlock()
	l.builder.AddEdge(cur, header)
	var defs, uses []string
	if left := node.ChildByFieldName("left"); left != nil {
		defs = assignTargets(left, l.source)
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

// lowerWhile lowers a while loop with a back-edge from the body to the header.
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
