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

// lowerStmtList lowers each statement in a block in source order. It returns the
// block control reaches after the list and whether control falls through (false
// once a statement terminates flow, for example a return, so later statements are
// unreachable and not lowered).
func (l *lowerer) lowerStmtList(blockNode *tree_sitter.Node, cur cfg.BlockID) (cfg.BlockID, bool) {
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

// lowerStmt lowers a single statement, returning the block control reaches after
// it and whether control falls through. A return or raise terminates flow.
func (l *lowerer) lowerStmt(node *tree_sitter.Node, cur cfg.BlockID) (cfg.BlockID, bool) {
	switch node.Kind() {
	case "block":
		return l.lowerStmtList(node, cur)
	case "if_statement":
		return l.lowerIf(node, cur)
	case "for_statement":
		return l.lowerFor(node, cur)
	case "while_statement":
		return l.lowerWhile(node, cur)
	case "return_statement", "raise_statement":
		l.addUses(cur, node)
		return cur, false
	case "expression_statement":
		l.lowerExpressionStatement(node, cur)
		return cur, true
	default:
		l.addUses(cur, node)
		return cur, true
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
func (l *lowerer) lowerIf(node *tree_sitter.Node, cur cfg.BlockID) (cfg.BlockID, bool) {
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

	cursor := node.Walk()
	defer cursor.Close()
	alternatives := node.ChildrenByFieldName("alternative", cursor)
	if len(alternatives) == 0 {
		// No else: the condition-false path falls through to the merge.
		l.builder.AddEdge(cur, merge)
		return merge, true
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
			elifReach := true
			if cons := alt.ChildByFieldName("consequence"); cons != nil {
				thenElif, elifReach = l.lowerStmt(cons, thenElif)
			}
			if elifReach {
				l.builder.AddEdge(thenElif, merge)
				mergeReachable = true
			}
			next := l.builder.AddBlock()
			l.builder.AddEdge(chain, next)
			chain = next
		case "else_clause":
			end := chain
			elseReach := true
			if body := alt.ChildByFieldName("body"); body != nil {
				end, elseReach = l.lowerStmt(body, chain)
			}
			if elseReach {
				l.builder.AddEdge(end, merge)
				mergeReachable = true
			}
			chainOpen = false
		}
	}
	if chainOpen {
		// No else: the last elif's false path falls through to the merge.
		l.builder.AddEdge(chain, merge)
		mergeReachable = true
	}
	return merge, mergeReachable
}

// lowerFor lowers a for-in loop: the loop target is defined each iteration from
// the iterable, with a back-edge from the body to the header.
func (l *lowerer) lowerFor(node *tree_sitter.Node, cur cfg.BlockID) (cfg.BlockID, bool) {
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
	bodyReach := true
	if b := node.ChildByFieldName("body"); b != nil {
		body, bodyReach = l.lowerStmt(b, body)
	}
	if bodyReach {
		l.builder.AddEdge(body, header) // back-edge only when the body falls through
	}
	exit := l.builder.AddBlock()
	l.builder.AddEdge(header, exit)
	// The loop may run zero times, so control after it is always reachable.
	return exit, true
}

// lowerWhile lowers a while loop with a back-edge from the body to the header.
func (l *lowerer) lowerWhile(node *tree_sitter.Node, cur cfg.BlockID) (cfg.BlockID, bool) {
	header := l.builder.AddBlock()
	l.builder.AddEdge(cur, header)
	if cond := node.ChildByFieldName("condition"); cond != nil {
		l.addUses(header, cond)
	}
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
