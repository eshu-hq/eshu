// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// Guard for #7597: every statement that sets a relationship_generations row
// active must run through activateWithConsumerWake, so the wake commits in the
// same transaction. The writers are enumerated from the Go source of the
// whole module, so a fourth activation writer added later is held to the rule
// without anyone editing this test.

// activationWriterPattern matches SQL that makes a relationship generation
// active: an UPDATE that assigns status = 'active', or an INSERT that writes
// an 'active' row.
var activationWriterPattern = regexp.MustCompile(
	`(?is)UPDATE\s+relationship_generations\s+SET\s+status\s*=\s*'active'|INSERT\s+INTO\s+relationship_generations\b[^;]*?'active'`)

// activationWakeHelper is the only function allowed to run an activation.
const activationWakeHelper = "activateWithConsumerWake"

// activationWriterReport lists the activation constants a source set declares
// and every use of one outside activateWithConsumerWake.
type activationWriterReport struct {
	writers    []string
	violations []string
}

// scanActivationWriters parses each file and reports activation writers and
// their uses. files maps a display name to Go source.
func scanActivationWriters(t *testing.T, files map[string]string) activationWriterReport {
	t.Helper()
	fset := token.NewFileSet()
	parsed := map[string]*ast.File{}
	writers := map[string]bool{}
	for name, src := range files {
		file, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		parsed[name] = file
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || (gen.Tok != token.CONST && gen.Tok != token.VAR) {
				continue
			}
			for _, spec := range gen.Specs {
				value := spec.(*ast.ValueSpec)
				for i, ident := range value.Names {
					if i >= len(value.Values) {
						continue
					}
					text := src[fset.Position(value.Values[i].Pos()).Offset:fset.Position(value.Values[i].End()).Offset]
					if activationWriterPattern.MatchString(text) {
						writers[ident.Name] = true
					}
				}
			}
		}
	}
	report := activationWriterReport{}
	for name := range writers {
		report.writers = append(report.writers, name)
	}
	sort.Strings(report.writers)
	for _, file := range parsed {
		allowed := map[*ast.Ident]bool{}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != activationWakeHelper || len(call.Args) < 2 {
				return true
			}
			if ident, ok := call.Args[1].(*ast.Ident); ok {
				allowed[ident] = true
			}
			return true
		})
		ast.Inspect(file, func(node ast.Node) bool {
			if _, ok := node.(*ast.ValueSpec); ok {
				// The declaration itself is not a use.
				return false
			}
			ident, ok := node.(*ast.Ident)
			if ok && writers[ident.Name] && !allowed[ident] {
				report.violations = append(report.violations,
					fset.Position(ident.Pos()).String()+": "+ident.Name+" used outside "+activationWakeHelper)
			}
			return true
		})
	}
	sort.Strings(report.violations)
	return report
}

// moduleNonTestSources reads every non-test Go file under go/.
func moduleNonTestSources(t *testing.T) map[string]string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	root := filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if name := entry.Name(); name == "vendor" || name == "testdata" || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(src), "relationship_generations") || strings.Contains(string(src), activationWakeHelper) {
			files[path] = string(src)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return files
}

func TestEveryRelationshipActivationWakesConsumers(t *testing.T) {
	t.Parallel()
	report := scanActivationWriters(t, moduleNonTestSources(t))
	want := []string{"activateGenerationSQL", "activateResolutionGenerationForClaimSQL", "activateResolutionGenerationSQL"}
	for _, name := range want {
		found := false
		for _, writer := range report.writers {
			found = found || writer == name
		}
		if !found {
			t.Fatalf("activation writers = %v, missing %s; the scan is broken, not the code", report.writers, name)
		}
	}
	if len(report.violations) > 0 {
		t.Fatalf("relationship generation activations that skip the #7597 consumer wake:\n  %s",
			strings.Join(report.violations, "\n  "))
	}
}

// TestActivationWriterGuardBitesOnAPlantedWriter is the seeded RED/GREEN pair
// for the guard: a planted fourth writer executed directly fails it, and the
// same writer routed through the helper passes.
func TestActivationWriterGuardBitesOnAPlantedWriter(t *testing.T) {
	t.Parallel()
	const planted = "package postgres\n" +
		"const plantedActivateSQL = `INSERT INTO relationship_generations (generation_id, scope, status) VALUES ($1, $2, 'active')`\n" +
		"func (s *RelationshipStore) planted() { s.database.ExecContext(nil, plantedActivateSQL) }\n"
	red := scanActivationWriters(t, map[string]string{"planted.go": planted})
	if len(red.writers) != 1 || len(red.violations) != 1 {
		t.Fatalf("planted direct writer: writers=%v violations=%v, want one of each", red.writers, red.violations)
	}
	routed := strings.Replace(planted, "s.database.ExecContext(nil, plantedActivateSQL)",
		"s.activateWithConsumerWake(nil, plantedActivateSQL, nil, \"\", \"\", time.Time{})", 1)
	green := scanActivationWriters(t, map[string]string{"planted.go": routed})
	if len(green.writers) != 1 || len(green.violations) != 0 {
		t.Fatalf("planted routed writer: writers=%v violations=%v, want one writer and none", green.writers, green.violations)
	}
	supersede := "package postgres\nconst retireSQL = `UPDATE relationship_generations SET status = 'superseded' WHERE status = 'active'`\n"
	if got := scanActivationWriters(t, map[string]string{"retire.go": supersede}); len(got.writers) != 0 {
		t.Fatalf("a retire statement counted as an activation writer: %v", got.writers)
	}
}

// TestActivationWakeSetClauseMatchesReplayReset keeps the wake's SET list in
// lockstep with the recovery replay reset list it mirrors: the same
// assignments, plus reopened_at. The replay list is derived from the shipped
// template, not copied.
func TestActivationWakeSetClauseMatchesReplayReset(t *testing.T) {
	t.Parallel()
	normalize := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	template := replayFailedWorkItemsTemplate
	start := strings.Index(template, "SET ")
	end := strings.Index(template, "WHERE status IN ('dead_letter', 'failed')")
	if start < 0 || end < start {
		t.Fatal("cannot find the SET list in replayFailedWorkItemsTemplate")
	}
	replay := normalize(template[start+len("SET ") : end])
	wake := normalize(activationWakeSetClause)
	if want := replay + ", reopened_at = $1"; wake != want {
		t.Fatalf("activation wake SET list drifted from the replay reset list:\n got: %s\nwant: %s", wake, want)
	}
}

// lockOrderPattern captures the ORDER BY clause that sets the lock order of a
// FOR UPDATE / FOR NO KEY UPDATE row lock.
var lockOrderPattern = regexp.MustCompile(`(?s)ORDER BY ([^\n]+)\n\s*(?:LIMIT [^\n]+\n\s*)?FOR (?:NO KEY )?UPDATE`)

// TestOwnGateLocksShareTheBatchAckOrder pins one lock order for every
// multi-row locker of reducer work rows the activation wake can meet: the
// batch ack and both wake statements sort on work_item_id COLLATE "C", so
// they cannot lock two rows in opposite orders (#7597 review N1).
func TestOwnGateLocksShareTheBatchAckOrder(t *testing.T) {
	t.Parallel()
	for name, query := range map[string]string{
		"ackReducerWorkBatchQuery":         ackReducerWorkBatchQuery(),
		"lockInflightOwnGateConsumersSQL":  lockInflightOwnGateConsumersSQL,
		"wakeProducerTerminalConsumersSQL": wakeProducerTerminalConsumersSQL,
	} {
		orders := lockOrderPattern.FindAllStringSubmatch(query, -1)
		if len(orders) == 0 {
			t.Fatalf("%s: no ORDER BY before a row lock; the lock order is undefined", name)
		}
		for _, order := range orders {
			if !strings.Contains(order[1], `work_item_id COLLATE "C"`) {
				t.Fatalf("%s locks in order %q, want work_item_id COLLATE \"C\"", name, order[1])
			}
		}
	}
}
