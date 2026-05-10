package golang

import (
	"fmt"
	"strings"

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

	return goFunctionParamImportedInterfaceMethods(tree.RootNode(), source), nil
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
			if goIdentifierIsExported(rawName) {
				exportedFunctions[strings.ToLower(rawName)] = struct{}{}
			}
		case "type_spec":
			name := strings.ToLower(strings.TrimSpace(nodeText(node.ChildByFieldName("name"), source)))
			typeNode := node.ChildByFieldName("type")
			if name != "" && typeNode != nil && typeNode.Kind() == "interface_type" {
				interfaceMethods[name] = goInterfaceMethodNames(typeNode, source)
			}
		}
	})

	targets := goFunctionParamInterfaceTargets(root, source, interfaceMethods)
	importedMethods := make(shared.GoImportedInterfaceParamMethods)
	for functionName, byIndex := range targets {
		if _, ok := exportedFunctions[functionName]; !ok {
			continue
		}
		for index, target := range byIndex {
			if target.localInterface == "" {
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
		methodNames = appendUniqueMethods(methodNames, methods)
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
	importAliases := goImportAliasIndex(root, source)
	interfaceMethodReturns := goLocalInterfaceImportedMethodReturns(root, source, importAliases)
	roots := make(shared.GoDirectMethodCallRoots)
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "call_expression" {
			return
		}
		variableTypes := goKnownImportedVariableTypesForCall(root, node, source, importAliases)
		key := goImportedDirectMethodCallKey(node, source, importAliases, variableTypes, interfaceMethodReturns)
		if key != "" {
			roots[key] = appendUniqueImportAlias(roots[key], "go.imported_direct_method_call")
		}
		for _, stringerKey := range goImportedFmtStringerCallKeys(node, source, importAliases, variableTypes, interfaceMethodReturns) {
			roots[stringerKey] = appendUniqueImportAlias(roots[stringerKey], "go.imported_fmt_stringer_method")
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

	importAliases := goImportAliasIndex(root, source)
	roots := make(shared.GoDirectMethodCallRoots)
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "call_expression" {
			return
		}
		variableTypes := goKnownImportedVariableTypesForCall(root, node, source, importAliases)
		key := goImportedDirectMethodCallKey(node, source, importAliases, variableTypes, interfaceMethodReturns)
		if key != "" {
			roots[key] = appendUniqueImportAlias(roots[key], "go.imported_direct_method_call")
		}
		for _, stringerKey := range goImportedFmtStringerCallKeys(node, source, importAliases, variableTypes, interfaceMethodReturns) {
			roots[stringerKey] = appendUniqueImportAlias(roots[stringerKey], "go.imported_fmt_stringer_method")
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

	return goLocalInterfaceImportedMethodReturns(root, source, goImportAliasIndex(root, source)), nil
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
			methods[name] = goInterfaceMethodNames(typeNode, source)
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
		for _, name := range goTypeParameterConstraintCandidates(nodeText(node, source)) {
			names = appendUniqueImportAlias(names, name)
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
		receiver := strings.ToLower(goReceiverContext(node, source))
		name := strings.ToLower(strings.TrimSpace(nodeText(node.ChildByFieldName("name"), source)))
		if receiver != "" && name != "" {
			keys = appendUniqueImportAlias(keys, receiver+"."+name)
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

func goTypeParameterConstraintCandidates(text string) []string {
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return r != '_' && r != '.' && r != '*' && r != '~' &&
			(r < '0' || r > '9') &&
			(r < 'a' || r > 'z')
	})
	if len(fields) < 2 {
		return nil
	}
	names := make([]string, 0, len(fields))
	for _, field := range fields[1:] {
		field = strings.Trim(field, "*~")
		if field == "" || field == "any" || field == "comparable" {
			continue
		}
		names = appendUniqueImportAlias(names, field)
	}
	return names
}
