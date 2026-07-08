// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package php

import (
	"log/slog"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// phpParseByteCap bounds the size of a single PHP file handed to tree-sitter.
// Normal hand-written source is tens of KB; the pathological tail is large
// generated files -- a 1.3MB TCPDF CID font-map measured 6.0s (~273x a normal
// parse), and a 1.3MB mPDF file measured 2.5s (#4766) -- that tree-sitter
// parses superlinearly. 1 MiB is generous headroom above any hand-written
// file while remaining well below the pathological range.
const phpParseByteCap = 1 << 20

// phpBoundedFileEvent records one file whose size exceeded phpParseByteCap
// and whose tree-sitter parse was skipped entirely.
type phpBoundedFileEvent struct {
	path          string
	originalBytes int
}

// row renders one bounded-file event as a payload row for
// payload["php_parse_bounded"].
func (e phpBoundedFileEvent) row() map[string]any {
	return map[string]any{
		"path":           e.path,
		"original_bytes": e.originalBytes,
		"action":         "file_skipped",
	}
}

// recordPHPBoundedFile appends a php_parse_bounded payload row for one
// bounded file and emits a matching structured log line so a dropped parse
// is observable rather than silent.
func recordPHPBoundedFile(payload map[string]any, path string, originalBytes int) {
	event := phpBoundedFileEvent{path: path, originalBytes: originalBytes}
	payload["php_parse_bounded"] = append(
		payload["php_parse_bounded"].([]map[string]any),
		event.row(),
	)
	slog.Warn(
		"php parse file bounded",
		"component", "parser.php",
		"path", event.path,
		"original_bytes", event.originalBytes,
		"action", "file_skipped",
	)
}

// phpParseState carries the cross-statement evidence that PHP type inference
// and dead-code root classification need while walking the AST. The maps mirror
// the contracts the parser payload exposes: property and parent types resolve
// receiver chains, return-type maps resolve method and function call chains, and
// import aliases normalize use-imported short names back to their canonical
// types.
//
// Gather slices hold phase-2 resolution-candidate node pointers collected
// during phase 1's WalkNamed so phase 2 can run as in-memory loops instead of
// a second full-tree traversal. Tree-sitter *tree_sitter.Node values point at
// stack-allocated cursors during the recursive walk; every gathered node is
// cloned (shared.CloneNode) so the slices are valid after the walk returns.
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
	routeAttributes     []*tree_sitter.Node

	// Phase-2 gather slices: candidate nodes collected during phase 1's
	// WalkNamed and resolved in-memory after phase 1 completes, eliminating
	// the second full-tree WalkNamed traversal.
	gatheredVariableNames   []*tree_sitter.Node
	gatheredMemberCalls     []*tree_sitter.Node
	gatheredScopedCalls     []*tree_sitter.Node
	gatheredObjectCreations []*tree_sitter.Node
	gatheredFunctionCalls   []*tree_sitter.Node
}

// Parse extracts PHP declarations, imports, variables, and calls from the
// tree-sitter AST and emits the parser payload buckets.
func Parse(path string, isDependency bool, options shared.Options, parser *tree_sitter.Parser) (map[string]any, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}

	payload := shared.BasePayload(path, "php", isDependency)
	payload["traits"] = []map[string]any{}
	payload["interfaces"] = []map[string]any{}
	payload["php_parse_bounded"] = []map[string]any{}

	if len(source) > phpParseByteCap {
		recordPHPBoundedFile(payload, path, len(source))
		return payload, nil
	}

	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, parseError(path)
	}
	defer tree.Close()

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

	// Phase 1: collect declarations, imports, type evidence, dead-code facts,
	// and candidate route attributes so call and variable inference in phase 2
	// sees the whole file.
	state.parents = parents
	collectPHPDeclarations(state, root)

	// Phase 2: resolve gathered phase-2 nodes in-memory against the now-complete
	// type evidence instead of re-walking the full tree.
	for _, node := range state.gatheredVariableNames {
		emitPHPVariableName(state, node)
	}
	for _, node := range state.gatheredMemberCalls {
		emitPHPMemberCall(state, node)
	}
	for _, node := range state.gatheredScopedCalls {
		emitPHPScopedCall(state, node)
	}
	for _, node := range state.gatheredObjectCreations {
		emitPHPObjectCreation(state, node)
	}
	for _, node := range state.gatheredFunctionCalls {
		emitPHPFunctionCall(state, node)
	}

	// Route attributes recorded during phase 1 resolve against the now-complete
	// import set below (buildPHPFrameworkSemantics); no further tree walk.

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
	if semantics := buildPHPFrameworkSemantics(state.routeAttributes, source, state.payload); len(semantics["frameworks"].([]string)) > 0 {
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
// import rows and aliases, property and return-type evidence, dead-code
// facts, and candidate route attributes. Calls and free variables are emitted
// in a later pass. Route attributes are recorded here rather than walked
// again in a dedicated pass because this pass already visits every "attribute"
// node for observePHPAttribute; resolving the recorded candidates against
// exact Symfony Route import names happens after this walk completes (see
// buildPHPFrameworkSemantics), once phase 1 has collected every import in the
// file, so candidate order never depends on where in the file a "use"
// statement appears relative to the attribute that references it.
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
			state.routeAttributes = append(state.routeAttributes, shared.CloneNode(node))
		case "function_call_expression":
			observePHPWordPressHookCall(state, node)
			state.gatheredFunctionCalls = append(state.gatheredFunctionCalls, shared.CloneNode(node))
		case "array_creation_expression":
			collectPHPLiteralRouteTarget(state, node)
		// Gather phase-2 resolution-candidate nodes during phase 1's
		// WalkNamed so phase 2 can resolve them in-memory instead of
		// re-walking the full tree. Cloned with shared.CloneNode because
		// tree-sitter *tree_sitter.Node values point at stack-allocated
		// cursors during the recursive walk.
		case "variable_name":
			state.gatheredVariableNames = append(state.gatheredVariableNames, shared.CloneNode(node))
		case "member_call_expression", "nullsafe_member_call_expression":
			state.gatheredMemberCalls = append(state.gatheredMemberCalls, shared.CloneNode(node))
		case "scoped_call_expression":
			state.gatheredScopedCalls = append(state.gatheredScopedCalls, shared.CloneNode(node))
		case "object_creation_expression":
			state.gatheredObjectCreations = append(state.gatheredObjectCreations, shared.CloneNode(node))
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
