// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package golang

import (
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/golang/symbols"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

type goDeadCodeEvidenceSet struct {
	functionRootKinds  map[string][]string
	interfaceRootKinds map[string][]string
	structRootKinds    map[string][]string
}

func goDeadCodeEvidence(
	root *tree_sitter.Node,
	source []byte,
	importAliases map[string][]string,
	importedParamMethods GoImportedInterfaceParamMethods,
	directMethodCallRoots GoDirectMethodCallRoots,
	packageImportPath string,
	localNameBindings []symbols.LocalNameBinding,
	constructorReturns map[string]string,
	lookup *symbols.ParentLookup,
) goDeadCodeEvidenceSet {
	evidence := goDeadCodeEvidenceSet{
		functionRootKinds:  goRegisteredDeadCodeRootKinds(root, source, importAliases),
		interfaceRootKinds: make(map[string][]string),
		structRootKinds:    make(map[string][]string),
	}
	goMergePackageDirectMethodRoots(root, source, directMethodCallRoots, packageImportPath, evidence.functionRootKinds)
	goCollectSemanticDeadCodeRoots(
		root,
		source,
		importAliases,
		importedParamMethods,
		localNameBindings,
		constructorReturns,
		evidence.functionRootKinds,
		evidence.interfaceRootKinds,
		evidence.structRootKinds,
		lookup,
	)
	return evidence
}

func goMergePackageDirectMethodRoots(
	root *tree_sitter.Node,
	source []byte,
	directMethodCallRoots GoDirectMethodCallRoots,
	packageImportPath string,
	functionRootKinds map[string][]string,
) {
	importPath := strings.ToLower(strings.TrimSpace(packageImportPath))
	if importPath == "" || len(directMethodCallRoots) == 0 {
		return
	}
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "method_declaration" {
			return
		}
		receiver := strings.ToLower(symbols.ReceiverContext(node, source))
		name := strings.ToLower(strings.TrimSpace(nodeText(node.ChildByFieldName("name"), source)))
		if receiver == "" || name == "" {
			return
		}
		qualifiedKey := importPath + "." + receiver + "." + name
		for _, kind := range directMethodCallRoots[qualifiedKey] {
			localKey := receiver + "." + name
			functionRootKinds[localKey] = symbols.AppendUniqueImportAlias(functionRootKinds[localKey], kind)
		}
	})
}

func goDeadCodeRootKinds(
	node *tree_sitter.Node,
	source []byte,
	importAliases map[string][]string,
	registeredRootKinds map[string][]string,
) []string {
	params := goCompactSignature(node.ChildByFieldName("parameters"), source)
	results := goCompactSignature(node.ChildByFieldName("result"), source)
	name := strings.TrimSpace(nodeText(node.ChildByFieldName("name"), source))

	rootKinds := make([]string, 0, 5)
	if node.Kind() == "function_declaration" {
		for _, kind := range registeredRootKinds[strings.ToLower(name)] {
			rootKinds = symbols.AppendUniqueImportAlias(rootKinds, kind)
		}
	}
	if node.Kind() == "method_declaration" {
		methodKey := strings.ToLower(symbols.ReceiverContext(node, source) + "." + name)
		for _, kind := range registeredRootKinds[methodKey] {
			rootKinds = symbols.AppendUniqueImportAlias(rootKinds, kind)
		}
	}
	if goSignatureMatchesHTTPHandler(params, importAliases) {
		rootKinds = symbols.AppendUniqueImportAlias(rootKinds, "go.net_http_handler_signature")
	}
	if goSignatureMatchesCobraRun(params, importAliases) {
		rootKinds = symbols.AppendUniqueImportAlias(rootKinds, "go.cobra_run_signature")
	}
	if name == "Reconcile" && goSignatureMatchesControllerRuntimeReconcile(params, results, importAliases) {
		rootKinds = symbols.AppendUniqueImportAlias(rootKinds, "go.controller_runtime_reconcile_signature")
	}
	return rootKinds
}

func goCompactSignature(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	return strings.ToLower(strings.Join(strings.Fields(nodeText(node, source)), ""))
}

func goSignatureMatchesHTTPHandler(params string, importAliases map[string][]string) bool {
	if params == "" {
		return false
	}
	httpAliases := symbols.AliasesForImportPath(importAliases, "net/http")
	if len(httpAliases) == 0 {
		return false
	}
	return goSignatureContainsAnyQualifiedType(params, httpAliases, "responsewriter") &&
		goSignatureContainsAnyRequestPointer(params, httpAliases)
}

func goSignatureMatchesCobraRun(params string, importAliases map[string][]string) bool {
	if params == "" || !strings.Contains(params, "[]string") {
		return false
	}
	cobraAliases := symbols.AliasesForImportPath(importAliases, "github.com/spf13/cobra")
	if len(cobraAliases) == 0 {
		return false
	}
	return goSignatureContainsAnyPointerType(params, cobraAliases, "command")
}

func goSignatureMatchesControllerRuntimeReconcile(
	params string,
	results string,
	importAliases map[string][]string,
) bool {
	if params == "" || results == "" || !strings.Contains(results, "error") {
		return false
	}

	contextAliases := symbols.AliasesForImportPath(importAliases, "context")
	if len(contextAliases) == 0 || !goSignatureContainsAnyQualifiedType(params, contextAliases, "context") {
		return false
	}

	controllerAliases := goMergedAliasesForImportPaths(
		importAliases,
		"sigs.k8s.io/controller-runtime",
		"sigs.k8s.io/controller-runtime/pkg/reconcile",
	)
	if len(controllerAliases) == 0 {
		return false
	}

	return goSignatureContainsAnyQualifiedType(params, controllerAliases, "request") &&
		goSignatureContainsAnyQualifiedType(results, controllerAliases, "result")
}

func goMergedAliasesForImportPaths(index map[string][]string, importPaths ...string) []string {
	merged := make([]string, 0)
	for _, importPath := range importPaths {
		for _, alias := range index[importPath] {
			merged = symbols.AppendUniqueImportAlias(merged, alias)
		}
	}
	slices.Sort(merged)
	return merged
}

func goSignatureContainsAnyQualifiedType(signature string, aliases []string, typeName string) bool {
	for _, alias := range aliases {
		if strings.Contains(signature, strings.ToLower(alias)+"."+typeName) {
			return true
		}
	}
	return false
}

func goSignatureContainsAnyPointerType(signature string, aliases []string, typeName string) bool {
	for _, alias := range aliases {
		if strings.Contains(signature, "*"+strings.ToLower(alias)+"."+typeName) {
			return true
		}
	}
	return false
}

func goSignatureContainsAnyRequestPointer(signature string, aliases []string) bool {
	for _, alias := range aliases {
		lowerAlias := strings.ToLower(alias)
		if strings.Contains(signature, "*"+lowerAlias+".request") {
			return true
		}
	}
	return false
}
