// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package anchor

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The static sweep reads Cypher out of Go source. What it admits, by pattern:
//
//   - a write keyword (MERGE, CREATE, SET),
//   - and an id token or a dynamic map write (`+=`) or a parameter map,
//   - and a node pattern: labeled (`(n:Label`), a template label (`(n:%s`),
//     or unlabeled with an id key in its map (`(n {id: ...})`).
//
// Postgres SQL (SET col = value) has none of the node patterns and stays out.
var (
	writeShape        = regexp.MustCompile(`(?is)\b(MERGE|CREATE|SET)\b.*(\bid\b|\+=)`)
	labeledShape      = regexp.MustCompile(`\(\s*\w*\s*:\s*[A-Za-z_` + "`" + `]`)
	unlabeledIDShape  = regexp.MustCompile(`\(\s*\w*\s*\{[^{}]*\bid\s*:`)
	templateLabelNode = regexp.MustCompile(`\(\s*\w*\s*:\s*%(\[\d+\])?[sdvq]`)
	templateWriteNode = regexp.MustCompile(`(?is)\b(MERGE|CREATE)\b.*\(\s*\w*\s*:\s*%(\[\d+\])?[sdvq]`)
	// parameterMapNode finds a node whose property map is a bound parameter:
	// (n $props) and (n:Label $props), a map the text cannot read.
	parameterMapNode = regexp.MustCompile(`(?is)\b(MERGE|CREATE)\s*\(\s*\w*\s*(:[^{)$]*)?\s\$\w+\s*\)`)
	// schemaStatement keeps DDL (CREATE CONSTRAINT/INDEX ... FOR (n:%s)) out of
	// the sweep: it writes no node.
	schemaStatement = regexp.MustCompile(`(?i)\b(CREATE|DROP)\s+(OR\s+REPLACE\s+)?(CONSTRAINT|INDEX|FULLTEXT|RANGE|TEXT|POINT|VECTOR|LOOKUP)\b`)
)

// placeholder stands for a non-literal operand of a string concatenation, in
// the same form a fmt verb takes, so one rule covers both templates.
const placeholder = "%s"

// cypherSite is one admitted Cypher literal.
type cypherSite struct {
	// Key is "relative/path.go:line".
	Key  string
	File string
	Text string
	// Line is the literal's first line in File.
	Line int
	// DynamicLabel is true when a node pattern's label is a placeholder.
	DynamicLabel bool
}

// walkGoFiles visits the Go files under root: the _test.go files when tests is
// true, the others otherwise. It skips testdata, vendor, and build-cache
// directories.
func walkGoFiles(t *testing.T, root string, tests bool, visit func(rel, path string)) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "testdata", "vendor", "node_modules", ".gocache":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") != tests {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		visit(rel, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
}

// packageConsts returns, per directory, the string constants its non-test files
// declare, with constants built from other constants resolved. A named
// constant in a `+` chain then reads as its value, not as a placeholder.
func packageConsts(t *testing.T, root string) map[string]map[string]string {
	t.Helper()
	out := make(map[string]map[string]string)
	type decl struct {
		dir, name string
		expr      ast.Expr
	}
	var decls []decl
	walkGoFiles(t, root, false, func(rel, path string) {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		dir := filepath.Dir(rel)
		for _, d := range file.Decls {
			gen, ok := d.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, name := range vs.Names {
					if i < len(vs.Values) {
						decls = append(decls, decl{dir: dir, name: name.Name, expr: vs.Values[i]})
					}
				}
			}
		}
	})
	for pass := 0; pass < 4; pass++ {
		for _, d := range decls {
			if out[d.dir] == nil {
				out[d.dir] = make(map[string]string)
			}
			if _, done := out[d.dir][d.name]; done {
				continue
			}
			if value, ok := foldWithPlaceholders(d.expr, out[d.dir]); ok && !strings.Contains(value, placeholder) {
				out[d.dir][d.name] = value
			}
		}
	}
	return out
}

// productionCypherSites returns every admitted Cypher literal in the non-test
// Go files under root. A `+` chain of literals, named constants, and other
// operands folds to one template whose unresolved operands read as a
// placeholder.
func productionCypherSites(t *testing.T, root string) []cypherSite {
	t.Helper()
	consts := packageConsts(t, root)
	var out []cypherSite
	walkGoFiles(t, root, false, func(rel, path string) {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			expr, ok := node.(ast.Expr)
			if !ok {
				return true
			}
			if _, bare := expr.(*ast.Ident); bare {
				// A bare name is a declaration or a use of a constant the
				// declaration already supplied; only a `+` chain folds names.
				return true
			}
			text, hasLiteral := foldWithPlaceholders(expr, consts[filepath.Dir(rel)])
			if !hasLiteral {
				return true
			}
			if schemaStatement.MatchString(text) {
				return false
			}
			dynamic := templateLabelNode.MatchString(text)
			admitted := dynamic && templateWriteNode.MatchString(text) ||
				writeShape.MatchString(text) && (labeledShape.MatchString(text) || unlabeledIDShape.MatchString(text)) ||
				parameterMapNode.MatchString(text)
			if admitted {
				line := fset.Position(expr.Pos()).Line
				out = append(out, cypherSite{
					Key: rel + ":" + strconv.Itoa(line), File: rel, Line: line,
					Text: text, DynamicLabel: dynamic,
				})
			}
			return false
		})
	})
	return out
}

// foldWithPlaceholders returns the value of a string literal, of a named
// constant in consts, or of a `+` chain of them that holds at least one literal
// or constant. Any other operand of the chain reads as a placeholder.
// hasLiteral is false for anything else, including arithmetic on non-strings.
func foldWithPlaceholders(expr ast.Expr, consts map[string]string) (string, bool) {
	switch typed := expr.(type) {
	case *ast.BasicLit:
		if typed.Kind != token.STRING {
			return placeholder, false
		}
		value, err := strconv.Unquote(typed.Value)
		return value, err == nil
	case *ast.Ident:
		if value, ok := consts[typed.Name]; ok {
			return value, true
		}
		return placeholder, false
	case *ast.ParenExpr:
		return foldWithPlaceholders(typed.X, consts)
	case *ast.BinaryExpr:
		if typed.Op != token.ADD {
			return placeholder, false
		}
		left, lok := foldWithPlaceholders(typed.X, consts)
		right, rok := foldWithPlaceholders(typed.Y, consts)
		return left + right, lok || rok
	}
	return placeholder, false
}
