package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListRepositoriesFallsBackToContentCatalogWithoutGraph(t *testing.T) {
	t.Parallel()

	handler := &RepositoryHandler{
		Content: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{
				{
					ID:        "repository:r_local",
					Name:      "eshu",
					Path:      "/repos/eshu",
					LocalPath: "/repos/eshu",
					RemoteURL: "https://github.com/eshu-hq/eshu",
					RepoSlug:  "eshu-hq/eshu",
					HasRemote: true,
				},
			},
		},
		Profile: ProfileLocalLightweight,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	rec := httptest.NewRecorder()

	handler.listRepositories(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}

	repositories, ok := body["repositories"].([]any)
	if !ok || len(repositories) != 1 {
		t.Fatalf("repositories = %#v, want one repository", body["repositories"])
	}
	repo, ok := repositories[0].(map[string]any)
	if !ok {
		t.Fatalf("repositories[0] = %#v, want object", repositories[0])
	}
	if got, want := repo["id"], "repository:r_local"; got != want {
		t.Fatalf("repository id = %#v, want %#v", got, want)
	}
	if got, want := repo["name"], "eshu"; got != want {
		t.Fatalf("repository name = %#v, want %#v", got, want)
	}
}

func TestGetRepositoryCoverageFallsBackToContentCatalogWithoutGraph(t *testing.T) {
	t.Parallel()

	handler := &RepositoryHandler{
		Content: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{
				{
					ID:        "repository:r_local",
					Name:      "eshu",
					LocalPath: "/repos/eshu",
					RepoSlug:  "eshu-hq/eshu",
				},
			},
			coverage: RepositoryContentCoverage{
				Available:   true,
				FileCount:   4,
				EntityCount: 9,
				Languages: []RepositoryLanguageCount{
					{Language: "go", FileCount: 4},
				},
			},
		},
		Profile: ProfileLocalLightweight,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/eshu-hq%2Feshu/coverage", nil)
	req.SetPathValue("repo_id", "eshu-hq/eshu")
	rec := httptest.NewRecorder()

	handler.getRepositoryCoverage(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}

	if got, want := body["repo_id"], "repository:r_local"; got != want {
		t.Fatalf("repo_id = %#v, want %#v", got, want)
	}
	if got, want := body["file_count"], float64(4); got != want {
		t.Fatalf("file_count = %#v, want %#v", got, want)
	}
	if got, want := body["entity_count"], float64(9); got != want {
		t.Fatalf("entity_count = %#v, want %#v", got, want)
	}
}
