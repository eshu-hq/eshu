// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package swift

import (
	"slices"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// swiftSemanticFacts holds the same-file conformance, protocol-method, and Vapor
// route evidence used to classify dead-code root kinds. It is derived from the
// tree-sitter AST so the classification does not depend on line-scan heuristics.
type swiftSemanticFacts struct {
	protocolMethods    map[string]map[string]struct{}
	typeConformances   map[string]map[string]struct{}
	vaporRouteHandlers map[string]struct{}
	vaporRouteEntries  []map[string]string
	// hasVaporImport reports whether the file carries `import Vapor`. It gates
	// the swift.vapor_route_handler dead-code root (see
	// swiftFunctionDeadCodeRootKinds) the same way it already gates
	// vaporRouteEntries: a `use:` labeled call argument is real Vapor route
	// evidence only when the file actually imports Vapor, not merely because
	// some call anywhere in the file happens to use a `use:` argument label.
	hasVaporImport bool
}

// Parse extracts Swift imports, types, functions, variables, and calls.
func Parse(path string, isDependency bool, options shared.Options, parser *tree_sitter.Parser) (map[string]any, error) {
	source, tree, err := swiftSourceAndTree(path, parser)
	if err != nil {
		return nil, err
	}
	defer tree.Close()

	root := tree.RootNode()
	payload := shared.BasePayload(path, "swift", isDependency)
	payload["structs"] = []map[string]any{}
	payload["enums"] = []map[string]any{}
	payload["protocols"] = []map[string]any{}

	facts := collectSwiftSemanticFacts(root, source)
	if semantics := swiftFrameworkSemantics(facts); semantics != nil {
		payload["framework_semantics"] = semantics
	}
	extractor := newSwiftExtractor(payload, source, isDependency, options, facts)
	extractor.extract(root)

	for _, bucket := range []string{"functions", "classes", "structs", "enums", "protocols", "variables", "imports", "function_calls"} {
		shared.SortNamedBucket(payload, bucket)
	}

	return payload, nil
}

func swiftFrameworkSemantics(facts swiftSemanticFacts) map[string]any {
	if len(facts.vaporRouteEntries) == 0 {
		return nil
	}
	return map[string]any{
		"frameworks": []string{"vapor"},
		"vapor": map[string]any{
			"route_entries": facts.vaporRouteEntries,
		},
	}
}

// PreScan returns Swift names used by the collector import-map pre-scan.
func PreScan(path string, parser *tree_sitter.Parser) ([]string, error) {
	payload, err := Parse(path, false, shared.Options{}, parser)
	if err != nil {
		return nil, err
	}
	names := shared.CollectBucketNames(payload, "functions", "classes", "structs", "enums", "protocols")
	slices.Sort(names)
	return names, nil
}
