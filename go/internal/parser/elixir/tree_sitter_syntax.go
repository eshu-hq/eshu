package elixir

import (
	"fmt"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

type elixirFunctionSpan struct {
	keyword    string
	name       string
	moduleName string
	decorators []string
	args       []string
	source     string
	startLine  int
	endLine    int
}

type elixirSyntaxIndex struct {
	functions []elixirFunctionSpan
}

func elixirSourceAndSyntax(path string, parser *tree_sitter.Parser) ([]byte, elixirSyntaxIndex, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, elixirSyntaxIndex{}, err
	}
	if parser == nil {
		return nil, elixirSyntaxIndex{}, fmt.Errorf("parse elixir tree: nil parser")
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, elixirSyntaxIndex{}, fmt.Errorf("parse elixir tree: parser returned nil tree")
	}
	defer tree.Close()

	index := elixirSyntaxIndex{}
	lines := strings.Split(string(source), "\n")
	index.collect(tree.RootNode(), source, lines, "")
	return source, index, nil
}

func (i *elixirSyntaxIndex) collect(node *tree_sitter.Node, source []byte, lines []string, moduleName string) {
	if node == nil {
		return
	}

	nextModuleName := moduleName
	if node.Kind() == "call" {
		head := elixirCallHead(node, source)
		if elixirModuleKeyword(head) {
			if name := elixirDefinitionName(node, source); name != "" {
				nextModuleName = name
			}
		}
		if elixirFunctionKeyword(head) {
			if fn := elixirFunctionFromTreeCall(node, source, lines, moduleName, head); fn.name != "" {
				i.functions = append(i.functions, fn)
			}
		}
	}

	for _, child := range elixirNamedChildren(node) {
		child := child
		i.collect(&child, source, lines, nextModuleName)
	}
}

func elixirFunctionFromTreeCall(
	node *tree_sitter.Node,
	source []byte,
	lines []string,
	moduleName string,
	keyword string,
) elixirFunctionSpan {
	target := elixirDefinitionTargetCall(node)
	if target == nil {
		return elixirFunctionSpan{}
	}
	name := elixirCallHead(target, source)
	if name == "" {
		return elixirFunctionSpan{}
	}
	startLine := shared.NodeLine(node)
	endLine := shared.NodeEndLine(node)
	return elixirFunctionSpan{
		keyword:    keyword,
		name:       name,
		moduleName: moduleName,
		decorators: elixirDecoratorsBeforeLine(lines, startLine),
		args:       elixirCallArgumentTexts(target, source),
		source:     elixirLineRangeSource(lines, startLine, endLine),
		startLine:  startLine,
		endLine:    endLine,
	}
}

func elixirDefinitionTargetCall(node *tree_sitter.Node) *tree_sitter.Node {
	for _, child := range elixirNamedChildren(node) {
		child := child
		if child.Kind() != "arguments" {
			continue
		}
		for _, argument := range elixirNamedChildren(&child) {
			argument := argument
			if target := elixirDefinitionTargetArgument(&argument); target != nil {
				target := *target
				return &target
			}
		}
	}
	return nil
}

func elixirDefinitionTargetArgument(argument *tree_sitter.Node) *tree_sitter.Node {
	if argument == nil {
		return nil
	}
	if argument.Kind() == "call" {
		return argument
	}
	if argument.Kind() != "binary_operator" {
		return nil
	}
	for _, child := range elixirNamedChildren(argument) {
		child := child
		if target := elixirDefinitionTargetArgument(&child); target != nil {
			return target
		}
	}
	return nil
}

func elixirDefinitionName(node *tree_sitter.Node, source []byte) string {
	for _, child := range elixirNamedChildren(node) {
		child := child
		if child.Kind() != "arguments" {
			continue
		}
		for _, argument := range elixirNamedChildren(&child) {
			argument := argument
			switch argument.Kind() {
			case "alias", "identifier":
				return strings.TrimSpace(shared.NodeText(&argument, source))
			}
		}
	}
	return ""
}

func elixirCallHead(node *tree_sitter.Node, source []byte) string {
	children := elixirNamedChildren(node)
	if len(children) == 0 {
		return ""
	}
	child := children[0]
	switch child.Kind() {
	case "identifier", "operator_identifier":
		return strings.TrimSpace(shared.NodeText(&child, source))
	default:
		return ""
	}
}

func elixirCallArgumentTexts(node *tree_sitter.Node, source []byte) []string {
	for _, child := range elixirNamedChildren(node) {
		child := child
		if child.Kind() != "arguments" {
			continue
		}
		args := make([]string, 0)
		for _, argument := range elixirNamedChildren(&child) {
			argument := argument
			text := strings.TrimSpace(shared.NodeText(&argument, source))
			if text != "" {
				args = append(args, text)
			}
		}
		return args
	}
	return nil
}

func elixirNamedChildren(node *tree_sitter.Node) []tree_sitter.Node {
	if node == nil {
		return nil
	}
	cursor := node.Walk()
	defer cursor.Close()
	return node.NamedChildren(cursor)
}

func elixirModuleKeyword(keyword string) bool {
	switch keyword {
	case "defmodule", "defprotocol", "defimpl":
		return true
	default:
		return false
	}
}

func elixirFunctionKeyword(keyword string) bool {
	switch keyword {
	case "def", "defp", "defmacro", "defmacrop", "defdelegate", "defguard", "defguardp":
		return true
	default:
		return false
	}
}

func elixirDecoratorsBeforeLine(lines []string, line int) []string {
	decorators := make([]string, 0)
	for index := line - 2; index >= 0; index-- {
		trimmed := strings.TrimSpace(lines[index])
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, "@") {
			break
		}
		decorators = append(decorators, trimmed)
	}
	slices.Reverse(decorators)
	return decorators
}

func elixirLineRangeSource(lines []string, startLine int, endLine int) string {
	if startLine <= 0 || endLine < startLine || startLine > len(lines) {
		return ""
	}
	if endLine > len(lines) {
		endLine = len(lines)
	}
	return strings.Join(lines[startLine-1:endLine], "\n")
}

func applyElixirTreeFunctionMetadata(
	payload map[string]any,
	syntax elixirSyntaxIndex,
	facts elixirDeadCodeFacts,
	options shared.Options,
) {
	functions, _ := payload["functions"].([]map[string]any)
	for _, fn := range syntax.functions {
		item := elixirFunctionItem(functions, fn)
		if item == nil {
			item = map[string]any{
				"name":          fn.name,
				"line_number":   fn.startLine,
				"lang":          "elixir",
				"is_dependency": payload["is_dependency"],
				"visibility":    "public",
				"type":          fn.keyword,
				"semantic_kind": functionSemanticKind(fn.keyword),
			}
			shared.AppendBucket(payload, "functions", item)
			functions, _ = payload["functions"].([]map[string]any)
		}
		item["args"] = slices.Clone(fn.args)
		item["end_line"] = fn.endLine
		if fn.moduleName != "" {
			item["class_context"] = fn.moduleName
			item["context_type"] = "module"
			item["context"] = []any{fn.moduleName, "module", elixirModuleLine(payload, fn.moduleName)}
		}
		if len(fn.decorators) > 0 {
			item["decorators"] = slices.Clone(fn.decorators)
		}
		if options.IndexSource {
			item["source"] = fn.source
		}
		if strings.HasSuffix(fn.keyword, "p") {
			item["visibility"] = "private"
		}
		rootKinds := elixirFunctionDeadCodeRootKinds(
			fn.keyword,
			fn.name,
			fn.args,
			fn.moduleName,
			elixirModuleKind(payload, fn.moduleName),
			elixirHasImplDecorator(fn.decorators),
			facts,
		)
		for _, rootKind := range rootKinds {
			item["dead_code_root_kinds"] = appendElixirMetadataString(item["dead_code_root_kinds"], rootKind)
		}
		if rootKinds, ok := item["dead_code_root_kinds"].([]string); ok {
			slices.Sort(rootKinds)
			item["dead_code_root_kinds"] = rootKinds
		}
	}
}

func applyElixirTreeCallContext(payload map[string]any, syntax elixirSyntaxIndex) {
	calls, _ := payload["function_calls"].([]map[string]any)
	for _, call := range calls {
		fn := elixirInnermostFunctionSpan(syntax.functions, shared.IntValue(call["line_number"]))
		if fn == nil {
			continue
		}
		call["context"] = []any{fn.name, "function", fn.startLine}
		if fn.moduleName != "" {
			call["class_context"] = fn.moduleName
		}
	}
}

func elixirInnermostFunctionSpan(functions []elixirFunctionSpan, line int) *elixirFunctionSpan {
	if line <= 0 {
		return nil
	}
	var best *elixirFunctionSpan
	for index := range functions {
		fn := &functions[index]
		if line < fn.startLine || line > fn.endLine {
			continue
		}
		if best == nil || fn.startLine >= best.startLine && fn.endLine <= best.endLine {
			best = fn
		}
	}
	return best
}

func elixirFunctionItem(functions []map[string]any, fn elixirFunctionSpan) map[string]any {
	for _, item := range functions {
		if item["name"] == fn.name && shared.IntValue(item["line_number"]) == fn.startLine {
			return item
		}
	}
	for _, item := range functions {
		if item["name"] == fn.name && item["class_context"] == fn.moduleName {
			return item
		}
	}
	return nil
}

func elixirModuleLine(payload map[string]any, moduleName string) int {
	for _, bucket := range []string{"modules", "protocols"} {
		items, _ := payload[bucket].([]map[string]any)
		for _, item := range items {
			if item["name"] == moduleName {
				return shared.IntValue(item["line_number"])
			}
		}
	}
	return 0
}

func elixirModuleKind(payload map[string]any, moduleName string) string {
	for _, bucket := range []string{"modules", "protocols"} {
		items, _ := payload[bucket].([]map[string]any)
		for _, item := range items {
			if item["name"] == moduleName {
				kind, _ := item["module_kind"].(string)
				return kind
			}
		}
	}
	return ""
}

func elixirHasImplDecorator(decorators []string) bool {
	return slices.ContainsFunc(decorators, func(decorator string) bool {
		return elixirDecoratorIsImpl(strings.TrimSpace(decorator))
	})
}

func elixirDecoratorIsImpl(decorator string) bool {
	if decorator == "@impl" {
		return true
	}
	if !strings.HasPrefix(decorator, "@impl") {
		return false
	}
	switch decorator[len("@impl")] {
	case ' ', '\t', '(':
		return true
	default:
		return false
	}
}
