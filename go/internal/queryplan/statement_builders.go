// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"crypto/sha256"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// StatementBuilderCoverage records every production sourcecypher.Statement
// construction site in one source file.
type StatementBuilderCoverage struct {
	File     string             `yaml:"file"`
	Builders []StatementBuilder `yaml:"builders"`
}

// StatementVariant is the statically known text of one built statement:
// the whitespace-normalized Cypher template when the text is static, or
// the ordered static fragments the runtime text is composed from when it
// is dynamic (label-interpolated, Sprintf-composed). A recording matches
// a template by equality and fragments by ordered containment. A variant
// with neither matches nothing: user-supplied or fully computed text is
// unattributable by construction.
type StatementVariant struct {
	Template  string   `yaml:"template,omitempty"`
	Fragments []string `yaml:"fragments,omitempty"`
}

// StatementBuilder records one enclosing production symbol that builds
// sourcecypher.Statements: how many literals it builds, the Operation
// expression text (empty when the operation is computed, not a literal or
// package-qualified name), and one match variant per literal in source
// order. A symbol whose literals build different texts needs every
// variant: matching only the first would prove one statement and silently
// assume the rest.
type StatementBuilder struct {
	Symbol       string             `yaml:"symbol"`
	Count        int                `yaml:"count"`
	Operation    string             `yaml:"operation,omitempty"`
	Variants     []StatementVariant `yaml:"variants"`
	SourceDigest string             `yaml:"source_sha256,omitempty"`
	// Exempt excuses execution proof with a reason when the builder's
	// text is statically unknowable. It never excuses drift: variants
	// and digest still pin the symbol. Discovery leaves it empty; only
	// the manifest sets it.
	Exempt string `yaml:"exempt,omitempty"`
}

// DiscoverStatementBuilders returns every sourcecypher.Statement composite
// literal in non-test Go files recursively beneath sourceDir, grouped by
// file and enclosing symbol. Testdata plus hidden and underscore-prefixed
// directories are excluded; no other directory is.
//
// The walk fails rather than under-reporting. A symbol that only forwards
// a statement it received (decorators, adapters) still builds no literal
// and is therefore invisible here by construction: only composite literals
// count, never pass-through parameters.
func DiscoverStatementBuilders(sourceDir string) ([]StatementBuilderCoverage, error) {
	coverage := make([]StatementBuilderCoverage, 0)
	resolver := newConstResolver(sourceDir)
	err := filepath.WalkDir(sourceDir, func(path string, dirEntry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if dirEntry.IsDir() {
			name := dirEntry.Name()
			if path != sourceDir && (name == "testdata" ||
				strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		builders, err := discoverFileStatementBuilders(path, resolver)
		if err != nil {
			return err
		}
		if len(builders) == 0 {
			return nil
		}
		relative, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return fmt.Errorf("resolve statement builder source path %s: %w", path, err)
		}
		coverage = append(coverage, StatementBuilderCoverage{File: filepath.ToSlash(relative), Builders: builders})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk statement builder source directory: %w", err)
	}
	sort.Slice(coverage, func(i, j int) bool { return coverage[i].File < coverage[j].File })
	return coverage, nil
}

func discoverFileStatementBuilders(path string, resolver *constResolver) ([]StatementBuilder, error) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, path, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("parse statement builder source %s: %w", path, err)
	}
	source, err := os.ReadFile(path) // #nosec G304 -- path is discovered beneath the caller-provided source directory
	if err != nil {
		return nil, fmt.Errorf("read statement builder source %s: %w", path, err)
	}
	type site struct {
		operation string
		variants  []StatementVariant
	}
	counts := make(map[string]int)
	details := make(map[string]site)
	digests := make(map[string]string)
	fileDir := filepath.Dir(path)
	imports := fileImports(file)
	record := func(symbol string, literal *ast.CompositeLit, scope *cypherScope) {
		counts[symbol]++
		operation, template, fragments := describeStatementLiteral(literal, scope)
		detail := details[symbol]
		if detail.operation == "" {
			detail.operation = operation
		}
		detail.variants = append(detail.variants, StatementVariant{Template: template, Fragments: fragments})
		details[symbol] = detail
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil {
			continue
		}
		symbol := functionSymbol(function)
		start := fileSet.Position(function.Pos()).Offset
		end := fileSet.Position(function.End()).Offset
		if start < 0 || end < start || end > len(source) {
			return nil, fmt.Errorf("invalid source offsets for %s", symbol)
		}
		assigns := collectAssignments(function.Body)
		ast.Inspect(function.Body, func(node ast.Node) bool {
			literal, ok := node.(*ast.CompositeLit)
			if !ok {
				return true
			}
			scope := &cypherScope{
				resolver: resolver,
				fileDir:  fileDir,
				imports:  imports,
				funcVars: visibleVars(assigns, node.Pos()),
			}
			if isSourceCypherStatement(literal.Type) {
				record(symbol, literal, scope)
				return true
			}
			// Elements of a []sourcecypher.Statement slice literal carry
			// no explicit type; they inherit it from the slice.
			if isSourceCypherStatementSlice(literal.Type) {
				for _, element := range literal.Elts {
					if item, ok := element.(*ast.CompositeLit); ok && item.Type == nil {
						record(symbol, item, scope)
					}
				}
			}
			return true
		})
		if _, seen := counts[symbol]; seen {
			digests[symbol] = fmt.Sprintf("%x", sha256.Sum256(source[start:end]))
		}
	}
	result := make([]StatementBuilder, 0, len(counts))
	for symbol, count := range counts {
		if count == 0 {
			continue
		}
		detail := details[symbol]
		result = append(result, StatementBuilder{
			Symbol:       symbol,
			Count:        count,
			Operation:    detail.operation,
			Variants:     detail.variants,
			SourceDigest: digests[symbol],
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Symbol < result[j].Symbol })
	return result, nil
}

// isSourceCypherStatement reports whether the composite literal type is a
// sourcecypher.Statement: a selector on the sourcecypher package name.
// Any other Statement type (sql, test fakes) does not match, and a
// rebound sourcecypher alias would surface in review of the manifest.
func isSourceCypherStatement(expr ast.Expr) bool {
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "Statement" {
		return false
	}
	ident, ok := selector.X.(*ast.Ident)
	return ok && ident.Name == "sourcecypher"
}

// isSourceCypherStatementSlice reports whether the type is a slice of
// sourcecypher.Statement in any of its spellings.
func isSourceCypherStatementSlice(expr ast.Expr) bool {
	array, ok := expr.(*ast.ArrayType)
	if !ok {
		return false
	}
	return isSourceCypherStatement(array.Elt)
}

// describeStatementLiteral extracts what is statically known about one
// sourcecypher.Statement literal: the Operation expression text when it is
// a literal or package-qualified name (empty otherwise), and the
// whitespace-normalized Cypher template when the Cypher field resolves to
// static text (fragments of the composed expression otherwise). Text
// resolution lives in statement_text.go; this stays the per-literal
// orchestrator.
func describeStatementLiteral(literal *ast.CompositeLit, scope *cypherScope) (operation, template string, fragments []string) {
	for _, element := range literal.Elts {
		pair, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := pair.Key.(*ast.Ident)
		if !ok {
			continue
		}
		switch key.Name {
		case "Operation":
			operation = staticExpressionText(pair.Value)
		case "Cypher":
			if text, ok := scope.templateText(pair.Value, 0); ok {
				template = normalizeStatementTemplate(text)
			} else {
				fragments = scope.textFragments(pair.Value, 0)
			}
		}
	}
	return operation, template, fragments
}
