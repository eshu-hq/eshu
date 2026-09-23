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
	"strconv"
	"strings"
)

// StatementBuilderCoverage records every production sourcecypher.Statement
// construction site in one source file.
type StatementBuilderCoverage struct {
	File     string             `yaml:"file"`
	Builders []StatementBuilder `yaml:"builders"`
}

// StatementBuilder records one enclosing production symbol that builds a
// sourcecypher.Statement, how many it builds, and what is statically known
// about the built statement: the Operation expression text (empty when the
// operation is computed, not a literal or package-qualified name) and the
// whitespace-normalized Cypher template (empty with Dynamic set when the
// Cypher text is composed at runtime, e.g. label-interpolated).
type StatementBuilder struct {
	Symbol       string `yaml:"symbol"`
	Count        int    `yaml:"count"`
	Operation    string `yaml:"operation,omitempty"`
	Template     string `yaml:"template,omitempty"`
	Dynamic      bool   `yaml:"dynamic,omitempty"`
	SourceDigest string `yaml:"source_sha256,omitempty"`
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
		builders, err := discoverFileStatementBuilders(path)
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

func discoverFileStatementBuilders(path string) ([]StatementBuilder, error) {
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
		template  string
		dynamic   bool
	}
	counts := make(map[string]int)
	details := make(map[string]site)
	digests := make(map[string]string)
	record := func(symbol string, literal *ast.CompositeLit, fileSet *token.FileSet, source []byte) {
		counts[symbol]++
		if _, seen := details[symbol]; seen {
			return
		}
		operation, template, dynamic := describeStatementLiteral(literal)
		details[symbol] = site{operation: operation, template: template, dynamic: dynamic}
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
		ast.Inspect(function.Body, func(node ast.Node) bool {
			literal, ok := node.(*ast.CompositeLit)
			if !ok {
				return true
			}
			if isSourceCypherStatement(literal.Type) {
				record(symbol, literal, fileSet, source)
				return true
			}
			// Elements of a []sourcecypher.Statement slice literal carry
			// no explicit type; they inherit it from the slice.
			if isSourceCypherStatementSlice(literal.Type) {
				for _, element := range literal.Elts {
					if item, ok := element.(*ast.CompositeLit); ok && item.Type == nil {
						record(symbol, item, fileSet, source)
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
			Template:     detail.template,
			Dynamic:      detail.dynamic,
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
// whitespace-normalized Cypher template when the Cypher field is a string
// literal (dynamic otherwise, e.g. label-interpolated composition).
func describeStatementLiteral(literal *ast.CompositeLit) (operation, template string, dynamic bool) {
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
			if text, ok := staticString(pair.Value); ok {
				template = normalizeStatementTemplate(text)
			} else {
				dynamic = true
			}
		}
	}
	return operation, template, dynamic
}

// staticExpressionText renders short static operation expressions: string
// literals and package-qualified names. Anything computed (calls,
// variables, concatenation) renders empty so the manifest marks the
// operation unknown instead of recording a guess.
func staticExpressionText(expr ast.Expr) string {
	switch value := expr.(type) {
	case *ast.BasicLit:
		if value.Kind == token.STRING {
			if text, err := strconv.Unquote(value.Value); err == nil {
				return text
			}
		}
		return ""
	case *ast.SelectorExpr:
		ident, ok := value.X.(*ast.Ident)
		if !ok {
			return ""
		}
		return ident.Name + "." + value.Sel.Name
	default:
		return ""
	}
}

// staticString reports whether the expression is a static string: a plain
// literal or a concatenation of plain literals. Anything else (calls,
// variables, formatting) is dynamic.
func staticString(expr ast.Expr) (string, bool) {
	switch value := expr.(type) {
	case *ast.BasicLit:
		if value.Kind != token.STRING {
			return "", false
		}
		text, err := strconv.Unquote(value.Value)
		if err != nil {
			return "", false
		}
		return text, true
	case *ast.BinaryExpr:
		if value.Op != token.ADD {
			return "", false
		}
		left, ok := staticString(value.X)
		if !ok {
			return "", false
		}
		right, ok := staticString(value.Y)
		if !ok {
			return "", false
		}
		return left + right, true
	default:
		return "", false
	}
}

// normalizeStatementTemplate collapses whitespace runs so template
// comparison ignores formatting. Case, comments, and literals are
// preserved: the template identifies the statement shape, not its data.
func normalizeStatementTemplate(cypher string) string {
	return strings.Join(strings.Fields(cypher), " ")
}
