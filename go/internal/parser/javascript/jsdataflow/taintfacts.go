package jsdataflow

import (
	"github.com/eshu-hq/eshu/go/internal/parser/cfg"
	"github.com/eshu-hq/eshu/go/internal/parser/taint"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// The TS/JS source/sink/sanitizer catalog is deliberately small and conservative,
// recognized by the final call name or by an Express-style parameter convention.
// It is meant to grow. Over-broad generic names (send, write, execute) are
// excluded to avoid false sinks.
//
// Sinks are matched by method/function name only. As in the Go catalog, this is a
// known v1 precision limit: a same-named method on an unrelated object (a cache
// query, a queue exec) could match. The names kept here are strongly associated
// with their sink; broaden only with a qualified match. exec maps to command
// (child_process); a SQLite db.exec is a SQL sink not modeled here.
var (
	// jsSinkMethodKinds maps a recognized sink call's final method/function name
	// to a sink kind. db.query / connection.query => sql; child_process.exec and
	// eval => command.
	jsSinkMethodKinds = map[string]taint.Kind{
		"query":     "sql",
		"queryRaw":  "sql",
		"exec":      "command",
		"execSync":  "command",
		"spawn":     "command",
		"spawnSync": "command",
		"eval":      "command",
	}
	// jsSanitizerCallKinds maps a recognized sanitizer call to the sink kinds it
	// neutralizes.
	jsSanitizerCallKinds = map[string][]taint.Kind{
		"escape":             {"html"},
		"escapeHtml":         {"html"},
		"encodeURIComponent": {"url"},
	}
	// jsSourceParamNames maps a parameter name convention to a source kind. An
	// Express handler's request object is the canonical untrusted input.
	jsSourceParamNames = map[string]string{
		"req":     "http_request",
		"request": "http_request",
	}
)

// TaintFacts derives intraprocedural taint annotations for one TS/JS function
// from its parsed tree, mapped onto the resolved control-flow graph. Sources are
// request-style parameters; sinks and sanitizers are recognized calls.
func TaintFacts(funcNode *tree_sitter.Node, source []byte, fn cfg.Function) taint.Facts {
	index := newLineIndex(fn)
	facts := taint.Facts{
		Sources:    map[taint.StmtBinding]taint.SourceMark{},
		Sanitizers: map[int]taint.SanitizerMark{},
		Sinks:      map[int]taint.SinkMark{},
	}

	funcLine := nodeLine(funcNode)
	for _, name := range paramNames(funcNode, source) {
		kind, ok := jsSourceParamNames[name]
		if !ok {
			continue
		}
		if stmtID, ok := index.defStmt(funcLine, name); ok {
			facts.Sources[taint.StmtBinding{Stmt: stmtID, Binding: name}] = taint.SourceMark{Kind: kind, Label: name}
		}
	}

	walkInFunction(funcNode, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "call_expression":
			classifySinkCall(node, source, index, &facts)
		case "variable_declarator":
			classifyDeclaratorSanitizer(node, source, index, &facts)
		case "assignment_expression":
			classifyAssignmentSanitizer(node, source, index, &facts)
		}
	})
	return facts
}

// classifySinkCall marks a sink call's enclosing statement.
func classifySinkCall(node *tree_sitter.Node, source []byte, index *lineIndex, facts *taint.Facts) {
	kind, ok := jsSinkMethodKinds[callFinalName(node, source)]
	if !ok {
		return
	}
	if stmtID, ok := index.useStmt(nodeLine(node)); ok {
		if _, exists := facts.Sinks[stmtID]; !exists {
			facts.Sinks[stmtID] = taint.SinkMark{Kind: kind, Label: callFinalName(node, source)}
		}
	}
}

// classifyDeclaratorSanitizer marks `const safe = escape(x)` declarations.
func classifyDeclaratorSanitizer(node *tree_sitter.Node, source []byte, index *lineIndex, facts *taint.Facts) {
	name := node.ChildByFieldName("name")
	value := node.ChildByFieldName("value")
	if name == nil || name.Kind() != "identifier" || value == nil {
		return
	}
	markSanitizer(value, source, nodeText(name, source), nodeLine(node), index, facts)
}

// classifyAssignmentSanitizer marks `safe = escape(x)` assignments.
func classifyAssignmentSanitizer(node *tree_sitter.Node, source []byte, index *lineIndex, facts *taint.Facts) {
	left := node.ChildByFieldName("left")
	right := node.ChildByFieldName("right")
	if left == nil || left.Kind() != "identifier" || right == nil {
		return
	}
	markSanitizer(right, source, nodeText(left, source), nodeLine(node), index, facts)
}

// markSanitizer records a sanitizer when the produced value is DIRECTLY a
// recognized sanitizer call. It deliberately does not descend into the value: a
// sanitizer call inside a conditional or logical expression
// (cond ? raw : escape(raw)) leaves an unsanitized branch, so marking the whole
// binding as neutralized would wrongly suppress a real finding.
func markSanitizer(value *tree_sitter.Node, source []byte, target string, line int, index *lineIndex, facts *taint.Facts) {
	if value == nil || value.Kind() != "call_expression" {
		return
	}
	neutralizes, ok := jsSanitizerCallKinds[callFinalName(value, source)]
	if !ok {
		return
	}
	stmtID, ok := index.defStmt(line, target)
	if !ok {
		return
	}
	existing := facts.Sanitizers[stmtID]
	existing.Neutralizes = unionKinds(existing.Neutralizes, neutralizes)
	facts.Sanitizers[stmtID] = existing
}

// callFinalName returns a call's final function name: the identifier for a bare
// call (eval), or the property for a member call (db.query => query).
func callFinalName(call *tree_sitter.Node, source []byte) string {
	fnNode := call.ChildByFieldName("function")
	if fnNode == nil {
		return ""
	}
	switch fnNode.Kind() {
	case "identifier":
		return nodeText(fnNode, source)
	case "member_expression":
		if prop := fnNode.ChildByFieldName("property"); prop != nil {
			return nodeText(prop, source)
		}
	}
	return ""
}

// walkInFunction visits named descendants of a function body without descending
// into nested function, arrow, generator, or method-definition bodies (see
// isNestedFunction), so a sink inside a nested closure is not attributed to the
// enclosing function.
func walkInFunction(funcNode *tree_sitter.Node, visit func(*tree_sitter.Node)) {
	body := funcNode.ChildByFieldName("body")
	if body == nil {
		return
	}
	var walk func(*tree_sitter.Node)
	walk = func(current *tree_sitter.Node) {
		if current == nil || isNestedFunction(current.Kind()) {
			return
		}
		visit(current)
		cursor := current.Walk()
		defer cursor.Close()
		for _, child := range current.NamedChildren(cursor) {
			child := child
			walk(&child)
		}
	}
	walk(body)
}

// unionKinds appends new sink kinds to an existing list, de-duplicating and
// preserving order.
func unionKinds(existing, additional []taint.Kind) []taint.Kind {
	out := existing
	for _, kind := range additional {
		found := false
		for _, have := range out {
			if have == kind {
				found = true
				break
			}
		}
		if !found {
			out = append(out, kind)
		}
	}
	return out
}
