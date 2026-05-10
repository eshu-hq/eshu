package rust

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

var (
	rustMacroRulesPattern  = regexp.MustCompile(`\bmacro_rules!\s*([A-Za-z_][A-Za-z0-9_]*)`)
	rustWhereClausePattern = regexp.MustCompile(`(?s)\s+where\s+`)
	rustIdentifierPattern  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

func firstNamedDescendant(node *tree_sitter.Node, kinds ...string) *tree_sitter.Node {
	var result *tree_sitter.Node
	shared.WalkNamed(node, func(child *tree_sitter.Node) {
		if result != nil {
			return
		}
		for _, kind := range kinds {
			if child.Kind() == kind {
				result = shared.CloneNode(child)
				return
			}
		}
	})
	return result
}

func lastNamedDescendant(node *tree_sitter.Node, kinds ...string) *tree_sitter.Node {
	var result *tree_sitter.Node
	shared.WalkNamed(node, func(child *tree_sitter.Node) {
		for _, kind := range kinds {
			if child.Kind() == kind {
				result = shared.CloneNode(child)
				return
			}
		}
	})
	return result
}

func sortSystemsPayload(payload map[string]any, keys ...string) {
	for _, key := range keys {
		shared.SortNamedBucket(payload, key)
	}
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

func rustTrimWhereClause(text string) string {
	return strings.TrimSpace(rustWhereClausePattern.Split(text, 2)[0])
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
		return lastNamedDescendant(functionNode, "identifier", "field_identifier")
	}
	return firstNamedDescendant(node, "identifier", "field_identifier")
}

func appendRustCall(payload map[string]any, node *tree_sitter.Node, source []byte) {
	nameNode := rustCallNameNode(node)
	if nameNode == nil {
		return
	}
	name := strings.TrimSpace(shared.NodeText(nameNode, source))
	if name == "" {
		return
	}

	item := map[string]any{
		"name":        name,
		"line_number": shared.NodeLine(nameNode),
		"lang":        "rust",
	}
	if fullName := rustCallFullName(node, source); fullName != "" {
		item["full_name"] = fullName
	}
	shared.AppendBucket(payload, "function_calls", item)
}

func rustCallFullName(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	if functionNode := node.ChildByFieldName("function"); functionNode != nil {
		return strings.TrimSpace(shared.NodeText(functionNode, source))
	}
	if nameNode := firstNamedDescendant(node, "identifier", "field_identifier"); nameNode != nil {
		return strings.TrimSpace(shared.NodeText(nameNode, source))
	}
	return ""
}

func rustFunctionPrefix(signature string, name string) string {
	marker := "fn " + name
	idx := strings.Index(signature, marker)
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(signature[:idx])
}

func rustContainsWord(text string, word string) bool {
	for _, field := range strings.Fields(text) {
		if field == word {
			return true
		}
	}
	return false
}

func rustVisibility(signature string) string {
	trimmed := rustStripLeadingAttributeText(strings.TrimSpace(signature))
	if strings.HasPrefix(trimmed, "pub(") {
		end := strings.Index(trimmed, ")")
		if end > len("pub(") {
			return trimmed[:end+1]
		}
	}
	if trimmed == "pub" || strings.HasPrefix(trimmed, "pub ") {
		return "pub"
	}
	return ""
}

func rustDeadCodeRootKinds(path string, name string, attributes []string) []string {
	rootKinds := make([]string, 0, 3)
	if name == "main" && rustMainFunctionRootPath(path, attributes) {
		rootKinds = appendUniqueString(rootKinds, "rust.main_function")
	}
	for _, attribute := range attributes {
		attrPath := rustAttributePath(attribute)
		switch attrPath {
		case "test":
			rootKinds = appendUniqueString(rootKinds, "rust.test_function")
		case "tokio::main":
			rootKinds = appendUniqueString(rootKinds, "rust.tokio_main")
		case "tokio::test":
			rootKinds = appendUniqueString(rootKinds, "rust.tokio_test")
		}
	}
	return rootKinds
}

// rustBenchmarkFunctionNames collects benchmark targets declared by file-local macros.
func rustBenchmarkFunctionNames(root *tree_sitter.Node, source []byte) map[string]struct{} {
	names := make(map[string]struct{})
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "macro_invocation" {
			return
		}
		for _, name := range rustCriterionGroupBenchmarkNames(shared.NodeText(node, source)) {
			names[name] = struct{}{}
		}
	})
	return names
}

// rustCriterionGroupBenchmarkNames extracts direct identifier targets from criterion_group!.
func rustCriterionGroupBenchmarkNames(text string) []string {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "criterion_group!") {
		return nil
	}
	body, ok := rustMacroBody(strings.TrimPrefix(trimmed, "criterion_group!"))
	if !ok {
		return nil
	}
	if targets := rustCriterionTargetsSegment(body); targets != "" {
		return rustIdentifierList(targets)
	}
	parts := rustSplitTopLevel(body, ',')
	if len(parts) <= 1 {
		return nil
	}
	names := make([]string, 0, len(parts)-1)
	for _, part := range parts[1:] {
		name := rustIdentifierOnly(part)
		if name != "" {
			names = appendUniqueString(names, name)
		}
	}
	return names
}

// rustMacroBody trims the outer delimiter from a Rust macro invocation body.
func rustMacroBody(text string) (string, bool) {
	trimmed := strings.TrimSpace(text)
	trimmed = strings.TrimSpace(strings.TrimSuffix(trimmed, ";"))
	if trimmed == "" {
		return "", false
	}
	open := trimmed[0]
	var close byte
	switch open {
	case '(':
		close = ')'
	case '{':
		close = '}'
	default:
		return "", false
	}
	if len(trimmed) < 2 || trimmed[len(trimmed)-1] != close {
		return "", false
	}
	return strings.TrimSpace(trimmed[1 : len(trimmed)-1]), true
}

// rustCriterionTargetsSegment returns the named-form targets list when present.
func rustCriterionTargetsSegment(body string) string {
	idx := strings.Index(body, "targets")
	if idx < 0 {
		return ""
	}
	remainder := strings.TrimSpace(body[idx+len("targets"):])
	if !strings.HasPrefix(remainder, "=") {
		return ""
	}
	remainder = strings.TrimSpace(strings.TrimPrefix(remainder, "="))
	if end := strings.Index(remainder, ";"); end >= 0 {
		remainder = remainder[:end]
	}
	return strings.TrimSpace(remainder)
}

// rustIdentifierList keeps only simple same-file function names from a comma list.
func rustIdentifierList(text string) []string {
	names := make([]string, 0)
	for _, part := range rustSplitTopLevel(text, ',') {
		name := rustIdentifierOnly(part)
		if name != "" {
			names = appendUniqueString(names, name)
		}
	}
	return names
}

// rustIdentifierOnly rejects paths, calls, and expressions to keep root evidence local.
func rustIdentifierOnly(text string) string {
	trimmed := strings.TrimSpace(strings.TrimSuffix(text, ","))
	if rustIdentifierPattern.MatchString(trimmed) {
		return trimmed
	}
	return ""
}

func rustMainFunctionRootPath(path string, attributes []string) bool {
	for _, attribute := range attributes {
		if rustAttributePath(attribute) == "tokio::main" {
			return true
		}
	}
	cleanPath := filepath.ToSlash(filepath.Clean(path))
	if cleanPath == "build.rs" || strings.HasSuffix(cleanPath, "/build.rs") {
		return true
	}
	return strings.HasSuffix(cleanPath, "/src/main.rs") ||
		strings.Contains(cleanPath, "/src/bin/") ||
		strings.Contains(cleanPath, "/examples/")
}

func rustLeadingAttributes(node *tree_sitter.Node, source []byte) []string {
	if node == nil {
		return nil
	}
	if attributes := rustDirectAttributes(node, source); len(attributes) > 0 {
		return attributes
	}
	startByte := int(node.StartByte())
	if startByte < 0 {
		return nil
	}
	if startByte > len(source) {
		startByte = len(source)
	}
	lines := strings.Split(string(source[:startByte]), "\n")
	attributes := make([]string, 0, 2)
	idx := len(lines) - 1
	if idx >= 0 && strings.TrimSpace(lines[idx]) == "" {
		idx--
	}
	for idx >= 0 {
		trimmed := strings.TrimSpace(lines[idx])
		if trimmed == "" {
			break
		}
		start := idx
		for start >= 0 && !strings.HasPrefix(strings.TrimSpace(lines[start]), "#[") {
			if strings.TrimSpace(lines[start]) == "" {
				break
			}
			start--
		}
		if start < 0 || !strings.HasPrefix(strings.TrimSpace(lines[start]), "#[") {
			break
		}
		attribute := strings.TrimSpace(strings.Join(lines[start:idx+1], "\n"))
		if rustAttributeEnd(attribute) < 0 {
			break
		}
		for _, value := range rustLeadingAttributeTexts(attribute) {
			attributes = appendUniqueString(attributes, value)
		}
		idx = start - 1
	}
	if len(attributes) == 0 {
		return nil
	}
	return attributes
}

func rustLeadingAttributeTexts(text string) []string {
	remaining := strings.TrimSpace(text)
	attributes := make([]string, 0, 1)
	for strings.HasPrefix(remaining, "#[") {
		end := rustAttributeEnd(remaining)
		if end < 0 {
			break
		}
		attributes = append(attributes, strings.TrimSpace(remaining[:end+1]))
		remaining = strings.TrimSpace(remaining[end+1:])
	}
	return attributes
}

func rustDirectAttributes(node *tree_sitter.Node, source []byte) []string {
	attributes := make([]string, 0, 2)
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child == nil {
			continue
		}
		if child.Kind() != "attribute_item" && child.Kind() != "inner_attribute_item" {
			continue
		}
		attribute := strings.TrimSpace(shared.NodeText(child, source))
		if attribute != "" {
			attributes = appendUniqueString(attributes, attribute)
		}
	}
	return attributes
}

func rustAttributePath(attribute string) string {
	trimmed := strings.TrimSpace(attribute)
	trimmed = strings.TrimPrefix(trimmed, "#[")
	trimmed = strings.TrimSuffix(trimmed, "]")
	trimmed = strings.TrimSpace(trimmed)
	if idx := strings.Index(trimmed, "("); idx >= 0 {
		trimmed = trimmed[:idx]
	}
	if idx := strings.Index(trimmed, "="); idx >= 0 {
		trimmed = trimmed[:idx]
	}
	return strings.TrimSpace(trimmed)
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func rustMacroDefinitionName(node *tree_sitter.Node, source []byte) string {
	if nameNode := firstNamedDescendant(node, "identifier"); nameNode != nil {
		if name := strings.TrimSpace(shared.NodeText(nameNode, source)); name != "" {
			return name
		}
	}
	matches := rustMacroRulesPattern.FindStringSubmatch(shared.NodeText(node, source))
	if len(matches) < 2 {
		return ""
	}
	return strings.TrimSpace(matches[1])
}
