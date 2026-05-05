package query

import (
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetEntityContextFallsBackToKustomizeOverlayTypedDeploySources(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"kustomize-overlay-typed-1", "repo-1", "deploy/kustomization.yaml", "KustomizeOverlay", "kustomization",
					int64(1), int64(20), "yaml", "kind: Kustomization", []byte(`{"resource_refs":"https://github.com/myorg/shared-manifests.git//payments?ref=main,shared/component","helm_refs":"https://charts.bitnami.com/bitnami,ingress-nginx,nginx","image_refs":"ghcr.io/example/nginx,nginx"}`),
				},
			},
		},
	})

	handler := &EntityHandler{Content: NewContentReader(db)}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/kustomize-overlay-typed-1/context", nil)
	req.SetPathValue("entity_id", "kustomize-overlay-typed-1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := resp["semantic_summary"], "KustomizeOverlay kustomization deploys from https://github.com/myorg/shared-manifests.git//payments?ref=main, shared/component, https://charts.bitnami.com/bitnami, ingress-nginx, nginx, ghcr.io/example/nginx, nginx."; got != want {
		t.Fatalf("resp[semantic_summary] = %#v, want %#v", got, want)
	}

	relationships, ok := resp["relationships"].([]any)
	if !ok {
		t.Fatalf("resp[relationships] type = %T, want []any", resp["relationships"])
	}
	if len(relationships) != 7 {
		t.Fatalf("len(resp[relationships]) = %d, want 7", len(relationships))
	}

	first, ok := relationships[0].(map[string]any)
	if !ok {
		t.Fatalf("resp[relationships][0] type = %T, want map[string]any", relationships[0])
	}
	if got, want := first["reason"], "kustomize_resource_reference"; got != want {
		t.Fatalf("relationships[0][reason] = %#v, want %#v", got, want)
	}
}
