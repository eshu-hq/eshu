package haskell

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// haskellModuleSymbol is the tree-sitter view of a module header: its dotted
// name, the 1-based header line span, and the explicitly exported names.
type haskellModuleSymbol struct {
	name      string
	startLine int
	endLine   int
	exports   map[string]struct{}
}

// haskellTypeSymbol is the tree-sitter view of a data, newtype, type-synonym,
// data-family, class, or instance declaration that lands in the classes bucket.
type haskellTypeSymbol struct {
	name         string
	semanticKind string
	startLine    int
	endLine      int
}

// haskellMethodSymbol is the tree-sitter view of a class-method type signature
// or instance-method binding that lands in the functions bucket with a class or
// instance context.
type haskellMethodSymbol struct {
	name      string
	context   string
	rootKind  string
	startLine int
	endLine   int
	source    string
	hasSource bool
	params    map[string]struct{}
}

// haskellValueSymbol is the tree-sitter view of a top-level value binding: a
// function with patterns, a guarded function, or a parameterless bind. It lands
// in the functions bucket without a class context.
type haskellValueSymbol struct {
	name      string
	startLine int
	endLine   int
	source    string
	params    map[string]struct{}
	firstLine string
	hasEqual  bool
}

// collectSymbols walks the declaration scopes of the syntax tree and records the
// module header, classes-bucket declarations, class/instance methods, and
// top-level value bindings so ParseWithParser can build every primary symbol
// bucket from the AST instead of a line scan.
func (i *haskellSyntaxIndex) collectSymbols(node *tree_sitter.Node, source []byte, lines []string) {
	if node == nil {
		return
	}
	switch node.Kind() {
	case "header":
		i.module = haskellModuleFromHeader(node, source)
		return
	case "data_type", "newtype", "type_synomym", "data_family":
		if sym, ok := haskellTypeDeclFromNode(node, source); ok {
			i.types = append(i.types, sym)
		}
		return
	case "class":
		i.collectClass(node, source, lines)
		return
	case "instance":
		i.collectInstance(node, source, lines)
		return
	case "function", "bind":
		if sym, ok := haskellValueFromNode(node, source, lines); ok {
			i.values = append(i.values, sym)
		}
		return
	}
	for _, child := range haskellNamedChildren(node) {
		child := child
		i.collectSymbols(&child, source, lines)
	}
}

func haskellModuleFromHeader(node *tree_sitter.Node, source []byte) *haskellModuleSymbol {
	moduleNode := node.ChildByFieldName("module")
	if moduleNode == nil {
		return nil
	}
	name := strings.TrimSpace(shared.NodeText(moduleNode, source))
	if name == "" {
		return nil
	}
	return &haskellModuleSymbol{
		name:      name,
		startLine: shared.NodeLine(node),
		endLine:   shared.NodeEndLine(node),
		exports:   haskellExportNames(node.ChildByFieldName("exports"), source),
	}
}

// haskellExportNames reads the explicit export list, mirroring the line-scan
// export parser: each export contributes its leading value or type name.
func haskellExportNames(exportsNode *tree_sitter.Node, source []byte) map[string]struct{} {
	exports := make(map[string]struct{})
	if exportsNode == nil {
		return exports
	}
	for _, child := range haskellNamedChildren(exportsNode) {
		child := child
		if child.Kind() != "export" {
			continue
		}
		if name := haskellExportName(&child, source); name != "" {
			exports[name] = struct{}{}
		}
	}
	return exports
}

func haskellExportName(exportNode *tree_sitter.Node, source []byte) string {
	if named := exportNode.ChildByFieldName("type"); named != nil {
		return strings.TrimSpace(shared.NodeText(named, source))
	}
	for _, child := range haskellNamedChildren(exportNode) {
		child := child
		switch child.Kind() {
		case "variable", "name", "constructor", "qualified":
			return strings.TrimSpace(shared.NodeText(&child, source))
		}
	}
	return ""
}

func haskellTypeDeclFromNode(node *tree_sitter.Node, source []byte) (haskellTypeSymbol, bool) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return haskellTypeSymbol{}, false
	}
	name := strings.TrimSpace(shared.NodeText(nameNode, source))
	if name == "" {
		return haskellTypeSymbol{}, false
	}
	line := shared.NodeLine(node)
	return haskellTypeSymbol{
		name:         name,
		semanticKind: haskellTypeSemanticKind(node.Kind()),
		startLine:    line,
		endLine:      line,
	}, true
}

// haskellTypeSemanticKind maps tree-sitter declaration node kinds to the
// semantic_kind values the line-scan extractor emitted: the data/newtype/type
// keyword. data_family declarations are reported as data, matching the prior
// `(data|newtype|type)\s+(?:family\s+)?` capture group.
func haskellTypeSemanticKind(kind string) string {
	switch kind {
	case "newtype":
		return "newtype"
	case "type_synomym":
		return "type"
	default:
		return "data"
	}
}

func (i *haskellSyntaxIndex) collectClass(node *tree_sitter.Node, source []byte, lines []string) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	name := strings.TrimSpace(shared.NodeText(nameNode, source))
	if name == "" {
		return
	}
	line := shared.NodeLine(node)
	i.types = append(i.types, haskellTypeSymbol{
		name:         name,
		semanticKind: "typeclass",
		startLine:    line,
		endLine:      line,
	})
	decls := node.ChildByFieldName("declarations")
	if decls == nil {
		return
	}
	for _, child := range haskellNamedChildren(decls) {
		child := child
		if child.Kind() != "signature" {
			continue
		}
		methodName := haskellSignatureName(&child, source)
		if methodName == "" || haskellIsKeyword(methodName) {
			continue
		}
		sigLine := shared.NodeLine(&child)
		i.methods = append(i.methods, haskellMethodSymbol{
			name:      methodName,
			context:   name,
			rootKind:  "haskell.typeclass_method",
			startLine: sigLine,
			endLine:   sigLine,
		})
	}
}

func (i *haskellSyntaxIndex) collectInstance(node *tree_sitter.Node, source []byte, lines []string) {
	context := haskellInstanceContext(node, source)
	if context == "" {
		return
	}
	decls := node.ChildByFieldName("declarations")
	if decls == nil {
		return
	}
	for _, child := range haskellNamedChildren(decls) {
		child := child
		if child.Kind() != "function" && child.Kind() != "bind" {
			continue
		}
		nameNode := child.ChildByFieldName("name")
		if nameNode == nil {
			continue
		}
		methodName := strings.TrimSpace(shared.NodeText(nameNode, source))
		if methodName == "" || haskellIsKeyword(methodName) {
			continue
		}
		startLine := shared.NodeLine(&child)
		endLine := shared.NodeEndLine(&child)
		i.methods = append(i.methods, haskellMethodSymbol{
			name:      methodName,
			context:   context,
			rootKind:  "haskell.instance_method",
			startLine: startLine,
			endLine:   endLine,
			source:    haskellNodeFirstLineSource(lines, startLine),
			hasSource: true,
			params:    haskellTreeFunctionParameters(&child, source),
		})
	}
}

// haskellInstanceContext renders the instance head as the line-scan extractor
// did: the head class name joined with its type patterns by single spaces, for
// example "Runner Worker".
func haskellInstanceContext(node *tree_sitter.Node, source []byte) string {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return ""
	}
	parts := []string{strings.TrimSpace(shared.NodeText(nameNode, source))}
	if patterns := node.ChildByFieldName("patterns"); patterns != nil {
		for _, child := range haskellNamedChildren(patterns) {
			child := child
			text := strings.TrimSpace(shared.NodeText(&child, source))
			if text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(strings.Fields(strings.Join(parts, " ")), " ")
}

func haskellSignatureName(node *tree_sitter.Node, source []byte) string {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return ""
	}
	return strings.TrimSpace(shared.NodeText(nameNode, source))
}

func haskellValueFromNode(node *tree_sitter.Node, source []byte, lines []string) (haskellValueSymbol, bool) {
	if !haskellTreeFunctionInDeclarationScope(node) {
		return haskellValueSymbol{}, false
	}
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return haskellValueSymbol{}, false
	}
	name := strings.TrimSpace(shared.NodeText(nameNode, source))
	if name == "" || haskellIsKeyword(name) {
		return haskellValueSymbol{}, false
	}
	startLine := shared.NodeLine(node)
	endLine := shared.NodeEndLine(node)
	firstLine := ""
	if startLine >= 1 && startLine <= len(lines) {
		firstLine = lines[startLine-1]
	}
	return haskellValueSymbol{
		name:      name,
		startLine: startLine,
		endLine:   endLine,
		source:    haskellLineRangeSource(lines, startLine, endLine),
		params:    haskellTreeFunctionParameters(node, source),
		firstLine: firstLine,
		hasEqual:  strings.Contains(firstLine, "="),
	}, true
}

func haskellNodeFirstLineSource(lines []string, startLine int) string {
	if startLine < 1 || startLine > len(lines) {
		return ""
	}
	return lines[startLine-1]
}
