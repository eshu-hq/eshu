package golang

import (
	"github.com/eshu-hq/eshu/go/internal/parser/cfg"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// goLowerFunction lowers one Go function, method, or function literal body into
// a control-flow graph and resolves reaching definitions over it. Parameters
// (and the method receiver) are modeled as definitions in the entry block so
// value flow from a parameter into the body is captured. Control flow is lowered
// precisely for blocks, if/else, and for loops; constructs not modeled precisely
// yet (switch, select) contribute their identifier uses but no definitions,
// which can miss a reaching definition but never invents a false edge.
func goLowerFunction(node *tree_sitter.Node, source []byte, limits cfg.Limits) cfg.Function {
	builder := cfg.NewBuilder(limits)
	lowerer := &goCFGLowerer{builder: builder, source: source}

	entry := builder.AddBlock()
	builder.SetEntry(entry)
	if params := goFunctionParamNames(node, source); len(params) > 0 {
		builder.AddStmt(entry, nodeLine(node), params, nil)
	}
	if body := node.ChildByFieldName("body"); body != nil {
		lowerer.lowerStmtList(body, entry)
	}
	return builder.Build()
}

// goCFGLowerer threads the active builder and source through the recursive
// statement lowering.
type goCFGLowerer struct {
	builder *cfg.Builder
	source  []byte
}

// lowerStmtList lowers each statement in a block in source order, threading the
// current block through so straight-line statements share one basic block.
func (l *goCFGLowerer) lowerStmtList(blockNode *tree_sitter.Node, cur cfg.BlockID) cfg.BlockID {
	cursor := blockNode.Walk()
	defer cursor.Close()
	for _, child := range blockNode.NamedChildren(cursor) {
		child := child
		cur = l.lowerStmt(&child, cur)
	}
	return cur
}

// lowerStmt lowers a single statement and returns the block that control reaches
// after it.
func (l *goCFGLowerer) lowerStmt(node *tree_sitter.Node, cur cfg.BlockID) cfg.BlockID {
	switch node.Kind() {
	case "block", "statement_list":
		// tree-sitter-go wraps a block's statements in a statement_list node;
		// recurse through either wrapper to reach the real statements.
		return l.lowerStmtList(node, cur)
	case "if_statement":
		return l.lowerIf(node, cur)
	case "for_statement":
		return l.lowerFor(node, cur)
	case "labeled_statement":
		return l.lowerLabeled(node, cur)
	case "short_var_declaration", "assignment_statement", "var_declaration",
		"const_declaration", "inc_statement", "dec_statement":
		defs, uses := goStmtDefsUses(node, l.source)
		l.addStmt(cur, nodeLine(node), defs, uses)
		return cur
	default:
		l.addUses(cur, node)
		return cur
	}
}

// lowerIf lowers an if/else, modeling the condition as a use in the current
// block, a then branch, an optional else branch, and a merge block.
func (l *goCFGLowerer) lowerIf(node *tree_sitter.Node, cur cfg.BlockID) cfg.BlockID {
	if init := node.ChildByFieldName("initializer"); init != nil {
		cur = l.lowerStmt(init, cur)
	}
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

// lowerFor lowers a for loop, threading a back-edge from the loop body to the
// header so in-loop definitions reach the header on later iterations.
func (l *goCFGLowerer) lowerFor(node *tree_sitter.Node, cur cfg.BlockID) cfg.BlockID {
	clause := goForClause(node)

	// for range: the range clause defines its loop variables each iteration.
	if clause != nil && clause.Kind() == "range_clause" {
		return l.lowerForRange(node, clause, cur)
	}

	// for-clause init runs once before the loop in the current block.
	var post *tree_sitter.Node
	var cond *tree_sitter.Node
	if clause != nil && clause.Kind() == "for_clause" {
		if init := clause.ChildByFieldName("initializer"); init != nil {
			cur = l.lowerStmt(init, cur)
		}
		cond = clause.ChildByFieldName("condition")
		post = clause.ChildByFieldName("update")
	}

	header := l.builder.AddBlock()
	l.builder.AddEdge(cur, header)
	if cond != nil {
		l.addUses(header, cond)
	}

	bodyBlk := l.builder.AddBlock()
	l.builder.AddEdge(header, bodyBlk)
	if body := node.ChildByFieldName("body"); body != nil {
		bodyBlk = l.lowerStmt(body, bodyBlk)
	}
	if post != nil {
		bodyBlk = l.lowerStmt(post, bodyBlk)
	}
	l.builder.AddEdge(bodyBlk, header) // back-edge

	exit := l.builder.AddBlock()
	l.builder.AddEdge(header, exit)
	return exit
}

// lowerForRange lowers a for-range loop: the range clause defines its loop
// variables at the header on every iteration and reads the ranged expression.
func (l *goCFGLowerer) lowerForRange(node *tree_sitter.Node, clause *tree_sitter.Node, cur cfg.BlockID) cfg.BlockID {
	header := l.builder.AddBlock()
	l.builder.AddEdge(cur, header)

	var defs, uses []string
	if left := clause.ChildByFieldName("left"); left != nil {
		defs = goAssignTargets(left, l.source)
	}
	if right := clause.ChildByFieldName("right"); right != nil {
		uses = goExprUses(right, l.source)
	}
	l.addStmt(header, nodeLine(clause), defs, uses)

	bodyBlk := l.builder.AddBlock()
	l.builder.AddEdge(header, bodyBlk)
	if body := node.ChildByFieldName("body"); body != nil {
		bodyBlk = l.lowerStmt(body, bodyBlk)
	}
	l.builder.AddEdge(bodyBlk, header) // back-edge

	exit := l.builder.AddBlock()
	l.builder.AddEdge(header, exit)
	return exit
}

// lowerLabeled lowers the statement carried by a labeled statement, ignoring the
// label itself.
func (l *goCFGLowerer) lowerLabeled(node *tree_sitter.Node, cur cfg.BlockID) cfg.BlockID {
	cursor := node.Walk()
	defer cursor.Close()
	children := node.NamedChildren(cursor)
	for i := len(children) - 1; i >= 0; i-- {
		child := children[i]
		if child.Kind() == "statement_identifier" || child.Kind() == "label_name" {
			continue
		}
		return l.lowerStmt(&child, cur)
	}
	return cur
}

// addStmt records a statement on a block when it carries at least one binding.
func (l *goCFGLowerer) addStmt(block cfg.BlockID, line int, defs, uses []string) {
	if len(defs) == 0 && len(uses) == 0 {
		return
	}
	l.builder.AddStmt(block, line, defs, uses)
}

// addUses records the identifier uses of an expression-bearing node.
func (l *goCFGLowerer) addUses(block cfg.BlockID, node *tree_sitter.Node) {
	uses := goExprUses(node, l.source)
	l.addStmt(block, nodeLine(node), nil, uses)
}

// goForClause returns the for_clause or range_clause child of a for statement,
// or nil for an unconditional loop.
func goForClause(node *tree_sitter.Node) *tree_sitter.Node {
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		switch child.Kind() {
		case "for_clause", "range_clause":
			cloned := child
			return &cloned
		}
	}
	return nil
}
