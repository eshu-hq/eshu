// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"strings"
	"testing"
)

// TestCodeCallRetractStatementsUseSingleSourceLabel guards the #5116 fix.
//
// The code-call edge retract must emit one statement per source label, each with
// a single-label source anchor `(source:Label)`. On NornicDB a node-label
// disjunction `(source:A|B)` matches zero rows, and on v1.1.11 an unlabeled
// `(source)` scan silently drops some source labels (e.g. File-sourced
// REFERENCES), so a single combined statement deletes nothing or only part.
// This static guard (no backend) fails fast if a future edit regresses to a
// disjunction or unlabeled source; the live proof is
// TestReducerCodeCallEdgeRetractGraphTruth in internal/replay/offlinetier.
func TestCodeCallRetractStatementsUseSingleSourceLabel(t *testing.T) {
	t.Parallel()

	for _, evidenceSource := range []string{"parser/code-calls", "parser/python-metaclass", "other/fallback"} {
		wantLabels := codeCallRetractSourceLabelsFor(evidenceSource)
		for _, build := range []struct {
			name  string
			stmts []Statement
		}{
			{"repo", BuildRetractCodeCallEdgeStatements([]string{"r"}, evidenceSource)},
			{"file", BuildRetractCodeCallEdgeStatementsByFilePath([]string{"p"}, evidenceSource)},
		} {
			if len(build.stmts) != len(wantLabels) {
				t.Fatalf("%s/%s: %d statements, want %d (one per source label)",
					evidenceSource, build.name, len(build.stmts), len(wantLabels))
			}
			seen := map[string]bool{}
			for _, stmt := range build.stmts {
				label := sourceLabelFromRetract(stmt.Cypher)
				if label == "" {
					t.Errorf("%s/%s: statement has no single-label source anchor (unlabeled scan?) (#5116): %q",
						evidenceSource, build.name, stmt.Cypher)
					continue
				}
				if strings.Contains(label, "|") {
					t.Errorf("%s/%s: node-label disjunction reintroduced (#5116): %q",
						evidenceSource, build.name, stmt.Cypher)
				}
				seen[label] = true
			}
			for _, want := range wantLabels {
				if !seen[want] {
					t.Errorf("%s/%s: no retract statement for source label %q", evidenceSource, build.name, want)
				}
			}
		}
	}
}

// sourceLabelFromRetract returns the source label from a retract statement of
// the form `MATCH (source:Label)-[...`. It returns "" when there is no
// `(source:<label>)` anchor — an unlabeled source, which is itself a #5116
// regression on NornicDB v1.1.11.
func sourceLabelFromRetract(cypher string) string {
	const anchor = "(source:"
	idx := strings.Index(cypher, anchor)
	if idx < 0 {
		return ""
	}
	rest := cypher[idx+len(anchor):]
	end := strings.Index(rest, ")")
	if end < 0 {
		return ""
	}
	return rest[:end]
}
