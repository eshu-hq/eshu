// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package anchor

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// The static sweep reads Cypher out of Go source. What it admits, by pattern:
//
//   - a write keyword (MERGE, CREATE, SET),
//   - and an id token or a dynamic map write (`+=`) or a parameter map,
//   - and a node pattern: labeled (`(n:Label` or `(n IS Label`), a template
//     label (`(n:%s`), or unlabeled with an id key in its map (`(n {id: ...})`),
//   - or a REMOVE of a label beside a labeled node pattern.
//
// Postgres SQL (SET col = value) has none of the node patterns and stays out.
var (
	writeShape   = regexp.MustCompile(`(?is)\b(MERGE|CREATE|SET)\b.*(\bid\b|\+=)`)
	labeledShape = regexp.MustCompile(`\(\s*\w*\s*(?::\s*[A-Za-z_` + "`" + `]|\s+IS\s+[A-Z][A-Za-z0-9_]*[a-z0-9_])`)
	// labelRemoval finds REMOVE n:Label, which can strand an id on a node with no
	// anchor label even when the statement writes no id itself.
	labelRemoval      = regexp.MustCompile(`(?is)\bREMOVE\s+\w+\s*:\s*[A-Za-z_` + "`" + `]`)
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

type goFileEntry struct {
	rel  string
	path string
}

type parsedGoFile struct {
	rel  string
	dir  string
	fset *token.FileSet
	file *ast.File
	err  error
}

// collectGoFiles lists the Go files under root in lexical walk order:
// _test.go files when tests is true, non-test Go files otherwise. It skips
// testdata, vendor, node_modules, and build-cache directories.
func collectGoFiles(root string, tests bool) ([]goFileEntry, error) {
	var files []goFileEntry
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
		files = append(files, goFileEntry{rel: rel, path: path})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", root, err)
	}
	return files, nil
}

// runFileWorkers executes fn for every index in [0, n) across bounded
// goroutines. Callers store per-index results without shared locks.
func runFileWorkers(n int, fn func(idx int)) {
	if n <= 0 {
		return
	}
	workers := runtime.GOMAXPROCS(0)
	if workers < 1 {
		workers = 1
	}
	if workers > n {
		workers = n
	}
	var next atomic.Int64
	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for {
				idx := int(next.Add(1) - 1)
				if idx >= n {
					return
				}
				fn(idx)
			}
		}()
	}
	wg.Wait()
}

// resolvePackageConsts returns, per directory, the string constants its parsed
// non-test files declare, with constants built from other constants resolved.
// A named constant in a `+` chain then reads as its value, not as a placeholder.
func resolvePackageConsts(parsed []parsedGoFile) map[string]map[string]string {
	out := make(map[string]map[string]string)
	type decl struct {
		dir, name string
		expr      ast.Expr
	}
	var decls []decl
	for i := range parsed {
		pf := &parsed[i]
		for _, d := range pf.file.Decls {
			gen, ok := d.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vs := spec.(*ast.ValueSpec)
				for j, name := range vs.Names {
					if j < len(vs.Values) {
						decls = append(decls, decl{dir: pf.dir, name: name.Name, expr: vs.Values[j]})
					}
				}
			}
		}
	}
	for range 4 {
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

// productionCypherSitesErr parses every non-test Go file under root once in
// parallel and returns every admitted Cypher literal in deterministic order.
// A `+` chain of literals, named constants, and other operands folds to one
// template whose unresolved operands read as a placeholder.
func productionCypherSitesErr(root string) ([]cypherSite, error) {
	files, err := collectGoFiles(root, false)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, nil
	}
	parsed := make([]parsedGoFile, len(files))
	runFileWorkers(len(files), func(idx int) {
		entry := files[idx]
		fset := token.NewFileSet()
		file, parseErr := parser.ParseFile(fset, entry.path, nil, 0)
		parsed[idx] = parsedGoFile{
			rel:  entry.rel,
			dir:  filepath.Dir(entry.rel),
			fset: fset,
			file: file,
			err:  parseErr,
		}
	})
	for i := range parsed {
		if parsed[i].err != nil {
			return nil, fmt.Errorf("parse %s: %w", files[i].path, parsed[i].err)
		}
	}
	consts := resolvePackageConsts(parsed)
	perFile := make([][]cypherSite, len(parsed))
	runFileWorkers(len(parsed), func(idx int) {
		pf := &parsed[idx]
		dirConsts := consts[pf.dir]
		var sites []cypherSite
		ast.Inspect(pf.file, func(node ast.Node) bool {
			expr, ok := node.(ast.Expr)
			if !ok {
				return true
			}
			if _, bare := expr.(*ast.Ident); bare {
				// A bare name is a declaration or a use of a constant the
				// declaration already supplied; only a `+` chain folds names.
				return true
			}
			text, hasLiteral := foldWithPlaceholders(expr, dirConsts)
			if !hasLiteral {
				return true
			}
			if schemaStatement.MatchString(text) {
				return false
			}
			dynamic := templateLabelNode.MatchString(text)
			admitted := dynamic && templateWriteNode.MatchString(text) ||
				writeShape.MatchString(text) && (labeledShape.MatchString(text) || unlabeledIDShape.MatchString(text)) ||
				parameterMapNode.MatchString(text) ||
				labelRemoval.MatchString(text) && labeledShape.MatchString(text)
			if admitted {
				line := pf.fset.Position(expr.Pos()).Line
				sites = append(sites, cypherSite{
					Key: pf.rel + ":" + strconv.Itoa(line), File: pf.rel, Line: line,
					Text: text, DynamicLabel: dynamic,
				})
			}
			return false
		})
		perFile[idx] = sites
	})
	var out []cypherSite
	for _, sites := range perFile {
		out = append(out, sites...)
	}
	return out, nil
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
