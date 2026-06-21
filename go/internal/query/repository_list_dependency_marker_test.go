package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestListRepositoriesScopedDependencyMarkerFiltersDepender proves that the
// EXISTS subquery used to derive is_dependency applies the caller's tenant
// access predicate to the depending Repository node (the one that holds the
// outbound DEPENDS_ON edge). A scoped caller must not learn dependency-marker
// truth about in-scope repositories based on depending nodes that are outside
// their grant.
func TestListRepositoriesScopedDependencyMarkerFiltersDepender(t *testing.T) {
	t.Parallel()

	var capturedListCypher string
	reader := fakeRepoGraphReader{
		run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
			if strings.Contains(cypher, "MATCH (r:Repository)") && strings.Contains(cypher, "DEPENDS_ON") {
				capturedListCypher = cypher
				return []map[string]any{
					{"id": "repository:lib", "name": "lib", "is_dependency": true},
				}, nil
			}
			// COUNT query for total
			return []map[string]any{{"total": 1}}, nil
		},
	}

	handler := &RepositoryHandler{Neo4j: reader, Profile: ProfileLocalAuthoritative}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories?limit=10", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		SubjectClass:         "team",
		SubjectIDHash:        "sha256:team-a",
		PolicyRevisionHash:   "sha256:policy",
		AllowedRepositoryIDs: []string{"repository:lib"},
	}))
	rec := httptest.NewRecorder()

	handler.listRepositories(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}

	// The EXISTS inner MATCH must scope the depending node to the caller's grant.
	// It must contain the $allowed_repository_ids predicate inside the EXISTS block.
	if capturedListCypher == "" {
		t.Fatal("list cypher was not captured; check that the fake matched the MATCH (r:Repository) + DEPENDS_ON query")
	}
	existsIdx := strings.Index(capturedListCypher, "EXISTS")
	if existsIdx < 0 {
		t.Fatalf("list cypher has no EXISTS block:\n%s", capturedListCypher)
	}
	existsBlock := capturedListCypher[existsIdx:]
	if !strings.Contains(existsBlock, "allowed_repository_ids") {
		t.Fatalf("EXISTS block does not scope the depending repo to $allowed_repository_ids — scoped callers can see markers from out-of-scope dependers:\n%s", capturedListCypher)
	}
}

// TestListRepositoriesMarksDependencyFromInboundEdge proves the repositories
// list query derives is_dependency from an inbound DEPENDS_ON edge rather than
// reading a Repository node property that no writer populates. A "dependency
// repo" is a repository other repositories depend on, i.e. the target of an
// admitted (:Repository)-[:DEPENDS_ON]->(:Repository) edge.
func TestListRepositoriesMarksDependencyFromInboundEdge(t *testing.T) {
	t.Parallel()

	var capturedListCypher string
	reader := fakeRepoGraphReader{
		run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			if strings.Contains(cypher, "MATCH (r:Repository)") {
				capturedListCypher = cypher
				return []map[string]any{
					{"id": "repository:lib", "name": "lib", "is_dependency": true},
					{"id": "repository:app", "name": "app", "is_dependency": false},
				}, nil
			}
			return nil, nil
		},
	}

	handler := &RepositoryHandler{Neo4j: reader, Profile: ProfileLocalAuthoritative}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories?limit=10", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.listRepositories(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}

	// The dependency marker must come from an inbound DEPENDS_ON edge existence
	// check, not from the phantom Repository node property.
	if strings.Contains(capturedListCypher, "coalesce(r.is_dependency") {
		t.Fatalf("list cypher still reads phantom r.is_dependency node property:\n%s", capturedListCypher)
	}
	if !strings.Contains(capturedListCypher, "<-[:DEPENDS_ON]-") {
		t.Fatalf("list cypher does not derive is_dependency from inbound DEPENDS_ON edge:\n%s", capturedListCypher)
	}
	if !strings.Contains(capturedListCypher, "as is_dependency") {
		t.Fatalf("list cypher does not alias the dependency marker as is_dependency:\n%s", capturedListCypher)
	}

	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want map", envelope.Data)
	}
	repositories, ok := data["repositories"].([]any)
	if !ok || len(repositories) != 2 {
		t.Fatalf("repositories = %#v, want two rows", data["repositories"])
	}

	markers := map[string]bool{}
	for _, raw := range repositories {
		repo, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("repository row type = %T, want map", raw)
		}
		markers[StringVal(repo, "id")] = BoolVal(repo, "is_dependency")
	}
	if !markers["repository:lib"] {
		t.Fatalf("repository:lib is_dependency = false, want true (inbound DEPENDS_ON target)")
	}
	if markers["repository:app"] {
		t.Fatalf("repository:app is_dependency = true, want false (no inbound DEPENDS_ON)")
	}
}
