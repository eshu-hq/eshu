package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListRepositoriesReturnsBoundedEnvelopeFromContentCatalog(t *testing.T) {
	t.Parallel()

	handler := &RepositoryHandler{
		Content: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{
				{ID: "repository:one", Name: "one"},
				{ID: "repository:two", Name: "two"},
				{ID: "repository:three", Name: "three"},
			},
		},
		Profile: ProfileLocalAuthoritative,
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories?limit=2", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.listRepositories(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want map", envelope.Data)
	}
	repositories, ok := data["repositories"].([]any)
	if !ok || len(repositories) != 2 {
		t.Fatalf("repositories = %#v, want two rows", data["repositories"])
	}
	if got, want := data["limit"], float64(2); got != want {
		t.Fatalf("limit = %#v, want %#v", got, want)
	}
	if got, want := data["truncated"], true; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
	if envelope.Truth == nil || envelope.Truth.Capability != "platform_impact.context_overview" {
		t.Fatalf("truth = %#v, want context overview truth", envelope.Truth)
	}
}
