package parser

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	jsparser "github.com/eshu-hq/eshu/go/internal/parser/javascript"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

const (
	typeScriptPublicAPIReexportRoot      = "typescript.public_api_reexport"
	typeScriptPublicAPIExportRoot        = "typescript.public_api_export"
	typeScriptPublicAPITypeReferenceRoot = "typescript.public_api_type_reference"
	typeScriptStaticRegistryMemberRoot   = "typescript.static_registry_member"
)

type javaScriptTypeScriptSurfaceReexport struct {
	exportedName string
	originalName string
	source       string
}

var (
	javaScriptTypeScriptNamedReExportRe = regexp.MustCompile(`(?s)\bexport\s+(?:type\s+)?\{([^}]*)\}\s+from\s+["']([^"']+)["']`)
	javaScriptTypeScriptStarReExportRe  = regexp.MustCompile(`(?s)\bexport\s+(?:type\s+)?\*\s+from\s+["']([^"']+)["']`)
)

func javaScriptTypeScriptSurfaceRootKinds(
	repoRoot string,
	path string,
	root *tree_sitter.Node,
	source []byte,
) map[string][]string {
	rootKinds := make(map[string][]string)
	if root == nil || !javaScriptIsTypeScriptSourcePath(path) {
		return rootKinds
	}

	exportedNames := javaScriptTypeScriptExportedDeclarationNames(root, source)
	if len(exportedNames) == 0 {
		return rootKinds
	}

	publicNames := make(map[string]struct{})
	for _, publicPath := range javaScriptPackagePublicSourcePaths(repoRoot, path) {
		if sameJavaScriptPath(publicPath, path) {
			for name := range exportedNames {
				publicNames[name] = struct{}{}
				rootKinds[name] = appendUniqueString(rootKinds[name], typeScriptPublicAPIExportRoot)
			}
			continue
		}
		for _, reexport := range javaScriptTypeScriptStaticReexportsFromFile(publicPath) {
			for _, targetPath := range javaScriptTypeScriptReexportSourceCandidates(repoRoot, publicPath, reexport.source) {
				if !sameJavaScriptPath(targetPath, path) {
					continue
				}
				if reexport.exportedName == "*" {
					for name := range exportedNames {
						publicNames[name] = struct{}{}
						rootKinds[name] = appendUniqueString(rootKinds[name], typeScriptPublicAPIReexportRoot)
					}
					continue
				}
				name := strings.TrimSpace(reexport.originalName)
				if _, ok := exportedNames[name]; ok {
					publicNames[name] = struct{}{}
					rootKinds[name] = appendUniqueString(rootKinds[name], typeScriptPublicAPIReexportRoot)
				}
			}
		}
	}

	for name := range javaScriptTypeScriptPublicTypeReferences(root, source, publicNames, exportedNames) {
		rootKinds[name] = appendUniqueString(rootKinds[name], typeScriptPublicAPITypeReferenceRoot)
	}
	for name := range javaScriptTypeScriptStaticRegistryMemberNames(root, source) {
		rootKinds[name] = appendUniqueString(rootKinds[name], typeScriptStaticRegistryMemberRoot)
	}
	return rootKinds
}

func javaScriptIsTypeScriptInterfaceImplementationMethod(node *tree_sitter.Node, name string, source []byte) bool {
	if node == nil || node.Kind() != "method_definition" {
		return false
	}
	if strings.TrimSpace(name) == "" || strings.TrimSpace(name) == "constructor" {
		return false
	}
	methodSource := strings.TrimSpace(nodeText(node, source))
	if strings.HasPrefix(methodSource, "private ") || strings.HasPrefix(methodSource, "protected ") {
		return false
	}
	for current := node.Parent(); current != nil; current = current.Parent() {
		switch current.Kind() {
		case "class_declaration", "abstract_class_declaration":
			return javaScriptClassHasImplementsClause(current)
		case "program":
			return false
		}
	}
	return false
}

func javaScriptClassHasImplementsClause(node *tree_sitter.Node) bool {
	if node == nil {
		return false
	}
	return javaScriptNodeContainsKind(node, "implements_clause")
}

func javaScriptTypeScriptExportedDeclarationNames(root *tree_sitter.Node, source []byte) map[string]struct{} {
	names := make(map[string]struct{})
	walkNamed(root, func(node *tree_sitter.Node) {
		if !javaScriptIsExported(node) {
			return
		}
		name := javaScriptTypeScriptDeclarationName(node, source)
		if name != "" {
			names[name] = struct{}{}
		}
	})
	return names
}

func javaScriptTypeScriptDeclarationName(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	switch node.Kind() {
	case "function_declaration", "generator_function_declaration", "class_declaration",
		"abstract_class_declaration", "interface_declaration", "type_alias_declaration",
		"enum_declaration", "method_definition", "variable_declarator":
	default:
		return ""
	}
	return strings.TrimSpace(javaScriptFunctionName(node.ChildByFieldName("name"), source))
}

func javaScriptPackagePublicSourcePaths(repoRoot string, path string) []string {
	return jsparser.PackagePublicSourcePaths(repoRoot, path)
}

func javaScriptTypeScriptStaticReexportsFromFile(path string) []javaScriptTypeScriptSurfaceReexport {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return javaScriptTypeScriptStaticReexportsFromSource(string(raw))
}

func javaScriptTypeScriptStaticReexportsFromSource(source string) []javaScriptTypeScriptSurfaceReexport {
	reexports := make([]javaScriptTypeScriptSurfaceReexport, 0)
	for _, match := range javaScriptTypeScriptStarReExportRe.FindAllStringSubmatch(source, -1) {
		if len(match) != 2 {
			continue
		}
		reexports = append(reexports, javaScriptTypeScriptSurfaceReexport{
			exportedName: "*",
			originalName: "*",
			source:       strings.TrimSpace(match[1]),
		})
	}
	for _, match := range javaScriptTypeScriptNamedReExportRe.FindAllStringSubmatch(source, -1) {
		if len(match) != 3 {
			continue
		}
		for _, part := range strings.Split(match[1], ",") {
			originalName, exportedName := javaScriptReExportSpecifierNames(part)
			if originalName == "" || exportedName == "" {
				continue
			}
			reexports = append(reexports, javaScriptTypeScriptSurfaceReexport{
				exportedName: exportedName,
				originalName: originalName,
				source:       strings.TrimSpace(match[2]),
			})
		}
	}
	return reexports
}

func javaScriptTypeScriptReexportSourceCandidates(repoRoot string, fromPath string, source string) []string {
	source = strings.TrimSpace(source)
	if source == "" {
		return nil
	}
	candidates := make([]string, 0, 8)
	appendCandidate := func(path string) {
		path = cleanJavaScriptPath(path)
		if path != "" {
			candidates = appendUniqueString(candidates, path)
		}
	}
	if strings.HasPrefix(source, ".") {
		basePath := filepath.Join(filepath.Dir(fromPath), filepath.FromSlash(source))
		for _, candidate := range jsparser.TSConfigSourceCandidates(basePath) {
			appendCandidate(candidate)
		}
		return candidates
	}

	resolver := jsparser.NewTSConfigImportResolver(repoRoot, fromPath)
	if resolved := resolver.ResolveSource(source); resolved != "" {
		appendCandidate(filepath.Join(repoRoot, filepath.FromSlash(resolved)))
	}
	return candidates
}

func javaScriptTypeScriptPublicTypeReferences(
	root *tree_sitter.Node,
	source []byte,
	publicNames map[string]struct{},
	exportedNames map[string]struct{},
) map[string]struct{} {
	references := make(map[string]struct{})
	if len(publicNames) == 0 {
		return references
	}
	walkNamed(root, func(node *tree_sitter.Node) {
		name := javaScriptTypeScriptDeclarationName(node, source)
		if name == "" {
			return
		}
		if _, ok := publicNames[name]; !ok {
			return
		}
		walkNamed(node, func(child *tree_sitter.Node) {
			switch child.Kind() {
			case "type_identifier", "nested_type_identifier", "scoped_type_identifier":
			default:
				return
			}
			typeName := javaScriptTypeReferenceLeafName(nodeText(child, source))
			if _, ok := exportedNames[typeName]; ok {
				references[typeName] = struct{}{}
			}
		})
	})
	return references
}

func javaScriptTypeScriptStaticRegistryMemberNames(root *tree_sitter.Node, source []byte) map[string]struct{} {
	members := make(map[string]struct{})
	if root == nil {
		return members
	}
	functionNames := javaScriptTypeScriptFunctionNames(root, source)
	if len(functionNames) == 0 {
		return members
	}
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "object" || !javaScriptObjectLiteralIsExportedRegistry(node, source) {
			return
		}
		for _, name := range javaScriptObjectAliasNames(node, source, "") {
			if _, ok := functionNames[name]; ok {
				members[name] = struct{}{}
			}
		}
	})
	return members
}

func javaScriptTypeScriptFunctionNames(root *tree_sitter.Node, source []byte) map[string]struct{} {
	names := make(map[string]struct{})
	walkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "function_declaration", "generator_function_declaration":
			name := strings.TrimSpace(javaScriptFunctionName(node.ChildByFieldName("name"), source))
			if name != "" {
				names[name] = struct{}{}
			}
		case "variable_declarator":
			if !javaScriptVariableDeclaratorHasFunctionValue(node) {
				return
			}
			name := strings.TrimSpace(javaScriptFunctionName(node.ChildByFieldName("name"), source))
			if name != "" {
				names[name] = struct{}{}
			}
		}
	})
	return names
}

func javaScriptVariableDeclaratorHasFunctionValue(node *tree_sitter.Node) bool {
	if node == nil || node.Kind() != "variable_declarator" {
		return false
	}
	valueNode := node.ChildByFieldName("value")
	if valueNode == nil {
		return false
	}
	switch valueNode.Kind() {
	case "arrow_function", "function", "function_expression", "generator_function":
		return true
	default:
		return false
	}
}

func javaScriptObjectLiteralIsExportedRegistry(objectNode *tree_sitter.Node, source []byte) bool {
	if objectNode == nil || objectNode.Kind() != "object" {
		return false
	}
	parent := objectNode.Parent()
	if parent == nil || parent.Kind() != "variable_declarator" {
		return false
	}
	if !javaScriptNodeSameRange(parent.ChildByFieldName("value"), objectNode) || !javaScriptIsExported(parent) {
		return false
	}
	return strings.TrimSpace(javaScriptTypeScriptDeclarationName(parent, source)) != ""
}

func javaScriptIsTypeScriptSourcePath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".ts", ".tsx", ".mts", ".cts":
		return true
	default:
		return false
	}
}

func sameJavaScriptPath(left string, right string) bool {
	left = cleanJavaScriptPath(left)
	right = cleanJavaScriptPath(right)
	return left != "" && right != "" && left == right
}
