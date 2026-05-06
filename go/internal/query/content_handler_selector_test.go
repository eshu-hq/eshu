package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

type selectorAwareContentStore struct {
	fakePortContentStore
	fileRepoID          string
	fileLinesRepoID     string
	searchFileRepoIDs   []string
	searchEntityRepoIDs []string
}

func (s *selectorAwareContentStore) GetFileContent(_ context.Context, repoID, relativePath string) (*FileContent, error) {
	s.fileRepoID = repoID
	return &FileContent{RepoID: repoID, RelativePath: relativePath}, nil
}

func (s *selectorAwareContentStore) GetFileLines(_ context.Context, repoID, relativePath string, startLine, endLine int) (*FileContent, error) {
	s.fileLinesRepoID = repoID
	return &FileContent{RepoID: repoID, RelativePath: relativePath, Content: "selected"}, nil
}

func (s *selectorAwareContentStore) SearchFileContent(_ context.Context, repoID, pattern string, limit int) ([]FileContent, error) {
	s.searchFileRepoIDs = append(s.searchFileRepoIDs, repoID)
	return []FileContent{{RepoID: repoID, RelativePath: "src/app.go"}}, nil
}

func (s *selectorAwareContentStore) SearchEntityContent(_ context.Context, repoID, pattern string, limit int) ([]EntityContent, error) {
	s.searchEntityRepoIDs = append(s.searchEntityRepoIDs, repoID)
	return []EntityContent{{RepoID: repoID, RelativePath: "src/app.go", EntityName: "handler"}}, nil
}

func TestContentHandlerReadFileResolvesRepositorySelectorAlias(t *testing.T) {
	t.Parallel()

	store := &selectorAwareContentStore{
		fakePortContentStore: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{{
				ID:        "repo-1",
				Name:      "payments",
				LocalPath: "/repos/payments",
				RepoSlug:  "acme/payments",
			}},
		},
	}
	handler := &ContentHandler{Content: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/content/files/read",
		bytes.NewBufferString(`{"repo_id":"acme/payments","relative_path":"src/app.go"}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if got, want := store.fileRepoID, "repo-1"; got != want {
		t.Fatalf("GetFileContent repo_id = %q, want %q", got, want)
	}
}

func TestContentHandlerSearchFilesResolvesRepositorySelectorAliases(t *testing.T) {
	t.Parallel()

	store := &selectorAwareContentStore{
		fakePortContentStore: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{
				{ID: "repo-1", Name: "payments", RepoSlug: "acme/payments", LocalPath: "/repos/payments"},
				{ID: "repo-2", Name: "orders", RepoSlug: "acme/orders", LocalPath: "/repos/orders"},
			},
		},
	}
	handler := &ContentHandler{Content: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/content/files/search",
		bytes.NewBufferString(`{"pattern":"renderApp","repo_ids":["acme/payments","/repos/orders"],"limit":10}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if got, want := store.searchFileRepoIDs, []string{"repo-1", "repo-2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SearchFileContent repo_ids = %#v, want %#v", got, want)
	}
}

func TestContentHandlerSearchEntitiesResolvesRepositorySelectorAlias(t *testing.T) {
	t.Parallel()

	store := &selectorAwareContentStore{
		fakePortContentStore: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{{
				ID:        "repo-1",
				Name:      "payments",
				LocalPath: "/repos/payments",
				RepoSlug:  "acme/payments",
			}},
		},
	}
	handler := &ContentHandler{Content: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/content/entities/search",
		bytes.NewBufferString(`{"pattern":"handler","repo_id":"/repos/payments","limit":10}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if got, want := store.searchEntityRepoIDs, []string{"repo-1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SearchEntityContent repo_ids = %#v, want %#v", got, want)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := int(resp["count"].(float64)), 1; got != want {
		t.Fatalf("response count = %d, want %d", got, want)
	}
}
