package python

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

var pythonPublicNameLiteralRe = regexp.MustCompile(`["']([A-Za-z_][A-Za-z0-9_]*)["']`)

func pythonPublicAPIRootKinds(
	repoRoot string,
	sourcePath string,
	root *tree_sitter.Node,
	source []byte,
) map[string][]string {
	rootKinds := make(map[string][]string)
	for name := range pythonModuleAllNames(root, source) {
		rootKinds[name] = appendUniqueString(rootKinds[name], "python.module_all_export")
	}
	for name := range pythonPackageInitExportedNames(repoRoot, sourcePath) {
		rootKinds[name] = appendUniqueString(rootKinds[name], "python.package_init_export")
	}
	pythonAddPublicAPIBaseRoots(root, source, rootKinds)
	return rootKinds
}

func pythonModuleAllNames(root *tree_sitter.Node, source []byte) map[string]struct{} {
	names := make(map[string]struct{})
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "assignment" {
			return
		}
		left := node.ChildByFieldName("left")
		if strings.TrimSpace(nodeText(left, source)) != "__all__" {
			return
		}
		right := node.ChildByFieldName("right")
		for _, name := range pythonPublicNameLiterals(nodeText(right, source)) {
			names[name] = struct{}{}
		}
	})
	return names
}

func pythonPackageInitExportedNames(repoRoot string, sourcePath string) map[string]struct{} {
	names := make(map[string]struct{})
	repoRoot = pythonLambdaRepoRoot(repoRoot, sourcePath)
	sourcePath = filepath.Clean(sourcePath)
	for _, initPath := range pythonPackageInitCandidates(repoRoot, sourcePath) {
		source, err := os.ReadFile(initPath)
		if err != nil {
			continue
		}
		moduleSpecs := pythonModuleSpecsForInit(repoRoot, initPath, sourcePath)
		if len(moduleSpecs) == 0 {
			continue
		}
		for _, statement := range pythonFromImportStatements(string(source)) {
			modulePath, importClause, ok := strings.Cut(strings.TrimPrefix(statement, "from "), " import ")
			if !ok {
				continue
			}
			if _, ok := moduleSpecs[strings.TrimSpace(modulePath)]; !ok {
				continue
			}
			for _, clause := range pythonSplitImportClauses(strings.Trim(strings.TrimSpace(importClause), "()")) {
				name, _ := pythonSplitImportAlias(clause)
				if name != "" && name != "*" {
					names[name] = struct{}{}
				}
			}
		}
	}
	return names
}

func pythonPackageInitCandidates(repoRoot string, sourcePath string) []string {
	sourcePath = filepath.Clean(sourcePath)
	repoRoot = filepath.Clean(repoRoot)
	dir := filepath.Dir(sourcePath)
	candidates := make([]string, 0)
	seen := make(map[string]struct{})
	for pythonPathWithin(repoRoot, dir) {
		candidate := filepath.Join(dir, "__init__.py")
		if candidate != sourcePath {
			if _, ok := seen[candidate]; !ok {
				seen[candidate] = struct{}{}
				candidates = append(candidates, candidate)
			}
		}
		if dir == repoRoot {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return candidates
}

func pythonModuleSpecsForInit(repoRoot string, initPath string, sourcePath string) map[string]struct{} {
	specs := make(map[string]struct{})
	appendSpec := func(spec string) {
		spec = strings.TrimSpace(spec)
		if spec != "" {
			specs[spec] = struct{}{}
		}
	}
	if rel, err := filepath.Rel(filepath.Dir(initPath), sourcePath); err == nil && !strings.HasPrefix(rel, "..") {
		appendSpec("." + pythonModulePathWithoutExtension(rel))
	}
	if rel, err := filepath.Rel(filepath.Clean(repoRoot), sourcePath); err == nil && !strings.HasPrefix(rel, "..") {
		appendSpec(pythonModulePathWithoutExtension(rel))
	}
	return specs
}

func pythonFromImportStatements(source string) []string {
	lines := strings.Split(source, "\n")
	statements := make([]string, 0)
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(trimmed, "from ") {
			continue
		}
		statement := trimmed
		depth := strings.Count(trimmed, "(") - strings.Count(trimmed, ")")
		for depth > 0 && i+1 < len(lines) {
			i++
			next := strings.TrimSpace(lines[i])
			statement += " " + next
			depth += strings.Count(next, "(") - strings.Count(next, ")")
		}
		statements = append(statements, strings.Join(strings.Fields(statement), " "))
	}
	return statements
}

func pythonPublicNameLiterals(source string) []string {
	matches := pythonPublicNameLiteralRe.FindAllStringSubmatch(source, -1)
	names := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) == 2 {
			names = appendUniqueString(names, match[1])
		}
	}
	return names
}

func pythonMergeRootKinds(existing any, additional []string) []string {
	merged := make([]string, 0)
	switch values := existing.(type) {
	case []string:
		for _, value := range values {
			merged = appendUniqueString(merged, value)
		}
	case []any:
		for _, value := range values {
			merged = appendUniqueString(merged, strings.TrimSpace(fmt.Sprint(value)))
		}
	}
	for _, value := range additional {
		merged = appendUniqueString(merged, value)
	}
	return merged
}

func pythonPublicAPIClassMember(classRootKinds []string, memberName string) bool {
	memberName = strings.TrimSpace(memberName)
	if memberName == "" || strings.HasPrefix(memberName, "_") {
		return false
	}
	for _, rootKind := range classRootKinds {
		if pythonPublicAPIClassRootKind(rootKind) {
			return true
		}
	}
	return false
}

func pythonAddPublicAPIBaseRoots(
	root *tree_sitter.Node,
	source []byte,
	rootKinds map[string][]string,
) {
	for {
		changed := false
		walkNamed(root, func(node *tree_sitter.Node) {
			if node.Kind() != "class_definition" {
				return
			}
			name := strings.TrimSpace(nodeText(node.ChildByFieldName("name"), source))
			if name == "" || !pythonHasPublicAPIClassRoot(rootKinds[name]) {
				return
			}
			for _, base := range pythonClassBaseNames(node, source) {
				before := len(rootKinds[base])
				rootKinds[base] = appendUniqueString(rootKinds[base], "python.public_api_base")
				if len(rootKinds[base]) != before {
					changed = true
				}
			}
		})
		if !changed {
			return
		}
	}
}

func pythonHasPublicAPIClassRoot(rootKinds []string) bool {
	for _, rootKind := range rootKinds {
		if pythonPublicAPIClassRootKind(rootKind) {
			return true
		}
	}
	return false
}

func pythonPublicAPIClassRootKind(rootKind string) bool {
	switch rootKind {
	case "python.module_all_export", "python.package_init_export", "python.public_api_base":
		return true
	default:
		return false
	}
}

func pythonModulePathWithoutExtension(path string) string {
	path = strings.TrimSuffix(filepath.Clean(path), ".py")
	return strings.ReplaceAll(path, string(filepath.Separator), ".")
}

func pythonPathWithin(root string, path string) bool {
	if root == "" {
		return true
	}
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
