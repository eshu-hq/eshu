package perl

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

type perlFunctionSpan struct {
	packageName string
	item        map[string]any
}

type perlSyntaxIndex struct {
	classes          []map[string]any
	imports          []map[string]any
	functions        []perlFunctionSpan
	variables        []map[string]any
	calls            []map[string]any
	exportsByPackage map[string]map[string]struct{}
	seenVariables    map[string]struct{}
	seenCalls        map[string]struct{}
}

func perlSourceAndSyntax(path string, parser *tree_sitter.Parser) ([]byte, perlSyntaxIndex, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, perlSyntaxIndex{}, err
	}
	if parser == nil {
		return nil, perlSyntaxIndex{}, fmt.Errorf("parse perl tree: nil parser")
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, perlSyntaxIndex{}, fmt.Errorf("parse perl tree: parser returned nil tree")
	}
	defer tree.Close()

	index := perlSyntaxIndex{
		exportsByPackage: make(map[string]map[string]struct{}),
		seenVariables:    make(map[string]struct{}),
		seenCalls:        make(map[string]struct{}),
	}
	index.collect(tree.RootNode(), source, path, "")
	return source, index, nil
}

func (i *perlSyntaxIndex) collect(node *tree_sitter.Node, source []byte, path string, packageName string) string {
	if node == nil {
		return packageName
	}

	switch node.Kind() {
	case "package_statement":
		if name := perlFirstDescendantText(node, source, "package"); name != "" {
			packageName = name
			item := map[string]any{
				"name":        shared.LastPathSegment(name, "::"),
				"full_name":   name,
				"line_number": shared.NodeLine(node),
				"end_line":    shared.NodeEndLine(node),
				"lang":        "perl",
			}
			if perlIsPublicPackage(name) {
				item["dead_code_root_kinds"] = []string{"perl.package_namespace"}
			}
			i.classes = append(i.classes, item)
		}
	case "use_statement":
		if name := perlFirstDescendantText(node, source, "package"); name != "" {
			i.imports = append(i.imports, map[string]any{
				"name":        name,
				"line_number": shared.NodeLine(node),
				"lang":        "perl",
			})
		}
	case "assignment_expression":
		i.collectExportNames(node, source, packageName)
	case "phaser_statement":
		if name := perlPhaserName(node, source); name != "" {
			item := map[string]any{
				"name":                 name,
				"line_number":          shared.NodeLine(node),
				"end_line":             shared.NodeEndLine(node),
				"lang":                 "perl",
				"decorators":           []string{},
				"dead_code_root_kinds": []string{"perl.special_block"},
			}
			if packageName != "" {
				item["class_context"] = shared.LastPathSegment(packageName, "::")
				item["full_name"] = packageName + "::" + name
			}
			i.functions = append(i.functions, perlFunctionSpan{packageName: packageName, item: item})
		}
	case "subroutine_declaration_statement":
		if name := perlFirstDirectChildText(node, source, "bareword", "identifier"); name != "" {
			item := map[string]any{
				"name":                  name,
				"line_number":           shared.NodeLine(node),
				"end_line":              shared.NodeEndLine(node),
				"lang":                  "perl",
				"decorators":            []string{},
				"cyclomatic_complexity": perlCyclomaticComplexity(node, source),
			}
			if packageName != "" {
				item["class_context"] = shared.LastPathSegment(packageName, "::")
				item["full_name"] = packageName + "::" + name
			}
			addPerlRootKind(item, perlFunctionRootKinds(name, packageName, i.exportsByPackage[packageName], path)...)
			i.functions = append(i.functions, perlFunctionSpan{packageName: packageName, item: item})
		}
	case "variable_declaration":
		i.appendVariables(node, source)
	case "function_call_expression", "ambiguous_function_call_expression", "func0op_call_expression", "func1op_call_expression", "method_call_expression":
		if name := perlCallName(node, source); name != "" {
			i.appendCall(name, shared.NodeLine(node))
		}
	}

	for _, child := range perlNamedChildren(node) {
		child := child
		packageName = i.collect(&child, source, path, packageName)
	}
	return packageName
}

func (i *perlSyntaxIndex) collectExportNames(node *tree_sitter.Node, source []byte, packageName string) {
	exportName := ""
	shared.WalkNamed(node, func(child *tree_sitter.Node) {
		if child.Kind() == "varname" {
			name := strings.TrimSpace(shared.NodeText(child, source))
			if name == "EXPORT" || name == "EXPORT_OK" {
				exportName = name
			}
		}
	})
	if exportName == "" {
		return
	}
	exportedSubs := perlExportsForPackage(i.exportsByPackage, packageName)
	shared.WalkNamed(node, func(child *tree_sitter.Node) {
		if child.Kind() == "string_content" {
			perlCollectExportNames(shared.NodeText(child, source), exportedSubs)
		}
	})
}

func (i *perlSyntaxIndex) appendVariables(node *tree_sitter.Node, source []byte) {
	shared.WalkNamed(node, func(child *tree_sitter.Node) {
		if child.Kind() != "varname" {
			return
		}
		name := strings.TrimSpace(shared.NodeText(child, source))
		if name == "" || name == "_" || name == "EXPORT" || name == "EXPORT_OK" {
			return
		}
		if _, ok := i.seenVariables[name]; ok {
			return
		}
		i.seenVariables[name] = struct{}{}
		i.variables = append(i.variables, map[string]any{
			"name":        name,
			"line_number": shared.NodeLine(child),
			"end_line":    shared.NodeEndLine(child),
			"lang":        "perl",
		})
	})
}

func (i *perlSyntaxIndex) appendCall(name string, lineNumber int) {
	if strings.TrimSpace(name) == "" {
		return
	}
	if _, ok := i.seenCalls[name]; ok {
		return
	}
	i.seenCalls[name] = struct{}{}
	i.calls = append(i.calls, map[string]any{
		"name":        name,
		"full_name":   name,
		"line_number": lineNumber,
		"lang":        "perl",
	})
}

func perlCallName(node *tree_sitter.Node, source []byte) string {
	switch node.Kind() {
	case "method_call_expression":
		return perlFirstDescendantText(node, source, "method")
	default:
		return perlFirstDescendantText(node, source, "function", "bareword", "identifier")
	}
}

func perlFirstDescendantText(node *tree_sitter.Node, source []byte, kinds ...string) string {
	if node == nil {
		return ""
	}
	kindSet := make(map[string]struct{}, len(kinds))
	for _, kind := range kinds {
		kindSet[kind] = struct{}{}
	}
	result := ""
	shared.WalkNamed(node, func(child *tree_sitter.Node) {
		if result != "" {
			return
		}
		if _, ok := kindSet[child.Kind()]; ok {
			result = strings.TrimSpace(shared.NodeText(child, source))
		}
	})
	return result
}

func perlFirstDirectChildText(node *tree_sitter.Node, source []byte, kinds ...string) string {
	kindSet := make(map[string]struct{}, len(kinds))
	for _, kind := range kinds {
		kindSet[kind] = struct{}{}
	}
	for _, child := range perlNamedChildren(node) {
		child := child
		if _, ok := kindSet[child.Kind()]; ok {
			return strings.TrimSpace(shared.NodeText(&child, source))
		}
	}
	return ""
}

func perlPhaserName(node *tree_sitter.Node, source []byte) string {
	text := strings.TrimSpace(shared.NodeText(node, source))
	name, _, _ := strings.Cut(text, " ")
	name, _, _ = strings.Cut(name, "{")
	switch name {
	case "BEGIN", "UNITCHECK", "CHECK", "INIT", "END":
		return name
	default:
		return ""
	}
}

func perlNamedChildren(node *tree_sitter.Node) []tree_sitter.Node {
	if node == nil {
		return nil
	}
	cursor := node.Walk()
	defer cursor.Close()
	return node.NamedChildren(cursor)
}

func perlLineRangeSource(source []byte, startLine int, endLine int) string {
	if startLine <= 0 || endLine < startLine {
		return ""
	}
	lines := strings.Split(string(source), "\n")
	if startLine > len(lines) {
		return ""
	}
	if endLine > len(lines) {
		endLine = len(lines)
	}
	return strings.Join(lines[startLine-1:endLine], "\n")
}
