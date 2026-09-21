// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package golang

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/golang/symbols"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// ImportedInterfaceParamMethods returns same-file Go function signatures that
// accept known imported interfaces. The parent parser groups these rows by
// package directory before feeding them into per-file parse options.
func ImportedInterfaceParamMethods(
	parser *tree_sitter.Parser,
	path string,
) (shared.GoImportedInterfaceParamMethods, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, fmt.Errorf("parse go file %q: parser returned nil tree", path)
	}
	defer tree.Close()

	return symbols.FunctionParamImportedInterfaceMethods(tree.RootNode(), source), nil
}

// ExportedInterfaceParamMethods returns exported Go function signatures whose
// parameters accept package-local interfaces. Parent repo pre-scan qualifies
// these by module import path so callers in other packages can root concrete
// values that escape into those interfaces.
func ExportedInterfaceParamMethods(
	parser *tree_sitter.Parser,
	path string,
) (shared.GoImportedInterfaceParamMethods, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, fmt.Errorf("parse go file %q: parser returned nil tree", path)
	}
	defer tree.Close()

	root := tree.RootNode()
	interfaceMethods := make(map[string][]string)
	exportedFunctions := make(map[string]struct{})
	walkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "function_declaration":
			rawName := strings.TrimSpace(nodeText(node.ChildByFieldName("name"), source))
			if symbols.IdentifierIsExported(rawName) {
				exportedFunctions[strings.ToLower(rawName)] = struct{}{}
			}
		case "type_spec":
			name := strings.ToLower(strings.TrimSpace(nodeText(node.ChildByFieldName("name"), source)))
			typeNode := node.ChildByFieldName("type")
			if name != "" && typeNode != nil && typeNode.Kind() == "interface_type" {
				interfaceMethods[name] = symbols.InterfaceMethodNames(typeNode, source)
			}
		}
	})

	targets := symbols.FunctionParamInterfaceTargets(root, source, interfaceMethods)
	importedMethods := make(shared.GoImportedInterfaceParamMethods)
	for functionName, byIndex := range targets {
		if _, ok := exportedFunctions[functionName]; !ok {
			continue
		}
		for index, target := range byIndex {
			if target.LocalInterface == "" {
				continue
			}
			if _, ok := importedMethods[functionName]; !ok {
				importedMethods[functionName] = make(map[int][]string)
			}
			importedMethods[functionName][index] = goPackageInterfaceMethodNames(interfaceMethods)
		}
	}
	return importedMethods, nil
}

func goPackageInterfaceMethodNames(interfaceMethods map[string][]string) []string {
	methodNames := make([]string, 0)
	for _, methods := range interfaceMethods {
		methodNames = symbols.AppendUniqueMethods(methodNames, methods)
	}
	return methodNames
}

// ImportedDirectMethodCallRoots returns qualified method declarations that are
// called through imported package types in one Go file. Parent repo pre-scan
// routes those roots back to the package that defines the methods.
func ImportedDirectMethodCallRoots(
	parser *tree_sitter.Parser,
	path string,
) (shared.GoDirectMethodCallRoots, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, fmt.Errorf("parse go file %q: parser returned nil tree", path)
	}
	defer tree.Close()

	root := tree.RootNode()
	lookup := symbols.BuildParentLookup(root)
	importAliases := symbols.ImportAliasIndex(root, source)
	interfaceMethodReturns := symbols.LocalInterfaceImportedMethodReturns(root, source, importAliases)
	// Build the imported-variable-type index once per file so per-call lookups
	// inside this walk are O(package_vars + scope_bindings_before_call), not
	// O(tree_size). The prior pattern called goKnownImportedVariableTypesForCall
	// per call_expression, and that helper internally re-walked the entire
	// file's tree — quadratic behavior that hung the Terraform ingest (#161).
	variableTypeIndex := symbols.BuildImportedVariableTypeIndex(root, source, importAliases, lookup)
	roots := make(shared.GoDirectMethodCallRoots)
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "call_expression" {
			return
		}
		functionNode := node.ChildByFieldName("function")
		if functionNode == nil || functionNode.Kind() != "selector_expression" {
			return
		}
		variableTypes := variableTypeIndex.ForCall(node)
		key := symbols.ImportedDirectMethodCallKey(node, source, importAliases, variableTypes, interfaceMethodReturns)
		if key != "" {
			roots[key] = symbols.AppendUniqueImportAlias(roots[key], "go.imported_direct_method_call")
		}
		for _, stringerKey := range symbols.ImportedFmtStringerCallKeys(node, source, importAliases, variableTypes, interfaceMethodReturns) {
			roots[stringerKey] = symbols.AppendUniqueImportAlias(roots[stringerKey], "go.imported_fmt_stringer_method")
		}
	})
	return roots, nil
}

// ImportedDirectMethodCallRootsWithInterfaceReturns returns qualified method
// roots for one Go file using package-level local-interface return metadata.
func ImportedDirectMethodCallRootsWithInterfaceReturns(
	parser *tree_sitter.Parser,
	path string,
	interfaceMethodReturns map[string]string,
) (shared.GoDirectMethodCallRoots, error) {
	source, root, closeTree, err := parseGoPreScanFile(parser, path)
	if err != nil {
		return nil, err
	}
	defer closeTree()

	lookup := symbols.BuildParentLookup(root)
	importAliases := symbols.ImportAliasIndex(root, source)
	// See symbols.BuildImportedVariableTypeIndex doc-comment in
	// imported_variable_type_index.go. The interface-returns variant shares
	// the same call_expression hot path and therefore the same fix (#161).
	variableTypeIndex := symbols.BuildImportedVariableTypeIndex(root, source, importAliases, lookup)
	roots := make(shared.GoDirectMethodCallRoots)
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "call_expression" {
			return
		}
		functionNode := node.ChildByFieldName("function")
		if functionNode == nil || functionNode.Kind() != "selector_expression" {
			return
		}
		variableTypes := variableTypeIndex.ForCall(node)
		key := symbols.ImportedDirectMethodCallKey(node, source, importAliases, variableTypes, interfaceMethodReturns)
		if key != "" {
			roots[key] = symbols.AppendUniqueImportAlias(roots[key], "go.imported_direct_method_call")
		}
		for _, stringerKey := range symbols.ImportedFmtStringerCallKeys(node, source, importAliases, variableTypes, interfaceMethodReturns) {
			roots[stringerKey] = symbols.AppendUniqueImportAlias(roots[stringerKey], "go.imported_fmt_stringer_method")
		}
	})
	return roots, nil
}

// LocalInterfaceImportedMethodReturns returns local interface methods whose
// results are imported receiver types. Parent pre-scan combines these rows
// across package files before scanning chained receiver calls.
func LocalInterfaceImportedMethodReturns(
	parser *tree_sitter.Parser,
	path string,
) (map[string]string, error) {
	source, root, closeTree, err := parseGoPreScanFile(parser, path)
	if err != nil {
		return nil, err
	}
	defer closeTree()

	return symbols.LocalInterfaceImportedMethodReturns(root, source, symbols.ImportAliasIndex(root, source)), nil
}

// LocalInterfaceMethods returns package-local interface method names from one
// Go file. Parent package pre-scan combines these rows across files before
// deriving generic constraint roots.
func LocalInterfaceMethods(
	parser *tree_sitter.Parser,
	path string,
) (map[string][]string, error) {
	source, root, closeTree, err := parseGoPreScanFile(parser, path)
	if err != nil {
		return nil, err
	}
	defer closeTree()

	methods := make(map[string][]string)
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "type_spec" {
			return
		}
		name := strings.ToLower(strings.TrimSpace(nodeText(node.ChildByFieldName("name"), source)))
		typeNode := node.ChildByFieldName("type")
		if name != "" && typeNode != nil && typeNode.Kind() == "interface_type" {
			methods[name] = symbols.InterfaceMethodNames(typeNode, source)
		}
	})
	return methods, nil
}

// GenericConstraintInterfaceNames returns lower-case identifiers used inside Go
// type parameter constraints. The parent pre-scan intersects these names with
// package-local interfaces before rooting matching method declarations.
func GenericConstraintInterfaceNames(
	parser *tree_sitter.Parser,
	path string,
) ([]string, error) {
	source, root, closeTree, err := parseGoPreScanFile(parser, path)
	if err != nil {
		return nil, err
	}
	defer closeTree()

	names := make([]string, 0)
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "type_parameter_declaration" {
			return
		}
		for _, name := range symbols.TypeParameterConstraintCandidates(nodeText(node, source)) {
			names = symbols.AppendUniqueImportAlias(names, name)
		}
	})
	return names, nil
}

// MethodDeclarationKeys returns lower-case receiver.method keys declared in one
// Go file.
func MethodDeclarationKeys(
	parser *tree_sitter.Parser,
	path string,
) ([]string, error) {
	source, root, closeTree, err := parseGoPreScanFile(parser, path)
	if err != nil {
		return nil, err
	}
	defer closeTree()

	keys := make([]string, 0)
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "method_declaration" {
			return
		}
		receiver := strings.ToLower(symbols.ReceiverContext(node, source))
		name := strings.ToLower(strings.TrimSpace(nodeText(node.ChildByFieldName("name"), source)))
		if receiver != "" && name != "" {
			keys = symbols.AppendUniqueImportAlias(keys, receiver+"."+name)
		}
	})
	return keys, nil
}

func parseGoPreScanFile(
	parser *tree_sitter.Parser,
	path string,
) ([]byte, *tree_sitter.Node, func(), error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, nil, nil, err
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, nil, nil, fmt.Errorf("parse go file %q: parser returned nil tree", path)
	}
	return source, tree.RootNode(), tree.Close, nil
}
