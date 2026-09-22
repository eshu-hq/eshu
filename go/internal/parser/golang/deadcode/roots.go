// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode

import (
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/golang/deadcode/semantic"
	"github.com/eshu-hq/eshu/go/internal/parser/golang/symbols"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// EvidenceSet is the dead-code root evidence gathered for one file: the
// registration- and semantic-derived root kinds keyed by lower-cased
// function/method, interface, and struct identity. The golang parser reads
// its fields directly when rendering each declaration's "dead_code_root_kinds"
// payload field.
type EvidenceSet struct {
	FunctionRootKinds  map[string][]string
	InterfaceRootKinds map[string][]string
	StructRootKinds    map[string][]string
}

// Evidence gathers the dead-code root evidence for one parsed Go file:
// explicit registrations (net/http handler and cobra command wiring),
// same-package direct method call roots, and the semantic evidence collected
// by the deadcode/semantic package (interface satisfaction, function-value
// references, generic constraints, and dependency-injection callbacks).
func Evidence(
	root *tree_sitter.Node,
	source []byte,
	importAliases map[string][]string,
	importedParamMethods shared.GoImportedInterfaceParamMethods,
	directMethodCallRoots shared.GoDirectMethodCallRoots,
	packageImportPath string,
	localNameBindings []symbols.LocalNameBinding,
	constructorReturns map[string]string,
	lookup *symbols.ParentLookup,
) EvidenceSet {
	evidence := EvidenceSet{
		FunctionRootKinds:  goRegisteredDeadCodeRootKinds(root, source, importAliases),
		InterfaceRootKinds: make(map[string][]string),
		StructRootKinds:    make(map[string][]string),
	}
	goMergePackageDirectMethodRoots(root, source, directMethodCallRoots, packageImportPath, evidence.FunctionRootKinds)
	semantic.CollectRoots(
		root,
		source,
		importAliases,
		importedParamMethods,
		localNameBindings,
		constructorReturns,
		evidence.FunctionRootKinds,
		evidence.InterfaceRootKinds,
		evidence.StructRootKinds,
		lookup,
	)
	return evidence
}

func goMergePackageDirectMethodRoots(
	root *tree_sitter.Node,
	source []byte,
	directMethodCallRoots shared.GoDirectMethodCallRoots,
	packageImportPath string,
	functionRootKinds map[string][]string,
) {
	importPath := strings.ToLower(strings.TrimSpace(packageImportPath))
	if importPath == "" || len(directMethodCallRoots) == 0 {
		return
	}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "method_declaration" {
			return
		}
		receiver := strings.ToLower(symbols.ReceiverContext(node, source))
		name := strings.ToLower(strings.TrimSpace(shared.NodeText(node.ChildByFieldName("name"), source)))
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

// RootKinds returns the dead-code root kinds recognized for one function or
// method declaration node: explicit registration matches from
// registeredRootKinds, plus signature-shape matches (an HTTP handler, a cobra
// RunE function, or a controller-runtime Reconcile method).
func RootKinds(
	node *tree_sitter.Node,
	source []byte,
	importAliases map[string][]string,
	registeredRootKinds map[string][]string,
) []string {
	params := goCompactSignature(node.ChildByFieldName("parameters"), source)
	results := goCompactSignature(node.ChildByFieldName("result"), source)
	name := strings.TrimSpace(shared.NodeText(node.ChildByFieldName("name"), source))

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
	return strings.ToLower(strings.Join(strings.Fields(shared.NodeText(node, source)), ""))
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
