package csharp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/cfg"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	"github.com/eshu-hq/eshu/go/internal/parser/taint"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// csharpSourceAttributeSpec marks a model-binding attribute that introduces an
// HTTP-request-tainted action parameter. The attribute alone is not enough: an
// Import (a using directive) MUST also be present, mirroring the Java
// annotation+import evidence model so a same-named local attribute is not a
// false-positive source.
type csharpSourceAttributeSpec struct {
	Attribute string `json:"attribute"`
	Import    string `json:"import"`
	Kind      string `json:"kind"`
}

// csharpSinkSpec marks a dangerous receiver method (e.g. ADO.NET SQL execution).
// A call matches only when the receiver's inferred type equals Type AND one of
// Imports is present as a using directive, so a same-named local class without
// the framework using is not a false-positive sink.
type csharpSinkSpec struct {
	Type    string     `json:"type"`
	Imports []string   `json:"imports"`
	Method  string     `json:"method"`
	Kind    taint.Kind `json:"kind"`
}

// csharpSourceMatcherVersion identifies the attribute+using evidence model used
// to recognize C# taint sources. It feeds the catalog content version so a
// matcher change invalidates downstream materialization.
const csharpSourceMatcherVersion = "aspnetcore-binding-attribute-using-evidence-v1"

var (
	// csharpSourceAttributeSpecs lists ASP.NET Core model-binding attributes that
	// taint an action parameter with attacker-controlled HTTP input. Each requires
	// the Microsoft.AspNetCore.Mvc using directive as corroborating evidence.
	csharpSourceAttributeSpecs = []csharpSourceAttributeSpec{
		{Attribute: "FromQuery", Import: "Microsoft.AspNetCore.Mvc", Kind: "http_request"},
		{Attribute: "FromBody", Import: "Microsoft.AspNetCore.Mvc", Kind: "http_request"},
		{Attribute: "FromRoute", Import: "Microsoft.AspNetCore.Mvc", Kind: "http_request"},
		{Attribute: "FromForm", Import: "Microsoft.AspNetCore.Mvc", Kind: "http_request"},
	}
	// csharpSinkSpecs lists framework sinks that execute attacker-influenced data.
	// ADO.NET SQL execution is verified by the receiver type SqlCommand plus a
	// System.Data.SqlClient or Microsoft.Data.SqlClient using; Process.Start is
	// verified by a System.Diagnostics using.
	csharpSinkSpecs = []csharpSinkSpec{
		{Type: "SqlCommand", Imports: []string{"System.Data.SqlClient", "Microsoft.Data.SqlClient"}, Method: "ExecuteReader", Kind: "sql"},
		{Type: "SqlCommand", Imports: []string{"System.Data.SqlClient", "Microsoft.Data.SqlClient"}, Method: "ExecuteNonQuery", Kind: "sql"},
		{Type: "SqlCommand", Imports: []string{"System.Data.SqlClient", "Microsoft.Data.SqlClient"}, Method: "ExecuteScalar", Kind: "sql"},
		{Type: "Process", Imports: []string{"System.Diagnostics"}, Method: "Start", Kind: "command_injection"},
	}
)

// csharpTaintFacts derives the intraprocedural source/sink/sanitizer marks for a
// single C# method or constructor. Sources are model-binding parameters; sinks
// are framework calls whose receiver type and using evidence both match. The
// local-type environment supplies receiver type inference because the C# parser
// has no global call-inference index.
func csharpTaintFacts(
	funcNode *tree_sitter.Node,
	source []byte,
	fn cfg.Function,
	imports map[string]struct{},
	env csharpTypeEnv,
) taint.Facts {
	index := newCSharpLineIndex(fn)
	facts := taint.Facts{
		Sources:    map[taint.StmtBinding]taint.SourceMark{},
		Sanitizers: map[int]taint.SanitizerMark{},
		Sinks:      map[int]taint.SinkMark{},
	}

	funcLine := shared.NodeLine(funcNode)
	for _, param := range csharpSourceParams(funcNode, source, imports) {
		if stmtID, ok := index.defStmt(funcLine, param.name); ok {
			facts.Sources[taint.StmtBinding{Stmt: stmtID, Binding: param.name}] = taint.SourceMark{Kind: param.kind, Label: param.label}
		}
	}

	walkInCSharpFunction(funcNode, func(node *tree_sitter.Node) {
		if node.Kind() != "invocation_expression" {
			return
		}
		label, kind, ok := csharpClassifySinkCall(node, source, imports, env)
		if !ok {
			return
		}
		if stmtID, ok := index.useStmt(shared.NodeLine(node)); ok {
			if _, exists := facts.Sinks[stmtID]; !exists {
				facts.Sinks[stmtID] = taint.SinkMark{Kind: kind, Label: label}
			}
		}
	})
	return facts
}

type csharpSourceParam struct {
	name  string
	kind  string
	label string
}

// csharpSourceParams returns the action parameters whose model-binding attribute
// and using evidence both match a source spec.
func csharpSourceParams(funcNode *tree_sitter.Node, source []byte, imports map[string]struct{}) []csharpSourceParam {
	params := funcNode.ChildByFieldName("parameters")
	if params == nil {
		return nil
	}
	var out []csharpSourceParam
	walkDirectNamed(params, func(child *tree_sitter.Node) {
		if child.Kind() != "parameter" {
			return
		}
		name := csharpParameterName(child, source)
		if name == "" {
			return
		}
		for _, attr := range csharpParameterAttributeNames(child, source) {
			for _, spec := range csharpSourceAttributeSpecs {
				if attr == spec.Attribute && csharpHasUsing(imports, spec.Import) {
					out = append(out, csharpSourceParam{name: name, kind: spec.Kind, label: "[" + attr + "]"})
					return
				}
			}
		}
	})
	return out
}

// csharpClassifySinkCall reports whether a call is a framework sink, returning a
// "receiver.method" label, the sink kind, and ok. The receiver's type comes from
// the per-function type environment; same-named methods on unknown or
// non-matching receivers are rejected.
func csharpClassifySinkCall(
	call *tree_sitter.Node,
	source []byte,
	imports map[string]struct{},
	env csharpTypeEnv,
) (string, taint.Kind, bool) {
	functionNode := call.ChildByFieldName("function")
	if functionNode == nil || functionNode.Kind() != "member_access_expression" {
		return "", "", false
	}
	receiverNode := functionNode.ChildByFieldName("expression")
	methodNode := functionNode.ChildByFieldName("name")
	if receiverNode == nil || methodNode == nil {
		return "", "", false
	}
	method := strings.TrimSpace(shared.NodeText(methodNode, source))
	receiver := strings.TrimSpace(shared.NodeText(receiverNode, source))
	if method == "" || receiver == "" {
		return "", "", false
	}
	typeName := env.lookup(receiver)
	if typeName == "" {
		return "", "", false
	}
	for _, spec := range csharpSinkSpecs {
		if method == spec.Method && csharpSinkTypeMatches(spec, typeName, imports) {
			return receiver + "." + method, spec.Kind, true
		}
	}
	return "", "", false
}

// csharpSinkTypeMatches confirms the receiver's simple type equals the spec type
// and at least one corroborating using directive is present.
func csharpSinkTypeMatches(spec csharpSinkSpec, typeName string, imports map[string]struct{}) bool {
	if csharpLastTypeSegment(typeName) != spec.Type {
		return false
	}
	for _, importPath := range spec.Imports {
		if csharpHasUsing(imports, importPath) {
			return true
		}
	}
	return false
}

// csharpHasUsing reports whether a using directive covers the qualified
// namespace, treating an exact namespace match as sufficient.
func csharpHasUsing(imports map[string]struct{}, qualified string) bool {
	_, ok := imports[qualified]
	return ok
}

// csharpImportSet collects every using-directive namespace visible to a node so
// source and sink evidence checks can corroborate framework membership.
func csharpImportSet(root *tree_sitter.Node, source []byte) map[string]struct{} {
	imports := map[string]struct{}{}
	shared.WalkNamed(root, func(current *tree_sitter.Node) {
		if current.Kind() != "using_directive" {
			return
		}
		if name := strings.TrimSpace(csharpUsingName(current, source)); name != "" {
			imports[name] = struct{}{}
		}
	})
	return imports
}

// walkInCSharpFunction visits the descendants of a method/constructor body
// without crossing into a nested local function or lambda, mirroring the Java
// body walker so a sink in an inner function is not attributed to its parent.
func walkInCSharpFunction(funcNode *tree_sitter.Node, visit func(*tree_sitter.Node)) {
	body := funcNode.ChildByFieldName("body")
	if body == nil {
		return
	}
	var walk func(*tree_sitter.Node)
	walk = func(node *tree_sitter.Node) {
		if node == nil {
			return
		}
		if node != body && csharpIsNestedFunction(node.Kind()) {
			return
		}
		visit(node)
		cursor := node.Walk()
		defer cursor.Close()
		for _, child := range node.NamedChildren(cursor) {
			child := child
			walk(&child)
		}
	}
	walk(body)
}

// csharpIsNestedFunction reports node kinds that introduce a new function scope
// and therefore bound the body walk and use-collection.
func csharpIsNestedFunction(kind string) bool {
	switch kind {
	case "local_function_statement", "lambda_expression", "anonymous_method_expression":
		return true
	default:
		return false
	}
}

// walkDirectNamed visits only the immediate named children of a node.
func walkDirectNamed(node *tree_sitter.Node, visit func(*tree_sitter.Node)) {
	if node == nil {
		return
	}
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		visit(&child)
	}
}

// csharpTaintCatalogVersion returns a content hash of the C# source/sink catalog
// so a catalog change invalidates downstream materialization deterministically.
func csharpTaintCatalogVersion() string {
	payload := struct {
		SourceMatcher string                      `json:"source_matcher"`
		Sources       []csharpSourceAttributeSpec `json:"sources"`
		Sinks         []csharpSinkSpec            `json:"sinks"`
	}{
		SourceMatcher: csharpSourceMatcherVersion,
		Sources:       csharpSourceAttributeSpecs,
		Sinks:         csharpSinkSpecs,
	}
	encoded, _ := json.Marshal(payload)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}
