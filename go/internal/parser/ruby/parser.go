// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ruby

import (
	"fmt"
	"slices"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_ruby "github.com/tree-sitter/tree-sitter-ruby/bindings/go"
)

// Parse reads path and returns the Ruby parser payload built from the
// tree-sitter AST. Gemfile and lockfile manifests are handled by the bundler
// parser; all other Ruby sources flow through the AST extraction path.
func Parse(path string, isDependency bool, options shared.Options) (map[string]any, error) {
	parser := tree_sitter.NewParser()
	language := tree_sitter.NewLanguage(tree_sitter_ruby.Language())
	if err := parser.SetLanguage(language); err != nil {
		parser.Close()
		return nil, fmt.Errorf("set parser language ruby: %w", err)
	}
	defer parser.Close()

	return ParseWithParser(path, isDependency, options, parser)
}

// ParseWithParser builds the Ruby payload with a caller-owned tree-sitter
// parser so the runtime can reuse cached language handles.
func ParseWithParser(path string, isDependency bool, options shared.Options, parser *tree_sitter.Parser) (map[string]any, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}
	if payload, ok := parseBundlerPayload(path, source, isDependency); ok {
		return payload, nil
	}

	payload := shared.BasePayload(path, "ruby", isDependency)
	payload["modules"] = []map[string]any{}
	payload["module_inclusions"] = []map[string]any{}
	payload["framework_semantics"] = map[string]any{"frameworks": []string{}}

	if parser == nil {
		return nil, fmt.Errorf("parse ruby tree: nil parser")
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, fmt.Errorf("parse ruby tree: parser returned nil tree")
	}
	defer tree.Close()

	syntax := rubyBuildSyntax(source, tree, options)
	for _, item := range syntax.modules {
		shared.AppendBucket(payload, "modules", item)
	}
	for _, item := range syntax.classes {
		shared.AppendBucket(payload, "classes", item)
	}
	for _, item := range syntax.functions {
		shared.AppendBucket(payload, "functions", item)
	}
	for _, item := range syntax.variables {
		shared.AppendBucket(payload, "variables", item)
	}
	for _, item := range syntax.imports {
		shared.AppendBucket(payload, "imports", item)
	}
	for _, item := range syntax.inclusions {
		shared.AppendBucket(payload, "module_inclusions", item)
	}

	appendRubyCalls(payload, syntax)
	deadCodeNames, routesByFramework, railsRouteAmbiguous := rubyCollectSemantics(syntax)
	annotateRubyDeadCodeRoots(payload, deadCodeNames)
	payload["framework_semantics"] = buildRubyFrameworkSemantics(routesByFramework, railsRouteAmbiguous)

	shared.SortNamedBucket(payload, "functions")
	shared.SortNamedBucket(payload, "classes")
	shared.SortNamedBucket(payload, "modules")
	shared.SortNamedBucket(payload, "variables")
	shared.SortNamedBucket(payload, "imports")
	shared.SortNamedBucket(payload, "function_calls")
	return payload, nil
}

// PreScan returns Ruby function, class, and module names used by repository
// pre-scan.
func PreScan(path string) ([]string, error) {
	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		return nil, err
	}
	names := shared.CollectBucketNames(payload, "functions", "classes", "modules")
	slices.Sort(names)
	return names, nil
}

// PreScanWithParser returns Ruby pre-scan names with a caller-owned parser.
func PreScanWithParser(path string, parser *tree_sitter.Parser) ([]string, error) {
	payload, err := ParseWithParser(path, false, shared.Options{}, parser)
	if err != nil {
		return nil, err
	}
	names := shared.CollectBucketNames(payload, "functions", "classes", "modules")
	slices.Sort(names)
	return names, nil
}

// appendRubyCalls drains the function-call rows the AST walk recorded on syntax
// into the payload bucket. Each row already carries its full name, line, and the
// enclosing definition, module, or class context resolved during the walk.
func appendRubyCalls(payload map[string]any, syntax *rubySyntax) {
	for _, item := range syntax.calls {
		shared.AppendBucket(payload, "function_calls", item)
	}
}
