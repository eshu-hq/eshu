// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package php

import (
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// phpParseState carries the cross-statement evidence that PHP type inference
// and dead-code root classification need while walking the AST. The maps mirror
// the contracts the parser payload exposes: property and parent types resolve
// receiver chains, return-type maps resolve method and function call chains, and
// import aliases normalize use-imported short names back to their canonical
// types.
type phpParseState struct {
	payload             map[string]any
	source              []byte
	indexSource         bool
	seenVariables       map[string]struct{}
	seenCalls           map[string]struct{}
	classPropertyTypes  map[string]map[string]string
	classParentTypes    map[string]string
	localVariableTypes  map[string]map[string]string
	methodReturnTypes   map[string]map[string]string
	functionReturnTypes map[string]string
	importAliases       map[string]string
	parents             *phpParentLookup
	deadCodeFacts       phpDeadCodeFacts
	deadCodeFunctions   []phpDeadCodeFunctionFact
}

// Parse extracts PHP declarations, imports, variables, and calls from the
// tree-sitter AST and emits the parser payload buckets.
func Parse(path string, isDependency bool, options shared.Options, parser *tree_sitter.Parser) (map[string]any, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}

	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, parseError(path)
	}
	defer tree.Close()

	payload := shared.BasePayload(path, "php", isDependency)
	payload["traits"] = []map[string]any{}
	payload["interfaces"] = []map[string]any{}

	state := &phpParseState{
		payload:             payload,
		source:              source,
		indexSource:         options.IndexSource,
		seenVariables:       make(map[string]struct{}),
		seenCalls:           make(map[string]struct{}),
		classPropertyTypes:  make(map[string]map[string]string),
		classParentTypes:    make(map[string]string),
		localVariableTypes:  make(map[string]map[string]string),
		methodReturnTypes:   make(map[string]map[string]string),
		functionReturnTypes: make(map[string]string),
		importAliases:       make(map[string]string),
		deadCodeFacts:       newPHPDeadCodeFacts(),
		deadCodeFunctions:   make([]phpDeadCodeFunctionFact, 0),
	}

	root := tree.RootNode()
	parents := buildPHPParentLookup(root)

	// Phase 1: collect declarations, imports, type evidence, and dead-code
	// facts so call and variable inference in phase 2 sees the whole file.
	state.parents = parents
	collectPHPDeclarations(state, root)

	// Phase 2: emit variables and call rows that depend on the type evidence.
	emitPHPVariablesAndCalls(state, root)

	// Assign dead-code root kinds now that every interface, trait, route, and
	// hook fact has been observed.
	for _, function := range state.deadCodeFunctions {
		rootKinds := phpDeadCodeRootKinds(
			function.name,
			function.contextName,
			function.contextKind,
			function.lineNumber,
			function.parameters,
			function.isPublic,
			state.deadCodeFacts,
		)
		if len(rootKinds) > 0 {
			function.item["dead_code_root_kinds"] = rootKinds
		}
	}

	if namespace := phpNamespaceName(root, source); namespace != "" {
		payload["namespace"] = namespace
	}
	if semantics := buildPHPFrameworkSemantics(root, source, state.payload); len(semantics["frameworks"].([]string)) > 0 {
		payload["framework_semantics"] = semantics
	}

	for _, bucket := range []string{
		"functions", "classes", "traits", "interfaces", "variables", "imports", "function_calls",
	} {
		shared.SortNamedBucket(payload, bucket)
	}

	return payload, nil
}

// PreScan returns PHP function, class, trait, and interface names used by repository pre-scan.
func PreScan(path string, parser *tree_sitter.Parser) ([]string, error) {
	return preScanNames(path, parser)
}

// collectPHPDeclarations walks the AST once to populate declaration buckets,
// import rows and aliases, property and return-type evidence, and dead-code
// facts. Calls and free variables are emitted in a later pass.
func collectPHPDeclarations(state *phpParseState, root *tree_sitter.Node) {
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "namespace_use_declaration":
			collectPHPImports(state, node)
		case "class_declaration":
			collectPHPClassDeclaration(state, node)
		case "interface_declaration":
			collectPHPInterfaceDeclaration(state, node)
		case "trait_declaration":
			collectPHPTraitDeclaration(state, node)
		case "anonymous_class":
			collectPHPAnonymousClass(state, node)
		case "function_definition":
			collectPHPFunction(state, node, "", "")
		case "attribute":
			observePHPAttribute(state, node)
		case "function_call_expression":
			observePHPWordPressHookCall(state, node)
		case "array_creation_expression":
			collectPHPLiteralRouteTarget(state, node)
		}
	})
}

// emitPHPVariablesAndCalls walks the AST a second time to emit property,
// assignment, and parameter variable rows plus every call row with inferred
// receiver types.
func emitPHPVariablesAndCalls(state *phpParseState, root *tree_sitter.Node) {
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "variable_name":
			emitPHPVariableName(state, node)
		case "member_call_expression", "nullsafe_member_call_expression":
			emitPHPMemberCall(state, node)
		case "scoped_call_expression":
			emitPHPScopedCall(state, node)
		case "object_creation_expression":
			emitPHPObjectCreation(state, node)
		case "function_call_expression":
			emitPHPFunctionCall(state, node)
		}
	})
}

func parseError(path string) error {
	return &phpParseFailure{path: path}
}

type phpParseFailure struct{ path string }

func (e *phpParseFailure) Error() string {
	return "parse php file " + strconv.Quote(e.path) + ": parser returned nil tree"
}

// phpNamespaceName returns the first namespace name declared in the file, or
// the empty string when the file declares no namespace.
func phpNamespaceName(root *tree_sitter.Node, source []byte) string {
	var name string
	cursor := root.Walk()
	defer cursor.Close()
	for _, child := range root.NamedChildren(cursor) {
		child := child
		if child.Kind() != "namespace_definition" {
			continue
		}
		nameNode := child.ChildByFieldName("name")
		if nameNode == nil {
			continue
		}
		name = strings.TrimSpace(shared.NodeText(nameNode, source))
		break
	}
	return name
}
