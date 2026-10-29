// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ruby

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/fingerprint"
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
	source, original, err := shared.ReadSourceWithOriginal(path)
	if err != nil {
		return nil, err
	}
	if payload, ok := parseBundlerPayload(path, source, isDependency); ok {
		if evidence, present := payload["ruby_runtime_evidence"].(RubyRuntimeEvidence); present {
			evidence.Digest = RubyRuntimeDigest(original)
			evidence.ByteSize = len(original)
			checkGemfileRuntimeSyntax(&evidence, path, source, parser)
			payload["ruby_runtime_evidence"] = evidence
		}
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
	payload[fingerprint.StatsKey] = syntax.fpStats.Map()
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

func checkGemfileRuntimeSyntax(evidence *RubyRuntimeEvidence, path string, source []byte, parser *tree_sitter.Parser) {
	if !strings.EqualFold(filepath.Base(path), bundlerGemfileName) || len(evidence.Values) == 0 || evidence.State == "over_cap" {
		return
	}
	if parser == nil {
		evidence.State = "unavailable"
		evidence.Values = nil
		return
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		evidence.State = "unavailable"
		evidence.Values = nil
		return
	}
	if tree.RootNode().HasError() {
		evidence.State = "unavailable"
		evidence.Values = nil
		tree.Close()
		return
	}
	// The Bundler line scanner records candidate literals, but it cannot tell
	// whether a `ruby` call is inside a method, class, or module. Only a direct
	// program child proves a repository-level directive.
	topLevelLines := make(map[int]struct{}, len(evidence.Values))
	root := tree.RootNode()
	cursor := root.Walk()
	for _, child := range root.NamedChildren(cursor) {
		child := child
		if child.Kind() != "call" || child.ChildByFieldName("receiver") != nil ||
			shared.NodeText(child.ChildByFieldName("method"), source) != "ruby" {
			continue
		}
		topLevelLines[shared.NodeLine(&child)] = struct{}{}
	}
	cursor.Close()
	kept := evidence.Values[:0]
	for _, value := range evidence.Values {
		if _, ok := topLevelLines[value.Line]; ok {
			kept = append(kept, value)
		}
	}
	evidence.Values = kept
	if len(kept) == 0 && evidence.Rejected == 0 {
		evidence.State = "parsed_empty"
	}
	tree.Close()
}
