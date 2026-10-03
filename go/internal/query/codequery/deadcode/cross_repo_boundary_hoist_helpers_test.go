// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/deadcode"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// boundaryRelationship is one incoming repository-level relationship, the shape
// the repository relationship read model feeds the boundary fallback.
func boundaryRelationship(sourceID string) map[string]any {
	return map[string]any{
		"direction":         "incoming",
		"type":              "DEPENDS_ON",
		"source_id":         sourceID,
		"source_name":       sourceID + "-name",
		"target_id":         "repo-producer",
		"resolved_id":       "resolved-" + sourceID,
		"generation_id":     "relationship-generation-1",
		"confidence":        0.91,
		"resolution_source": "evidence",
	}
}

func boundaryHoistEntity(entityID string) deadcode.EntityContent {
	return deadcode.EntityContent{
		EntityID:     entityID,
		RepoID:       "repo-producer",
		RelativePath: "pkg/payments/" + entityID + ".go",
		EntityType:   "Function",
		EntityName:   entityID,
		Language:     "go",
		SourceCache:  "func " + entityID + "() {}",
	}
}

// boundaryHoistStore builds a producer with one entity that has its own
// consumer evidence, two with none, and the given incoming boundary
// relationships, so one request exercises the entity path and the fallback.
func boundaryHoistStore(relationships []map[string]any) *crossRepoDeadCodeContentStore {
	entities := map[string]deadcode.EntityContent{}
	rows := []map[string]any{}
	for i, id := range []string{"p-entity", "p-boundary-a", "p-boundary-b"} {
		entities[id] = boundaryHoistEntity(id)
		rows = append(rows, deadCodeInvestigationRow(id, id, "go", "pkg/payments/"+id+".go", 10+i, 12+i))
	}
	store := &crossRepoDeadCodeContentStore{
		fakeDeadCodeContentStore: fakeDeadCodeContentStore{
			FakePortContentStore: content.FakePortContentStore{
				Repositories: []querycontract.RepositoryCatalogEntry{
					{ID: "repo-producer", Name: "payments-lib"},
					{ID: "consumer-1", Name: "consumer-1-name"},
					{ID: "consumer-2", Name: "consumer-2-name"},
				},
			},
			entities: entities,
		},
		rows: rows,
		evidenceByEntity: map[string][]deadcode.CrossRepoDeadCodeEvidence{
			"p-entity": {{
				ConsumerRepoID:   "consumer-1",
				ConsumerEntityID: "consumer-root",
				RelationshipType: "CALLS",
				EvidenceFamily:   "direct_code",
				Citation:         "code_reachability_rows:scope-a/gen-a/consumer-1/consumer-root/p-entity",
				Confidence:       codeprovenance.Confidence(codeprovenance.MethodImportBinding),
				ConfidenceLabel:  "high",
				ResolutionMethod: codeprovenance.MethodImportBinding,
				GenerationID:     "gen-a",
				GenerationStatus: "active",
			}},
		},
	}
	if len(relationships) > 0 {
		store.RelationshipReadModel = querycontract.RepositoryRelationshipReadModel{
			Available:     true,
			Relationships: relationships,
		}
	}
	return store
}

// postBoundaryHoistRequest runs the real cross-repo handler. A nil allowed list
// is an unscoped caller; otherwise the caller's grant is exactly that list.
func postBoundaryHoistRequest(
	t *testing.T,
	store *crossRepoDeadCodeContentStore,
	body string,
	allowed []string,
) map[string]any {
	t.Helper()

	handler := &codequery.CodeHandler{Profile: querycontract.ProfileLocalAuthoritative, Content: store, Neo4j: graph.FakeGraphReader{}}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/dead-code/cross-repo", bytes.NewBufferString(body))
	if allowed != nil {
		req = req.WithContext(auth.ContextWithAuthContext(req.Context(), auth.AuthContext{
			Mode:                 auth.AuthModeScoped,
			AllowedRepositoryIDs: allowed,
		}))
	}
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", w.Code, w.Body.String())
	}
	return testutil.DecodeEnvelopeData(t, w.Body.Bytes())
}
