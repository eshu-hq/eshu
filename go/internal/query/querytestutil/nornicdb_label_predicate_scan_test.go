// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestProductionCypherHasNoIgnoredLabelPredicate walks every non-test Go file
// under go/internal and go/cmd and fails on any Cypher string literal, or
// literal-plus-identifier concatenation, that carries an X11 label predicate
// (#6786). Fragments joined across separate Go statements are covered by the
// per-builder unit tests that call AssertCypherHasNoIgnoredLabelPredicate on
// the rendered statement.
func TestProductionCypherHasNoIgnoredLabelPredicate(t *testing.T) {
	t.Parallel()
	violations := scanProductionCypherForIgnoredLabelPredicates(t, "../..", "../../../cmd")
	if len(violations) > 0 {
		t.Fatalf("production Cypher filters on a label where NornicDB v1.3.3 ignores it (#6786 X11):\n%s",
			strings.Join(violations, "\n"))
	}
}

// TestProductionCypherScanFindsSeededViolation is the scan's RED/GREEN pair:
// a seeded file with the infrastructure-read shape must be reported, and the
// IN labels() rewrite of the same file must not be.
func TestProductionCypherScanFindsSeededViolation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	seed := func(name, where string) {
		src := "package seeded\n\nconst q = `\n\t\tMATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)-[:CONTAINS]->(infra)\n\t\t" +
			where + "\n\t\tRETURN infra.id`\n\nvar p = `MATCH (s)-[:X]->(t)` + suffix + `\nWHERE any(l IN labels(t) WHERE l IN $ls) RETURN t.id`\n\nvar suffix = \"\"\n"
		sub := filepath.Join(dir, name)
		if err := mkdirWrite(sub, "seeded.go", src); err != nil {
			t.Fatal(err)
		}
	}
	seed("red", "WHERE infra:K8sResource OR infra:HelmChart")
	seed("green", "WHERE 'K8sResource' IN labels(infra) OR 'HelmChart' IN labels(infra)")

	red := scanProductionCypherForIgnoredLabelPredicates(t, filepath.Join(dir, "red"))
	if len(red) != 2 {
		t.Fatalf("seeded RED file: got %d violations, want 2 (label OR + concatenated any()):\n%s", len(red), strings.Join(red, "\n"))
	}
	green := scanProductionCypherForIgnoredLabelPredicates(t, filepath.Join(dir, "green"))
	if len(green) != 1 || !strings.Contains(green[0], "labels()") {
		t.Fatalf("seeded GREEN file: want only the concatenated any() violation, got:\n%s", strings.Join(green, "\n"))
	}
}

func scanProductionCypherForIgnoredLabelPredicates(t *testing.T, roots ...string) []string {
	t.Helper()
	var violations []string
	fset := token.NewFileSet()
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			for _, text := range cypherCandidates(file) {
				if problem := IgnoredLabelPredicate(text.value); problem != "" {
					violations = append(violations, fset.Position(text.pos).String()+": "+problem)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s: %v", root, err)
		}
	}
	sort.Strings(violations)
	return violations
}

type cypherCandidate struct {
	pos   token.Pos
	value string
}

// cypherCandidates returns every outermost string concatenation (non-literal
// operands rendered as a space) and every standalone string literal that
// contains both MATCH and WHERE.
func cypherCandidates(file *ast.File) []cypherCandidate {
	var out []cypherCandidate
	inConcat := map[*ast.BasicLit]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.BinaryExpr:
			if node.Op != token.ADD {
				return true
			}
			parts, lits, ok := flattenConcat(node)
			if !ok {
				return true
			}
			for _, lit := range lits {
				inConcat[lit] = true
			}
			out = appendCypherCandidate(out, node.Pos(), strings.Join(parts, ""))
			return false
		case *ast.BasicLit:
			if node.Kind == token.STRING && !inConcat[node] {
				if value, err := strconv.Unquote(node.Value); err == nil {
					out = appendCypherCandidate(out, node.Pos(), value)
				}
			}
		}
		return true
	})
	return out
}

func appendCypherCandidate(out []cypherCandidate, pos token.Pos, value string) []cypherCandidate {
	if strings.Contains(value, "MATCH") && strings.Contains(value, "WHERE") {
		out = append(out, cypherCandidate{pos: pos, value: value})
	}
	return out
}

// flattenConcat flattens a + chain. It reports ok only when at least one
// operand is a string literal; other operands render as a single space.
func flattenConcat(expr ast.Expr) ([]string, []*ast.BasicLit, bool) {
	switch node := expr.(type) {
	case *ast.BinaryExpr:
		if node.Op != token.ADD {
			return []string{" "}, nil, false
		}
		left, leftLits, leftOK := flattenConcat(node.X)
		right, rightLits, rightOK := flattenConcat(node.Y)
		return append(left, right...), append(leftLits, rightLits...), leftOK || rightOK
	case *ast.BasicLit:
		if node.Kind != token.STRING {
			return []string{" "}, nil, false
		}
		value, err := strconv.Unquote(node.Value)
		if err != nil {
			return []string{" "}, nil, false
		}
		return []string{value}, []*ast.BasicLit{node}, true
	case *ast.ParenExpr:
		return flattenConcat(node.X)
	default:
		return []string{" "}, nil, false
	}
}
