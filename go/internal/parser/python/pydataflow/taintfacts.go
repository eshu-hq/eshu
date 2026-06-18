package pydataflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/cfg"
	"github.com/eshu-hq/eshu/go/internal/parser/taint"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

type pySourceParam struct {
	Name string
	Kind string
}

type pySourceTypeSpec struct {
	TypeName string `json:"type_name"`
	Kind     string `json:"kind"`
}

type pySinkSpec struct {
	Receiver string     `json:"receiver"`
	Method   string     `json:"method"`
	Kind     taint.Kind `json:"kind"`
}

const pySourceMatcherVersion = "source-annotation-exact-v2"

// The Python source/sink/sanitizer catalog is deliberately small and
// conservative. Sources require framework request type evidence; sinks require a
// qualified receiver/module except for Python's eval/exec builtins.
var (
	pySourceTypeSpecs = []pySourceTypeSpec{
		{TypeName: "Request", Kind: "http_request"},
		{TypeName: "flask.Request", Kind: "http_request"},
		{TypeName: "fastapi.Request", Kind: "http_request"},
		{TypeName: "starlette.requests.Request", Kind: "http_request"},
		{TypeName: "django.http.HttpRequest", Kind: "http_request"},
	}
	pySinkSpecs = []pySinkSpec{
		{Receiver: "cursor", Method: "execute", Kind: "sql"},
		{Receiver: "cursor", Method: "executemany", Kind: "sql"},
		{Receiver: "cur", Method: "execute", Kind: "sql"},
		{Receiver: "db", Method: "execute", Kind: "sql"},
		{Receiver: "conn", Method: "execute", Kind: "sql"},
		{Receiver: "connection", Method: "execute", Kind: "sql"},
		{Receiver: "session", Method: "execute", Kind: "sql"},
		{Receiver: "engine", Method: "execute", Kind: "sql"},
		{Receiver: "os", Method: "system", Kind: "command"},
		{Receiver: "subprocess", Method: "Popen", Kind: "command"},
		{Receiver: "subprocess", Method: "call", Kind: "command"},
		{Receiver: "subprocess", Method: "check_output", Kind: "command"},
		{Receiver: "subprocess", Method: "run", Kind: "command"},
		{Receiver: "", Method: "eval", Kind: "command"},
		{Receiver: "", Method: "exec", Kind: "command"},
	}
	// pySanitizerCallKinds maps a recognized sanitizer call to the sink kinds it
	// neutralizes. html.escape / markupsafe.escape / cgi.escape neutralize HTML.
	pySanitizerCallKinds = map[string][]taint.Kind{
		"escape": {"html"},
	}
)

// TaintFacts derives intraprocedural taint annotations for one Python function
// from its parsed tree, mapped onto the resolved control-flow graph. Sources are
// typed framework request parameters; sinks are qualified calls; sanitizers are
// recognized direct calls.
func TaintFacts(funcNode *tree_sitter.Node, source []byte, fn cfg.Function) taint.Facts {
	index := newLineIndex(fn)
	facts := taint.Facts{
		Sources:    map[taint.StmtBinding]taint.SourceMark{},
		Sanitizers: map[int]taint.SanitizerMark{},
		Sinks:      map[int]taint.SinkMark{},
	}

	funcLine := nodeLine(funcNode)
	for _, param := range sourceParams(funcNode, source) {
		if stmtID, ok := index.defStmt(funcLine, param.Name); ok {
			facts.Sources[taint.StmtBinding{Stmt: stmtID, Binding: param.Name}] = taint.SourceMark{Kind: param.Kind, Label: param.Name}
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
	label, kind, ok := classifyCallSink(node, source)
	if !ok {
		return
	}
	if stmtID, ok := index.useStmt(nodeLine(node)); ok {
		if _, exists := facts.Sinks[stmtID]; !exists {
			facts.Sinks[stmtID] = taint.SinkMark{Kind: kind, Label: label}
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
	_, name, _ := callReceiverFinalName(call, source)
	return name
}

func callReceiverFinalName(call *tree_sitter.Node, source []byte) (receiver, name string, bare bool) {
	fnNode := call.ChildByFieldName("function")
	if fnNode == nil {
		return "", "", false
	}
	switch fnNode.Kind() {
	case "identifier":
		return "", nodeText(fnNode, source), true
	case "attribute":
		if object := fnNode.ChildByFieldName("object"); object != nil {
			receiver = strings.TrimSpace(nodeText(object, source))
		}
		if attr := fnNode.ChildByFieldName("attribute"); attr != nil {
			name = nodeText(attr, source)
		}
	}
	return receiver, name, false
}

func classifyCallSink(call *tree_sitter.Node, source []byte) (string, taint.Kind, bool) {
	receiver, method, bare := callReceiverFinalName(call, source)
	if method == "" {
		return "", "", false
	}
	normalizedReceiver := strings.ToLower(receiver)
	for _, spec := range pySinkSpecs {
		if spec.Method != method {
			continue
		}
		if spec.Receiver == "" && bare {
			return method, spec.Kind, true
		}
		if spec.Receiver != "" && normalizedReceiver == strings.ToLower(spec.Receiver) {
			return receiver + "." + method, spec.Kind, true
		}
	}
	return "", "", false
}

func sourceParams(node *tree_sitter.Node, source []byte) []pySourceParam {
	params := node.ChildByFieldName("parameters")
	if params == nil {
		return nil
	}
	cursor := params.Walk()
	defer cursor.Close()
	var out []pySourceParam
	for _, param := range params.NamedChildren(cursor) {
		param := param
		name := parameterName(&param, source)
		if name == "" {
			continue
		}
		if kind, ok := frameworkRequestKind(nodeText(&param, source)); ok {
			out = append(out, pySourceParam{Name: name, Kind: kind})
		}
	}
	return out
}

func parameterName(node *tree_sitter.Node, source []byte) string {
	switch node.Kind() {
	case "identifier":
		return nodeText(node, source)
	case "default_parameter", "typed_default_parameter":
		if nameNode := node.ChildByFieldName("name"); nameNode != nil && nameNode.Kind() == "identifier" {
			return nodeText(nameNode, source)
		}
	case "typed_parameter":
		return firstIdentifier(node, source)
	}
	return ""
}

func frameworkRequestKind(paramText string) (string, bool) {
	typeTokens := annotationTypeTokens(paramText)
	for _, spec := range pySourceTypeSpecs {
		for _, token := range typeTokens {
			if token == spec.TypeName {
				return spec.Kind, true
			}
		}
	}
	return "", false
}

func annotationTypeTokens(paramText string) []string {
	_, annotation, ok := strings.Cut(paramText, ":")
	if !ok {
		return nil
	}
	if beforeDefault, _, hasDefault := strings.Cut(annotation, "="); hasDefault {
		annotation = beforeDefault
	}
	annotation = strings.TrimSpace(annotation)
	if annotation == "" {
		return nil
	}
	var tokens []string
	for _, part := range strings.FieldsFunc(annotation, func(r rune) bool {
		return r == '|'
	}) {
		token := strings.TrimSpace(part)
		if token != "" {
			tokens = append(tokens, token)
		}
	}
	return tokens
}

// TaintCatalogVersion returns a deterministic content hash for the Python
// source/sink/sanitizer catalog.
func TaintCatalogVersion() string {
	payload := struct {
		SourceMatcher string                  `json:"source_matcher"`
		SourceTypes   []pySourceTypeSpec      `json:"source_types"`
		Sinks         []pySinkSpec            `json:"sinks"`
		Sanitizers    map[string][]taint.Kind `json:"sanitizers"`
	}{SourceMatcher: pySourceMatcherVersion, SourceTypes: pySourceTypeSpecs, Sinks: pySinkSpecs, Sanitizers: pySanitizerCallKinds}
	encoded, _ := json.Marshal(payload)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
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
