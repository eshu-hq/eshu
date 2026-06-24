// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestExtractShellExecRowsFromEmbeddedCommand(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		sqlRelationshipRepositoryEnvelope(false, nil),
		{
			FactKind: factKindFile,
			ScopeID:  "scope-db",
			Payload: map[string]any{
				"repo_id":       "repo-123",
				"relative_path": "cmd/archive/main.go",
				"path":          "/repo/cmd/archive/main.go",
				"parsed_file_data": map[string]any{
					"functions": []any{
						map[string]any{
							"name":        "runArchive",
							"line_number": 7,
							"uid":         "function:runArchive",
						},
					},
					"embedded_shell_commands": []any{
						map[string]any{
							"function_name":        "runArchive",
							"function_line_number": 7,
							"line_number":          8,
							"api":                  "os/exec.CommandContext",
							"language":             "go",
						},
					},
				},
			},
		},
	}

	repoIDs, rows := ExtractShellExecRows(envelopes)
	if len(repoIDs) != 1 || repoIDs[0] != "repo-123" {
		t.Fatalf("repoIDs = %v, want [repo-123]", repoIDs)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	row := rows[0]
	if got, want := row["source_entity_id"], "function:runArchive"; got != want {
		t.Fatalf("source_entity_id = %v, want %v", got, want)
	}
	if got, want := row["relationship_type"], "EXECUTES_SHELL"; got != want {
		t.Fatalf("relationship_type = %v, want %v", got, want)
	}
	if got, want := row["source_entity_type"], "Function"; got != want {
		t.Fatalf("source_entity_type = %v, want %v", got, want)
	}
	if got, want := row["target_entity_type"], "ShellCommand"; got != want {
		t.Fatalf("target_entity_type = %v, want %v", got, want)
	}
	targetID, _ := row["target_entity_id"].(string)
	if !strings.HasPrefix(targetID, "shell-command:") {
		t.Fatalf("target_entity_id = %q, want shell-command prefix", targetID)
	}
	if strings.Contains(targetID, "tar") {
		t.Fatalf("target_entity_id leaked command text: %q", targetID)
	}
}

func TestShellExecHandlerEmitsRefreshAndEdgeIntents(t *testing.T) {
	t.Parallel()

	writer := &recordingSQLRelationshipIntentWriter{}
	handler := ShellExecMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			sqlRelationshipRepositoryEnvelope(false, nil),
			{
				FactKind: factKindFile,
				ScopeID:  "scope-db",
				Payload: map[string]any{
					"repo_id":       "repo-123",
					"relative_path": "cmd/archive/main.go",
					"path":          "/repo/cmd/archive/main.go",
					"parsed_file_data": map[string]any{
						"functions": []any{
							map[string]any{"name": "runArchive", "line_number": 7, "uid": "function:runArchive"},
						},
						"embedded_shell_commands": []any{
							map[string]any{
								"function_name":        "runArchive",
								"function_line_number": 7,
								"line_number":          8,
								"api":                  "os/exec.CommandContext",
								"language":             "go",
							},
						},
					},
				},
			},
		}},
		IntentWriter: writer,
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-shell-1",
		ScopeID:      "scope-db",
		GenerationID: "gen-1",
		SourceSystem: "git",
		Domain:       DomainShellExecMaterialization,
		EnqueuedAt:   time.Date(2026, time.June, 18, 12, 0, 0, 0, time.UTC),
		AvailableAt:  time.Date(2026, time.June, 18, 12, 0, 0, 0, time.UTC),
		Status:       IntentStatusPending,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.CanonicalWrites != 2 {
		t.Fatalf("CanonicalWrites = %d, want 2", result.CanonicalWrites)
	}

	refresh := writer.refreshRows()
	if len(refresh) != 1 {
		t.Fatalf("refresh rows = %d, want 1", len(refresh))
	}
	if got := refresh[0].ProjectionDomain; got != DomainShellExec {
		t.Fatalf("refresh domain = %q, want %q", got, DomainShellExec)
	}

	edges := writer.edgeRows()
	if len(edges) != 1 {
		t.Fatalf("edge rows = %d, want 1", len(edges))
	}
	if got := edges[0].ProjectionDomain; got != DomainShellExec {
		t.Fatalf("edge domain = %q, want %q", got, DomainShellExec)
	}
	if !rowUsesRefreshFence(edges[0]) {
		t.Fatalf("edge intent %q not marked retract_via_refresh", edges[0].IntentID)
	}
}
