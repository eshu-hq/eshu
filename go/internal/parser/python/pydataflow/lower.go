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
	lowerer := &lowerer{builder: builder, source: source, limits: limits, aliases: pyBindingAliases{}}

	entry := builder.AddBlock()
	builder.SetEntry(entry)
	if params := paramNames(node, source); len(params) > 0 {
		builder.AddStmt(entry, nodeLine(node), params, nil)
	}
	if body := node.ChildByFieldName("body"); body != nil {
		lowerer.lowerStmt(body, entry)
	}
	fn := builder.Build()
	fn.Overflow.AccessPaths += lowerer.accessPathOverflows
	return fn
}

type lowerer struct {
	builder             *cfg.Builder
	source              []byte
	limits              cfg.Limits
	aliases             pyBindingAliases
	accessPathOverflows int
}

// accessPathOptions returns the field-sensitivity options for this lowering,
// wired to the shared overflow counter so truncated paths are counted.
func (l *lowerer) accessPathOptions() pyAccessPathOptions {
	maxParts := l.limits.MaxAccessPathParts
	if maxParts <= 0 {
		maxParts = cfg.DefaultLimits().MaxAccessPathParts
	}
	return pyAccessPathOptions{maxParts: maxParts, truncated: &l.accessPathOverflows}
}

// uses returns the field-sensitive reads of a node under the current alias map.
func (l *lowerer) uses(node *tree_sitter.Node) []string {
	return exprUsesWithOptions(node, l.source, l.aliases, l.accessPathOptions())
}

// updateAliasesFromAssignment keeps the alias map current after an assignment.
// A plain identifier-to-identifier assignment records an alias; any other write
// (an attribute/subscript target, a compound, or a non-identifier value) clears
// the affected identifier's alias.
func (l *lowerer) updateAliasesFromAssignment(node *tree_sitter.Node) {
	left := node.ChildByFieldName("left")
	if left == nil || left.Kind() != "identifier" {
		return
	}
	target := nodeText(left, l.source)
	if node.Kind() == "augmented_assignment" {
		delete(l.aliases, target)
		return
	}
	l.aliases.applyAssignment(target, node.ChildByFieldName("right"), l.source)
}

// dropAliases removes the alias entries for a set of newly-defined targets, so a
// definition through a non-assignment binder (a loop target, a with/except `as`)
// does not leave a stale reference alias.
func (l *lowerer) dropAliases(defs []string) {
	for _, def := range defs {
		delete(l.aliases, accessPathBase(def))
	}
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
	case "with_statement":
		return l.lowerWith(node, cur)
	case "try_statement":
		return l.lowerTry(node, cur)
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
		defs, uses := assignDefsUsesWithOptions(expr, l.source, l.aliases, l.accessPathOptions())
		l.addStmt(block, nodeLine(expr), defs, uses)
		l.updateAliasesFromAssignment(expr)
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
	// Each branch starts from the same pre-if alias state; the post-if state is
	// the intersection of every path that reaches the merge, so an alias set on
	// only one path (or set differently across paths) is dropped — never a leak.
	entryAliases := l.aliases.clone()
	var reaching []pyBindingAliases
	merge := l.builder.AddBlock()
	mergeReachable := false

	thenBlk := l.builder.AddBlock()
	l.builder.AddEdge(cur, thenBlk)
	l.aliases = entryAliases.clone()
	thenReach := true
	if cons := node.ChildByFieldName("consequence"); cons != nil {
		thenBlk, thenReach = l.lowerStmt(cons, thenBlk)
	}
	if thenReach {
		l.builder.AddEdge(thenBlk, merge)
		mergeReachable = true
		reaching = append(reaching, l.aliases.clone())
	}

	cursor := node.Walk()
	defer cursor.Close()
	alternatives := node.ChildrenByFieldName("alternative", cursor)
	if len(alternatives) == 0 {
		// No else: the condition-false path falls through to the merge carrying
		// the pre-if aliases.
		l.builder.AddEdge(cur, merge)
		reaching = append(reaching, entryAliases.clone())
		l.aliases = pyMergeAllAliases(reaching)
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
			// The elif's condition and consequence both run only when every prior
			// condition was false, so both start from the pre-if alias state.
			l.aliases = entryAliases.clone()
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
				reaching = append(reaching, l.aliases.clone())
			}
			next := l.builder.AddBlock()
			l.builder.AddEdge(chain, next)
			chain = next
		case "else_clause":
			l.aliases = entryAliases.clone()
			end := chain
			elseReach := true
			if body := alt.ChildByFieldName("body"); body != nil {
				end, elseReach = l.lowerStmt(body, chain)
			}
			if elseReach {
				l.builder.AddEdge(end, merge)
				mergeReachable = true
				reaching = append(reaching, l.aliases.clone())
			}
			chainOpen = false
		}
	}
	if chainOpen {
		// No else: the last elif's false path falls through to the merge carrying
		// the pre-if aliases.
		l.builder.AddEdge(chain, merge)
		mergeReachable = true
		reaching = append(reaching, entryAliases.clone())
	}
	l.aliases = pyMergeAllAliases(reaching)
	return merge, mergeReachable
}

// lowerFor lowers a for-in loop: the loop target is defined each iteration from
// the iterable, with a back-edge from the body to the header.
func (l *lowerer) lowerFor(node *tree_sitter.Node, cur cfg.BlockID) (cfg.BlockID, bool) {
	entryAliases := l.aliases.clone()
	header := l.builder.AddBlock()
	l.builder.AddEdge(cur, header)
	var defs, uses []string
	if left := node.ChildByFieldName("left"); left != nil {
		defs = assignTargets(left, l.source)
	}
	if right := node.ChildByFieldName("right"); right != nil {
		uses = l.uses(right)
	}
	// The loop target is rebound each iteration, so any prior reference alias on
	// it is stale inside the body.
	l.dropAliases(defs)
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
	// The loop may run zero times (exit aliases = entry) or more (= body end);
	// keep only aliases that agree on both so a body rebinding never leaks.
	merged := pyMergeAliases(entryAliases, l.aliases)
	// The loop target is rebound every iteration, so even if entry and body
	// agree on its alias it does not hold after the loop — drop it so an
	// attribute write through the target is never falsely normalized.
	for _, def := range defs {
		delete(merged, accessPathBase(def))
	}
	l.aliases = merged
	return exit, true
}

// lowerWhile lowers a while loop with a back-edge from the body to the header.
func (l *lowerer) lowerWhile(node *tree_sitter.Node, cur cfg.BlockID) (cfg.BlockID, bool) {
	entryAliases := l.aliases.clone()
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
	// Zero or more iterations: keep only aliases agreeing between entry and body.
	l.aliases = pyMergeAliases(entryAliases, l.aliases)
	return exit, true
}

// addStmt records a statement on a block when it carries at least one binding.
func (l *lowerer) addStmt(block cfg.BlockID, line int, defs, uses []string) {
	if len(defs) == 0 && len(uses) == 0 {
		return
	}
	l.builder.AddStmt(block, line, defs, uses)
}

// addUses records the field-sensitive uses of an expression-bearing node under
// the current alias map.
func (l *lowerer) addUses(block cfg.BlockID, node *tree_sitter.Node) {
	l.addStmt(block, nodeLine(node), nil, l.uses(node))
}

func nodeText(node *tree_sitter.Node, source []byte) string { return shared.NodeText(node, source) }
func nodeLine(node *tree_sitter.Node) int                   { return shared.NodeLine(node) }
