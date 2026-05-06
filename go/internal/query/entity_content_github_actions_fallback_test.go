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
					int64(1), int64(20), "yaml", "jobs:\n  deploy:\n    uses: myorg/deployment-helm/.github/workflows/deploy.yaml@main\n  local:\n    uses: ./.github/workflows/release.yaml\n", []byte(`{"workflow_refs":["myorg/deployment-helm/.github/workflows/deploy.yaml@main","./.github/workflows/release.yaml@main"]}`),
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
