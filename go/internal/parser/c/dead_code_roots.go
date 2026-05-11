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

var cBlockCommentPattern = regexp.MustCompile(`(?s)/\*.*?\*/`)

var cLineCommentPattern = regexp.MustCompile(`(?m)//.*$`)

var cFunctionPointerTypedefPattern = regexp.MustCompile(
	`(?s)\btypedef\b[^;]*\(\s*\*\s*([A-Za-z_]\w*)\s*\)\s*\([^;]*\)\s*;`,
)

var cDirectInitializerTargetPattern = regexp.MustCompile(
	`=\s*&?\s*([A-Za-z_]\w*)\s*(?:[,;]|$)`,
)

var cBraceInitializerPattern = regexp.MustCompile(`(?s)=\s*\{([^{}]*)\}`)

type cRepoRootBounds struct {
	abs      string
	resolved string
}

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
	functionPointerTypedefs := cFunctionPointerTypedefNames(string(source))

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
			annotateCFunctionPointerTargetRoot(functions, functionPointerTypedefs, node, source)
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
	rootBounds, ok := cRepoRootBoundsFor(repoRoot)
	if !ok {
		return nil
	}
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
		candidates = append(candidates, filepath.Clean(filepath.Join(rootBounds.abs, name)))
		for _, candidate := range candidates {
			headerPath, ok := cExistingHeaderWithinRepo(candidate, rootBounds)
			if !ok {
				continue
			}
			if _, ok := seen[headerPath]; ok {
				break
			}
			seen[headerPath] = struct{}{}
			paths = append(paths, headerPath)
			break
		}
	}
	return paths
}

func cRepoRootBoundsFor(repoRoot string) (cRepoRootBounds, bool) {
	trimmed := strings.TrimSpace(repoRoot)
	if trimmed == "" {
		return cRepoRootBounds{}, false
	}
	absRoot, err := filepath.Abs(trimmed)
	if err != nil {
		return cRepoRootBounds{}, false
	}
	absRoot = filepath.Clean(absRoot)
	resolvedRoot := absRoot
	if resolved, err := filepath.EvalSymlinks(absRoot); err == nil {
		resolvedRoot = filepath.Clean(resolved)
	}
	return cRepoRootBounds{abs: absRoot, resolved: resolvedRoot}, true
}

func cExistingHeaderWithinRepo(candidate string, rootBounds cRepoRootBounds) (string, bool) {
	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return "", false
	}
	absCandidate = filepath.Clean(absCandidate)
	if !cPathWithinRoot(absCandidate, rootBounds.abs) {
		return "", false
	}
	info, err := os.Stat(absCandidate)
	if err != nil || info.IsDir() {
		return "", false
	}
	resolvedCandidate := absCandidate
	if resolved, err := filepath.EvalSymlinks(absCandidate); err == nil {
		resolvedCandidate = filepath.Clean(resolved)
		if !cPathWithinRoot(resolvedCandidate, rootBounds.resolved) {
			return "", false
		}
	}
	return resolvedCandidate, true
}

func cPathWithinRoot(path string, root string) bool {
	if path == root {
		return true
	}
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	if relative == "." {
		return true
	}
	if strings.HasPrefix(relative, ".."+string(filepath.Separator)) || relative == ".." {
		return false
	}
	return !filepath.IsAbs(relative)
}

func cFunctionPointerTypedefNames(source string) map[string]struct{} {
	matches := cFunctionPointerTypedefPattern.FindAllStringSubmatch(source, -1)
	names := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		if len(match) != 2 {
			continue
		}
		name := strings.TrimSpace(match[1])
		if name != "" {
			names[name] = struct{}{}
		}
	}
	return names
}

func cDeclarationHasFunctionPointerTarget(left string, functionPointerTypedefs map[string]struct{}) bool {
	left = strings.TrimSpace(left)
	if strings.Contains(left, "(*") {
		return true
	}
	fields := strings.FieldsFunc(left, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
	})
	for _, field := range fields {
		if _, ok := functionPointerTypedefs[field]; ok {
			return true
		}
	}
	return false
}

func cDirectFunctionPointerInitializerTargets(text string) []string {
	matches := cDirectInitializerTargetPattern.FindAllStringSubmatch(text, -1)
	braceInitializers := cBraceInitializerPattern.FindAllStringSubmatch(text, -1)
	targets := make([]string, 0, len(matches)+len(braceInitializers))
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		if len(match) != 2 {
			continue
		}
		targets = appendCFunctionPointerTarget(targets, seen, match[1])
	}
	for _, match := range braceInitializers {
		if len(match) != 2 {
			continue
		}
		for _, target := range cBraceInitializerTargets(match[1]) {
			targets = appendCFunctionPointerTarget(targets, seen, target)
		}
	}
	return targets
}

func appendCFunctionPointerTarget(targets []string, seen map[string]struct{}, target string) []string {
	target = strings.TrimSpace(target)
	if target == "" || !cIdentifierLike(target) {
		return targets
	}
	if _, ok := seen[target]; ok {
		return targets
	}
	seen[target] = struct{}{}
	return append(targets, target)
}

func cBraceInitializerTargets(initializer string) []string {
	parts := strings.Split(initializer, ",")
	targets := make([]string, 0, len(parts))
	for _, part := range parts {
		target := part
		if index := strings.LastIndex(target, "="); index >= 0 {
			target = target[index+1:]
		}
		target = cDirectIdentifierExpression(target)
		if target == "" {
			continue
		}
		targets = append(targets, target)
	}
	return targets
}

func cStringVal(item map[string]any, key string) string {
	value, _ := item[key].(string)
	return value
}

func cHeaderPrototypeNames(source string) []string {
	matches := cHeaderPrototypePattern.FindAllStringSubmatch(cStripCComments(source), -1)
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

func cStripCComments(source string) string {
	source = cBlockCommentPattern.ReplaceAllString(source, "")
	return cLineCommentPattern.ReplaceAllString(source, "")
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
	functionPointerTypedefs map[string]struct{},
	node *tree_sitter.Node,
	source []byte,
) {
	text := strings.TrimSpace(shared.NodeText(node, source))
	if !strings.Contains(text, "=") {
		return
	}
	left := text[:strings.LastIndex(text, "=")]
	if !cDeclarationHasFunctionPointerTarget(left, functionPointerTypedefs) {
		return
	}
	for _, target := range cDirectFunctionPointerInitializerTargets(text) {
		for _, function := range functions[target] {
			appendCDeadCodeRootKind(function, cFunctionPointerTargetRoot)
		}
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
		value := cDirectIdentifierExpression(shared.NodeText(&child, source))
		if value == "" {
			continue
		}
		arguments = append(arguments, value)
	}
	return arguments
}

func cDirectIdentifierExpression(expression string) string {
	value := strings.TrimSpace(expression)
	value = strings.TrimPrefix(value, "&")
	value = strings.TrimSpace(value)
	for strings.HasPrefix(value, "(") && strings.HasSuffix(value, ")") {
		value = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(value, "("), ")"))
		value = strings.TrimPrefix(value, "&")
		value = strings.TrimSpace(value)
	}
	if !cIdentifierLike(value) {
		return ""
	}
	return value
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
