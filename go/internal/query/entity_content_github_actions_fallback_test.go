// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetEntityContextFallsBackToGitHubActionsWorkflowLocalReusablePath(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"gha-workflow-1", "repo-1", ".github/workflows/deploy.yaml", "File", "deploy",
					int64(1), int64(20), "yaml", "jobs:\n  deploy:\n    uses: myorg/deployment-helm/.github/workflows/deploy.yaml@main\n  local:\n    uses: ./.github/workflows/release.yaml\n", []byte(`{}`),
				},
			},
		},
	})

	handler := &EntityHandler{Content: NewContentReader(db)}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/gha-workflow-1/context", nil)
	req.SetPathValue("entity_id", "gha-workflow-1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}

	relationships, ok := resp["relationships"].([]any)
	if !ok {
		t.Fatalf("resp[relationships] type = %T, want []any", resp["relationships"])
	}
	if len(relationships) != 2 {
		t.Fatalf("len(resp[relationships]) = %d, want 2", len(relationships))
	}

	first, ok := relationships[0].(map[string]any)
	if !ok {
		t.Fatalf("resp[relationships][0] type = %T, want map[string]any", relationships[0])
	}
	if got, want := first["type"], "DEPLOYS_FROM"; got != want {
		t.Fatalf("relationships[0][type] = %#v, want %#v", got, want)
	}
	if got, want := first["target_name"], "myorg/deployment-helm"; got != want {
		t.Fatalf("relationships[0][target_name] = %#v, want %#v", got, want)
	}
	if got, want := first["reason"], "github_actions_reusable_workflow_ref"; got != want {
		t.Fatalf("relationships[0][reason] = %#v, want %#v", got, want)
	}

	second, ok := relationships[1].(map[string]any)
	if !ok {
		t.Fatalf("resp[relationships][1] type = %T, want map[string]any", relationships[1])
	}
	if got, want := second["type"], "DEPLOYS_FROM"; got != want {
		t.Fatalf("relationships[1][type] = %#v, want %#v", got, want)
	}
	if got, want := second["target_name"], ".github/workflows/release.yaml"; got != want {
		t.Fatalf("relationships[1][target_name] = %#v, want %#v", got, want)
	}
	if got, want := second["reason"], "github_actions_local_reusable_workflow_ref"; got != want {
		t.Fatalf("relationships[1][reason] = %#v, want %#v", got, want)
	}
}

func TestGetEntityContextFallsBackToGitHubActionsWorkflowActions(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"gha-workflow-actions", "repo-1", ".github/workflows/ci.yml", "File", "ci",
					int64(1), int64(12), "yaml", "jobs:\n  terraform:\n    steps:\n      - uses: hashicorp/setup-terraform@v3\n      - run: |\n          echo octocat/example-action@v1\n", []byte(`{}`),
				},
			},
		},
	})

	handler := &EntityHandler{Content: NewContentReader(db)}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/gha-workflow-actions/context", nil)
	req.SetPathValue("entity_id", "gha-workflow-actions")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}

	relationships, ok := resp["relationships"].([]any)
	if !ok {
		t.Fatalf("resp[relationships] type = %T, want []any", resp["relationships"])
	}
	if !hasGitHubActionsRelationship(relationships, "DEPENDS_ON", "github_actions_action_repository", "hashicorp/setup-terraform") {
		t.Fatalf("relationships = %#v, want hashicorp/setup-terraform action dependency", relationships)
	}
	if hasGitHubActionsRelationship(relationships, "DEPENDS_ON", "github_actions_action_repository", "octocat/example-action") {
		t.Fatalf("relationships = %#v, run-block foil octocat/example-action must not become an action dependency", relationships)
	}
}

func hasGitHubActionsRelationship(relationships []any, relationshipType string, reason string, targetName string) bool {
	for _, raw := range relationships {
		relationship, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if relationship["type"] == relationshipType && relationship["reason"] == reason && relationship["target_name"] == targetName {
			return true
		}
	}
	return false
}
