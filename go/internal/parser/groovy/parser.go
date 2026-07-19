// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package groovy

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	tree_sitter_groovy "github.com/dekobon/tree-sitter-groovy/bindings/go"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// Parse builds the parent parser payload for a Groovy or Jenkinsfile source.
func Parse(path string, isDependency bool, options shared.Options) (map[string]any, error) {
	parser := tree_sitter.NewParser()
	language := tree_sitter.NewLanguage(tree_sitter_groovy.Language())
	if err := parser.SetLanguage(language); err != nil {
		parser.Close()
		return nil, fmt.Errorf("set parser language groovy: %w", err)
	}
	defer parser.Close()

	return ParseWithParser(path, isDependency, options, parser)
}

// ParseWithParser builds the Groovy payload with a caller-owned tree-sitter
// parser.
func ParseWithParser(path string, isDependency bool, options shared.Options, parser *tree_sitter.Parser) (map[string]any, error) {
	sourceBytes, syntax, err := groovySourceAndSyntax(path, parser)
	if err != nil {
		return nil, err
	}

	sourceText := string(sourceBytes)
	payload := shared.BasePayload(path, "groovy", isDependency)
	payload["modules"] = []map[string]any{}
	payload["module_inclusions"] = []map[string]any{}

	// PipelineMetadata is Jenkins/Groovy delivery evidence: it must only be
	// extracted for an actual Jenkins artifact (a Jenkinsfile, or a shared
	// library vars/*.groovy step), never for an arbitrary .groovy source
	// file. Without this gate, a plain class whose method happens to be
	// named e.g. pipelineDeploy would fabricate pipeline_calls evidence
	// that the reducer's isJenkinsArtifact and query deployment surfaces
	// then treat as real Jenkins pipeline truth.
	if isJenkinsfile(path) || isSharedLibraryVarsFile(path) {
		for key, value := range PipelineMetadata(sourceText).Map() {
			payload[key] = value
		}
	}
	for _, class := range syntax.classes {
		shared.AppendBucket(payload, "classes", class)
	}
	for _, function := range syntax.functions {
		if isSharedLibraryVarsFile(path) && function["name"] == "call" {
			function["framework"] = "jenkins"
			function["dead_code_root_kinds"] = []string{groovyRootSharedLibraryCall}
		}
		shared.AppendBucket(payload, "functions", function)
	}
	if isJenkinsfile(path) && groovyJenkinsEntrypointPattern.MatchString(sourceText) && !hasGroovyRoot(syntax.functions, groovyRootJenkinsPipelineEntrypoint) {
		shared.AppendBucket(payload, "functions", map[string]any{
			"name":                 "Jenkinsfile",
			"line_number":          firstGroovyJenkinsEntrypointLine(sourceText),
			"end_line":             firstGroovyJenkinsEntrypointLine(sourceText),
			"framework":            "jenkins",
			"dead_code_root_kinds": []string{groovyRootJenkinsPipelineEntrypoint},
		})
	}
	for _, call := range syntax.calls {
		shared.AppendBucket(payload, "function_calls", call)
	}
	for _, imported := range syntax.imports {
		shared.AppendBucket(payload, "imports", imported)
	}
	shared.SortNamedBucket(payload, "classes")
	shared.SortNamedBucket(payload, "functions")
	shared.SortNamedBucket(payload, "function_calls")
	shared.SortNamedBucket(payload, "imports")
	if options.IndexSource {
		payload["source"] = sourceText
	}
	return payload, nil
}

// PreScan returns deterministic Groovy metadata names for repository pre-scan
// import maps.
func PreScan(path string) ([]string, error) {
	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		return nil, err
	}
	return preScanFromPayload(payload), nil
}

// PreScanWithParser returns deterministic Groovy metadata names with a
// caller-owned tree-sitter parser.
func PreScanWithParser(path string, parser *tree_sitter.Parser) ([]string, error) {
	payload, err := ParseWithParser(path, false, shared.Options{}, parser)
	if err != nil {
		return nil, err
	}
	return preScanFromPayload(payload), nil
}

func preScanFromPayload(payload map[string]any) []string {
	names := make([]string, 0)
	for _, key := range []string{"shared_libraries", "pipeline_calls", "entry_points"} {
		values, _ := payload[key].([]string)
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			if slices.Contains(names, value) {
				continue
			}
			names = append(names, value)
		}
	}
	slices.Sort(names)
	return names
}

func isSharedLibraryVarsFile(path string) bool {
	normalized := filepath.ToSlash(filepath.Clean(path))
	return (strings.HasPrefix(normalized, "vars/") || strings.Contains(normalized, "/vars/")) &&
		strings.HasSuffix(strings.ToLower(normalized), ".groovy")
}
