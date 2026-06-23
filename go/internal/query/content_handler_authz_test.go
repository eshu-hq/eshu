package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func TestContentHandlerScopedSearchFilesUsesAllowedReposWithoutAnyRepoFallback(t *testing.T) {
	t.Parallel()

	store := &recordingContentAuthzStore{
		byFileRepo: map[string][]FileContent{
			"repo-team-a": {{RepoID: "repo-team-a", RelativePath: "src/app.go"}},
		},
	}
	handler := &ContentHandler{Content: store, Profile: ProfileLocalAuthoritative}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/content/files/search",
		bytes.NewBufferString(`{"pattern":"handler","limit":5}`),
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-team-a"},
	}))
	rec := httptest.NewRecorder()

	handler.searchFiles(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if store.anyFileCalls != 0 {
		t.Fatalf("anyFileCalls = %d, want 0", store.anyFileCalls)
	}
	if got, want := store.fileRepoCalls, []string{"repo-team-a"}; !slices.Equal(got, want) {
		t.Fatalf("fileRepoCalls = %#v, want %#v", got, want)
	}
	body := decodeContentAuthzBody(t, rec)
	if got, want := body["count"], float64(1); got != want {
		t.Fatalf("count = %#v, want %#v", got, want)
	}
}

func TestContentHandlerScopedSearchEntitiesEmptyGrantReturnsEmptyWithoutBroadScan(t *testing.T) {
	t.Parallel()

	store := &recordingContentAuthzStore{}
	handler := &ContentHandler{Content: store, Profile: ProfileLocalAuthoritative}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/content/entities/search",
		bytes.NewBufferString(`{"pattern":"handler","limit":5}`),
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:        AuthModeScoped,
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-a",
	}))
	rec := httptest.NewRecorder()

	handler.searchEntities(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if store.anyEntityCalls != 0 || len(store.entityRepoCalls) != 0 {
		t.Fatalf("entity search calls = any:%d repo:%#v, want no calls", store.anyEntityCalls, store.entityRepoCalls)
	}
	body := decodeContentAuthzBody(t, rec)
	if got, want := body["count"], float64(0); got != want {
		t.Fatalf("count = %#v, want %#v", got, want)
	}
	if got := body["truncated"]; got != false {
		t.Fatalf("truncated = %#v, want false", got)
	}
}

func TestContentHandlerAllScopeContentSearchKeepsAnyRepoFallback(t *testing.T) {
	t.Parallel()

	store := &recordingContentAuthzStore{
		anyFiles: []FileContent{{RepoID: "repo-team-a", RelativePath: "src/app.go"}},
	}
	handler := &ContentHandler{Content: store, Profile: ProfileLocalAuthoritative}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/content/files/search",
		bytes.NewBufferString(`{"pattern":"handler","limit":5}`),
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:        AuthModeScoped,
		TenantID:    "tenant-admin",
		WorkspaceID: "workspace-admin",
		AllScopes:   true,
	}))
	rec := httptest.NewRecorder()

	handler.searchFiles(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if store.anyFileCalls != 1 {
		t.Fatalf("anyFileCalls = %d, want 1", store.anyFileCalls)
	}
}

func TestContentHandlerScopedSearchFilesWithStaleGrantReturnsEmpty(t *testing.T) {
	t.Parallel()

	store := &recordingContentAuthzStore{}
	handler := &ContentHandler{Content: store, Profile: ProfileLocalAuthoritative}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/content/files/search",
		bytes.NewBufferString(`{"pattern":"handler","limit":5}`),
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-missing"},
	}))
	rec := httptest.NewRecorder()

	handler.searchFiles(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if store.anyFileCalls != 0 {
		t.Fatalf("anyFileCalls = %d, want 0", store.anyFileCalls)
	}
	if got, want := store.fileRepoCalls, []string{"repo-missing"}; !slices.Equal(got, want) {
		t.Fatalf("fileRepoCalls = %#v, want %#v", got, want)
	}
	body := decodeContentAuthzBody(t, rec)
	if got, want := body["count"], float64(0); got != want {
		t.Fatalf("count = %#v, want %#v", got, want)
	}
}

func TestContentHandlerScopedSearchFilesFiltersDuplicateRepositoryNames(t *testing.T) {
	t.Parallel()

	store := &recordingContentAuthzStore{
		fakePortContentStore: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{
				{ID: "repo-team-a", Name: "payments", RepoSlug: "acme/payments"},
				{ID: "repo-team-b", Name: "payments", RepoSlug: "other/payments"},
			},
		},
		byFileRepo: map[string][]FileContent{
			"repo-team-a": {{RepoID: "repo-team-a", RelativePath: "src/app.go"}},
		},
	}
	handler := &ContentHandler{Content: store, Profile: ProfileLocalAuthoritative}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/content/files/search",
		bytes.NewBufferString(`{"pattern":"handler","repo_id":"payments","limit":5}`),
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-team-a"},
	}))
	rec := httptest.NewRecorder()

	handler.searchFiles(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d after scoped duplicate filtering; body = %s", got, want, rec.Body.String())
	}
	if got, want := store.fileRepoCalls, []string{"repo-team-a"}; !slices.Equal(got, want) {
		t.Fatalf("fileRepoCalls = %#v, want %#v", got, want)
	}
}

func TestContentHandlerScopedSearchEntitiesDeniesOutOfScopeRepositoryList(t *testing.T) {
	t.Parallel()

	store := &recordingContentAuthzStore{
		fakePortContentStore: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{
				{ID: "repo-team-a", Name: "payments", RepoSlug: "acme/payments"},
				{ID: "repo-team-b", Name: "orders", RepoSlug: "acme/orders"},
			},
		},
	}
	handler := &ContentHandler{Content: store, Profile: ProfileLocalAuthoritative}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/content/entities/search",
		bytes.NewBufferString(`{"pattern":"handler","repo_ids":["acme/orders"],"limit":5}`),
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-team-a"},
	}))
	rec := httptest.NewRecorder()

	handler.searchEntities(rec, req)

	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if len(store.entityRepoCalls) != 0 || store.anyEntityCalls != 0 {
		t.Fatalf("entity calls = any:%d repo:%#v, want no search", store.anyEntityCalls, store.entityRepoCalls)
	}
}

func TestContentHandlerScopedReadFileDeniesOutOfScopeSelector(t *testing.T) {
	t.Parallel()

	store := &recordingContentAuthzStore{
		fakePortContentStore: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{
				{ID: "repo-team-a", Name: "payments", RepoSlug: "acme/payments"},
				{ID: "repo-team-b", Name: "orders", RepoSlug: "acme/orders"},
			},
		},
	}
	handler := &ContentHandler{Content: store, Profile: ProfileLocalAuthoritative}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/content/files/read",
		bytes.NewBufferString(`{"repo_id":"acme/orders","relative_path":"src/app.go"}`),
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-team-a"},
	}))
	rec := httptest.NewRecorder()

	handler.readFile(rec, req)

	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if store.fileReadCalls != 0 {
		t.Fatalf("fileReadCalls = %d, want 0", store.fileReadCalls)
	}
}

func TestContentHandlerScopedReadEntityHidesOutOfScopeEntity(t *testing.T) {
	t.Parallel()

	store := &recordingContentAuthzStore{
		entitiesByID: map[string]*EntityContent{
			"entity-b": {EntityID: "entity-b", RepoID: "repo-team-b", RelativePath: "src/app.go"},
		},
	}
	handler := &ContentHandler{Content: store, Profile: ProfileLocalAuthoritative}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/content/entities/read",
		bytes.NewBufferString(`{"entity_id":"entity-b"}`),
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-team-a"},
	}))
	rec := httptest.NewRecorder()

	handler.readEntity(rec, req)

	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if store.entityReadCalls != 0 {
		t.Fatalf("entityReadCalls = %d, want 0 before repository authorization", store.entityReadCalls)
	}
	if got, want := store.entityScopedReads, []entityScopedRead{{entityID: "entity-b", repoIDs: []string{"repo-team-a"}}}; !slices.EqualFunc(got, want, func(a, b entityScopedRead) bool {
		return a.entityID == b.entityID && slices.Equal(a.repoIDs, b.repoIDs)
	}) {
		t.Fatalf("entityScopedReads = %#v, want %#v", got, want)
	}
}

func decodeContentAuthzBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() error = %v; body = %s", err, rec.Body.String())
	}
	return body
}

type recordingContentAuthzStore struct {
	fakePortContentStore
	byFileRepo        map[string][]FileContent
	byEntityRepo      map[string][]EntityContent
	anyFiles          []FileContent
	entitiesByID      map[string]*EntityContent
	fileRepoCalls     []string
	entityRepoCalls   []string
	anyFileCalls      int
	anyEntityCalls    int
	fileReadCalls     int
	entityReadCalls   int
	entityScopedReads []entityScopedRead
}

type entityScopedRead struct {
	entityID string
	repoIDs  []string
}

func (s *recordingContentAuthzStore) GetFileContent(
	ctx context.Context,
	repoID string,
	relativePath string,
) (*FileContent, error) {
	s.fileReadCalls++
	return s.fakePortContentStore.GetFileContent(ctx, repoID, relativePath)
}

func (s *recordingContentAuthzStore) GetEntityContent(
	_ context.Context,
	entityID string,
) (*EntityContent, error) {
	s.entityReadCalls++
	if s.entitiesByID == nil {
		return nil, nil
	}
	return s.entitiesByID[entityID], nil
}

func (s *recordingContentAuthzStore) GetEntityContentInRepositories(
	_ context.Context,
	entityID string,
	repoIDs []string,
) (*EntityContent, error) {
	s.entityScopedReads = append(s.entityScopedReads, entityScopedRead{
		entityID: entityID,
		repoIDs:  append([]string(nil), repoIDs...),
	})
	entity, ok := s.entitiesByID[entityID]
	if !ok || entity == nil {
		return nil, nil
	}
	for _, repoID := range repoIDs {
		if entity.RepoID == repoID {
			return entity, nil
		}
	}
	return nil, nil
}

func (s *recordingContentAuthzStore) SearchFileContent(
	_ context.Context,
	repoID string,
	_ string,
	limit int,
) ([]FileContent, error) {
	s.fileRepoCalls = append(s.fileRepoCalls, repoID)
	return limitFileContentAuthzRows(s.byFileRepo[repoID], limit), nil
}

func (s *recordingContentAuthzStore) SearchFileContentAnyRepo(
	_ context.Context,
	_ string,
	limit int,
) ([]FileContent, error) {
	s.anyFileCalls++
	return limitFileContentAuthzRows(s.anyFiles, limit), nil
}

func (s *recordingContentAuthzStore) SearchEntityContent(
	_ context.Context,
	repoID string,
	_ string,
	limit int,
) ([]EntityContent, error) {
	s.entityRepoCalls = append(s.entityRepoCalls, repoID)
	return limitEntityContentAuthzRows(s.byEntityRepo[repoID], limit), nil
}

func (s *recordingContentAuthzStore) SearchEntityContentAnyRepo(
	context.Context,
	string,
	int,
) ([]EntityContent, error) {
	s.anyEntityCalls++
	return nil, nil
}

func limitFileContentAuthzRows(rows []FileContent, limit int) []FileContent {
	if limit > 0 && limit < len(rows) {
		return append([]FileContent(nil), rows[:limit]...)
	}
	return append([]FileContent(nil), rows...)
}

func limitEntityContentAuthzRows(rows []EntityContent, limit int) []EntityContent {
	if limit > 0 && limit < len(rows) {
		return append([]EntityContent(nil), rows[:limit]...)
	}
	return append([]EntityContent(nil), rows...)
}
