package golang

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/cfg"
	"github.com/eshu-hq/eshu/go/internal/parser/taint"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// The Go source/sink/sanitizer catalog is deliberately small and conservative —
// recognized by the final call name (the selector field or function identifier)
// — and is meant to grow. Ambiguous names are excluded (for example "Query" is a
// SQL sink, so net/url's Query is not a source).
var (
	// goSourceCallKinds maps a recognized source call's final name to a source
	// kind. These introduce untrusted input into the value they return.
	goSourceCallKinds = map[string]string{
		"FormValue":     "http_request",
		"PostFormValue": "http_request",
		"FormFile":      "http_request",
		"Getenv":        "environment",
	}
	// goSinkMethodKinds maps a recognized sink call's final method name to a sink
	// kind. These are methods invoked on a receiver value (for example db.Query),
	// where the receiver type is a variable and cannot be matched by package.
	// They are strongly associated with their sink (database/sql), but a
	// same-named method on an unrelated type is a known v1 precision limit.
	goSinkMethodKinds = map[string]taint.Kind{
		"Query":           "sql",
		"QueryContext":    "sql",
		"QueryRow":        "sql",
		"QueryRowContext": "sql",
		"Exec":            "sql",
		"ExecContext":     "sql",
	}
	// goSinkQualifiedKinds maps a package-qualified sink call (base.field) to a
	// sink kind. Qualifying avoids matching same-named functions on unrelated
	// receivers — template.HTML is a trust-assertion sink, but Gin/Echo's
	// c.HTML(...) renderer is not, so only the "template" base matches.
	goSinkQualifiedKinds = map[string]taint.Kind{
		"exec.Command":        "command",
		"exec.CommandContext": "command",
		"template.HTML":       "html",
	}
	// goSanitizerCallKinds maps a recognized sanitizer call's final name to the
	// sink kinds it neutralizes.
	goSanitizerCallKinds = map[string][]taint.Kind{
		"EscapeString":   {"html"},
		"JSEscapeString": {"js"},
		"QueryEscape":    {"url"},
	}
	// goSourceParamTypeMarkers maps a substring of a parameter type to a source
	// kind, for parameters that are inherently untrusted (an HTTP request). It is
	// an ordered slice, not a map, so the first match is deterministic as the
	// list grows.
	goSourceParamTypeMarkers = []struct {
		marker string
		kind   string
	}{
		{marker: "http.Request", kind: "http_request"},
	}
)

// goTaintFacts derives taint annotations for one Go function from its parsed
// tree, mapped onto the resolved control-flow graph. Statements are matched to
// CFG statement IDs by source line and defined/used binding, which is exact for
// idiomatic one-statement-per-line Go; ambiguous lines fall back to the first
// matching statement.
func goTaintFacts(funcNode *tree_sitter.Node, source []byte, fn cfg.Function) taint.Facts {
	index := newGoLineIndex(fn)
	facts := taint.Facts{
		Sources:    map[taint.StmtBinding]taint.SourceMark{},
		Sanitizers: map[int]taint.SanitizerMark{},
		Sinks:      map[int]taint.SinkMark{},
	}

	funcLine := nodeLine(funcNode)
	for name, kind := range goSourceParams(funcNode, source) {
		if stmtID, ok := index.defStmt(funcLine, name); ok {
			facts.Sources[taint.StmtBinding{Stmt: stmtID, Binding: name}] = taint.SourceMark{Kind: kind, Label: name}
		}
	}

	walkScopeBindings(funcNode, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "short_var_declaration", "assignment_statement":
			goClassifyAssignment(node, source, index, &facts)
		case "call_expression":
			goClassifySinkCall(node, source, index, &facts)
		}
	})
	return facts
}

// goClassifyAssignment marks an assignment whose right-hand side is a source or
// sanitizer call.
func goClassifyAssignment(node *tree_sitter.Node, source []byte, index *goLineIndex, facts *taint.Facts) {
	left := node.ChildByFieldName("left")
	right := node.ChildByFieldName("right")
	if left == nil || right == nil {
		return
	}
	call := firstNamedDescendant(right, "call_expression")
	if call == nil {
		return
	}
	targets := goAssignTargets(left, source)
	// Only single-target assignments are classified: for `a, b := f(), g()` the
	// first call cannot be attributed to a specific target, so marking either as
	// a source/sanitizer risks a false annotation. Skipping is a safe false
	// negative.
	if len(targets) != 1 {
		return
	}
	target := targets[0]
	name := goTaintCallName(call, source)
	line := nodeLine(node)
	stmtID, resolvedTarget, ok := index.defStmtOrOnlyDef(line, target)
	if !ok {
		return
	}

	if kind, ok := goSourceCallKinds[name]; ok {
		facts.Sources[taint.StmtBinding{Stmt: stmtID, Binding: resolvedTarget}] = taint.SourceMark{Kind: kind, Label: name}
	}
	if neutralizes, ok := goSanitizerCallKinds[name]; ok {
		existing := facts.Sanitizers[stmtID]
		existing.Neutralizes = unionKinds(existing.Neutralizes, neutralizes)
		facts.Sanitizers[stmtID] = existing
	}
}

// unionKinds appends new sink kinds to an existing list, de-duplicating and
// preserving order, so two sanitizers on one statement accumulate rather than
// overwrite.
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

// goClassifySinkCall marks a sink call's enclosing statement. A package-qualified
// match (base.field) is preferred so a same-named method on an unrelated type
// does not register as a sink; otherwise a known sink method name matches.
func goClassifySinkCall(node *tree_sitter.Node, source []byte, index *goLineIndex, facts *taint.Facts) {
	label := goTaintCallName(node, source)
	kind, ok := goSinkQualifiedKinds[goTaintCallQualified(node, source)]
	if !ok {
		kind, ok = goSinkMethodKinds[label]
	}
	if !ok {
		return
	}
	stmtID, ok := index.useStmt(nodeLine(node))
	if !ok {
		return
	}
	// Keep the first sink on a statement; do not overwrite (which would drop or
	// misattribute the earlier sink when two calls share a CFG statement).
	if _, exists := facts.Sinks[stmtID]; !exists {
		facts.Sinks[stmtID] = taint.SinkMark{Kind: kind, Label: label}
	}
}

// goTaintCallQualified returns the package-qualified "base.field" name of a call
// whose function is a selector on a bare identifier (for example "template.HTML"
// or "exec.Command"), or "" when the call is not of that shape.
func goTaintCallQualified(call *tree_sitter.Node, source []byte) string {
	fnNode := call.ChildByFieldName("function")
	if fnNode == nil || fnNode.Kind() != "selector_expression" {
		return ""
	}
	operand := fnNode.ChildByFieldName("operand")
	field := fnNode.ChildByFieldName("field")
	if operand == nil || field == nil || operand.Kind() != "identifier" {
		return ""
	}
	return nodeText(operand, source) + "." + nodeText(field, source)
}

// goTaintCallName returns the final name of a call's function: the selector field
// for a method call, or the identifier for a bare call.
func goTaintCallName(call *tree_sitter.Node, source []byte) string {
	fnNode := call.ChildByFieldName("function")
	if fnNode == nil {
		return ""
	}
	switch fnNode.Kind() {
	case "identifier":
		return nodeText(fnNode, source)
	case "selector_expression":
		return nodeText(fnNode.ChildByFieldName("field"), source)
	default:
		if field := firstNamedDescendant(fnNode, "field_identifier"); field != nil {
			return nodeText(field, source)
		}
		return ""
	}
}

// goSourceParams returns the source parameter names of a function keyed to their
// source kind, based on their declared type.
func goSourceParams(funcNode *tree_sitter.Node, source []byte) map[string]string {
	out := map[string]string{}
	params := funcNode.ChildByFieldName("parameters")
	if params == nil {
		return out
	}
	cursor := params.Walk()
	defer cursor.Close()
	for _, decl := range params.NamedChildren(cursor) {
		if decl.Kind() != "parameter_declaration" {
			continue
		}
		typeNode := decl.ChildByFieldName("type")
		if typeNode == nil {
			continue
		}
		typeText := nodeText(typeNode, source)
		kind := ""
		for _, entry := range goSourceParamTypeMarkers {
			if strings.Contains(typeText, entry.marker) {
				kind = entry.kind
				break
			}
		}
		if kind == "" {
			continue
		}
		declCursor := decl.Walk()
		for _, field := range decl.NamedChildren(declCursor) {
			if field.Kind() == "identifier" {
				if name := nodeText(&field, source); name != "" && name != blankIdentifier {
					out[name] = kind
				}
			}
		}
		declCursor.Close()
	}
	return out
}

// goLineIndex maps source lines to CFG statement IDs, so tree-sitter nodes can
// be matched to the statements the lowering produced.
type goLineIndex struct {
	defByLine map[int]map[string]int
	useByLine map[int]int
}

// newGoLineIndex builds the line index from a resolved function CFG.
func newGoLineIndex(fn cfg.Function) *goLineIndex {
	index := &goLineIndex{defByLine: map[int]map[string]int{}, useByLine: map[int]int{}}
	for _, block := range fn.Blocks {
		for _, stmt := range block.Stmts {
			for _, def := range stmt.Defs {
				byBinding := index.defByLine[stmt.Line]
				if byBinding == nil {
					byBinding = map[string]int{}
					index.defByLine[stmt.Line] = byBinding
				}
				if _, exists := byBinding[def]; !exists {
					byBinding[def] = stmt.ID
				}
			}
			if len(stmt.Uses) > 0 {
				if _, exists := index.useByLine[stmt.Line]; !exists {
					index.useByLine[stmt.Line] = stmt.ID
				}
			}
		}
	}
	return index
}

// defStmt returns the statement ID that defines a binding on a line.
func (g *goLineIndex) defStmt(line int, binding string) (int, bool) {
	byBinding, ok := g.defByLine[line]
	if !ok {
		return 0, false
	}
	stmtID, ok := byBinding[binding]
	return stmtID, ok
}

// defStmtOrOnlyDef returns the exact binding match on a line, or the line's
// only definition when the AST target was normalized by CFG lowering (for
// example alias.SQL -> data.SQL). It refuses ambiguous lines.
func (g *goLineIndex) defStmtOrOnlyDef(line int, binding string) (int, string, bool) {
	if stmtID, ok := g.defStmt(line, binding); ok {
		return stmtID, binding, true
	}
	byBinding, ok := g.defByLine[line]
	if !ok || len(byBinding) != 1 {
		return 0, "", false
	}
	for normalized, stmtID := range byBinding {
		return stmtID, normalized, true
	}
	return 0, "", false
}

// useStmt returns the first statement on a line that uses any binding.
func (g *goLineIndex) useStmt(line int) (int, bool) {
	stmtID, ok := g.useByLine[line]
	return stmtID, ok
}
