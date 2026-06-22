package swift

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// emitFunction appends one function or initializer row. classContext and
// scopeKind come from the enclosing type scope; both are empty for free
// functions. The payload key set matches the prior extractor: class_context is
// present only when classContext is non-empty, source only when requested, and
// dead_code_root_kinds only when non-empty.
func (b *swiftPayloadBuilder) emitFunction(
	node *tree_sitter.Node,
	source []byte,
	name string,
	classContext string,
	scopeKind string,
	attributes []string,
) {
	args := swiftTreeFunctionArgs(node, source)
	declarationLine := swiftDeclarationLine(node)
	functionSource := swiftFunctionSourceText(declarationLine, source)
	item := map[string]any{
		"name":        name,
		"args":        args,
		"context":     classContext,
		"line_number": declarationLine,
		"end_line":    shared.NodeEndLine(node),
		"lang":        "swift",
		"decorators":  []string{},
	}
	if classContext != "" {
		item["class_context"] = classContext
	}
	if b.indexSource {
		item["source"] = functionSource
	}
	if rootKinds := swiftFunctionDeadCodeRootKinds(name, functionSource, classContext, scopeKind, attributes, b.facts); len(rootKinds) > 0 {
		item["dead_code_root_kinds"] = rootKinds
	}
	shared.AppendBucket(b.payload, "functions", item)
}

// swiftFunctionSourceText returns the trimmed declaration header line for the
// function. The header (signature line) preserves the `override func` and
// signature text the dead-code root checks inspect.
func swiftFunctionSourceText(declarationLine int, source []byte) string {
	lines := swiftSourceLines(source)
	if declarationLine >= 1 && declarationLine <= len(lines) {
		return strings.TrimSpace(lines[declarationLine-1])
	}
	return ""
}

// swiftTreeFunctionArgs returns the internal parameter names for a function or
// initializer. Each `parameter` child carries the internal name in its `name`
// simple_identifier field; the return type also uses a `name` field but is a
// user_type, so only simple_identifier names count. Wildcard names ("_") are
// skipped.
func swiftTreeFunctionArgs(node *tree_sitter.Node, source []byte) []string {
	args := make([]string, 0, 2)
	for _, child := range swiftNamedChildren(node) {
		child := child
		if child.Kind() != "parameter" {
			continue
		}
		nameNode := child.ChildByFieldName("name")
		if nameNode == nil || nameNode.Kind() != "simple_identifier" {
			continue
		}
		name := swiftTrimText(nameNode, source)
		if name != "" && name != "_" {
			args = append(args, name)
		}
	}
	if len(args) == 0 {
		return nil
	}
	return args
}
