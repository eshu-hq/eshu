// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// unwindBareMatchSetPattern flags the exact statement class issue #5652 found
// silently drops its write on the pinned production NornicDB image
// (nornicdb-cpu-bge:v1.1.11): an UNWIND-batched statement whose write clause
// is a MATCH-anchored node/relationship pattern (a property-keyed lookup, not
// a MERGE) with no MERGE anywhere else in the statement as a safety net,
// immediately followed by SET. The statement reports success
// (PropertiesSet=0, ContainsUpdates=false) while the property is never
// written. See docs/internal/evidence/5652-nornic-bare-match-writeloss.md and
// posture_node_existence.go.
//
// This is deliberately narrow: it only flags a MATCH clause with zero MERGE
// anywhere in the same statement. A statement that also contains a MERGE
// (e.g. for a companion edge) is a structurally different, separately
// evaluated shape and is out of this gate's scope; see the "multi-clause
// silent edge-drop" finding in the evidence note for that class instead.
var unwindBareMatchSetPattern = regexp.MustCompile(`(?is)UNWIND\b.*?\bMATCH\s*\([^)]*\)\s*SET\b`)

// unwindBareMatchSetAllowlist names Go string constant identifiers that
// legitimately contain the UNWIND/MATCH/SET textual shape without hitting the
// bare-MATCH-anchor no-op: their MATCH clause anchors on a label only (no
// property lookup) or the SET body does not mutate the matched entity itself.
// Keep this list empty if possible; add an entry only with a comment proving
// why the specific statement is safe.
var unwindBareMatchSetAllowlist = map[string]bool{}

// TestNoUnwindBareMatchThenSetCyphersInPackage is the static class-gate for
// issue #5652. It parses every non-test .go file in this package directory,
// collects every string constant's literal value, and fails if any of them
// contain the UNWIND-batched bare-MATCH-then-SET-with-no-MERGE shape. This
// keeps a fixed writer (or a new one copy-pasted from an old pattern) from
// reintroducing the silent write-loss class.
func TestNoUnwindBareMatchThenSetCyphersInPackage(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}

	fset := token.NewFileSet()
	var violations []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		file, err := parser.ParseFile(fset, name, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}

		ast.Inspect(file, func(n ast.Node) bool {
			valueSpec, ok := n.(*ast.ValueSpec)
			if !ok {
				return true
			}
			for i, ident := range valueSpec.Names {
				if i >= len(valueSpec.Values) {
					continue
				}
				lit, ok := valueSpec.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				value, err := strconv.Unquote(lit.Value)
				if err != nil {
					// Raw string literals (backtick) unquote fine via
					// strconv.Unquote too; skip anything that fails rather
					// than fail the whole gate on an unrelated parse edge
					// case.
					continue
				}
				if !unwindBareMatchSetPattern.MatchString(value) {
					continue
				}
				if strings.Contains(value, "MERGE") {
					continue
				}
				if unwindBareMatchSetAllowlist[ident.Name] {
					continue
				}
				violations = append(violations, name+": "+ident.Name)
			}
			return true
		})
	}

	if len(violations) > 0 {
		t.Fatalf(
			"found %d Cypher constant(s) with the UNWIND-batched bare-MATCH-then-SET "+
				"shape (issue #5652: silently drops its write on the pinned production "+
				"NornicDB v1.1.11 image). Anchor with MERGE instead, filtering rows to "+
				"confirmed-existing identities first if the writer must never fabricate a "+
				"node (see posture_node_existence.go). Violations:\n  %s",
			len(violations), strings.Join(violations, "\n  "),
		)
	}
}
