package golang

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// PrescanFileEvidence carries every per-file Go evidence type the parent
// package's PreScanGoPackageSemanticRoots loop needs to aggregate, so the
// parent can collect each file's contribution in a single tree-sitter parse
// instead of one parse per evidence type.
//
// The fields mirror the existing per-evidence ImportedInterfaceParamMethods,
// ExportedInterfaceParamMethods, ImportedDirectMethodCallRoots,
// LocalInterfaceImportedMethodReturns, LocalInterfaceMethods,
// GenericConstraintInterfaceNames, and MethodDeclarationKeys public entrypoints.
// Pass 5 in the parent loop (chained method call roots using package-level
// interface returns) still runs in a second per-file parse because its input
// is only known after the parent finishes aggregating per-file returns.
type PrescanFileEvidence struct {
	ImportedInterfaceParamMethods       shared.GoImportedInterfaceParamMethods
	ExportedInterfaceParamMethods       shared.GoImportedInterfaceParamMethods
	ImportedDirectMethodCallRoots       shared.GoDirectMethodCallRoots
	LocalInterfaceImportedMethodReturns map[string]string
	LocalInterfaceMethods               map[string][]string
	GenericConstraintInterfaceNames     []string
	MethodDeclarationKeys               []string
}

// PreScanFileEvidence reads, parses, and walks a single Go source file once
// to populate every evidence type the parent package prescan needs. The
// returned *PrescanFileEvidence carries the same values the per-evidence
// public functions in this package produce; the parent prescan loop calls
// this in place of seven separate read+parse+walk passes.
//
// PreScanFileEvidence does not own the supplied parser. Callers must keep the
// parser alive for the duration of the call and close it when done. The
// function reuses the parser across files because tree-sitter parsers are
// stateful but reusable when callers serialize access.
func PreScanFileEvidence(
	parser *tree_sitter.Parser,
	path string,
) (*PrescanFileEvidence, error) {
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
	lookup := goBuildParentLookup(root)
	importAliases := goImportAliasIndex(root, source)
	interfaceMethods := extractLocalInterfaceMethods(root, source)
	variableTypeIndex := goBuildImportedVariableTypeIndex(root, source, importAliases, lookup)
	localInterfaceReturns := goLocalInterfaceImportedMethodReturns(root, source, importAliases)

	return &PrescanFileEvidence{
		ImportedInterfaceParamMethods:       goFunctionParamImportedInterfaceMethods(root, source),
		ExportedInterfaceParamMethods:       extractExportedInterfaceParamMethods(root, source, interfaceMethods),
		ImportedDirectMethodCallRoots:       extractImportedDirectMethodCallRoots(root, source, importAliases, variableTypeIndex, localInterfaceReturns),
		LocalInterfaceImportedMethodReturns: localInterfaceReturns,
		LocalInterfaceMethods:               interfaceMethods,
		GenericConstraintInterfaceNames:     extractGenericConstraintInterfaceNames(root, source),
		MethodDeclarationKeys:               extractMethodDeclarationKeys(root, source),
	}, nil
}

// extractLocalInterfaceMethods walks the file's type_spec nodes and returns
// each local interface name to its declared method names. Matches the
// behavior of LocalInterfaceMethods(parser, path).
func extractLocalInterfaceMethods(root *tree_sitter.Node, source []byte) map[string][]string {
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
	return methods
}

// extractExportedInterfaceParamMethods reproduces ExportedInterfaceParamMethods
// without re-reading or re-parsing the file. The caller supplies the local
// interface method map already extracted in the same parse pass so this helper
// only does the exported-function classification and target filtering work.
func extractExportedInterfaceParamMethods(
	root *tree_sitter.Node,
	source []byte,
	interfaceMethods map[string][]string,
) shared.GoImportedInterfaceParamMethods {
	exportedFunctions := make(map[string]struct{})
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "function_declaration" {
			return
		}
		rawName := strings.TrimSpace(nodeText(node.ChildByFieldName("name"), source))
		if goIdentifierIsExported(rawName) {
			exportedFunctions[strings.ToLower(rawName)] = struct{}{}
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
	return importedMethods
}

// extractImportedDirectMethodCallRoots reproduces ImportedDirectMethodCallRoots
// without re-reading or re-parsing the file. The caller supplies importAliases,
// variableTypeIndex, and per-file interfaceMethodReturns already built in the
// same parse pass.
func extractImportedDirectMethodCallRoots(
	root *tree_sitter.Node,
	source []byte,
	importAliases map[string][]string,
	variableTypeIndex *goImportedVariableTypeIndex,
	interfaceMethodReturns map[string]string,
) shared.GoDirectMethodCallRoots {
	roots := make(shared.GoDirectMethodCallRoots)
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "call_expression" {
			return
		}
		variableTypes := variableTypeIndex.ForCall(node)
		key := goImportedDirectMethodCallKey(node, source, importAliases, variableTypes, interfaceMethodReturns)
		if key != "" {
			roots[key] = appendUniqueImportAlias(roots[key], "go.imported_direct_method_call")
		}
		for _, stringerKey := range goImportedFmtStringerCallKeys(node, source, importAliases, variableTypes, interfaceMethodReturns) {
			roots[stringerKey] = appendUniqueImportAlias(roots[stringerKey], "go.imported_fmt_stringer_method")
		}
	})
	return roots
}

// extractGenericConstraintInterfaceNames reproduces GenericConstraintInterfaceNames
// without re-reading or re-parsing the file.
func extractGenericConstraintInterfaceNames(root *tree_sitter.Node, source []byte) []string {
	names := make([]string, 0)
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "type_parameter_declaration" {
			return
		}
		for _, name := range goTypeParameterConstraintCandidates(nodeText(node, source)) {
			names = appendUniqueImportAlias(names, name)
		}
	})
	return names
}

// extractMethodDeclarationKeys reproduces MethodDeclarationKeys without
// re-reading or re-parsing the file. Returns lower-case receiver.method keys.
func extractMethodDeclarationKeys(root *tree_sitter.Node, source []byte) []string {
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
	return keys
}
