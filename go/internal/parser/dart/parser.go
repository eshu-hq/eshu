// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dart

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	tree_sitter_dart "github.com/UserNobody14/tree-sitter-dart/bindings/go"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// Parse reads path and returns the legacy Dart parser payload.
func Parse(path string, isDependency bool, options shared.Options) (map[string]any, error) {
	parser := tree_sitter.NewParser()
	language := tree_sitter.NewLanguage(tree_sitter_dart.Language())
	if err := parser.SetLanguage(language); err != nil {
		parser.Close()
		return nil, fmt.Errorf("set parser language dart: %w", err)
	}
	defer parser.Close()

	return ParseWithParser(path, isDependency, options, parser)
}

// ParseWithParser extracts Dart declarations with a caller-owned tree-sitter parser.
func ParseWithParser(path string, isDependency bool, options shared.Options, parser *tree_sitter.Parser) (map[string]any, error) {
	source, syntax, err := dartSourceAndSyntax(path, parser)
	if err != nil {
		return nil, err
	}

	payload := shared.BasePayload(path, "dart", isDependency)
	publicLibraryPath := isPublicLibraryPath(path)
	seenVariables := make(map[string]struct{})
	seenCalls := make(map[string]struct{})

	for _, imported := range syntax.imports {
		item := map[string]any{
			"name":        imported.name,
			"line_number": imported.line,
			"lang":        "dart",
		}
		if imported.importType != "" {
			item["import_type"] = imported.importType
		}
		shared.AppendBucket(payload, "imports", item)
	}
	for _, typ := range syntax.types {
		item := map[string]any{
			"name":        typ.name,
			"line_number": typ.startLine,
			"end_line":    typ.endLine,
			"lang":        "dart",
		}
		addDartRootKind(item, dartClassRootKinds(typ.name, publicLibraryPath)...)
		shared.AppendBucket(payload, "classes", item)
	}
	for _, fn := range syntax.functions {
		item := map[string]any{
			"name":                  fn.name,
			"line_number":           fn.startLine,
			"end_line":              fn.endLine,
			"lang":                  "dart",
			"decorators":            []string{},
			"cyclomatic_complexity": fn.complexity,
		}
		if fn.classContext != "" {
			item["class_context"] = fn.classContext
		}
		if fn.isFactory {
			item["factory"] = true
		}
		if len(fn.decorators) > 0 {
			item["decorators"] = slices.Clone(fn.decorators)
		}
		addDartRootKind(item, dartFunctionRootKinds(
			fn.name,
			syntax.classScopeFor(fn.classContext),
			fn.decorators,
			publicLibraryPath,
		)...)
		if options.IndexSource {
			item["source"] = fn.source
		}
		shared.AppendBucket(payload, "functions", item)
	}
	for _, variable := range syntax.variables {
		if _, ok := seenVariables[variable.name]; ok {
			continue
		}
		seenVariables[variable.name] = struct{}{}
		shared.AppendBucket(payload, "variables", map[string]any{
			"name":        variable.name,
			"line_number": variable.line,
			"end_line":    variable.line,
			"lang":        "dart",
		})
	}
	appendDartCalls(payload, seenCalls, source)

	shared.SortNamedBucket(payload, "functions")
	shared.SortNamedBucket(payload, "classes")
	shared.SortNamedBucket(payload, "variables")
	shared.SortNamedBucket(payload, "imports")
	shared.SortNamedBucket(payload, "function_calls")
	return payload, nil
}

func (i dartSyntaxIndex) classScopeFor(name string) *classScope {
	if name == "" {
		return nil
	}
	for _, typ := range i.types {
		if typ.name == name {
			return &classScope{name: typ.name, extends: typ.extends}
		}
	}
	return &classScope{name: name}
}

type classScope struct {
	name    string
	extends string
}

func dartClassRootKinds(name string, publicLibraryPath bool) []string {
	if !publicLibraryPath || strings.HasPrefix(name, "_") {
		return nil
	}
	return []string{"dart.public_library_api"}
}

func dartFunctionRootKinds(
	name string,
	scope *classScope,
	decorators []string,
	publicLibraryPath bool,
) []string {
	var kinds []string
	if scope == nil {
		if name == "main" {
			kinds = append(kinds, "dart.main_function")
		}
		if publicLibraryPath && !strings.HasPrefix(name, "_") && name != "main" {
			kinds = append(kinds, "dart.public_library_api")
		}
		return kinds
	}
	if slices.Contains(decorators, "@override") {
		kinds = append(kinds, "dart.override_method")
	}
	if name == "build" && (scope.extends == "StatelessWidget" || strings.HasPrefix(scope.extends, "State<")) {
		kinds = append(kinds, "dart.flutter_widget_build")
	}
	if name == "createState" && scope.extends == "StatefulWidget" {
		kinds = append(kinds, "dart.flutter_create_state")
	}
	if name == scope.name || strings.HasPrefix(name, scope.name+".") {
		kinds = append(kinds, "dart.constructor")
	}
	if publicLibraryPath && !strings.HasPrefix(scope.name, "_") && !strings.HasPrefix(name, "_") {
		kinds = append(kinds, "dart.public_library_api")
	}
	return kinds
}

func addDartRootKind(item map[string]any, kinds ...string) {
	if len(kinds) == 0 {
		return
	}
	existing, _ := item["dead_code_root_kinds"].([]string)
	for _, kind := range kinds {
		if kind == "" || slices.Contains(existing, kind) {
			continue
		}
		existing = append(existing, kind)
	}
	if len(existing) > 0 {
		slices.Sort(existing)
		item["dead_code_root_kinds"] = existing
	}
}

func isPublicLibraryPath(path string) bool {
	slashed := filepath.ToSlash(path)
	return strings.Contains(slashed, "/lib/") && !strings.Contains(slashed, "/lib/src/")
}

// PreScan returns Dart function and class names used by repository pre-scan.
func PreScan(path string) ([]string, error) {
	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		return nil, err
	}
	names := shared.CollectBucketNames(payload, "functions", "classes")
	slices.Sort(names)
	return names, nil
}

// PreScanWithParser returns Dart declarations with a caller-owned tree-sitter parser.
func PreScanWithParser(path string, parser *tree_sitter.Parser) ([]string, error) {
	payload, err := ParseWithParser(path, false, shared.Options{}, parser)
	if err != nil {
		return nil, err
	}
	names := shared.CollectBucketNames(payload, "functions", "classes")
	slices.Sort(names)
	return names, nil
}
