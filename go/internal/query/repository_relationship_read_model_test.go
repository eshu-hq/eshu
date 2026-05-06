package query

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestContentReaderRepositoryRelationshipReadModelHydratesEvidence(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: contentReaderRelationshipReadModelColumns(),
			rows: [][]driver.Value{
				{
					"outgoing", "DEPLOYS_FROM", "repo-service", "service",
					"repo-iac", "terraform-live", "resolved-1", "generation-1",
					float64(0.93), int64(2), "app repo reference", "evidence",
					[]byte(`{"evidence_kinds":["TERRAFORM_APP_REPO"]}`),
				},
				{
					"incoming", "READS_CONFIG_FROM", "repo-consumer", "consumer",
					"repo-service", "service", "resolved-2", "generation-1",
					float64(0.82), int64(1), "application source", "evidence",
					[]byte(`{"evidence_preview":[{"kind":"ARGOCD_APPLICATION_SOURCE"}]}`),
				},
			},
		},
	})

	reader := NewContentReader(db)
	got, err := reader.repositoryRelationshipReadModel(context.Background(), "repo-service")
	if err != nil {
		t.Fatalf("repositoryRelationshipReadModel() error = %v, want nil", err)
	}
	if !got.Available {
		t.Fatal("repositoryRelationshipReadModel().Available = false, want true")
	}
	if len(got.Relationships) != 2 {
		t.Fatalf("len(Relationships) = %d, want 2", len(got.Relationships))
	}
	if got, want := StringVal(got.Relationships[0], "evidence_type"), "terraform_app_repo"; got != want {
		t.Fatalf("Relationships[0].evidence_type = %q, want %q", got, want)
	}
	if got, want := StringSliceVal(got.Relationships[0], "evidence_kinds"), []string{"TERRAFORM_APP_REPO"}; !stringSlicesEqual(got, want) {
		t.Fatalf("Relationships[0].evidence_kinds = %#v, want %#v", got, want)
	}
	if got, want := StringVal(got.Relationships[1], "evidence_type"), "argocd_application_source"; got != want {
		t.Fatalf("Relationships[1].evidence_type = %q, want %q", got, want)
	}
	if len(got.Consumers) != 1 {
		t.Fatalf("len(Consumers) = %d, want 1", len(got.Consumers))
	}
	if got, want := StringVal(got.Consumers[0], "name"), "consumer"; got != want {
		t.Fatalf("Consumers[0].name = %q, want %q", got, want)
	}
}

func TestGetRepositoryContextUsesReadModelForRelationshipsAndConsumers(t *testing.T) {
	t.Parallel()

	reader := fakeRepoGraphReader{
		runSingle: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
			if !strings.Contains(cypher, "MATCH (r:Repository {id: $repo_id})") {
				t.Fatalf("RunSingle cypher = %q, want repository base lookup", cypher)
			}
			return map[string]any{
				"id":         "repo-read-model",
				"name":       "read-model-service",
				"path":       "/repos/read-model-service",
				"local_path": "/repos/read-model-service",
				"has_remote": false,
			}, nil
		},
		run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			for _, forbidden := range []string{
				"MATCH (r:Repository {id: $repo_id})-[rel:DEPENDS_ON|USES_MODULE|DEPLOYS_FROM|DISCOVERS_CONFIG_IN|PROVISIONS_DEPENDENCY_FOR|READS_CONFIG_FROM|RUNS_ON]->(target:Repository)",
				"MATCH (source:Repository)-[rel:",
				"MATCH (consumer:Repository)-[rel:",
			} {
				if strings.Contains(cypher, forbidden) {
					t.Fatalf("cypher = %q, want read-model relationship rows instead of graph relationship fanout", cypher)
				}
			}
			return nil, nil
		},
	}
	handler := &RepositoryHandler{
		Neo4j: reader,
		Content: fakePortContentStore{
			summary: repositoryReadModelSummary{
				Available:       true,
				WorkloadNames:   []string{"read-model-service"},
				PlatformCount:   1,
				DependencyCount: 1,
			},
			relationshipReadModel: repositoryRelationshipReadModel{
				Available: true,
				Relationships: []map[string]any{
					{
						"direction":         "outgoing",
						"type":              "DEPLOYS_FROM",
						"source_name":       "read-model-service",
						"source_id":         "repo-read-model",
						"target_name":       "terraform-live",
						"target_id":         "repo-terraform",
						"evidence_type":     "terraform_app_repo",
						"resolved_id":       "resolved-out",
						"generation_id":     "generation-1",
						"confidence":        0.91,
						"evidence_count":    2,
						"evidence_kinds":    []string{"TERRAFORM_APP_REPO"},
						"resolution_source": "evidence",
						"rationale":         "app repository reference",
					},
					{
						"direction":      "incoming",
						"type":           "READS_CONFIG_FROM",
						"source_name":    "consumer-service",
						"source_id":      "repo-consumer",
						"target_name":    "read-model-service",
						"target_id":      "repo-read-model",
						"evidence_type":  "argocd_application_source",
						"resolved_id":    "resolved-in",
						"generation_id":  "generation-1",
						"confidence":     0.82,
						"evidence_count": 1,
					},
				},
				Consumers: []map[string]any{
					{"name": "consumer-service", "id": "repo-consumer"},
				},
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-read-model/context", nil)
	req.SetPathValue("repo_id", "repo-read-model")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	relationships, ok := resp["relationships"].([]any)
	if !ok || len(relationships) != 1 {
		t.Fatalf("relationships = %#v, want one outgoing read-model row", resp["relationships"])
	}
	relationship, ok := relationships[0].(map[string]any)
	if !ok {
		t.Fatalf("relationships[0] = %#v, want map", relationships[0])
	}
	if got, want := relationship["target_name"], "terraform-live"; got != want {
		t.Fatalf("relationships[0].target_name = %#v, want %#v", got, want)
	}
	if got, want := relationship["evidence_type"], "terraform_app_repo"; got != want {
		t.Fatalf("relationships[0].evidence_type = %#v, want %#v", got, want)
	}
	overview, ok := resp["relationship_overview"].(map[string]any)
	if !ok {
		t.Fatalf("relationship_overview = %#v, want map", resp["relationship_overview"])
	}
	if got, want := overview["relationship_count"], float64(2); got != want {
		t.Fatalf("relationship_count = %#v, want %#v", got, want)
	}
	rows, ok := overview["relationships"].([]any)
	if !ok || len(rows) != 2 {
		t.Fatalf("relationship_overview.relationships = %#v, want two rows", overview["relationships"])
	}
	consumers, ok := resp["consumers"].([]any)
	if !ok || len(consumers) != 1 {
		t.Fatalf("consumers = %#v, want one read-model consumer", resp["consumers"])
	}
	consumer, ok := consumers[0].(map[string]any)
	if !ok {
		t.Fatalf("consumers[0] = %#v, want map", consumers[0])
	}
	if got, want := consumer["name"], "consumer-service"; got != want {
		t.Fatalf("consumers[0].name = %#v, want %#v", got, want)
	}
}
