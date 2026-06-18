package java

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/cfg"
	"github.com/eshu-hq/eshu/go/internal/parser/taint"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

type javaSourceAnnotationSpec struct {
	Annotation string `json:"annotation"`
	Import     string `json:"import"`
	Kind       string `json:"kind"`
}

type javaSinkSpec struct {
	Type    string     `json:"type"`
	Imports []string   `json:"imports"`
	Method  string     `json:"method"`
	Kind    taint.Kind `json:"kind"`
}

const javaSourceMatcherVersion = "spring-param-import-evidence-v1"

var (
	javaSourceAnnotationSpecs = []javaSourceAnnotationSpec{
		{Annotation: "RequestParam", Import: "org.springframework.web.bind.annotation.RequestParam", Kind: "http_request"},
		{Annotation: "PathVariable", Import: "org.springframework.web.bind.annotation.PathVariable", Kind: "http_request"},
		{Annotation: "RequestBody", Import: "org.springframework.web.bind.annotation.RequestBody", Kind: "http_request"},
	}
	javaSinkSpecs = []javaSinkSpec{
		{Type: "Statement", Imports: []string{"java.sql.Statement"}, Method: "execute", Kind: "sql"},
		{Type: "Statement", Imports: []string{"java.sql.Statement"}, Method: "executeQuery", Kind: "sql"},
		{Type: "Statement", Imports: []string{"java.sql.Statement"}, Method: "executeUpdate", Kind: "sql"},
		{Type: "PreparedStatement", Imports: []string{"java.sql.PreparedStatement"}, Method: "execute", Kind: "sql"},
		{Type: "PreparedStatement", Imports: []string{"java.sql.PreparedStatement"}, Method: "executeQuery", Kind: "sql"},
		{Type: "PreparedStatement", Imports: []string{"java.sql.PreparedStatement"}, Method: "executeUpdate", Kind: "sql"},
		{Type: "EntityManager", Imports: []string{"jakarta.persistence.EntityManager", "javax.persistence.EntityManager"}, Method: "createQuery", Kind: "sql"},
		{Type: "EntityManager", Imports: []string{"jakarta.persistence.EntityManager", "javax.persistence.EntityManager"}, Method: "createNativeQuery", Kind: "sql"},
	}
)

func javaTaintFacts(
	funcNode *tree_sitter.Node,
	source []byte,
	fn cfg.Function,
	callInference *javaCallInferenceIndex,
) taint.Facts {
	index := newJavaLineIndex(fn)
	facts := taint.Facts{
		Sources:    map[taint.StmtBinding]taint.SourceMark{},
		Sanitizers: map[int]taint.SanitizerMark{},
		Sinks:      map[int]taint.SinkMark{},
	}

	funcLine := nodeLine(funcNode)
	imports := javaImportSet(funcNode, source)
	for _, param := range javaSourceParams(funcNode, source, imports) {
		if stmtID, ok := index.defStmt(funcLine, param.name); ok {
			facts.Sources[taint.StmtBinding{Stmt: stmtID, Binding: param.name}] = taint.SourceMark{Kind: param.kind, Label: param.label}
		}
	}

	walkInJavaFunction(funcNode, func(node *tree_sitter.Node) {
		if node.Kind() != "method_invocation" {
			return
		}
		label, kind, ok := javaClassifySinkCall(node, source, callInference, imports)
		if !ok {
			return
		}
		if stmtID, ok := index.useStmt(nodeLine(node)); ok {
			if _, exists := facts.Sinks[stmtID]; !exists {
				facts.Sinks[stmtID] = taint.SinkMark{Kind: kind, Label: label}
			}
		}
	})
	return facts
}

type javaSourceParam struct {
	name  string
	kind  string
	label string
}

func javaSourceParams(funcNode *tree_sitter.Node, source []byte, imports map[string]struct{}) []javaSourceParam {
	params := funcNode.ChildByFieldName("parameters")
	if params == nil {
		return nil
	}
	var out []javaSourceParam
	walkDirectNamed(params, func(child *tree_sitter.Node) {
		if child.Kind() != "formal_parameter" && child.Kind() != "spread_parameter" {
			return
		}
		name := javaParameterName(child, source)
		if name == "" {
			return
		}
		for _, ann := range javaDirectAnnotations(child, source) {
			for _, spec := range javaSourceAnnotationSpecs {
				if ann == spec.Annotation && javaHasImport(imports, spec.Import) {
					out = append(out, javaSourceParam{name: name, kind: spec.Kind, label: "@" + ann})
					return
				}
			}
		}
	})
	return out
}

func javaDirectAnnotations(node *tree_sitter.Node, source []byte) []string {
	raw := nodeText(node, source)
	var out []string
	for _, field := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '(' || r == ')'
	}) {
		if !strings.HasPrefix(field, "@") {
			continue
		}
		name := strings.TrimPrefix(strings.TrimSpace(field), "@")
		if name != "" {
			out = append(out, javaSimpleName(name))
		}
	}
	return out
}

func javaImportSet(node *tree_sitter.Node, source []byte) map[string]struct{} {
	root := node
	for root.Parent() != nil {
		root = root.Parent()
	}
	imports := map[string]struct{}{}
	walkNamed(root, func(current *tree_sitter.Node) {
		if current.Kind() != "import_declaration" {
			return
		}
		name := strings.TrimSpace(javaImportName(current, source))
		if name != "" {
			imports[name] = struct{}{}
		}
	})
	return imports
}

func javaHasImport(imports map[string]struct{}, qualified string) bool {
	_, ok := imports[qualified]
	return ok
}

func javaClassifySinkCall(
	call *tree_sitter.Node,
	source []byte,
	callInference *javaCallInferenceIndex,
	imports map[string]struct{},
) (string, taint.Kind, bool) {
	methodNode := call.ChildByFieldName("name")
	objectNode := call.ChildByFieldName("object")
	if methodNode == nil || objectNode == nil {
		return "", "", false
	}
	method := strings.TrimSpace(nodeText(methodNode, source))
	if method == "" {
		return "", "", false
	}
	typeName, qualifiedTypeName := javaCallInferredObjectTypes(call, source, callInference)
	if typeName == "" {
		return "", "", false
	}
	for _, spec := range javaSinkSpecs {
		if method == spec.Method && javaSinkTypeMatches(spec, typeName, qualifiedTypeName, imports) {
			receiver := strings.TrimSpace(nodeText(objectNode, source))
			return receiver + "." + method, spec.Kind, true
		}
	}
	return "", "", false
}

func javaSinkTypeMatches(spec javaSinkSpec, typeName string, qualifiedTypeName string, imports map[string]struct{}) bool {
	if javaSimpleName(typeName) != spec.Type {
		return false
	}
	qualifiedTypeName = strings.TrimSpace(qualifiedTypeName)
	for _, importPath := range spec.Imports {
		if qualifiedTypeName == importPath || javaHasImport(imports, importPath) {
			return true
		}
	}
	return false
}

func walkInJavaFunction(funcNode *tree_sitter.Node, visit func(*tree_sitter.Node)) {
	body := funcNode.ChildByFieldName("body")
	if body == nil {
		return
	}
	var walk func(*tree_sitter.Node)
	walk = func(node *tree_sitter.Node) {
		if node == nil {
			return
		}
		if node != body && (node.Kind() == "method_declaration" || node.Kind() == "constructor_declaration" || node.Kind() == "lambda_expression") {
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

func javaSimpleName(name string) string {
	name = strings.TrimSpace(name)
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		return name[idx+1:]
	}
	return name
}

func javaTaintCatalogVersion() string {
	payload := struct {
		SourceMatcher string                     `json:"source_matcher"`
		Sources       []javaSourceAnnotationSpec `json:"sources"`
		Sinks         []javaSinkSpec             `json:"sinks"`
	}{
		SourceMatcher: javaSourceMatcherVersion,
		Sources:       javaSourceAnnotationSpecs,
		Sinks:         javaSinkSpecs,
	}
	encoded, _ := json.Marshal(payload)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}
