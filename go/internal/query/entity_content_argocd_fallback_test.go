package query

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResolveEntityFallsBackToArgoCDApplicationContentEntity(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"argocd-app-1", "repo-1", "argocd/payments.yaml", "ArgoCDApplication", "payments-app",
					int64(1), int64(20), "yaml", "kind: Application", []byte(`{"source_repo":"https://github.com/myorg/payments-service.git","source_path":"deploy/overlays/prod","dest_server":"https://kubernetes.default.svc","dest_namespace":"payments","sync_policy":"automated(prune=true,selfHeal=true)"}`),
				},
			},
		},
	})

	handler := &EntityHandler{Content: NewContentReader(db)}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/entities/resolve",
		bytes.NewBufferString(`{"name":"payments-app","type":"argocd_application","repo_id":"repo-1"}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	entities, ok := resp["entities"].([]any)
	if !ok || len(entities) != 1 {
		t.Fatalf("entities = %#v, want one content-backed entity", resp["entities"])
	}
	entity, ok := entities[0].(map[string]any)
	if !ok {
		t.Fatalf("entity type = %T, want map[string]any", entities[0])
	}
	if got, want := entity["semantic_summary"], "ArgoCDApplication payments-app deploys from https://github.com/myorg/payments-service.git at deploy/overlays/prod and targets https://kubernetes.default.svc namespace payments."; got != want {
		t.Fatalf("entity[semantic_summary] = %#v, want %#v", got, want)
	}
}

func TestGetEntityContextFallsBackToArgoCDApplicationSetContentEntity(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"argocd-appset-1", "repo-1", "argocd/applicationset.yaml", "ArgoCDApplicationSet", "platform-appset",
					int64(1), int64(24), "yaml", "kind: ApplicationSet", []byte(`{"generator_source_repos":"https://github.com/myorg/platform-config.git","generator_source_paths":"argocd/platform/*/config.yaml","template_source_repos":"https://github.com/myorg/platform-runtime.git","template_source_paths":"deploy/overlays/prod","dest_server":"https://kubernetes.default.svc","dest_namespace":"platform"}`),
				},
			},
		},
	})

	handler := &EntityHandler{Content: NewContentReader(db)}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/argocd-appset-1/context", nil)
	req.SetPathValue("entity_id", "argocd-appset-1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := resp["semantic_summary"], "ArgoCDApplicationSet platform-appset discovers config in https://github.com/myorg/platform-config.git, deploys templates from https://github.com/myorg/platform-runtime.git, and targets https://kubernetes.default.svc namespace platform."; got != want {
		t.Fatalf("resp[semantic_summary] = %#v, want %#v", got, want)
	}
	relationships, ok := resp["relationships"].([]any)
	if !ok {
		t.Fatalf("resp[relationships] type = %T, want []any", resp["relationships"])
	}
	if len(relationships) != 3 {
		t.Fatalf("len(resp[relationships]) = %d, want 3", len(relationships))
	}
	first, ok := relationships[0].(map[string]any)
	if !ok {
		t.Fatalf("resp[relationships][0] type = %T, want map[string]any", relationships[0])
	}
	if got, want := first["type"], "DISCOVERS_CONFIG_IN"; got != want {
		t.Fatalf("relationships[0][type] = %#v, want %#v", got, want)
	}
	if got, want := first["target_name"], "https://github.com/myorg/platform-config.git"; got != want {
		t.Fatalf("relationships[0][target_name] = %#v, want %#v", got, want)
	}
}
