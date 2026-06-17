package pydataflow

import (
	"github.com/eshu-hq/eshu/go/internal/parser/cfg"
	"github.com/eshu-hq/eshu/go/internal/parser/taint"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// The Python source/sink/sanitizer catalog is deliberately small and
// conservative, recognized by the final call name or by a web-framework
// parameter convention. It is meant to grow. Over-broad generic names (run, call,
// write) are excluded to avoid false sinks.
//
// Sinks are matched by method/function name only. As in the Go and TS catalogs,
// this is a known v1 precision limit: a same-named method on an unrelated object
// (a cache execute, an ORM eval) could match. The names kept here are strongly
// associated with their sink; broaden only with a qualified match.
//
// Sanitizers are kept narrow and unambiguous on purpose. A name that neutralizes
// different kinds depending on the import (quote is urllib URL-encoding but also
// shlex shell-quoting) is omitted: marking such a value neutralized would wrongly
// suppress a real flow (URL-encoding does not stop shell metacharacters), and a
// missed sanitizer is safer than a missed vulnerability.
var (
	// pySinkCallKinds maps a recognized sink call's final function name to a sink
	// kind. cursor.execute / executemany => sql (DB-API 2.0); os.system,
	// subprocess.Popen, and the eval/exec builtins => command.
	pySinkCallKinds = map[string]taint.Kind{
		"execute":     "sql",
		"executemany": "sql",
		"system":      "command",
		"Popen":       "command",
		"eval":        "command",
		"exec":        "command",
	}
	// pySanitizerCallKinds maps a recognized sanitizer call to the sink kinds it
	// neutralizes. html.escape / markupsafe.escape / cgi.escape neutralize HTML.
	pySanitizerCallKinds = map[string][]taint.Kind{
		"escape": {"html"},
	}
	// pySourceParamNames maps a parameter name convention to a source kind. A
	// Django or Flask view's request object is the canonical untrusted input.
	pySourceParamNames = map[string]string{
		"request": "http_request",
		"req":     "http_request",
	}
)

// TaintFacts derives intraprocedural taint annotations for one Python function
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
		kind, ok := pySourceParamNames[name]
		if !ok {
			continue
		}
		if stmtID, ok := index.defStmt(funcLine, name); ok {
			facts.Sources[taint.StmtBinding{Stmt: stmtID, Binding: name}] = taint.SourceMark{Kind: kind, Label: name}
		}
	}

	walkInFunction(funcNode, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "call":
			classifySinkCall(node, source, index, &facts)
		case "assignment":
			classifyAssignmentSanitizer(node, source, index, &facts)
		}
	})
	return facts
}

// classifySinkCall marks a sink call's enclosing statement.
func classifySinkCall(node *tree_sitter.Node, source []byte, index *lineIndex, facts *taint.Facts) {
	name := callFinalName(node, source)
	kind, ok := pySinkCallKinds[name]
	if !ok {
		return
	}
	if stmtID, ok := index.useStmt(nodeLine(node)); ok {
		if _, exists := facts.Sinks[stmtID]; !exists {
			facts.Sinks[stmtID] = taint.SinkMark{Kind: kind, Label: name}
		}
	}
}

// classifyAssignmentSanitizer marks `safe = escape(x)` assignments. Only a single
// bare-identifier target is handled; attribute and tuple targets are not modeled.
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
// sanitizer call inside a conditional or boolean expression
// (escape(raw) if cond else raw) leaves an unsanitized branch, so marking the
// whole binding as neutralized would wrongly suppress a real finding.
func markSanitizer(value *tree_sitter.Node, source []byte, target string, line int, index *lineIndex, facts *taint.Facts) {
	if value == nil || value.Kind() != "call" {
		return
	}
	neutralizes, ok := pySanitizerCallKinds[callFinalName(value, source)]
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
// call (eval), or the attribute for a method call (cursor.execute => execute).
func callFinalName(call *tree_sitter.Node, source []byte) string {
	fnNode := call.ChildByFieldName("function")
	if fnNode == nil {
		return ""
	}
	switch fnNode.Kind() {
	case "identifier":
		return nodeText(fnNode, source)
	case "attribute":
		if attr := fnNode.ChildByFieldName("attribute"); attr != nil {
			return nodeText(attr, source)
		}
	}
	return ""
}

// walkInFunction visits named descendants of a function body without descending
// into nested function or lambda bodies (see isNestedFunction), so a sink inside
// a nested closure is not attributed to the enclosing function.
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
