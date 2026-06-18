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
	lowerer := &goCFGLowerer{
		builder: builder,
		source:  source,
		labels:  map[string]cfg.BlockID{},
		aliases: goBindingAliases{},
		limits:  limits,
	}

	entry := builder.AddBlock()
	builder.SetEntry(entry)
	if params := goFunctionParamNames(node, source); len(params) > 0 {
		builder.AddStmt(entry, nodeLine(node), params, nil)
	}
	if body := node.ChildByFieldName("body"); body != nil {
		lowerer.lowerStmtList(body, entry)
	}
	lowerer.resolveGotos()
	fn := builder.Build()
	fn.Overflow.AccessPaths += lowerer.accessPathOverflows
	return fn
}

// goCFGLowerer threads the active builder and source through the recursive
// statement lowering. labels maps a label name to the block its labeled
// statement begins in; gotos records each goto site so its edge to the target
// label can be added after lowering (a forward goto references a label not yet
// lowered, so edges are resolved in a second pass).
type goCFGLowerer struct {
	builder             *cfg.Builder
	source              []byte
	labels              map[string]cfg.BlockID
	aliases             goBindingAliases
	limits              cfg.Limits
	accessPathOverflows int
	gotos               []pendingGoto
}

// pendingGoto is a goto site awaiting its edge to the target label's block.
type pendingGoto struct {
	block cfg.BlockID
	label string
}

// resolveGotos adds an edge from each goto's block to its target label's block.
// A goto whose label is absent (an out-of-scope or unmodeled target) adds no
// edge rather than a wrong one.
func (l *goCFGLowerer) resolveGotos() {
	for _, g := range l.gotos {
		if target, ok := l.labels[g.label]; ok {
			l.builder.AddEdge(g.block, target)
		}
	}
}

// goLabelName returns the label identifier of a labeled or goto statement.
func goLabelName(node *tree_sitter.Node, source []byte) string {
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		if child.Kind() == "statement_identifier" || child.Kind() == "label_name" {
			child := child
			return nodeText(&child, source)
		}
	}
	return ""
}

// lowerStmtList lowers each statement in a block in source order, threading the
// current block through so straight-line statements share one basic block.
func (l *goCFGLowerer) lowerStmtList(blockNode *tree_sitter.Node, cur cfg.BlockID) (cfg.BlockID, bool) {
	cursor := blockNode.Walk()
	defer cursor.Close()
	reachable := true
	for _, child := range blockNode.NamedChildren(cursor) {
		child := child
		if child.Kind() == "labeled_statement" {
			// A label is a jump target, so it must begin its own single-entry
			// block: otherwise a goto to it would also enter the statements that
			// precede the label in the current block, inventing reaching
			// definitions the goto should skip. Fall through into the label block
			// only when control reaches here; a label after a terminator is
			// reachable solely via its goto edge (added by resolveGotos).
			labelBlock := l.builder.AddBlock()
			if reachable {
				l.builder.AddEdge(cur, labelBlock)
			}
			cur, reachable = l.lowerStmt(&child, labelBlock)
			continue
		}
		if !reachable {
			// Code after a terminating statement is dead and skipped.
			continue
		}
		cur, reachable = l.lowerStmt(&child, cur)
	}
	return cur, reachable
}

// lowerStmt lowers a single statement and returns the block that control reaches
// after it and whether control falls through. A return terminates flow, so a
// definition in a returning branch does not reach code after it.
func (l *goCFGLowerer) lowerStmt(node *tree_sitter.Node, cur cfg.BlockID) (cfg.BlockID, bool) {
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
	case "return_statement":
		l.addUses(cur, node)
		return cur, false
	case "goto_statement":
		// Record the goto site; its edge to the target label's block is added in
		// resolveGotos after lowering. Control jumps away, so fall-through ends.
		if label := goLabelName(node, l.source); label != "" {
			l.gotos = append(l.gotos, pendingGoto{block: cur, label: label})
		}
		return cur, false
	case "short_var_declaration", "assignment_statement", "var_declaration",
		"const_declaration", "inc_statement", "dec_statement":
		defs, uses := goStmtDefsUsesWithOptions(node, l.source, l.aliases, l.accessPathOptions())
		l.addStmt(cur, nodeLine(node), defs, uses)
		l.updateAliases(node)
		return cur, true
	default:
		l.addUses(cur, node)
		return cur, true
	}
}

// lowerIf lowers an if/else, modeling the condition as a use in the current
// block, a then branch, an optional else branch, and a merge block.
func (l *goCFGLowerer) lowerIf(node *tree_sitter.Node, cur cfg.BlockID) (cfg.BlockID, bool) {
	if init := node.ChildByFieldName("initializer"); init != nil {
		cur, _ = l.lowerStmt(init, cur)
	}
	if cond := node.ChildByFieldName("condition"); cond != nil {
		l.addUses(cur, cond)
	}
	entryAliases := l.aliases.clone()

	merge := l.builder.AddBlock()
	mergeReachable := false

	thenBlk := l.builder.AddBlock()
	l.builder.AddEdge(cur, thenBlk)
	thenReach := true
	if cons := node.ChildByFieldName("consequence"); cons != nil {
		thenBlk, thenReach = l.lowerStmt(cons, thenBlk)
	}
	thenAliases := l.aliases.clone()
	if thenReach {
		l.builder.AddEdge(thenBlk, merge)
		mergeReachable = true
	}

	elseAliases := entryAliases.clone()
	if alt := node.ChildByFieldName("alternative"); alt != nil {
		l.aliases = entryAliases.clone()
		elseBlk := l.builder.AddBlock()
		l.builder.AddEdge(cur, elseBlk)
		elseBlk, elseReach := l.lowerStmt(alt, elseBlk)
		elseAliases = l.aliases.clone()
		if elseReach {
			l.builder.AddEdge(elseBlk, merge)
			mergeReachable = true
		}
	} else {
		// No else: the condition-false path falls through to the merge.
		l.builder.AddEdge(cur, merge)
		mergeReachable = true
	}
	if !thenReach {
		thenAliases = elseAliases.clone()
	}
	l.aliases = goMergeAliases(thenAliases, elseAliases)
	return merge, mergeReachable
}

// lowerFor lowers a for loop, threading a back-edge from the loop body to the
// header so in-loop definitions reach the header on later iterations.
func (l *goCFGLowerer) lowerFor(node *tree_sitter.Node, cur cfg.BlockID) (cfg.BlockID, bool) {
	entryAliases := l.aliases.clone()
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
			cur, _ = l.lowerStmt(init, cur)
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
	bodyReach := true
	if body := node.ChildByFieldName("body"); body != nil {
		bodyBlk, bodyReach = l.lowerStmt(body, bodyBlk)
	}
	if bodyReach && post != nil {
		bodyBlk, bodyReach = l.lowerStmt(post, bodyBlk)
	}
	if bodyReach {
		l.builder.AddEdge(bodyBlk, header) // back-edge only when the body falls through
	}

	exit := l.builder.AddBlock()
	l.builder.AddEdge(header, exit)
	// The loop may run zero times, so control after it is reachable.
	l.aliases = entryAliases
	return exit, true
}

// lowerForRange lowers a for-range loop: the range clause defines its loop
// variables at the header on every iteration and reads the ranged expression.
func (l *goCFGLowerer) lowerForRange(node *tree_sitter.Node, clause *tree_sitter.Node, cur cfg.BlockID) (cfg.BlockID, bool) {
	entryAliases := l.aliases.clone()
	header := l.builder.AddBlock()
	l.builder.AddEdge(cur, header)

	var defs, uses []string
	if left := clause.ChildByFieldName("left"); left != nil {
		defs = goAssignTargetsWithOptions(left, l.source, l.aliases, l.accessPathOptions())
	}
	if right := clause.ChildByFieldName("right"); right != nil {
		uses = goExprUsesWithOptions(right, l.source, l.aliases, l.accessPathOptions())
	}
	l.addStmt(header, nodeLine(clause), defs, uses)

	bodyBlk := l.builder.AddBlock()
	l.builder.AddEdge(header, bodyBlk)
	bodyReach := true
	if body := node.ChildByFieldName("body"); body != nil {
		bodyBlk, bodyReach = l.lowerStmt(body, bodyBlk)
	}
	if bodyReach {
		l.builder.AddEdge(bodyBlk, header) // back-edge only when the body falls through
	}

	exit := l.builder.AddBlock()
	l.builder.AddEdge(header, exit)
	l.aliases = entryAliases
	return exit, true
}

// lowerLabeled records the label's target block (where its statement begins) so
// a goto to it can be wired in resolveGotos, then lowers the carried statement.
func (l *goCFGLowerer) lowerLabeled(node *tree_sitter.Node, cur cfg.BlockID) (cfg.BlockID, bool) {
	if name := goLabelName(node, l.source); name != "" {
		l.labels[name] = cur
	}
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
	return cur, true
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
	uses := goExprUsesWithOptions(node, l.source, l.aliases, l.accessPathOptions())
	l.addStmt(block, nodeLine(node), nil, uses)
}

func (l *goCFGLowerer) accessPathOptions() goAccessPathOptions {
	maxParts := l.limits.MaxAccessPathParts
	if maxParts <= 0 {
		maxParts = cfg.DefaultLimits().MaxAccessPathParts
	}
	return goAccessPathOptions{
		maxParts:  maxParts,
		truncated: &l.accessPathOverflows,
	}
}

func (l *goCFGLowerer) updateAliases(node *tree_sitter.Node) {
	switch node.Kind() {
	case "short_var_declaration", "assignment_statement":
		l.aliases.applyAssignment(node, l.source)
	case "var_declaration", "const_declaration", "inc_statement", "dec_statement":
		defs, _ := goStmtDefsUsesWithAliases(node, l.source, nil)
		for _, def := range defs {
			delete(l.aliases, def)
		}
	}
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
