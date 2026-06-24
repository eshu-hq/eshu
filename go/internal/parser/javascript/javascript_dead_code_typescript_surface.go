// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript

import (
	"path/filepath"
	"slices"
	"strings"

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

type javaScriptTypeScriptSurfaceWalkItem struct {
	path  string
	names map[string]struct{}
	star  bool
	depth int
}

func javaScriptTypeScriptSurfaceRootKinds(
	repoRoot string,
	path string,
	root *tree_sitter.Node,
	source []byte,
	siblingParser *javaScriptSiblingParser,
	parents *javaScriptParentLookup,
) map[string][]string {
	rootKinds := make(map[string][]string)
	if root == nil || !javaScriptIsTypeScriptSourcePath(path) {
		return rootKinds
	}

	exportedNames := javaScriptTypeScriptExportedDeclarationNames(root, source, parents)
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
		for name := range javaScriptTypeScriptPublicReexportNames(repoRoot, publicPath, path, exportedNames, siblingParser) {
			publicNames[name] = struct{}{}
			rootKinds[name] = appendUniqueString(rootKinds[name], typeScriptPublicAPIReexportRoot)
		}
		for name := range javaScriptTypeScriptPublicImportedTypeReferenceNames(repoRoot, publicPath, path, exportedNames, siblingParser) {
			rootKinds[name] = appendUniqueString(rootKinds[name], typeScriptPublicAPITypeReferenceRoot)
		}
	}

	for name := range javaScriptTypeScriptPublicTypeReferences(root, source, publicNames, exportedNames) {
		rootKinds[name] = appendUniqueString(rootKinds[name], typeScriptPublicAPITypeReferenceRoot)
	}
	for name := range javaScriptTypeScriptStaticRegistryMemberNames(root, source, parents) {
		rootKinds[name] = appendUniqueString(rootKinds[name], typeScriptStaticRegistryMemberRoot)
	}
	return rootKinds
}

func javaScriptTypeScriptPublicReexportNames(
	repoRoot string,
	publicPath string,
	targetPath string,
	exportedNames map[string]struct{},
	siblingParser *javaScriptSiblingParser,
) map[string]struct{} {
	const maxReexportDepth = 8
	publicNames := make(map[string]struct{})
	queue := []javaScriptTypeScriptSurfaceWalkItem{{path: publicPath, star: true}}
	visited := make(map[string]struct{})
	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]
		if item.depth >= maxReexportDepth {
			continue
		}
		visitKey := javaScriptTypeScriptSurfaceWalkKey(item)
		if _, ok := visited[visitKey]; ok {
			continue
		}
		visited[visitKey] = struct{}{}

		for _, reexport := range javaScriptTypeScriptStaticReexportsFromFile(item.path, siblingParser) {
			nextNames, nextStar, ok := javaScriptTypeScriptPropagateReexport(item, reexport)
			if !ok {
				continue
			}
			for _, candidatePath := range javaScriptTypeScriptReexportSourceCandidates(repoRoot, item.path, reexport.source) {
				if sameJavaScriptPath(candidatePath, targetPath) {
					javaScriptTypeScriptMarkPublicNames(publicNames, exportedNames, nextNames, nextStar)
					continue
				}
				queue = append(queue, javaScriptTypeScriptSurfaceWalkItem{
					path:  candidatePath,
					names: nextNames,
					star:  nextStar,
					depth: item.depth + 1,
				})
			}
		}
	}
	return publicNames
}

func javaScriptTypeScriptPropagateReexport(
	item javaScriptTypeScriptSurfaceWalkItem,
	reexport javaScriptTypeScriptSurfaceReexport,
) (map[string]struct{}, bool, bool) {
	if item.star {
		if reexport.exportedName == "*" {
			return nil, true, true
		}
		name := strings.TrimSpace(reexport.originalName)
		if name == "" {
			return nil, false, false
		}
		return map[string]struct{}{name: {}}, false, true
	}
	if len(item.names) == 0 {
		return nil, false, false
	}
	if reexport.exportedName == "*" {
		return cloneJavaScriptTypeScriptSurfaceNames(item.names), false, true
	}
	exportedName := strings.TrimSpace(reexport.exportedName)
	if _, ok := item.names[exportedName]; !ok {
		return nil, false, false
	}
	originalName := strings.TrimSpace(reexport.originalName)
	if originalName == "" {
		return nil, false, false
	}
	return map[string]struct{}{originalName: {}}, false, true
}

func javaScriptTypeScriptMarkPublicNames(
	publicNames map[string]struct{},
	exportedNames map[string]struct{},
	nextNames map[string]struct{},
	nextStar bool,
) {
	if nextStar {
		for name := range exportedNames {
			publicNames[name] = struct{}{}
		}
		return
	}
	for name := range nextNames {
		if _, ok := exportedNames[name]; ok {
			publicNames[name] = struct{}{}
		}
	}
}

func javaScriptTypeScriptSurfaceWalkKey(item javaScriptTypeScriptSurfaceWalkItem) string {
	path := cleanJavaScriptPath(item.path)
	if item.star {
		return path + "|*"
	}
	names := make([]string, 0, len(item.names))
	for name := range item.names {
		names = append(names, name)
	}
	slices.Sort(names)
	return path + "|" + strings.Join(names, ",")
}

func cloneJavaScriptTypeScriptSurfaceNames(names map[string]struct{}) map[string]struct{} {
	clone := make(map[string]struct{}, len(names))
	for name := range names {
		clone[name] = struct{}{}
	}
	return clone
}

func javaScriptIsTypeScriptInterfaceImplementationMethod(node *tree_sitter.Node, name string, source []byte, parents *javaScriptParentLookup) bool {
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
	for current := parents.parent(node); current != nil; current = parents.parent(current) {
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

func javaScriptTypeScriptExportedDeclarationNames(root *tree_sitter.Node, source []byte, parents *javaScriptParentLookup) map[string]struct{} {
	names := make(map[string]struct{})
	walkNamed(root, func(node *tree_sitter.Node) {
		if !javaScriptIsExported(node, parents) {
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
	return PackagePublicSourcePaths(repoRoot, path)
}

func javaScriptTypeScriptStaticReexportsFromFile(
	path string,
	siblingParser *javaScriptSiblingParser,
) []javaScriptTypeScriptSurfaceReexport {
	root, source, ok := siblingParser.rootForFile(path)
	if !ok {
		return nil
	}
	return javaScriptTypeScriptStaticReexportsFromRoot(root, source)
}

// javaScriptTypeScriptStaticReexportsFromRoot extracts every static re-export
// edge from a parsed module: named re-exports (export { A as B } from "..."),
// star re-exports (export * from "..."), and the declare-namespace export-type
// clause that re-exports previously imported names. It walks export_statement
// nodes and reuses the shared re-export specifier helpers.
func javaScriptTypeScriptStaticReexportsFromRoot(root *tree_sitter.Node, source []byte) []javaScriptTypeScriptSurfaceReexport {
	reexports := make([]javaScriptTypeScriptSurfaceReexport, 0)
	if root == nil {
		return reexports
	}
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "export_statement" {
			return
		}
		moduleSource := strings.TrimSpace(javaScriptReExportSource(node, source))
		if moduleSource == "" {
			return
		}
		if javaScriptIsStarReExport(node, source) {
			reexports = append(reexports, javaScriptTypeScriptSurfaceReexport{
				exportedName: "*",
				originalName: "*",
				source:       moduleSource,
			})
			return
		}
		for _, specifier := range javaScriptReExportSpecifiers(node, source) {
			reexports = append(reexports, javaScriptTypeScriptSurfaceReexport{
				exportedName: specifier.exportedName,
				originalName: specifier.originalName,
				source:       moduleSource,
			})
		}
	})
	reexports = append(reexports, javaScriptTypeScriptImportedExportClauseReexportsFromRoot(root, source)...)
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
		for _, candidate := range TSConfigSourceCandidates(basePath) {
			if !pathWithin(repoRoot, candidate) {
				continue
			}
			appendCandidate(candidate)
		}
		return candidates
	}

	resolver := NewTSConfigImportResolver(repoRoot, fromPath)
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

func javaScriptTypeScriptStaticRegistryMemberNames(root *tree_sitter.Node, source []byte, parents *javaScriptParentLookup) map[string]struct{} {
	members := make(map[string]struct{})
	if root == nil {
		return members
	}
	functionNames := javaScriptTypeScriptFunctionNames(root, source)
	if len(functionNames) == 0 {
		return members
	}
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "object" || !javaScriptObjectLiteralIsExportedRegistry(node, source, parents) {
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

func javaScriptObjectLiteralIsExportedRegistry(objectNode *tree_sitter.Node, source []byte, parents *javaScriptParentLookup) bool {
	if objectNode == nil || objectNode.Kind() != "object" {
		return false
	}
	parent := parents.parent(objectNode)
	if parent == nil || parent.Kind() != "variable_declarator" {
		return false
	}
	if !javaScriptNodeSameRange(parent.ChildByFieldName("value"), objectNode) || !javaScriptIsExported(parent, parents) {
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
