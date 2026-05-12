package cpp

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

var cppFreeHeaderPrototypePattern = regexp.MustCompile(
	`(?m)(?:^|;)\s*(?:[A-Za-z_]\w*|const|extern|inline|constexpr|noexcept|auto|\s|[*&:<>])+?\s+([A-Za-z_]\w*)\s*\([^;{}]*\)\s*(?:const\s*)?(?:noexcept\s*)?;`,
)

var cppClassBlockPattern = regexp.MustCompile(
	`(?s)\b(?:class|struct)\s+([A-Za-z_]\w*)[^{};]*\{(.*?)\}\s*;`,
)

var cppClassMethodPrototypePattern = regexp.MustCompile(
	`(?m)(?:^|;)\s*(?:virtual\s+)?(?:[A-Za-z_]\w*|const|inline|constexpr|noexcept|auto|\s|[*&:<>])+?\s+([A-Za-z_]\w*)\s*\([^;{}]*\)\s*(?:const\s*)?(?:override\s*)?(?:noexcept\s*)?;`,
)

var cppBlockCommentPattern = regexp.MustCompile(`(?s)/\*.*?\*/`)

var cppLineCommentPattern = regexp.MustCompile(`(?m)//.*$`)

type cppRepoRootBounds struct {
	abs      string
	resolved string
}

// AnnotatePublicHeaderRoots marks C++ functions and methods declared by local
// headers directly included by the same source file. It does not recurse
// through the include graph, and it refuses headers outside the repository
// root before reading file contents.
func AnnotatePublicHeaderRoots(payload map[string]any, repoRoot string, sourcePath string) {
	functions := cppFunctionItemsByKey(payload)
	if len(functions) == 0 {
		return
	}
	for _, headerPath := range cppIncludedLocalHeaderPaths(payload, repoRoot, sourcePath) {
		source, err := os.ReadFile(headerPath)
		if err != nil {
			continue
		}
		for _, declaration := range cppHeaderPublicDeclarations(string(source)) {
			for _, function := range functions[declaration] {
				appendCPPDeadCodeRootKind(function, cppPublicHeaderAPIRoot)
			}
		}
	}
}

func cppIncludedLocalHeaderPaths(payload map[string]any, repoRoot string, sourcePath string) []string {
	imports, _ := payload["imports"].([]map[string]any)
	rootBounds, ok := cppRepoRootBoundsFor(repoRoot)
	if !ok {
		return nil
	}
	sourceDir := filepath.Dir(sourcePath)
	seen := make(map[string]struct{}, len(imports))
	paths := make([]string, 0, len(imports))
	for _, item := range imports {
		if cppStringVal(item, "include_kind") != "local" {
			continue
		}
		name := strings.TrimSpace(cppStringVal(item, "name"))
		if name == "" || filepath.IsAbs(name) {
			continue
		}
		candidates := []string{filepath.Clean(filepath.Join(sourceDir, name))}
		candidates = append(candidates, filepath.Clean(filepath.Join(rootBounds.abs, name)))
		for _, candidate := range candidates {
			headerPath, ok := cppExistingHeaderWithinRepo(candidate, rootBounds)
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

func cppRepoRootBoundsFor(repoRoot string) (cppRepoRootBounds, bool) {
	trimmed := strings.TrimSpace(repoRoot)
	if trimmed == "" {
		return cppRepoRootBounds{}, false
	}
	absRoot, err := filepath.Abs(trimmed)
	if err != nil {
		return cppRepoRootBounds{}, false
	}
	absRoot = filepath.Clean(absRoot)
	resolvedRoot := absRoot
	if resolved, err := filepath.EvalSymlinks(absRoot); err == nil {
		resolvedRoot = filepath.Clean(resolved)
	}
	return cppRepoRootBounds{abs: absRoot, resolved: resolvedRoot}, true
}

func cppExistingHeaderWithinRepo(candidate string, rootBounds cppRepoRootBounds) (string, bool) {
	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return "", false
	}
	absCandidate = filepath.Clean(absCandidate)
	if !cppPathWithinRoot(absCandidate, rootBounds.abs) {
		return "", false
	}
	info, err := os.Stat(absCandidate)
	if err != nil || info.IsDir() {
		return "", false
	}
	resolvedCandidate := absCandidate
	if resolved, err := filepath.EvalSymlinks(absCandidate); err == nil {
		resolvedCandidate = filepath.Clean(resolved)
		if !cppPathWithinRoot(resolvedCandidate, rootBounds.resolved) {
			return "", false
		}
	}
	return resolvedCandidate, true
}

func cppPathWithinRoot(path string, root string) bool {
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

func cppHeaderPublicDeclarations(source string) []cppFunctionKey {
	source = cppStripComments(source)
	declarations := cppFreeHeaderDeclarations(source)
	declarations = append(declarations, cppPublicClassMethodDeclarations(source)...)
	return declarations
}

func cppFreeHeaderDeclarations(source string) []cppFunctionKey {
	matches := cppFreeHeaderPrototypePattern.FindAllStringSubmatch(source, -1)
	declarations := make([]cppFunctionKey, 0, len(matches))
	seen := make(map[cppFunctionKey]struct{}, len(matches))
	for _, match := range matches {
		if len(match) != 2 || cppHeaderPrototypeHasStaticStorage(match[0]) {
			continue
		}
		key := cppFunctionKey{name: strings.TrimSpace(match[1])}
		if key.name == "" || cppKeywordLikeIdentifier(key.name) {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		declarations = append(declarations, key)
	}
	return declarations
}

func cppPublicClassMethodDeclarations(source string) []cppFunctionKey {
	matches := cppClassBlockPattern.FindAllStringSubmatch(source, -1)
	var declarations []cppFunctionKey
	seen := make(map[cppFunctionKey]struct{})
	for _, match := range matches {
		if len(match) != 3 {
			continue
		}
		className := strings.TrimSpace(match[1])
		visibility := "private"
		if strings.Contains(match[0], "struct "+className) {
			visibility = "public"
		}
		for _, line := range strings.Split(match[2], "\n") {
			trimmed := strings.TrimSpace(line)
			switch trimmed {
			case "public:":
				visibility = "public"
				continue
			case "private:", "protected:":
				visibility = "private"
				continue
			}
			if visibility != "public" {
				continue
			}
			for _, method := range cppClassMethodNames(trimmed) {
				key := cppFunctionKey{class: className, name: method}
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				declarations = append(declarations, key)
			}
		}
	}
	return declarations
}

func cppClassMethodNames(line string) []string {
	matches := cppClassMethodPrototypePattern.FindAllStringSubmatch(line, -1)
	names := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) != 2 {
			continue
		}
		name := strings.TrimSpace(match[1])
		if name == "" || cppKeywordLikeIdentifier(name) {
			continue
		}
		names = append(names, name)
	}
	return names
}

func cppStripComments(source string) string {
	source = cppBlockCommentPattern.ReplaceAllString(source, "")
	return cppLineCommentPattern.ReplaceAllString(source, "")
}

func cppHeaderPrototypeHasStaticStorage(prototype string) bool {
	for _, field := range strings.FieldsFunc(prototype, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
	}) {
		if field == "static" {
			return true
		}
	}
	return false
}
