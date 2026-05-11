package c

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

const (
	cMainFunctionRoot          = "c.main_function"
	cPublicHeaderAPIRoot       = "c.public_header_api"
	cSignalHandlerRoot         = "c.signal_handler"
	cCallbackArgumentTarget    = "c.callback_argument_target"
	cFunctionPointerTargetRoot = "c.function_pointer_target"
)

var cHeaderPrototypePattern = regexp.MustCompile(
	`(?m)(?:^|;)\s*(?:[A-Za-z_]\w*|const|static|extern|inline|\s|\*)+\s+([A-Za-z_]\w*)\s*\([^;{}]*\)\s*;`,
)

// AnnotatePublicHeaderRoots marks C functions declared by local headers that
// the same source file includes. It intentionally avoids repo-wide header scans
// and transitive include resolution so parser cost stays bounded per file.
func AnnotatePublicHeaderRoots(payload map[string]any, repoRoot string, sourcePath string) {
	functions := cFunctionItemsByName(payload)
	if len(functions) == 0 {
		return
	}
	for _, headerPath := range cIncludedLocalHeaderPaths(payload, repoRoot, sourcePath) {
		source, err := os.ReadFile(headerPath)
		if err != nil {
			continue
		}
		for _, name := range cHeaderPrototypeNames(string(source)) {
			for _, function := range functions[name] {
				appendCDeadCodeRootKind(function, cPublicHeaderAPIRoot)
			}
		}
	}
}

func annotateCDeadCodeRoots(payload map[string]any, root *tree_sitter.Node, source []byte) {
	functions := cFunctionItemsByName(payload)
	if len(functions) == 0 {
		return
	}

	if mainFunctions, ok := functions["main"]; ok {
		for _, mainFunction := range mainFunctions {
			appendCDeadCodeRootKind(mainFunction, cMainFunctionRoot)
		}
	}

	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "call_expression":
			annotateCSignalHandlerRoot(functions, node, source)
			annotateCCallbackArgumentRoot(functions, node, source)
		case "declaration":
			annotateCFunctionPointerTargetRoot(functions, node, source)
		}
	})
}

func cFunctionItemsByName(payload map[string]any) map[string][]map[string]any {
	items, _ := payload["functions"].([]map[string]any)
	functions := make(map[string][]map[string]any, len(items))
	for _, item := range items {
		name, _ := item["name"].(string)
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		functions[name] = append(functions[name], item)
	}
	return functions
}

func cIncludedLocalHeaderPaths(payload map[string]any, repoRoot string, sourcePath string) []string {
	imports, _ := payload["imports"].([]map[string]any)
	sourceDir := filepath.Dir(sourcePath)
	seen := make(map[string]struct{}, len(imports))
	paths := make([]string, 0, len(imports))
	for _, item := range imports {
		if cStringVal(item, "include_kind") != "local" {
			continue
		}
		name := strings.TrimSpace(cStringVal(item, "name"))
		if name == "" || filepath.IsAbs(name) {
			continue
		}
		candidates := []string{filepath.Clean(filepath.Join(sourceDir, name))}
		if strings.TrimSpace(repoRoot) != "" {
			candidates = append(candidates, filepath.Clean(filepath.Join(repoRoot, name)))
		}
		for _, candidate := range candidates {
			if _, ok := seen[candidate]; ok {
				continue
			}
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				seen[candidate] = struct{}{}
				paths = append(paths, candidate)
				break
			}
		}
	}
	return paths
}

func cStringVal(item map[string]any, key string) string {
	value, _ := item[key].(string)
	return value
}

func cHeaderPrototypeNames(source string) []string {
	matches := cHeaderPrototypePattern.FindAllStringSubmatch(source, -1)
	names := make([]string, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		if len(match) != 2 {
			continue
		}
		if cHeaderPrototypeHasStaticStorage(match[0]) {
			continue
		}
		name := strings.TrimSpace(match[1])
		if name == "" || cKeywordLikeIdentifier(name) {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names
}

func cHeaderPrototypeHasStaticStorage(prototype string) bool {
	for _, field := range strings.FieldsFunc(prototype, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
	}) {
		if field == "static" {
			return true
		}
	}
	return false
}

func annotateCSignalHandlerRoot(
	functions map[string][]map[string]any,
	node *tree_sitter.Node,
	source []byte,
) {
	if cCallFullName(node, source) != "signal" {
		return
	}
	for _, argument := range cCallArguments(node, source) {
		for _, function := range functions[argument] {
			appendCDeadCodeRootKind(function, cSignalHandlerRoot)
		}
	}
}

func annotateCCallbackArgumentRoot(
	functions map[string][]map[string]any,
	node *tree_sitter.Node,
	source []byte,
) {
	if cCallFullName(node, source) == "" {
		return
	}
	for _, argument := range cCallArguments(node, source) {
		for _, function := range functions[argument] {
			appendCDeadCodeRootKind(function, cCallbackArgumentTarget)
		}
	}
}

func annotateCFunctionPointerTargetRoot(
	functions map[string][]map[string]any,
	node *tree_sitter.Node,
	source []byte,
) {
	text := strings.TrimSpace(shared.NodeText(node, source))
	if !strings.Contains(text, "(*") || !strings.Contains(text, "=") {
		return
	}
	target := strings.TrimSpace(text[strings.LastIndex(text, "=")+1:])
	target = strings.TrimSuffix(target, ";")
	target = strings.TrimSpace(target)
	if !cIdentifierLike(target) {
		return
	}
	for _, function := range functions[target] {
		appendCDeadCodeRootKind(function, cFunctionPointerTargetRoot)
	}
}

func cCallArguments(node *tree_sitter.Node, source []byte) []string {
	argumentsNode := node.ChildByFieldName("arguments")
	if argumentsNode == nil {
		return nil
	}
	var arguments []string
	cursor := argumentsNode.Walk()
	defer cursor.Close()
	for _, child := range argumentsNode.NamedChildren(cursor) {
		if child.Kind() != "identifier" {
			continue
		}
		value := strings.TrimSpace(shared.NodeText(&child, source))
		if value != "" {
			arguments = append(arguments, value)
		}
	}
	return arguments
}

func appendCDeadCodeRootKind(item map[string]any, rootKind string) {
	existing, _ := item["dead_code_root_kinds"].([]string)
	for _, value := range existing {
		if value == rootKind {
			return
		}
	}
	item["dead_code_root_kinds"] = append(existing, rootKind)
}

func cIdentifierLike(value string) bool {
	for index, r := range value {
		switch {
		case r == '_':
			continue
		case index == 0 && unicode.IsDigit(r):
			return false
		case unicode.IsLetter(r), unicode.IsDigit(r):
			continue
		default:
			return false
		}
	}
	return value != ""
}

func cKeywordLikeIdentifier(value string) bool {
	switch value {
	case "if", "for", "while", "switch", "return", "sizeof":
		return true
	default:
		return false
	}
}
