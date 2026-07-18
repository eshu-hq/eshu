// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

// SourceCoverage records every production Run or RunSingle call in one query
// package source file.
type SourceCoverage struct {
	File  string          `yaml:"file"`
	Calls []QueryCallsite `yaml:"calls"`
}

// QueryCallsite records the enclosing production symbol, its exact number of
// graph query calls, and whether the call is a registered hot path.
type QueryCallsite struct {
	Symbol   string   `yaml:"symbol"`
	Count    int      `yaml:"count"`
	EntryIDs []string `yaml:"entry_ids,omitempty"`
	Reason   string   `yaml:"non_hot_reason,omitempty"`
}

// DiscoverQueryCallsites returns every direct Run or RunSingle selector call
// in non-test Go files directly beneath queryDir.
func DiscoverQueryCallsites(queryDir string) ([]SourceCoverage, error) {
	coverage := make([]SourceCoverage, 0)
	err := filepath.WalkDir(queryDir, func(path string, dirEntry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if dirEntry.IsDir() {
			name := dirEntry.Name()
			if path != queryDir && (name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		calls, err := discoverFileQueryCallsites(path)
		if err != nil {
			return err
		}
		if len(calls) == 0 {
			return nil
		}
		relative, err := filepath.Rel(queryDir, path)
		if err != nil {
			return fmt.Errorf("resolve query source path %s: %w", path, err)
		}
		coverage = append(coverage, SourceCoverage{File: filepath.ToSlash(relative), Calls: calls})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk query source directory: %w", err)
	}
	sort.Slice(coverage, func(i, j int) bool { return coverage[i].File < coverage[j].File })
	return coverage, nil
}

// ValidateSourceCoverage rejects newly added, removed, or rehomed production
// graph query calls and requires each callsite to have an explicit disposition.
func ValidateSourceCoverage(manifest Manifest, discovered []SourceCoverage) error {
	entryIDs := make(map[string]struct{}, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		entryIDs[entry.ID] = struct{}{}
	}

	expected, violations := flattenCoverage(manifest.SourceCoverage, true, entryIDs)
	actual, actualViolations := flattenCoverage(discovered, false, nil)
	violations = append(violations, actualViolations...)
	for key, callsite := range actual {
		expectedCallsite, ok := expected[key]
		if !ok {
			violations = append(violations, fmt.Sprintf(
				"unregistered query callsite %s (count %d)",
				key,
				callsite.Count,
			))
			continue
		}
		if callsite.Count != expectedCallsite.Count {
			violations = append(violations, fmt.Sprintf(
				"%s: discovered call count %d, manifest requires %d",
				key,
				callsite.Count,
				expectedCallsite.Count,
			))
		}
	}
	for key := range expected {
		if _, ok := actual[key]; !ok {
			violations = append(violations, fmt.Sprintf("stale query callsite registration %s", key))
		}
	}
	if len(violations) > 0 {
		sort.Strings(violations)
		return errors.New(strings.Join(violations, "; "))
	}
	return nil
}

func discoverFileQueryCallsites(path string) ([]QueryCallsite, error) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, path, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("parse query source %s: %w", path, err)
	}
	counts := make(map[string]int)
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok {
			counts["<package-init>"] += countQueryCalls(declaration)
			continue
		}
		if function.Body == nil {
			continue
		}
		counts[functionSymbol(function)] += countQueryCalls(function.Body)
	}
	result := make([]QueryCallsite, 0, len(counts))
	for symbol, count := range counts {
		if count == 0 {
			continue
		}
		result = append(result, QueryCallsite{Symbol: symbol, Count: count})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Symbol < result[j].Symbol })
	return result, nil
}

func countQueryCalls(node ast.Node) int {
	count := 0
	ast.Inspect(node, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || (selector.Sel.Name != "Run" && selector.Sel.Name != "RunSingle") {
			return true
		}
		count++
		return true
	})
	return count
}

func functionSymbol(function *ast.FuncDecl) string {
	if function.Recv == nil || len(function.Recv.List) == 0 {
		return function.Name.Name
	}
	receiver := function.Recv.List[0].Type
	return "(" + receiverName(receiver) + ")." + function.Name.Name
}

func receiverName(expression ast.Expr) string {
	switch value := expression.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.StarExpr:
		return "*" + receiverName(value.X)
	case *ast.IndexExpr:
		return receiverName(value.X)
	case *ast.IndexListExpr:
		return receiverName(value.X)
	default:
		return "unknown"
	}
}

func flattenCoverage(
	coverage []SourceCoverage,
	validateDisposition bool,
	entryIDs map[string]struct{},
) (map[string]QueryCallsite, []string) {
	flattened := make(map[string]QueryCallsite)
	var violations []string
	for _, source := range coverage {
		if strings.TrimSpace(source.File) == "" {
			violations = append(violations, "query source coverage missing file")
			continue
		}
		for _, callsite := range source.Calls {
			key := source.File + ":" + callsite.Symbol
			if strings.TrimSpace(callsite.Symbol) == "" || callsite.Count <= 0 {
				violations = append(violations, fmt.Sprintf("%s: invalid symbol or call count", key))
				continue
			}
			if _, duplicate := flattened[key]; duplicate {
				violations = append(violations, fmt.Sprintf("duplicate query callsite registration %s", key))
				continue
			}
			flattened[key] = callsite
			if !validateDisposition {
				continue
			}
			if len(callsite.EntryIDs) == 0 && strings.TrimSpace(callsite.Reason) == "" {
				violations = append(violations, fmt.Sprintf("%s: requires entry_ids or a non-hot reason", key))
			}
			if len(callsite.EntryIDs) > 0 && strings.TrimSpace(callsite.Reason) != "" {
				violations = append(violations, fmt.Sprintf("%s: cannot declare both entry_ids and a non-hot reason", key))
			}
			for _, entryID := range callsite.EntryIDs {
				if _, ok := entryIDs[entryID]; !ok {
					violations = append(violations, fmt.Sprintf("%s: unknown hot path %s", key, entryID))
				}
			}
		}
	}
	return flattened, violations
}
