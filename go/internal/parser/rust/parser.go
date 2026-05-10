package rust

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

var rustLifetimePattern = regexp.MustCompile(`'([A-Za-z_][A-Za-z0-9_]*)`)

// Parse reads and parses a Rust file using a caller-owned tree-sitter parser.
func Parse(
	path string,
	isDependency bool,
	options shared.Options,
	parser *tree_sitter.Parser,
) (map[string]any, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, fmt.Errorf("parse rust file %q: parser returned nil tree", path)
	}
	defer tree.Close()

	payload := shared.BasePayload(path, "rust", isDependency)
	payload["impl_blocks"] = []map[string]any{}
	payload["macros"] = []map[string]any{}
	payload["modules"] = []map[string]any{}
	payload["traits"] = []map[string]any{}
	payload["type_aliases"] = []map[string]any{}
	root := tree.RootNode()

	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "const_item", "static_item":
			appendRustVariable(payload, node, source, options)
		case "impl_item":
			appendRustImplBlock(payload, node, source)
		case "function_item", "function_signature_item":
			appendRustFunction(payload, path, node, source, options)
		case "struct_item", "enum_item", "union_item":
			appendRustClass(payload, node, source)
		case "trait_item":
			appendRustTrait(payload, node, source)
		case "use_declaration":
			appendRustImportMetadata(payload, node, source)
		case "call_expression":
			appendRustCall(payload, node, source)
		case "macro_definition":
			appendRustMacroDefinition(payload, node, source, options)
		case "macro_invocation":
			appendRustCall(payload, node, source)
		case "mod_item":
			appendRustModule(payload, node, source, options)
		case "type_item":
			appendRustTypeAlias(payload, node, source, options)
		}
	})

	sortSystemsPayload(
		payload,
		"functions",
		"classes",
		"traits",
		"variables",
		"type_aliases",
		"macros",
		"modules",
		"imports",
		"function_calls",
		"impl_blocks",
	)
	payload["framework_semantics"] = map[string]any{"frameworks": []string{}}

	return payload, nil
}

// PreScan returns named Rust symbols used by dependency pre-scanning.
func PreScan(path string, parser *tree_sitter.Parser) ([]string, error) {
	payload, err := Parse(path, false, shared.Options{}, parser)
	if err != nil {
		return nil, err
	}
	return shared.CollectBucketNames(payload, "functions", "classes", "traits", "type_aliases", "macros", "impl_blocks", "modules"), nil
}

func appendRustClass(payload map[string]any, node *tree_sitter.Node, source []byte) {
	nameNode := firstNamedDescendant(node, "type_identifier")
	name := shared.NodeText(nameNode, source)
	if strings.TrimSpace(name) == "" {
		return
	}
	item := map[string]any{
		"name":        name,
		"line_number": shared.NodeLine(nameNode),
		"end_line":    shared.NodeEndLine(node),
		"lang":        "rust",
	}
	signature := rustSignatureHeader(shared.NodeText(node, source))
	if visibility := rustVisibility(signature); visibility != "" {
		item["visibility"] = visibility
	}
	rustApplyAttributeMetadata(item, rustLeadingAttributes(node, source))
	rustApplyGenericMetadata(item, rustGenericParametersAfterName(signature, name))
	shared.AppendBucket(payload, "classes", item)
}

func appendRustTrait(payload map[string]any, node *tree_sitter.Node, source []byte) {
	nameNode := firstNamedDescendant(node, "type_identifier")
	name := shared.NodeText(nameNode, source)
	if strings.TrimSpace(name) == "" {
		return
	}
	item := map[string]any{
		"name":        name,
		"line_number": shared.NodeLine(nameNode),
		"end_line":    shared.NodeEndLine(node),
		"lang":        "rust",
	}
	signature := rustSignatureHeader(shared.NodeText(node, source))
	if visibility := rustVisibility(signature); visibility != "" {
		item["visibility"] = visibility
	}
	rustApplyAttributeMetadata(item, rustLeadingAttributes(node, source))
	rustApplyGenericMetadata(item, rustGenericParametersAfterName(signature, name))
	shared.AppendBucket(payload, "traits", item)
}

func appendRustImplBlock(payload map[string]any, node *tree_sitter.Node, source []byte) {
	header := strings.TrimSpace(shared.NodeText(node, source))
	if idx := strings.Index(header, "{"); idx >= 0 {
		header = header[:idx]
	}
	header = strings.TrimSpace(strings.TrimPrefix(header, "impl"))
	lifetimeParameters := rustLeadingLifetimeParameters(header)
	leadingGenerics := rustLeadingGenericSegment(header)
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
	targetName = rustTrimWhereClause(targetName)

	item := map[string]any{
		"name":        rustBaseTypeName(targetName),
		"target":      targetName,
		"line_number": shared.NodeLine(node),
		"end_line":    shared.NodeEndLine(node),
		"kind":        kind,
		"lang":        "rust",
	}
	if traitName != "" {
		item["trait"] = rustBaseTypeName(traitName)
	}
	if len(lifetimeParameters) > 0 {
		item["lifetime_parameters"] = lifetimeParameters
	}
	rustApplyGenericMetadata(item, leadingGenerics)
	if len(signatureLifetimes) > 0 {
		item["signature_lifetimes"] = signatureLifetimes
	}
	shared.AppendBucket(payload, "impl_blocks", item)
}

func appendRustFunction(
	payload map[string]any,
	path string,
	node *tree_sitter.Node,
	source []byte,
	options shared.Options,
) {
	nameNode := firstNamedDescendant(node, "identifier")
	name := shared.NodeText(nameNode, source)
	if strings.TrimSpace(name) == "" {
		return
	}

	item := map[string]any{
		"name":        name,
		"line_number": shared.NodeLine(nameNode),
		"end_line":    shared.NodeEndLine(node),
		"decorators":  []string{},
		"lang":        "rust",
	}
	signature := rustSignatureHeader(shared.NodeText(node, source))
	if visibility := rustVisibility(signature); visibility != "" {
		item["visibility"] = visibility
	}
	prefix := rustFunctionPrefix(signature, name)
	if rustContainsWord(prefix, "async") {
		item["async"] = true
	}
	if rustContainsWord(prefix, "unsafe") {
		item["unsafe"] = true
	}
	attributes := rustLeadingAttributes(node, source)
	if len(attributes) > 0 {
		item["decorators"] = attributes
	}
	rustApplyAttributeMetadata(item, attributes)
	if rootKinds := rustDeadCodeRootKinds(path, name, attributes); len(rootKinds) > 0 {
		item["dead_code_root_kinds"] = rootKinds
	}
	if lifetimeParameters := rustFunctionLifetimeParameters(signature, name); len(lifetimeParameters) > 0 {
		item["lifetime_parameters"] = lifetimeParameters
	}
	rustApplyGenericMetadata(item, rustGenericParametersAfterName(signature, name))
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
		item["source"] = shared.NodeText(node, source)
	}
	shared.AppendBucket(payload, "functions", item)
}

func appendRustTypeAlias(payload map[string]any, node *tree_sitter.Node, source []byte, options shared.Options) {
	nameNode := firstNamedDescendant(node, "type_identifier")
	name := strings.TrimSpace(shared.NodeText(nameNode, source))
	if name == "" {
		return
	}

	item := map[string]any{
		"name":        name,
		"line_number": shared.NodeLine(nameNode),
		"end_line":    shared.NodeEndLine(node),
		"lang":        "rust",
	}
	raw := rustSignatureHeader(shared.NodeText(node, source))
	if visibility := rustVisibility(raw); visibility != "" {
		item["visibility"] = visibility
	}
	rustApplyAttributeMetadata(item, rustLeadingAttributes(node, source))
	rustApplyGenericMetadata(item, rustGenericParametersAfterName(raw, name))
	if options.IndexSource {
		item["source"] = shared.NodeText(node, source)
	}
	shared.AppendBucket(payload, "type_aliases", item)
}

func appendRustVariable(payload map[string]any, node *tree_sitter.Node, source []byte, options shared.Options) {
	nameNode := firstNamedDescendant(node, "identifier")
	name := strings.TrimSpace(shared.NodeText(nameNode, source))
	if name == "" {
		return
	}

	variableKind := strings.TrimSuffix(node.Kind(), "_item")
	item := map[string]any{
		"name":          name,
		"line_number":   shared.NodeLine(nameNode),
		"end_line":      shared.NodeEndLine(node),
		"variable_kind": variableKind,
		"lang":          "rust",
	}
	raw := rustSignatureHeader(shared.NodeText(node, source))
	if visibility := rustVisibility(raw); visibility != "" {
		item["visibility"] = visibility
	}
	rustApplyAttributeMetadata(item, rustLeadingAttributes(node, source))
	if options.IndexSource {
		item["source"] = shared.NodeText(node, source)
	}
	shared.AppendBucket(payload, "variables", item)
}

func appendRustMacroDefinition(payload map[string]any, node *tree_sitter.Node, source []byte, options shared.Options) {
	name := rustMacroDefinitionName(node, source)
	if name == "" {
		return
	}

	item := map[string]any{
		"name":        name,
		"line_number": shared.NodeLine(node),
		"end_line":    shared.NodeEndLine(node),
		"lang":        "rust",
	}
	if options.IndexSource {
		item["source"] = shared.NodeText(node, source)
	}
	rustApplyAttributeMetadata(item, rustLeadingAttributes(node, source))
	shared.AppendBucket(payload, "macros", item)
}

func appendRustModule(payload map[string]any, node *tree_sitter.Node, source []byte, options shared.Options) {
	nameNode := firstNamedDescendant(node, "identifier")
	name := strings.TrimSpace(shared.NodeText(nameNode, source))
	if name == "" {
		return
	}

	rawFull := shared.NodeText(node, source)
	raw := rustSignatureHeader(rawFull)
	item := map[string]any{
		"name":        name,
		"line_number": shared.NodeLine(nameNode),
		"end_line":    shared.NodeEndLine(node),
		"module_kind": rustModuleKind(rawFull),
		"lang":        "rust",
	}
	if visibility := rustVisibility(raw); visibility != "" {
		item["visibility"] = visibility
	}
	rustApplyAttributeMetadata(item, rustLeadingAttributes(node, source))
	if options.IndexSource {
		item["source"] = shared.NodeText(node, source)
	}
	shared.AppendBucket(payload, "modules", item)
}

func appendRustImportMetadata(payload map[string]any, node *tree_sitter.Node, source []byte) {
	raw := strings.TrimSpace(shared.NodeText(node, source))
	if raw == "" {
		return
	}

	importText := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(raw, "use "), ";"))
	if importText == "" {
		return
	}

	for _, entry := range rustImportEntries(importText) {
		shared.AppendBucket(payload, "imports", map[string]any{
			"name":             entry.name,
			"source":           entry.name,
			"alias":            entry.alias,
			"full_import_name": raw,
			"import_type":      entry.importType,
			"line_number":      shared.NodeLine(node),
			"lang":             "rust",
		})
	}
}

func rustImplContext(node *tree_sitter.Node, source []byte) string {
	for current := node.Parent(); current != nil; current = current.Parent() {
		if current.Kind() != "impl_item" {
			continue
		}
		typeNode := current.ChildByFieldName("type")
		implContext := shared.NodeText(typeNode, source)
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
	if segment, ok := rustLeadingAngleSegment(trimmed); ok {
		return strings.TrimSpace(trimmed[len(segment):])
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
