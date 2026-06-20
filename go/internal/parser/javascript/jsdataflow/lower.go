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
	lowerer := &lowerer{builder: builder, source: source, limits: limits, aliases: jsBindingAliases{}}

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
	aliases             jsBindingAliases
	accessPathOverflows int
}

// accessPathOptions returns the field-sensitivity options for this lowering,
// wired to the shared overflow counter so truncated paths are counted.
func (l *lowerer) accessPathOptions() jsAccessPathOptions {
	maxParts := l.limits.MaxAccessPathParts
	if maxParts <= 0 {
		maxParts = cfg.DefaultLimits().MaxAccessPathParts
	}
	return jsAccessPathOptions{maxParts: maxParts, truncated: &l.accessPathOverflows}
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
// it and whether control falls through. A return or throw terminates flow.
func (l *lowerer) lowerStmt(node *tree_sitter.Node, cur cfg.BlockID) (cfg.BlockID, bool) {
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
	case "return_statement", "throw_statement":
		l.addUses(cur, node)
		return cur, false
	case "lexical_declaration", "variable_declaration":
		l.lowerDeclaration(node, cur)
		return cur, true
	case "expression_statement":
		l.lowerExpressionStatement(node, cur)
		return cur, true
	case "assignment_expression", "augmented_assignment_expression", "update_expression":
		// A bare assignment in statement position (for example a C-style for-loop
		// initializer `for (i = 0; ...)`) must record its def, not just uses.
		l.lowerInlineExpr(node, cur)
		return cur, true
	default:
		l.addUses(cur, node)
		return cur, true
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
		if name == nil {
			continue
		}
		value := child.ChildByFieldName("value")
		var uses []string
		if value != nil {
			uses = exprUsesWithOptions(value, l.source, l.aliases, l.accessPathOptions())
		}
		targets := jsPatternBindingNames(name, l.source)
		if name.Kind() != "identifier" {
			uses = append(uses, jsPatternSourceUses(name, value, l.source, l.aliases, l.accessPathOptions())...)
		}
		l.addStmt(cur, nodeLine(&child), targets, uses)
		if name.Kind() == "identifier" && len(targets) == 1 {
			l.aliases.applyAssignment(targets[0], value, l.source)
			continue
		}
		for _, target := range targets {
			delete(l.aliases, accessPathBase(target))
		}
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
		defs, uses := assignDefsUsesWithOptions(expr, l.source, l.aliases, l.accessPathOptions())
		l.addStmt(block, nodeLine(expr), defs, uses)
		l.updateAliasesFromExpr(expr)
	default:
		l.addUses(block, expr)
	}
}

// updateAliasesFromExpr keeps the alias map current after an assignment or
// update lowered in statement position. A plain identifier-to-identifier
// assignment records an alias; any other write (including a member/subscript
// target or a compound/update) clears the affected identifier's alias.
func (l *lowerer) updateAliasesFromExpr(expr *tree_sitter.Node) {
	switch expr.Kind() {
	case "assignment_expression":
		left := expr.ChildByFieldName("left")
		if left != nil && left.Kind() == "identifier" {
			l.aliases.applyAssignment(nodeText(left, l.source), expr.ChildByFieldName("right"), l.source)
		}
	case "augmented_assignment_expression":
		if left := expr.ChildByFieldName("left"); left != nil && left.Kind() == "identifier" {
			delete(l.aliases, nodeText(left, l.source))
		}
	case "update_expression":
		if arg := expr.ChildByFieldName("argument"); arg != nil && arg.Kind() == "identifier" {
			delete(l.aliases, nodeText(arg, l.source))
		}
	}
}

// lowerIf lowers an if/else. The alias map is cloned for each branch and merged
// by intersection after, so an alias established on only one path does not leak
// past the merge.
func (l *lowerer) lowerIf(node *tree_sitter.Node, cur cfg.BlockID) (cfg.BlockID, bool) {
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
	// When the then branch terminates, the merge is reached only via the else
	// path, so carry its aliases. The mirror case (else terminates) keeps the
	// pre-if aliases via intersection rather than the then-branch aliases: that
	// is a conservative miss (a dropped alias normalization), never a false edge,
	// and matches the Go template (cfg_lower.go). Do not "fix" it by carrying the
	// then-branch aliases without proving the else path is truly unreachable.
	if !thenReach {
		thenAliases = elseAliases.clone()
	}
	l.aliases = jsMergeAliases(thenAliases, elseAliases)
	return merge, mergeReachable
}

// lowerFor lowers a C-style for loop with a back-edge from the body to the head.
// Post-loop aliases are the intersection of the pre-loop and body-exit states:
// a loop may run zero or more times, so aliases changed by a possible iteration
// are dropped rather than normalized into a false post-loop access path.
func (l *lowerer) lowerFor(node *tree_sitter.Node, cur cfg.BlockID) (cfg.BlockID, bool) {
	if init := node.ChildByFieldName("initializer"); init != nil {
		cur, _ = l.lowerStmt(init, cur)
	}
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
	bodyAliases := l.aliases.clone()
	if bodyReach {
		if inc := node.ChildByFieldName("increment"); inc != nil {
			l.lowerInlineExpr(inc, body)
			bodyAliases = l.aliases.clone()
		}
		l.builder.AddEdge(body, header) // back-edge only when the body falls through
	}
	exit := l.builder.AddBlock()
	l.builder.AddEdge(header, exit)
	l.aliases = loopExitAliases(entryAliases, bodyAliases, bodyReach)
	return exit, true
}

// lowerForIn lowers a for-in/for-of loop: the left binding is defined each
// iteration from the right expression.
func (l *lowerer) lowerForIn(node *tree_sitter.Node, cur cfg.BlockID) (cfg.BlockID, bool) {
	entryAliases := l.aliases.clone()
	header := l.builder.AddBlock()
	l.builder.AddEdge(cur, header)
	var defs, uses []string
	if left := node.ChildByFieldName("left"); left != nil {
		defs = forInTargets(left, l.source)
	}
	if right := node.ChildByFieldName("right"); right != nil {
		uses = exprUsesWithOptions(right, l.source, l.aliases, l.accessPathOptions())
		if left := node.ChildByFieldName("left"); left != nil && jsForLoopUsesIterableElements(node) {
			uses = append(uses, jsForInPatternSourceUses(left, right, l.source, l.aliases, l.accessPathOptions())...)
		}
	}
	l.addStmt(header, nodeLine(node), defs, uses)
	body := l.builder.AddBlock()
	l.builder.AddEdge(header, body)
	bodyReach := true
	if b := node.ChildByFieldName("body"); b != nil {
		body, bodyReach = l.lowerStmt(b, body)
	}
	bodyAliases := l.aliases.clone()
	if bodyReach {
		l.builder.AddEdge(body, header) // back-edge only when the body falls through
	}
	exit := l.builder.AddBlock()
	l.builder.AddEdge(header, exit)
	l.aliases = loopExitAliases(entryAliases, bodyAliases, bodyReach)
	// The for-in/of target is rebound by the loop header each iteration (not via
	// a tracked assignment), so even if entry and body agree on a prior alias for
	// it, it does not hold after the loop. Drop it so an attribute write through
	// the target is never falsely normalized.
	if bodyReach {
		for _, def := range defs {
			delete(l.aliases, accessPathBase(def))
		}
	}
	return exit, true
}

func jsForLoopUsesIterableElements(node *tree_sitter.Node) bool {
	if node == nil {
		return false
	}
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child != nil && child.Kind() == "of" {
			return true
		}
	}
	return false
}

func jsForInPatternSourceUses(pattern, iterable *tree_sitter.Node, source []byte, aliases jsBindingAliases, options jsAccessPathOptions) []string {
	base, ok := jsAccessPathParts(iterable, source)
	if !ok || len(base) == 0 {
		return nil
	}
	element := appendArrayElementPath(base)
	var uses []string
	for _, binding := range jsPatternBindings(pattern, source, element) {
		if len(binding.path) == 0 {
			continue
		}
		use := jsRenderAccessPathPartsWithOptions(binding.path, aliases, options)
		if use != "" {
			uses = appendUnique(uses, use)
		}
	}
	return uses
}

// lowerWhile lowers a while loop with a back-edge.
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
	bodyAliases := l.aliases.clone()
	if bodyReach {
		l.builder.AddEdge(body, header) // back-edge only when the body falls through
	}
	exit := l.builder.AddBlock()
	l.builder.AddEdge(header, exit)
	l.aliases = loopExitAliases(entryAliases, bodyAliases, bodyReach)
	return exit, true
}

func loopExitAliases(entryAliases, bodyAliases jsBindingAliases, bodyReach bool) jsBindingAliases {
	if !bodyReach {
		return entryAliases.clone()
	}
	return jsMergeAliases(entryAliases, bodyAliases)
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
	l.addStmt(block, nodeLine(node), nil, exprUsesWithOptions(node, l.source, l.aliases, l.accessPathOptions()))
}

func nodeText(node *tree_sitter.Node, source []byte) string { return shared.NodeText(node, source) }
func nodeLine(node *tree_sitter.Node) int                   { return shared.NodeLine(node) }
