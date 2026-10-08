// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package anchor

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// writeShape is the cheap prefilter for the static sweep: a literal that has a
// write keyword, mentions an id key or property, and carries a labeled node
// pattern. The pattern requirement keeps Postgres SQL (SET col = value) out of
// the sweep. CheckWriters then decides.
var (
	writeShape   = regexp.MustCompile(`(?is)\b(MERGE|CREATE|SET)\b.*\bid\b`)
	labeledShape = regexp.MustCompile(`\(\s*\w*\s*:\s*[A-Za-z_]`)
)

// productionCypherLiterals returns every string literal in the non-test Go
// files under root that looks like a Cypher write touching an id, keyed by
// "relative/path.go:line". Constant concatenation of literals is folded.
func productionCypherLiterals(t *testing.T, root string) map[string]string {
	t.Helper()
	out := make(map[string]string)
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
			text, folded := foldStringLiterals(expr)
			if !folded {
				return true
			}
			if writeShape.MatchString(text) && labeledShape.MatchString(text) {
				out[rel+":"+strconv.Itoa(fset.Position(expr.Pos()).Line)] = text
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

// foldStringLiterals returns the value of a string literal or of a `+` chain
// of string literals.
func foldStringLiterals(expr ast.Expr) (string, bool) {
	switch typed := expr.(type) {
	case *ast.BasicLit:
		if typed.Kind != token.STRING {
			return "", false
		}
		value, err := strconv.Unquote(typed.Value)
		return value, err == nil
	case *ast.BinaryExpr:
		if typed.Op != token.ADD {
			return "", false
		}
		left, lok := foldStringLiterals(typed.X)
		right, rok := foldStringLiterals(typed.Y)
		return left + right, lok && rok
	case *ast.ParenExpr:
		return foldStringLiterals(typed.X)
	}
	return "", false
}

// sweepForUncoveredIDWriters runs CheckWriters over every id-writing Cypher
// literal under root and returns the number of id writes seen and one line per
// uncovered write.
func sweepForUncoveredIDWriters(t *testing.T, root string, labels map[string]bool) (int, []string) {
	t.Helper()
	literals := productionCypherLiterals(t, root)
	var failures []string
	writing := 0
	for site, text := range literals {
		report := CheckWriters([]Statement{{Text: text, Callsite: site}}, labels)
		writing += report.IDWrites
		for _, finding := range report.Findings {
			failures = append(failures, site+" "+finding.Kind+" on "+finding.Variable+" labels="+strings.Join(finding.Labels, ":"))
		}
	}
	sort.Strings(failures)
	return writing, failures
}

// TestEveryProductionIDWriterNamesAnAnchorLabel is the static half of the
// writer-coverage gate (#7212): an independent sweep over the Cypher literals
// in go/ that does not depend on what a replay happens to execute. A write that
// sets an id on a label outside UIDLabels and IDLabels would leave an
// id-bearing node the labeled entity-context anchor cannot reach.
//
// The sweep reads literals and constant concatenations only. A statement whose
// label is built at run time ("MERGE (n:" + label + ...) is invisible to it;
// the replay half of the gate covers those.
func TestEveryProductionIDWriterNamesAnAnchorLabel(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("go module root %s: %v", root, err)
	}
	writing, failures := sweepForUncoveredIDWriters(t, root, Labels())
	if writing < 20 {
		t.Fatalf("static sweep saw %d id writes; the sweep is not reading go/", writing)
	}
	if len(failures) > 0 {
		t.Fatalf("%d id write(s) outside the anchor label set:\n%s", len(failures), strings.Join(failures, "\n"))
	}
}

// TestStaticSweepFailsOnAPlantedUnconstrainedIDWriter is the seeded violation:
// a Go file under a scratch root plants `MERGE (n:Unconstrained {id: ...})`
// and the sweep must name it. The same scratch root with the planted label
// covered passes, so the failure is the label and not the harness.
func TestStaticSweepFailsOnAPlantedUnconstrainedIDWriter(t *testing.T) {
	root := t.TempDir()
	source := "package planted\n\nconst plantedCypher = `MERGE (n:Unconstrained {id: $entity_id}) SET n.name = $name`\n"
	if err := os.WriteFile(filepath.Join(root, "planted.go"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	_, failures := sweepForUncoveredIDWriters(t, root, Labels())
	if len(failures) != 1 || !strings.HasPrefix(failures[0], "planted.go:3 map_key on n labels=Unconstrained") {
		t.Fatalf("failures = %v, want exactly the planted writer at planted.go:3", failures)
	}
	covered := Labels()
	covered["Unconstrained"] = true
	if _, failures := sweepForUncoveredIDWriters(t, root, covered); len(failures) != 0 {
		t.Fatalf("failures with the planted label covered = %v, want none", failures)
	}
}
