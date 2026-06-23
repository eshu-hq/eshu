package query

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func TestEvidenceHandlerScopedCitationPacketEmptyGrantDoesNotHydrate(t *testing.T) {
	t.Parallel()

	store := &citationPacketContentStore{
		files: map[evidenceCitationFileKey]FileContent{
			{repoID: "repo-team-a", relativePath: "README.md"}: {
				RepoID: "repo-team-a", RelativePath: "README.md", Content: "allowed",
			},
		},
		entities: map[string]*EntityContent{
			"entity-a": {EntityID: "entity-a", RepoID: "repo-team-a", SourceCache: "allowed"},
		},
	}
	handler := &EvidenceHandler{Content: store, Profile: ProfileLocalAuthoritative}
	rec := httptest.NewRecorder()
	req := evidenceCitationAuthzRequest(t, AuthContext{
		Mode:        AuthModeScoped,
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-a",
	})

	handler.buildEvidenceCitations(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if store.fileBatchCalls != 0 || store.entityBatchCalls != 0 {
		t.Fatalf("batch calls = file:%d entity:%d, want no hydration", store.fileBatchCalls, store.entityBatchCalls)
	}
	assertCitationAuthzCoverage(t, rec, 0, 2)
}

func TestEvidenceHandlerScopedCitationPacketFiltersOutOfScopeHandles(t *testing.T) {
	t.Parallel()

	store := &citationPacketContentStore{
		files: map[evidenceCitationFileKey]FileContent{
			{repoID: "repo-team-b", relativePath: "README.md"}: {
				RepoID: "repo-team-b", RelativePath: "README.md", Content: "blocked",
			},
		},
		entities: map[string]*EntityContent{
			"entity-b": {EntityID: "entity-b", RepoID: "repo-team-b", SourceCache: "blocked"},
		},
	}
	handler := &EvidenceHandler{Content: store, Profile: ProfileLocalAuthoritative}
	rec := httptest.NewRecorder()
	req := evidenceCitationAuthzRequest(t, AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-team-a"},
	})

	handler.buildEvidenceCitations(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if store.fileBatchCalls != 0 {
		t.Fatalf("fileBatchCalls = %d, want 0 for out-of-scope file handle", store.fileBatchCalls)
	}
	if store.entityBatchCalls != 0 {
		t.Fatalf("entityBatchCalls = %d, want 0 before repository authorization", store.entityBatchCalls)
	}
	if got, want := store.entityScopedBatchCalls, 1; got != want {
		t.Fatalf("entityScopedBatchCalls = %d, want %d", got, want)
	}
	if got, want := store.entityScopedBatchRepoIDs, []string{"repo-team-a"}; !slices.Equal(got, want) {
		t.Fatalf("entityScopedBatchRepoIDs = %#v, want %#v", got, want)
	}
	assertCitationAuthzCoverage(t, rec, 0, 2)
}

func TestEvidenceHandlerAllScopeCitationPacketKeepsHydration(t *testing.T) {
	t.Parallel()

	store := &citationPacketContentStore{
		files: map[evidenceCitationFileKey]FileContent{
			{repoID: "repo-team-b", relativePath: "README.md"}: {
				RepoID: "repo-team-b", RelativePath: "README.md", Content: "line one",
			},
		},
		entities: map[string]*EntityContent{
			"entity-b": {EntityID: "entity-b", RepoID: "repo-team-b", SourceCache: "func handler() {}"},
		},
	}
	handler := &EvidenceHandler{Content: store, Profile: ProfileLocalAuthoritative}
	rec := httptest.NewRecorder()
	req := evidenceCitationAuthzRequest(t, AuthContext{
		Mode:        AuthModeScoped,
		TenantID:    "tenant-admin",
		WorkspaceID: "workspace-admin",
		AllScopes:   true,
	})

	handler.buildEvidenceCitations(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if store.fileBatchCalls != 1 || store.entityBatchCalls != 1 {
		t.Fatalf("batch calls = file:%d entity:%d, want both hydration paths", store.fileBatchCalls, store.entityBatchCalls)
	}
	assertCitationAuthzCoverage(t, rec, 2, 0)
}

func evidenceCitationAuthzRequest(t *testing.T, auth AuthContext) *http.Request {
	t.Helper()
	body := map[string]any{
		"handles": []map[string]any{
			{"kind": "file", "repo_id": "repo-team-b", "relative_path": "README.md"},
			{"kind": "entity", "entity_id": "entity-b"},
		},
		"limit": 10,
	}
	reqBody, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v, want nil", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/evidence/citations", bytes.NewReader(reqBody))
	req.Header.Set("Accept", EnvelopeMIMEType)
	return req.WithContext(ContextWithAuthContext(req.Context(), auth))
}

func assertCitationAuthzCoverage(t *testing.T, rec *httptest.ResponseRecorder, resolved int, missing int) {
	t.Helper()
	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v; body = %s", err, rec.Body.String())
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope.Data type = %T, want map", envelope.Data)
	}
	coverage := data["coverage"].(map[string]any)
	if got, want := coverage["resolved_count"], float64(resolved); got != want {
		t.Fatalf("coverage.resolved_count = %#v, want %#v", got, want)
	}
	if got, want := coverage["missing_count"], float64(missing); got != want {
		t.Fatalf("coverage.missing_count = %#v, want %#v", got, want)
	}
}
