// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package jsdataflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/cfg"
	"github.com/eshu-hq/eshu/go/internal/parser/taint"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

type jsSourceParam struct {
	Name string
	Kind string
}

type jsSourceTypeSpec struct {
	TypeName string   `json:"type_name"`
	Kind     string   `json:"kind"`
	Modules  []string `json:"modules,omitempty"`
}

type jsSinkSpec struct {
	Receiver string     `json:"receiver"`
	Method   string     `json:"method"`
	Kind     taint.Kind `json:"kind"`
}

const jsSourceMatcherVersion = "source-import-evidence-v3"

// The TS/JS source/sink/sanitizer catalog is deliberately small and conservative.
// Sources require framework request type evidence; sinks require a qualified
// receiver/module except for the language builtin eval.
var (
	jsSourceTypeSpecs = []jsSourceTypeSpec{
		{TypeName: "Request", Kind: "http_request", Modules: []string{"express"}},
		{TypeName: "Express.Request", Kind: "http_request"},
		{TypeName: "express.Request", Kind: "http_request"},
		{TypeName: "FastifyRequest", Kind: "http_request", Modules: []string{"fastify"}},
		{TypeName: "NextApiRequest", Kind: "http_request", Modules: []string{"next", "next/types"}},
		{TypeName: "Koa.Context", Kind: "http_request"},
		{TypeName: "Context", Kind: "http_request", Modules: []string{"koa"}},
	}
	jsSinkSpecs = []jsSinkSpec{
		{Receiver: "db", Method: "query", Kind: "sql"},
		{Receiver: "db", Method: "execute", Kind: "sql"},
		{Receiver: "conn", Method: "query", Kind: "sql"},
		{Receiver: "connection", Method: "query", Kind: "sql"},
		{Receiver: "client", Method: "query", Kind: "sql"},
		{Receiver: "pool", Method: "query", Kind: "sql"},
		{Receiver: "knex", Method: "raw", Kind: "sql"},
		{Receiver: "prisma", Method: "$executeRaw", Kind: "sql"},
		{Receiver: "prisma", Method: "$queryRaw", Kind: "sql"},
		{Receiver: "prisma", Method: "executeRaw", Kind: "sql"},
		{Receiver: "prisma", Method: "queryRaw", Kind: "sql"},
		{Receiver: "sequelize", Method: "query", Kind: "sql"},
		{Receiver: "child_process", Method: "exec", Kind: "command"},
		{Receiver: "child_process", Method: "execSync", Kind: "command"},
		{Receiver: "child_process", Method: "spawn", Kind: "command"},
		{Receiver: "child_process", Method: "spawnSync", Kind: "command"},
		{Receiver: "", Method: "eval", Kind: "command"},
	}
	// jsSanitizerCallKinds maps a recognized sanitizer call to the sink kinds it
	// neutralizes.
	jsSanitizerCallKinds = map[string][]taint.Kind{
		"escape":             {"html"},
		"escapeHtml":         {"html"},
		"encodeURIComponent": {"url"},
	}
)

// TaintFacts derives intraprocedural taint annotations for one TS/JS function
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
	case "member_expression":
		if object := fnNode.ChildByFieldName("object"); object != nil {
			receiver = strings.TrimSpace(nodeText(object, source))
		}
		if prop := fnNode.ChildByFieldName("property"); prop != nil {
			name = nodeText(prop, source)
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
	for _, spec := range jsSinkSpecs {
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

func sourceParams(node *tree_sitter.Node, source []byte) []jsSourceParam {
	var out []jsSourceParam
	imports := jsFrameworkRequestImports(node, source)
	for _, param := range parameterNodes(node) {
		name := parameterName(&param, source)
		if name == "" {
			continue
		}
		if kind, ok := frameworkRequestKind(nodeText(&param, source), imports); ok {
			out = append(out, jsSourceParam{Name: name, Kind: kind})
		}
	}
	return out
}

func parameterNodes(node *tree_sitter.Node) []tree_sitter.Node {
	if single := node.ChildByFieldName("parameter"); single != nil {
		return []tree_sitter.Node{*single}
	}
	params := node.ChildByFieldName("parameters")
	if params == nil {
		return nil
	}
	cursor := params.Walk()
	defer cursor.Close()
	var nodes []tree_sitter.Node
	for _, child := range params.NamedChildren(cursor) {
		child := child
		nodes = append(nodes, child)
	}
	return nodes
}

func parameterName(node *tree_sitter.Node, source []byte) string {
	switch node.Kind() {
	case "required_parameter", "optional_parameter":
		if pattern := node.ChildByFieldName("pattern"); pattern != nil && pattern.Kind() == "identifier" {
			return nodeText(pattern, source)
		}
	case "identifier":
		return nodeText(node, source)
	}
	return ""
}

func frameworkRequestKind(paramText string, imports jsFrameworkRequestEvidence) (string, bool) {
	typeTokens := annotationTypeTokens(paramText)
	for _, spec := range jsSourceTypeSpecs {
		for _, token := range typeTokens {
			if len(spec.Modules) == 0 && token == spec.TypeName {
				return spec.Kind, true
			}
			if imports.TypeKind(token) == spec.Kind {
				return spec.Kind, true
			}
			if kind, ok := imports.NamespaceTypeKind(token); ok {
				return kind, true
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
	annotation = strings.TrimSpace(annotation)
	if beforeDefault, _, hasDefault := strings.Cut(annotation, "="); hasDefault {
		annotation = beforeDefault
	}
	if annotation == "" {
		return nil
	}
	var tokens []string
	for _, part := range strings.FieldsFunc(annotation, func(r rune) bool {
		return r == '|' || r == '&'
	}) {
		token := strings.TrimSpace(part)
		if token != "" {
			tokens = append(tokens, token)
		}
	}
	return tokens
}

// TaintCatalogVersion returns a deterministic content hash for the TS/JS
// source/sink/sanitizer catalog.
func TaintCatalogVersion() string {
	payload := struct {
		SourceMatcher string                  `json:"source_matcher"`
		SourceTypes   []jsSourceTypeSpec      `json:"source_types"`
		Sinks         []jsSinkSpec            `json:"sinks"`
		Sanitizers    map[string][]taint.Kind `json:"sanitizers"`
	}{SourceMatcher: jsSourceMatcherVersion, SourceTypes: jsSourceTypeSpecs, Sinks: jsSinkSpecs, Sanitizers: jsSanitizerCallKinds}
	encoded, _ := json.Marshal(payload)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
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
