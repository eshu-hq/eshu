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
	"unicode"
)

// unwindKeywordPattern, matchKeywordPattern, and setKeywordPattern locate the
// UNWIND/MATCH/SET clause keywords hasUnwindBareMatchThenSet scans between.
// Case-insensitive with \b word boundaries so e.g. "UNMATCHED" or "SETTINGS"
// never counts as the keyword.
var (
	unwindKeywordPattern = regexp.MustCompile(`(?i)\bUNWIND\b`)
	matchKeywordPattern  = regexp.MustCompile(`(?i)\bMATCH\b`)
	setKeywordPattern    = regexp.MustCompile(`(?i)\bSET\b`)
)

// hasUnwindBareMatchThenSet flags the exact statement class issue #5652 found
// silently drops its write on the pinned production NornicDB image
// (nornicdb-cpu-bge:v1.1.11): an UNWIND-batched statement whose write clause
// is a MATCH-anchored node/relationship pattern (with no MERGE anywhere else
// in the statement as a safety net -- the caller checks that separately),
// immediately followed by SET. The statement reports success
// (PropertiesSet=0, ContainsUpdates=false) while the property is never
// written. See docs/internal/evidence/5652-nornic-bare-match-writeloss.md and
// posture_node_existence.go.
//
// This replaced a `(?is)UNWIND\b.*?\bMATCH\s*\([^)]*\)\s*SET\b` regex: the
// `[^)]*` component cannot match a node pattern whose property value itself
// contains a nested `)`, e.g. `MATCH (n:Label {prop:
// coalesce(row.val,'default')})` -- `[^)]*` stops at coalesce's closing `)`,
// so the regex never reaches the pattern's OWN closing `)` and the whole
// match fails, letting an unsafe statement with a parenthesized property
// value escape the gate. hasUnwindBareMatchThenSet instead scans the MATCH
// pattern with an explicit paren-depth counter (matchingParenIndex) so nested
// parentheses balance correctly regardless of depth.
//
// This is deliberately narrow: it only flags a MATCH clause immediately
// followed by SET. A statement that also contains a MERGE (e.g. for a
// companion edge) is a structurally different, separately evaluated shape and
// is out of this gate's scope; see the "multi-clause silent edge-drop"
// finding in the evidence note for that class instead. The caller
// (TestNoUnwindBareMatchThenSetCyphersInPackage) applies the MERGE-anywhere
// exemption after calling this function.
func hasUnwindBareMatchThenSet(value string) bool {
	unwindLoc := unwindKeywordPattern.FindStringIndex(value)
	if unwindLoc == nil {
		return false
	}
	unwindEnd := unwindLoc[1]

	for _, matchLoc := range matchKeywordPattern.FindAllStringIndex(value, -1) {
		if matchLoc[0] < unwindEnd {
			// MATCH must textually follow the UNWIND this gate is scoped to.
			continue
		}

		i := matchLoc[1]
		for i < len(value) && unicode.IsSpace(rune(value[i])) {
			i++
		}
		if i >= len(value) || value[i] != '(' {
			// Not a parenthesized node/relationship pattern immediately
			// after MATCH (e.g. "MATCH p = ...") -- out of this gate's
			// narrow scope.
			continue
		}

		closeIdx := matchingParenIndex(value, i)
		if closeIdx < 0 {
			// Unbalanced parentheses: nothing this scan can safely conclude.
			continue
		}

		j := closeIdx + 1
		for j < len(value) && unicode.IsSpace(rune(value[j])) {
			j++
		}
		if loc := setKeywordPattern.FindStringIndex(value[j:]); loc != nil && loc[0] == 0 {
			return true
		}
	}
	return false
}

// matchingParenIndex returns the index in value of the ')' that closes the
// '(' at index open, counting nested parentheses (so
// `(prop: coalesce(a, b))` correctly resolves to the OUTER closing paren, not
// the first one encountered), or -1 if the parentheses never balance.
func matchingParenIndex(value string, open int) int {
	depth := 0
	for i := open; i < len(value); i++ {
		switch value[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

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
				if !hasUnwindBareMatchThenSet(value) {
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

// TestHasUnwindBareMatchThenSet is the unit-level proof that
// hasUnwindBareMatchThenSet closes the false-negative the prior
// `[^)]*`-based regex had on a MATCH pattern with a nested-parenthesis
// property value, while keeping the same detection behavior for every shape
// the regex already caught (and every shape it correctly let through).
func TestHasUnwindBareMatchThenSet(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		value string
		want  bool
	}{
		{
			name:  "flat bare match then set (no nested parens)",
			value: `UNWIND $rows AS row MATCH (n:CloudResource {uid: row.uid}) SET n.x = row.x`,
			want:  true,
		},
		{
			name: "nested-paren property value (the regex false negative this fixes)",
			value: `UNWIND $rows AS row
MATCH (n:CloudResource {prop: coalesce(row.val, 'default')})
SET n.x = row.x`,
			want: true,
		},
		{
			name: "doubly-nested-paren property value",
			value: `UNWIND $rows AS row
MATCH (n:CloudResource {prop: coalesce(trim(row.val), 'default')})
SET n.x = row.x`,
			want: true,
		},
		{
			name:  "label-only anchor, no property, still flagged",
			value: `UNWIND $rows AS row MATCH (n:CloudResource) SET n.x = row.x`,
			want:  true,
		},
		{
			name:  "MATCH before UNWIND does not count",
			value: `MATCH (n:CloudResource {uid: $uid}) SET n.x = 1 WITH n UNWIND $rows AS row RETURN row`,
			want:  false,
		},
		{
			name:  "MATCH with no trailing SET",
			value: `UNWIND $rows AS row MATCH (n:CloudResource {uid: row.uid}) RETURN n`,
			want:  false,
		},
		{
			name:  "no UNWIND at all",
			value: `MATCH (n:CloudResource {uid: $uid}) SET n.x = 1`,
			want:  false,
		},
		{
			name: "second MATCH in the statement is the bare one (nested parens on the first)",
			value: `UNWIND $rows AS row
MATCH (owner:Account {id: coalesce(row.owner_id, 'unknown')})
MATCH (n:CloudResource {uid: row.uid})
SET n.x = row.x`,
			want: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := hasUnwindBareMatchThenSet(tc.value); got != tc.want {
				t.Fatalf("hasUnwindBareMatchThenSet(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

// TestMatchingParenIndex proves the paren-depth counter itself resolves to
// the pattern's OWN closing paren rather than the first `)` encountered, the
// exact failure mode of the prior `[^)]*` regex.
func TestMatchingParenIndex(t *testing.T) {
	t.Parallel()

	value := `(prop: coalesce(row.val, 'default'))`
	open := strings.IndexByte(value, '(')
	got := matchingParenIndex(value, open)
	want := len(value) - 1
	if got != want {
		t.Fatalf("matchingParenIndex(%q, %d) = %d, want %d (the pattern's own outer closing paren)",
			value, open, got, want)
	}
}
