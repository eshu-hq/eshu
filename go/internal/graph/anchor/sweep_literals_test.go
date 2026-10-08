// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package anchor

import (
	"crypto/sha256"
	"encoding/hex"
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
//   - and an id token or a dynamic map write (`+=`),
//   - and a node pattern: labeled (`(n:Label`), a template label (`(n:%s`),
//     or unlabeled with an id key in its map (`(n {id: ...})`).
//
// Postgres SQL (SET col = value) has none of the node patterns and stays out.
var (
	writeShape        = regexp.MustCompile(`(?is)\b(MERGE|CREATE|SET)\b.*(\bid\b|\+=)`)
	labeledShape      = regexp.MustCompile(`\(\s*\w*\s*:\s*[A-Za-z_` + "`" + `]`)
	unlabeledIDShape  = regexp.MustCompile(`\(\s*\w*\s*\{[^{}]*\bid\s*:`)
	templateLabelNode = regexp.MustCompile(`\(\s*\w*\s*:\s*%[sdvq]`)
	templateWriteNode = regexp.MustCompile(`(?is)\b(MERGE|CREATE)\b.*\(\s*\w*\s*:\s*%[sdvq]`)
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
	// Hash identifies the template text: the first 12 hex digits of the SHA-256
	// of the whitespace-normalized statement.
	Hash string
	// DynamicLabel is true when a node pattern's label is a placeholder.
	DynamicLabel bool
}

func templateHash(text string) string {
	sum := sha256.Sum256([]byte(strings.Join(strings.Fields(text), " ")))
	return hex.EncodeToString(sum[:])[:12]
}

// productionCypherSites returns every admitted Cypher literal in the non-test
// Go files under root. A `+` chain of literals and non-literal operands folds
// to one template whose non-literal operands read as a placeholder.
func productionCypherSites(t *testing.T, root string) []cypherSite {
	t.Helper()
	var out []cypherSite
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
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return perr
		}
		rel, _ := filepath.Rel(root, path)
		ast.Inspect(file, func(node ast.Node) bool {
			expr, ok := node.(ast.Expr)
			if !ok {
				return true
			}
			text, hasLiteral := foldWithPlaceholders(expr)
			if !hasLiteral {
				return true
			}
			if schemaStatement.MatchString(text) {
				return false
			}
			dynamic := templateLabelNode.MatchString(text)
			admitted := dynamic && templateWriteNode.MatchString(text) ||
				writeShape.MatchString(text) && (labeledShape.MatchString(text) || unlabeledIDShape.MatchString(text))
			if admitted {
				out = append(out, cypherSite{
					Key: rel + ":" + strconv.Itoa(fset.Position(expr.Pos()).Line), File: rel,
					Text: text, Hash: templateHash(text), DynamicLabel: dynamic,
				})
			}
			return false
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return out
}

// foldWithPlaceholders returns the value of a string literal or of a `+` chain
// that holds at least one string literal. A non-literal operand of the chain
// reads as a placeholder. hasLiteral is false for anything else, including
// arithmetic on non-strings.
func foldWithPlaceholders(expr ast.Expr) (string, bool) {
	switch typed := expr.(type) {
	case *ast.BasicLit:
		if typed.Kind != token.STRING {
			return placeholder, false
		}
		value, err := strconv.Unquote(typed.Value)
		return value, err == nil
	case *ast.ParenExpr:
		return foldWithPlaceholders(typed.X)
	case *ast.BinaryExpr:
		if typed.Op != token.ADD {
			return placeholder, false
		}
		left, lok := foldWithPlaceholders(typed.X)
		right, rok := foldWithPlaceholders(typed.Y)
		return left + right, lok || rok
	}
	return placeholder, false
}
