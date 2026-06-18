package java

import (
	"github.com/eshu-hq/eshu/go/internal/parser/cfg"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func javaLowerFunction(node *tree_sitter.Node, source []byte, limits cfg.Limits) cfg.Function {
	builder := cfg.NewBuilder(limits)
	lowerer := &javaLowerer{builder: builder, source: source}

	entry := builder.AddBlock()
	builder.SetEntry(entry)
	if params := javaDataflowParamNames(node, source); len(params) > 0 {
		builder.AddStmt(entry, nodeLine(node), params, nil)
	}
	if body := node.ChildByFieldName("body"); body != nil {
		lowerer.lowerStmt(body, entry)
	}
	return builder.Build()
}

type javaLowerer struct {
	builder *cfg.Builder
	source  []byte
}

func (l *javaLowerer) lowerStmtList(blockNode *tree_sitter.Node, cur cfg.BlockID) (cfg.BlockID, bool) {
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

func (l *javaLowerer) lowerStmt(node *tree_sitter.Node, cur cfg.BlockID) (cfg.BlockID, bool) {
	switch node.Kind() {
	case "block":
		return l.lowerStmtList(node, cur)
	case "if_statement":
		return l.lowerIf(node, cur)
	case "for_statement", "enhanced_for_statement", "while_statement":
		return l.lowerLoop(node, cur)
	case "return_statement", "throw_statement":
		l.addUses(cur, node)
		return cur, false
	case "local_variable_declaration":
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

func (l *javaLowerer) lowerDeclaration(node *tree_sitter.Node, cur cfg.BlockID) {
	walkDirectNamed(node, func(child *tree_sitter.Node) {
		if child.Kind() != "variable_declarator" {
			return
		}
		name := child.ChildByFieldName("name")
		if name == nil || name.Kind() != "identifier" {
			return
		}
		var uses []string
		if value := child.ChildByFieldName("value"); value != nil {
			uses = javaExprUses(value, l.source)
		}
		l.addStmt(cur, nodeLine(child), []string{nodeText(name, l.source)}, uses)
	})
}

func (l *javaLowerer) lowerExpressionStatement(node *tree_sitter.Node, cur cfg.BlockID) {
	cursor := node.Walk()
	defer cursor.Close()
	children := node.NamedChildren(cursor)
	if len(children) == 0 {
		return
	}
	expr := children[0]
	l.lowerInlineExpr(&expr, cur)
}

func (l *javaLowerer) lowerInlineExpr(expr *tree_sitter.Node, block cfg.BlockID) {
	switch expr.Kind() {
	case "assignment_expression", "update_expression":
		defs, uses := javaAssignDefsUses(expr, l.source)
		l.addStmt(block, nodeLine(expr), defs, uses)
	default:
		l.addUses(block, expr)
	}
}

func (l *javaLowerer) lowerIf(node *tree_sitter.Node, cur cfg.BlockID) (cfg.BlockID, bool) {
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

func (l *javaLowerer) lowerLoop(node *tree_sitter.Node, cur cfg.BlockID) (cfg.BlockID, bool) {
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

func (l *javaLowerer) addStmt(block cfg.BlockID, line int, defs, uses []string) {
	if len(defs) == 0 && len(uses) == 0 {
		return
	}
	l.builder.AddStmt(block, line, defs, uses)
}

func (l *javaLowerer) addUses(block cfg.BlockID, node *tree_sitter.Node) {
	l.addStmt(block, nodeLine(node), nil, javaExprUses(node, l.source))
}
