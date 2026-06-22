package haskell

import (
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// haskellExtractor walks the Haskell tree-sitter AST and emits every symbol
// bucket directly from node spans. It replaces the former line-scan regex
// extraction of modules, imports, data/class/instance declarations, functions,
// type signatures, and where-block variables. Function-call rows remain bounded
// lexical evidence taken from the right-hand-side text of each binding node, the
// documented permanent exception this package keeps rather than resolving
// Haskell name binding.
type haskellExtractor struct {
	payload       map[string]any
	source        []byte
	lines         []string
	isDependency  bool
	options       shared.Options
	exports       map[string]struct{}
	seenFunctions map[string]struct{}
	seenVariables map[string]struct{}
	seenCalls     map[string]struct{}
	functionItems map[string]map[string]any
}

// newHaskellExtractor builds an extractor bound to one parsed file.
func newHaskellExtractor(
	payload map[string]any,
	source []byte,
	lines []string,
	isDependency bool,
	options shared.Options,
) *haskellExtractor {
	return &haskellExtractor{
		payload:       payload,
		source:        source,
		lines:         lines,
		isDependency:  isDependency,
		options:       options,
		exports:       make(map[string]struct{}),
		seenFunctions: make(map[string]struct{}),
		seenVariables: make(map[string]struct{}),
		seenCalls:     make(map[string]struct{}),
		functionItems: make(map[string]map[string]any),
	}
}

// extract walks the root, recording the module header first so export metadata is
// available to every later declaration, then descends the declarations block.
func (e *haskellExtractor) extract(root *tree_sitter.Node) {
	if root == nil {
		return
	}
	if header := haskellFirstChildOfKind(root, "header"); header != nil {
		e.handleHeader(header)
	}
	if imports := haskellFirstChildOfKind(root, "imports"); imports != nil {
		e.handleImports(imports)
	}
	if declarations := haskellFirstChildOfKind(root, "declarations"); declarations != nil {
		e.walkDeclarations(declarations, "", "")
	}
}

// handleHeader records the module row and captures the export-list names used for
// dead-code root metadata.
func (e *haskellExtractor) handleHeader(header *tree_sitter.Node) {
	moduleNode := header.ChildByFieldName("module")
	if moduleNode == nil {
		return
	}
	name := haskellModuleName(moduleNode, e.source)
	if name == "" {
		return
	}
	for export := range haskellExportNames(header, e.source) {
		e.exports[export] = struct{}{}
	}
	shared.AppendBucket(e.payload, "modules", map[string]any{
		"name":        name,
		"line_number": shared.NodeLine(header),
		"end_line":    shared.NodeEndLine(header),
		"lang":        "haskell",
	})
}

// handleImports records one import row per `import` node, carrying the alias when
// the import declares `as`.
func (e *haskellExtractor) handleImports(imports *tree_sitter.Node) {
	for _, child := range haskellNamedChildren(imports) {
		child := child
		if child.Kind() != "import" {
			continue
		}
		name, alias := haskellImportFields(&child, e.source)
		if name == "" {
			continue
		}
		item := map[string]any{
			"name":        name,
			"line_number": shared.NodeLine(&child),
			"lang":        "haskell",
		}
		if alias != "" {
			item["alias"] = alias
		}
		shared.AppendBucket(e.payload, "imports", item)
	}
}

// walkDeclarations dispatches each top-level declaration node to the handler that
// produces its payload rows. classContext and instanceContext carry the enclosing
// class or instance head so nested method rows attach the correct context.
func (e *haskellExtractor) walkDeclarations(node *tree_sitter.Node, classContext, instanceContext string) {
	for _, child := range haskellNamedChildren(node) {
		child := child
		switch child.Kind() {
		case "data_type", "newtype", "type_synomym":
			e.handleTypeDeclaration(&child)
		case "class":
			e.handleClass(&child)
		case "instance":
			e.handleInstance(&child)
		case "signature":
			e.handleSignature(&child, classContext, instanceContext)
		case "bind", "function":
			e.handleBinding(&child, classContext, instanceContext)
		}
	}
}

// handleTypeDeclaration records a data/newtype/type row in the classes bucket and
// marks it an exported-type dead-code root when the module exports the name.
func (e *haskellExtractor) handleTypeDeclaration(node *tree_sitter.Node) {
	name, kind, ok := haskellTypeDeclaration(node, e.source)
	if !ok || name == "" {
		return
	}
	item := map[string]any{
		"name":          name,
		"line_number":   shared.NodeLine(node),
		"end_line":      shared.NodeLine(node),
		"lang":          "haskell",
		"semantic_kind": kind,
	}
	if haskellIsExplicitExport(e.exports, name) {
		item["dead_code_root_kinds"] = []string{"haskell.exported_type"}
	}
	shared.AppendBucket(e.payload, "classes", item)
}

// handleClass records a typeclass row, then records each method signature inside
// the class body as a function with the class context and typeclass-method root.
func (e *haskellExtractor) handleClass(node *tree_sitter.Node) {
	name := haskellClassOrInstanceName(node, e.source)
	if name == "" {
		return
	}
	item := map[string]any{
		"name":          name,
		"line_number":   shared.NodeLine(node),
		"end_line":      shared.NodeLine(node),
		"lang":          "haskell",
		"semantic_kind": "typeclass",
	}
	if haskellIsExplicitExport(e.exports, name) {
		item["dead_code_root_kinds"] = []string{"haskell.exported_type"}
	}
	shared.AppendBucket(e.payload, "classes", item)

	body := node.ChildByFieldName("declarations")
	if body != nil {
		e.walkDeclarations(body, name, "")
	}
}

// handleInstance records each method definition inside an instance body as a
// function row with the combined instance context and instance-method root. The
// instance head itself does not produce a row, matching the prior extractor.
func (e *haskellExtractor) handleInstance(node *tree_sitter.Node) {
	context := haskellClassOrInstanceName(node, e.source)
	body := node.ChildByFieldName("declarations")
	if body != nil {
		e.walkDeclarations(body, "", context)
	}
}

// handleSignature records a function row for a type signature inside a class
// body. Top-level signatures with no class context only contribute a name that a
// later binding reuses, so they do not create a standalone function row.
func (e *haskellExtractor) handleSignature(node *tree_sitter.Node, classContext, instanceContext string) {
	if classContext == "" {
		return
	}
	name := haskellBindingName(node, e.source)
	if name == "" || haskellIsKeyword(name) {
		return
	}
	key := haskellFunctionKey(classContext, name)
	if _, ok := e.seenFunctions[key]; ok {
		return
	}
	e.seenFunctions[key] = struct{}{}
	item := map[string]any{
		"name":                 name,
		"line_number":          shared.NodeLine(node),
		"end_line":             shared.NodeLine(node),
		"lang":                 "haskell",
		"class_context":        classContext,
		"decorators":           []string{},
		"dead_code_root_kinds": []string{"haskell.typeclass_method"},
	}
	e.functionItems[key] = item
	shared.AppendBucket(e.payload, "functions", item)
}

// handleBinding records a top-level or method binding as a function row, then
// records its where-block locals as variables and mines its body for bounded call
// evidence. classContext and instanceContext attach typeclass or instance roots.
func (e *haskellExtractor) handleBinding(node *tree_sitter.Node, classContext, instanceContext string) {
	name := haskellBindingName(node, e.source)
	if name == "" || haskellIsKeyword(name) {
		return
	}
	startLine := shared.NodeLine(node)
	endLine := shared.NodeEndLine(node)
	sourceEndLine := haskellBindingSourceEndLine(node)
	context, rootKinds := haskellFunctionContextAndRoots(name, classContext, instanceContext, e.exports)
	key := haskellFunctionKey(context, name)

	e.ensureFunctionItem(key, name, context, rootKinds, startLine, endLine, sourceEndLine)
	params := haskellTreeFunctionParameters(node, e.source)
	e.appendBindingVariables(node)
	e.appendBindingCalls(name, context, params, startLine, endLine)
}

// haskellBindingSourceEndLine returns the last line of a binding's defining
// equation, excluding a trailing where-block and its `where` keyword. The
// function `source` field mirrors the defining clauses and guards but not the
// local where bindings, so the span ends at the last `match` clause line when a
// where-block is present.
func haskellBindingSourceEndLine(node *tree_sitter.Node) int {
	binds := node.ChildByFieldName("binds")
	if binds == nil {
		return shared.NodeEndLine(node)
	}
	lastMatchEnd := 0
	for _, child := range haskellNamedChildren(node) {
		child := child
		if child.Kind() == "match" {
			if end := shared.NodeEndLine(&child); end > lastMatchEnd {
				lastMatchEnd = end
			}
		}
	}
	if lastMatchEnd <= 0 {
		return shared.NodeEndLine(node)
	}
	return lastMatchEnd
}

// ensureFunctionItem returns the existing function row for key or appends a new
// one. End line is widened from the binding span so multi-clause functions keep
// their full extent; source covers the defining clauses up to sourceEndLine.
func (e *haskellExtractor) ensureFunctionItem(
	key, name, context string,
	rootKinds []string,
	startLine, endLine, sourceEndLine int,
) (map[string]any, bool) {
	if existing, ok := e.functionItems[key]; ok {
		if shared.IntValue(existing["end_line"]) < endLine {
			existing["end_line"] = endLine
		}
		return existing, false
	}
	item := map[string]any{
		"name":        name,
		"line_number": startLine,
		"end_line":    endLine,
		"lang":        "haskell",
		"decorators":  []string{},
	}
	if context != "" {
		item["class_context"] = context
	}
	if len(rootKinds) > 0 {
		item["dead_code_root_kinds"] = rootKinds
	}
	if e.options.IndexSource {
		item["source"] = haskellLineRangeSource(e.lines, startLine, sourceEndLine)
	}
	e.seenFunctions[key] = struct{}{}
	e.functionItems[key] = item
	shared.AppendBucket(e.payload, "functions", item)
	return item, true
}

// appendBindingVariables records the where-block local bindings of a function as
// variable rows, matching the prior where-block variable contract. Local names
// stay in the variables bucket and never become top-level functions.
func (e *haskellExtractor) appendBindingVariables(node *tree_sitter.Node) {
	binds := node.ChildByFieldName("binds")
	if binds == nil {
		return
	}
	for _, decl := range haskellNamedChildren(binds) {
		decl := decl
		if decl.Kind() != "bind" && decl.Kind() != "function" {
			continue
		}
		localName := haskellBindingName(&decl, e.source)
		if localName == "" || haskellIsKeyword(localName) {
			continue
		}
		if _, ok := e.seenVariables[localName]; !ok {
			e.seenVariables[localName] = struct{}{}
			localLine := shared.NodeLine(&decl)
			shared.AppendBucket(e.payload, "variables", map[string]any{
				"name":        localName,
				"line_number": localLine,
				"end_line":    localLine,
				"lang":        "haskell",
			})
		}
	}
}

// appendBindingCalls runs the bounded lexical call helper over each source line in
// the binding span. Call extraction is the documented evidence exception: it reads
// right-hand-side tokens rather than resolving Haskell name binding.
func (e *haskellExtractor) appendBindingCalls(
	functionName, context string,
	params map[string]struct{},
	startLine, endLine int,
) {
	for lineNumber := startLine; lineNumber <= endLine && lineNumber <= len(e.lines); lineNumber++ {
		line := e.lines[lineNumber-1]
		if lineNumber == startLine {
			haskellAppendFunctionCalls(e.payload, line, lineNumber, functionName, context, params, e.seenCalls)
			continue
		}
		haskellAppendExpressionCalls(e.payload, line, lineNumber, functionName, context, params, e.seenCalls)
	}
}
