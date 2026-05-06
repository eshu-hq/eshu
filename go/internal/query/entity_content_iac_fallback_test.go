package query

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResolveEntityFallsBackToTerraformBlockContentEntity(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"terraform-block-1", "repo-1", "infra/main.tf", "TerraformBlock", "terraform",
					int64(1), int64(8), "hcl", "terraform {}", []byte(`{"required_providers":"aws","required_provider_sources":"aws=hashicorp/aws","required_provider_count":1}`),
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
		bytes.NewBufferString(`{"name":"terraform","type":"terraform_block","repo_id":"repo-1"}`),
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
	if got, want := entity["semantic_summary"], "TerraformBlock terraform requires providers aws."; got != want {
		t.Fatalf("entity[semantic_summary] = %#v, want %#v", got, want)
	}
	metadata, ok := entity["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("entity[metadata] type = %T, want map[string]any", entity["metadata"])
	}
	if got, want := metadata["required_provider_sources"], "aws=hashicorp/aws"; got != want {
		t.Fatalf("entity[metadata][required_provider_sources] = %#v, want %#v", got, want)
	}
}

func TestResolveEntityFallsBackToKustomizeOverlayContentEntity(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"kustomize-overlay-1", "repo-1", "deploy/kustomization.yaml", "KustomizeOverlay", "kustomization",
					int64(1), int64(12), "yaml", "resources:\n- ../base", []byte(`{"bases":["../app","../base"],"patch_targets":["Deployment/comprehensive-app"]}`),
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
		bytes.NewBufferString(`{"name":"kustomization","type":"kustomize_overlay","repo_id":"repo-1"}`),
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
	if got, want := entity["semantic_summary"], "KustomizeOverlay kustomization references bases ../app, ../base and patches Deployment/comprehensive-app."; got != want {
		t.Fatalf("entity[semantic_summary] = %#v, want %#v", got, want)
	}
}

func TestGetEntityContextFallsBackToKustomizeOverlayContentEntity(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"kustomize-overlay-1", "repo-1", "deploy/kustomization.yaml", "KustomizeOverlay", "kustomization",
					int64(1), int64(12), "yaml", "resources:\n- ../base", []byte(`{"bases":["../app","../base"],"patch_targets":["Deployment/comprehensive-app"]}`),
				},
			},
		},
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"k8s-resource-1", "repo-1", "deploy/deployment.yaml", "K8sResource", "comprehensive-app",
					int64(1), int64(18), "yaml", "kind: Deployment", []byte(`{"kind":"Deployment","namespace":"prod","qualified_name":"prod/Deployment/comprehensive-app"}`),
				},
			},
		},
	})

	handler := &EntityHandler{Content: NewContentReader(db)}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/kustomize-overlay-1/context", nil)
	req.SetPathValue("entity_id", "kustomize-overlay-1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := resp["semantic_summary"], "KustomizeOverlay kustomization references bases ../app, ../base and patches Deployment/comprehensive-app."; got != want {
		t.Fatalf("resp[semantic_summary] = %#v, want %#v", got, want)
	}
	relationships, ok := resp["relationships"].([]any)
	if !ok {
		t.Fatalf("resp[relationships] type = %T, want []any", resp["relationships"])
	}
	if len(relationships) != 1 {
		t.Fatalf("len(resp[relationships]) = %d, want 1", len(relationships))
	}
	relationship, ok := relationships[0].(map[string]any)
	if !ok {
		t.Fatalf("resp[relationships][0] type = %T, want map[string]any", relationships[0])
	}
	if got, want := relationship["type"], "PATCHES"; got != want {
		t.Fatalf("relationship[type] = %#v, want %#v", got, want)
	}
	if got, want := relationship["target_name"], "comprehensive-app"; got != want {
		t.Fatalf("relationship[target_name] = %#v, want %#v", got, want)
	}
	if got, want := relationship["reason"], "kustomize_patch_target"; got != want {
		t.Fatalf("relationship[reason] = %#v, want %#v", got, want)
	}
}

func TestGetEntityContextFallsBackToKubernetesResourceContentEntity(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"k8s-resource-1", "repo-1", "deploy/deployment.yaml", "K8sResource", "demo",
					int64(1), int64(18), "yaml", "kind: Deployment", []byte(`{"kind":"Deployment","namespace":"prod","qualified_name":"prod/Deployment/demo","labels":"app=demo,tier=backend"}`),
				},
			},
		},
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"service-1", "repo-1", "deploy/service.yaml", "K8sResource", "demo",
					int64(1), int64(12), "yaml", "kind: Service", []byte(`{"kind":"Service","namespace":"prod","qualified_name":"prod/Service/demo"}`),
				},
				{
					"k8s-resource-1", "repo-1", "deploy/deployment.yaml", "K8sResource", "demo",
					int64(1), int64(18), "yaml", "kind: Deployment", []byte(`{"kind":"Deployment","namespace":"prod","qualified_name":"prod/Deployment/demo","labels":"app=demo,tier=backend"}`),
				},
			},
		},
	})

	handler := &EntityHandler{Content: NewContentReader(db)}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/k8s-resource-1/context", nil)
	req.SetPathValue("entity_id", "k8s-resource-1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := resp["semantic_summary"], "K8sResource demo is identified as prod/Deployment/demo and carries labels app=demo,tier=backend."; got != want {
		t.Fatalf("resp[semantic_summary] = %#v, want %#v", got, want)
	}
	metadata, ok := resp["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("resp[metadata] type = %T, want map[string]any", resp["metadata"])
	}
	if got, want := metadata["qualified_name"], "prod/Deployment/demo"; got != want {
		t.Fatalf("resp[metadata][qualified_name] = %#v, want %#v", got, want)
	}
	relationships, ok := resp["relationships"].([]any)
	if !ok {
		t.Fatalf("resp[relationships] type = %T, want []any", resp["relationships"])
	}
	if len(relationships) != 1 {
		t.Fatalf("len(resp[relationships]) = %d, want 1", len(relationships))
	}
	relationship, ok := relationships[0].(map[string]any)
	if !ok {
		t.Fatalf("resp[relationships][0] type = %T, want map[string]any", relationships[0])
	}
	if got, want := relationship["type"], "SELECTS"; got != want {
		t.Fatalf("relationship[type] = %#v, want %#v", got, want)
	}
	if got, want := relationship["source_name"], "demo"; got != want {
		t.Fatalf("relationship[source_name] = %#v, want %#v", got, want)
	}
	if got, want := relationship["reason"], "k8s_service_name_namespace"; got != want {
		t.Fatalf("relationship[reason] = %#v, want %#v", got, want)
	}
}
