// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/code/semantic"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

func legacyOrderRow(id, entityType string) semantic.EntityRow {
	return semantic.EntityRow{
		RepoID:       "repo-1",
		EntityID:     id,
		EntityType:   entityType,
		EntityName:   id,
		FilePath:     "/repo/main.go",
		RelativePath: "main.go",
		Language:     "go",
		StartLine:    1,
		EndLine:      2,
	}
}

func upsertsByLabel(calls []sourcecypher.Statement) map[string]string {
	upserts := map[string]string{}
	for _, call := range calls {
		if call.Operation != sourcecypher.OperationCanonicalUpsert {
			continue
		}
		label, _ := call.Parameters[sourcecypher.StatementMetadataEntityLabelKey].(string)
		if label != "" {
			upserts[label] = call.Cypher
		}
	}
	return upserts
}

// TestSemanticEntityLegacyOrderMatchesCanonicalOwnedTemplates pins #10110 at
// the writer seam: legacy-order mode emits byte-identical enrichment for
// canonical-owned labels as canonical-node mode, while semantic-owned
// Variable keeps the legacy MATCH-File-first template (no fileless nodes).
func TestSemanticEntityLegacyOrderMatchesCanonicalOwnedTemplates(t *testing.T) {
	t.Parallel()

	rows := []semantic.EntityRow{
		legacyOrderRow("function-go-1", "Function"),
		legacyOrderRow("variable-go-1", "Variable"),
	}

	canonicalExec := &recordingExecutor{}
	if _, err := NewSemanticEntityWriterWithCanonicalNodeRows(canonicalExec, 100).WriteSemanticEntities(
		context.Background(), semantic.EntityWrite{RepoIDs: []string{"repo-1"}, SkipRetract: true, Rows: rows},
	); err != nil {
		t.Fatalf("canonical-node WriteSemanticEntities() error = %v", err)
	}
	legacyExec := &recordingExecutor{}
	if _, err := NewSemanticEntityWriterWithCanonicalNodeRowsLegacyOrder(legacyExec, 100).WriteSemanticEntities(
		context.Background(), semantic.EntityWrite{RepoIDs: []string{"repo-1"}, SkipRetract: true, Rows: rows},
	); err != nil {
		t.Fatalf("legacy-order WriteSemanticEntities() error = %v", err)
	}

	canonical := upsertsByLabel(canonicalExec.calls)
	legacy := upsertsByLabel(legacyExec.calls)
	if legacy["Function"] == "" || legacy["Function"] != canonical["Function"] {
		t.Fatalf("Function upsert legacy-order = %q, want byte-identical to canonical-node %q", legacy["Function"], canonical["Function"])
	}
	if !strings.HasPrefix(legacy["Variable"], "UNWIND $rows AS row\nMATCH (f:File {path: row.file_path})\nMERGE (n:Variable {uid: row.entity_id})") {
		t.Fatalf("Variable upsert legacy-order = %q, want legacy MATCH-File-first template", legacy["Variable"])
	}
	if legacy["Variable"] != semanticVariableUpsertCypher {
		t.Fatalf("Variable upsert legacy-order = %q, want the untouched legacy template", legacy["Variable"])
	}
}

// TestSemanticEntityLegacyOrderRetractsBroadPlusClears pins the Neo4j
// retract shape: 3 broad statements + 9 REMOVE clears, full and delta.
func TestSemanticEntityLegacyOrderRetractsBroadPlusClears(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name  string
		write semantic.EntityWrite
		scope string
	}{
		{
			name:  "full",
			write: semantic.EntityWrite{RepoIDs: []string{"repo-1"}, Rows: []semantic.EntityRow{legacyOrderRow("function-go-1", "Function")}},
			scope: "n.repo_id IN $repo_ids",
		},
		{
			name: "delta",
			write: semantic.EntityWrite{
				RepoIDs:         []string{"repo-1"},
				Rows:            []semantic.EntityRow{legacyOrderRow("function-go-1", "Function")},
				DeltaProjection: true,
				DeltaFilePaths:  []string{"/repo/main.go"},
			},
			scope: "n.path IN $file_paths",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			executor := &recordingExecutor{}
			if _, err := NewSemanticEntityWriterWithCanonicalNodeRowsLegacyOrder(executor, 100).WriteSemanticEntities(context.Background(), tt.write); err != nil {
				t.Fatalf("WriteSemanticEntities() error = %v", err)
			}
			var broad, clears int
			for _, call := range executor.calls {
				if call.Operation != sourcecypher.OperationCanonicalRetract {
					continue
				}
				if !strings.Contains(call.Cypher, tt.scope) {
					t.Fatalf("retract = %q, want scope %q", call.Cypher, tt.scope)
				}
				switch {
				case strings.Contains(call.Cypher, ":Annotation|Typedef|TypeAlias"):
					broad++
				case strings.Contains(call.Cypher, "REMOVE n."):
					clears++
				default:
					t.Fatalf("retract = %q, want broad retract or canonical-node clear", call.Cypher)
				}
			}
			if broad != 3 || clears != 9 {
				t.Fatalf("retracts: broad=%d clears=%d, want broad=3 clears=9", broad, clears)
			}
		})
	}
}
