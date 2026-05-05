package parser

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

var rustLifetimePattern = regexp.MustCompile(`'([A-Za-z_][A-Za-z0-9_]*)`)

func (e *Engine) parseRust(
	path string,
	isDependency bool,
	options Options,
) (map[string]any, error) {
	parser, err := e.runtime.Parser("rust")
	if err != nil {
		return nil, err
	}
	defer parser.Close()

	source, err := readSource(path)
	if err != nil {
		return nil, err
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, fmt.Errorf("parse rust file %q: parser returned nil tree", path)
	}
	defer tree.Close()

	payload := basePayload(path, "rust", isDependency)
	payload["impl_blocks"] = []map[string]any{}
	payload["traits"] = []map[string]any{}
	root := tree.RootNode()

	walkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "impl_item":
			appendRustImplBlock(payload, node, source)
		case "function_item", "function_signature_item":
			appendRustFunction(payload, node, source, options)
		case "struct_item", "enum_item", "union_item":
			nameNode := firstNamedDescendant(node, "type_identifier")
			name := nodeText(nameNode, source)
			if strings.TrimSpace(name) == "" {
				return
			}
			appendBucket(payload, "classes", map[string]any{
				"name":        name,
				"line_number": nodeLine(nameNode),
				"end_line":    nodeEndLine(node),
				"lang":        "rust",
			})
		case "trait_item":
			nameNode := firstNamedDescendant(node, "type_identifier")
			name := nodeText(nameNode, source)
			if strings.TrimSpace(name) == "" {
				return
			}
			appendBucket(payload, "traits", map[string]any{
				"name":        name,
				"line_number": nodeLine(nameNode),
				"end_line":    nodeEndLine(node),
				"lang":        "rust",
			})
		case "use_declaration":
			appendRustImportMetadata(payload, node, source)
		case "call_expression":
			appendRustCall(payload, node, source)
		case "macro_invocation":
			appendRustCall(payload, node, source)
		}
	})

	sortSystemsPayload(payload, "functions", "classes", "traits", "imports", "function_calls", "impl_blocks")
	payload["framework_semantics"] = map[string]any{"frameworks": []string{}}

	return payload, nil
}

func (e *Engine) preScanRust(path string) ([]string, error) {
	payload, err := e.parseRust(path, false, Options{})
	if err != nil {
		return nil, err
	}
	names := collectBucketNames(payload, "functions", "classes", "traits", "impl_blocks")
	slices.Sort(names)
	return names, nil
}

func appendRustImplBlock(payload map[string]any, node *tree_sitter.Node, source []byte) {
	header := strings.TrimSpace(nodeText(node, source))
	if idx := strings.Index(header, "{"); idx >= 0 {
		header = header[:idx]
	}
	header = strings.TrimSpace(strings.TrimPrefix(header, "impl"))
	lifetimeParameters := rustLeadingLifetimeParameters(header)
	signatureLifetimes := rustLifetimeNames(header)
	header = strings.TrimSpace(rustStripTypeParameters(header))

	kind := "inherent_impl"
	traitName := ""
	targetName := header

	if idx := strings.Index(header, " for "); idx >= 0 {
		kind = "trait_impl"
		traitName = strings.TrimSpace(header[:idx])
		targetName = strings.TrimSpace(header[idx+len(" for "):])
	}
	if idx := strings.Index(targetName, " where "); idx >= 0 {
		targetName = strings.TrimSpace(targetName[:idx])
	}

	item := map[string]any{
		"name":        rustBaseTypeName(targetName),
		"target":      targetName,
		"line_number": nodeLine(node),
		"end_line":    nodeEndLine(node),
		"kind":        kind,
		"lang":        "rust",
	}
	if traitName != "" {
		item["trait"] = rustBaseTypeName(traitName)
	}
	if len(lifetimeParameters) > 0 {
		item["lifetime_parameters"] = lifetimeParameters
	}
	if len(signatureLifetimes) > 0 {
		item["signature_lifetimes"] = signatureLifetimes
	}
	appendBucket(payload, "impl_blocks", item)
}

func appendRustFunction(payload map[string]any, node *tree_sitter.Node, source []byte, options Options) {
	nameNode := firstNamedDescendant(node, "identifier")
	name := nodeText(nameNode, source)
	if strings.TrimSpace(name) == "" {
		return
	}

	item := map[string]any{
		"name":        name,
		"line_number": nodeLine(nameNode),
		"end_line":    nodeEndLine(node),
		"decorators":  []string{},
		"lang":        "rust",
	}
	signature := rustSignatureHeader(nodeText(node, source))
	if lifetimeParameters := rustFunctionLifetimeParameters(signature, name); len(lifetimeParameters) > 0 {
		item["lifetime_parameters"] = lifetimeParameters
	}
	if signatureLifetimes := rustLifetimeNames(signature); len(signatureLifetimes) > 0 {
		item["signature_lifetimes"] = signatureLifetimes
	}
	if returnLifetime := rustReturnLifetime(signature); returnLifetime != "" {
		item["return_lifetime"] = returnLifetime
	}
	if implContext := rustImplContext(node, source); implContext != "" {
		item["impl_context"] = implContext
	}
	if options.IndexSource {
		item["source"] = nodeText(node, source)
	}
	appendBucket(payload, "functions", item)
}

func appendRustImportMetadata(payload map[string]any, node *tree_sitter.Node, source []byte) {
	raw := strings.TrimSpace(nodeText(node, source))
	if raw == "" {
		return
	}

	importText := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(raw, "use "), ";"))
	if importText == "" {
		return
	}

	importType := "use"
	alias := rustImportAlias(importText)
	if aliasIndex := strings.Index(importText, " as "); aliasIndex >= 0 {
		importType = "alias"
		alias = strings.TrimSpace(importText[aliasIndex+len(" as "):])
		importText = strings.TrimSpace(importText[:aliasIndex])
	}

	appendBucket(payload, "imports", map[string]any{
		"name":             importText,
		"source":           importText,
		"alias":            alias,
		"full_import_name": raw,
		"import_type":      importType,
		"line_number":      nodeLine(node),
		"lang":             "rust",
	})
}

func rustImplContext(node *tree_sitter.Node, source []byte) string {
	for current := node.Parent(); current != nil; current = current.Parent() {
		if current.Kind() != "impl_item" {
			continue
		}
		typeNode := current.ChildByFieldName("type")
		implContext := nodeText(typeNode, source)
		implContext = strings.TrimSpace(implContext)
		if implContext == "" {
			return ""
		}
		implContext = strings.TrimSuffix(implContext, ";")
		implContext = strings.TrimSpace(implContext)
		if idx := strings.LastIndex(implContext, "::"); idx >= 0 {
			implContext = implContext[idx+2:]
		}
		if idx := strings.Index(implContext, "<"); idx >= 0 {
			implContext = implContext[:idx]
		}
		return strings.TrimSpace(implContext)
	}
	return ""
}

func rustStripTypeParameters(text string) string {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "<") {
		return trimmed
	}
	if end := strings.Index(trimmed, ">"); end >= 0 {
		return strings.TrimSpace(trimmed[end+1:])
	}
	return trimmed
}

func rustImportAlias(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	if strings.Contains(trimmed, "{") || strings.HasSuffix(trimmed, "::*") {
		return ""
	}
	if idx := strings.LastIndex(trimmed, "::"); idx >= 0 {
		return strings.TrimSpace(trimmed[idx+2:])
	}
	return trimmed
}

func rustBaseTypeName(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	if idx := strings.Index(trimmed, "<"); idx >= 0 {
		trimmed = trimmed[:idx]
	}
	if idx := strings.LastIndex(trimmed, "::"); idx >= 0 {
		trimmed = trimmed[idx+2:]
	}
	return strings.TrimSpace(trimmed)
}

func rustFunctionLifetimeParameters(signature string, name string) []string {
	marker := "fn " + name
	idx := strings.Index(signature, marker)
	if idx < 0 {
		return nil
	}
	remainder := strings.TrimSpace(signature[idx+len(marker):])
	if !strings.HasPrefix(remainder, "<") {
		return nil
	}
	segment, ok := rustLeadingAngleSegment(remainder)
	if !ok {
		return nil
	}
	return rustLifetimeNames(segment)
}

func rustLeadingLifetimeParameters(signature string) []string {
	trimmed := strings.TrimSpace(signature)
	if !strings.HasPrefix(trimmed, "<") {
		return nil
	}
	segment, ok := rustLeadingAngleSegment(trimmed)
	if !ok {
		return nil
	}
	return rustLifetimeNames(segment)
}

func rustLeadingAngleSegment(text string) (string, bool) {
	if !strings.HasPrefix(text, "<") {
		return "", false
	}
	depth := 0
	for idx, r := range text {
		switch r {
		case '<':
			depth++
		case '>':
			depth--
			if depth == 0 {
				return text[:idx+1], true
			}
		}
	}
	return "", false
}

func rustLifetimeNames(text string) []string {
	matches := rustLifetimePattern.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}

	names := make([]string, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		name := match[1]
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	if len(names) == 0 {
		return nil
	}
	return names
}

func rustReturnLifetime(signature string) string {
	idx := strings.Index(signature, "->")
	if idx < 0 {
		return ""
	}
	returnType := strings.TrimSpace(signature[idx+len("->"):])
	lifetimes := rustLifetimeNames(returnType)
	if len(lifetimes) == 0 {
		return ""
	}
	return lifetimes[0]
}

func rustSignatureHeader(text string) string {
	signature := strings.TrimSpace(text)
	if idx := strings.Index(signature, "{"); idx >= 0 {
		signature = signature[:idx]
	}
	return strings.TrimSpace(strings.TrimSuffix(signature, ";"))
}

func rustCallNameNode(node *tree_sitter.Node) *tree_sitter.Node {
	if node == nil {
		return nil
	}
	if functionNode := node.ChildByFieldName("function"); functionNode != nil {
		return firstNamedDescendant(functionNode, "identifier", "field_identifier")
	}
	return firstNamedDescendant(node, "identifier", "field_identifier")
}

func appendRustCall(payload map[string]any, node *tree_sitter.Node, source []byte) {
	nameNode := rustCallNameNode(node)
	if nameNode == nil {
		return
	}
	name := strings.TrimSpace(nodeText(nameNode, source))
	if name == "" {
		return
	}

	item := map[string]any{
		"name":        name,
		"line_number": nodeLine(nameNode),
		"lang":        "rust",
	}
	if fullName := rustCallFullName(node, source); fullName != "" {
		item["full_name"] = fullName
	}
	appendBucket(payload, "function_calls", item)
}

func rustCallFullName(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	if functionNode := node.ChildByFieldName("function"); functionNode != nil {
		return strings.TrimSpace(nodeText(functionNode, source))
	}
	if nameNode := firstNamedDescendant(node, "identifier", "field_identifier"); nameNode != nil {
		return strings.TrimSpace(nodeText(nameNode, source))
	}
	return ""
}

func cLikeInsideFunction(node *tree_sitter.Node) bool {
	for current := node.Parent(); current != nil; current = current.Parent() {
		if current.Kind() == "function_definition" {
			return true
		}
	}
	return false
}

func cLikeCallNameNode(node *tree_sitter.Node) *tree_sitter.Node {
	if node == nil {
		return nil
	}
	if node.Kind() == "identifier" || node.Kind() == "field_identifier" {
		return node
	}
	return firstNamedDescendant(node, "identifier", "field_identifier")
}
