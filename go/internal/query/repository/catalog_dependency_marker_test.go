// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// TestListCatalogMarksDependencyFromInboundEdgeNoExistsExpression proves
// GET /api/v0/catalog derives each repository's is_dependency the same way
// GET /api/v0/repositories does after issue #6786 defect 1: from the
// bounded, unscoped dependency-edge pre-pass computed in Go
// (loadRepositoryDependencyEdges / repositoryDependencyTargetSet), not a
// per-row EXISTS-as-RETURN-expression. The repository page query's RETURN
// must carry no EXISTS, DEPENDS_ON, or is_dependency text.
func TestListCatalogMarksDependencyFromInboundEdgeNoExistsExpression(t *testing.T) {
	t.Parallel()

	var capturedRepoPageCypher, capturedEdgeCypher string
	reader := querytestutil.FakeRepoGraphReader{
		RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			switch {
			case strings.Contains(cypher, "(s:Repository)-[:DEPENDS_ON]->(t:Repository)"):
				capturedEdgeCypher = cypher
				return []map[string]any{
					{"source_id": "repository:app", "target_id": "repository:lib"},
				}, nil
			case strings.Contains(cypher, "MATCH (r:Repository)"):
				capturedRepoPageCypher = cypher
				return []map[string]any{
					{"id": "repository:lib", "name": "lib"},
					{"id": "repository:app", "name": "app"},
				}, nil
			default:
				return nil, nil
			}
		},
	}

	handler := &Handler{Neo4j: reader, Profile: querycontract.ProfileLocalAuthoritative}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/catalog?limit=10", nil)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.listCatalog(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	if capturedRepoPageCypher == "" {
		t.Fatal("repository page cypher was not captured")
	}
	if capturedEdgeCypher == "" {
		t.Fatal("dependency-edge pre-pass cypher was not issued")
	}
	for _, forbidden := range []string{"EXISTS", "DEPENDS_ON", "is_dependency"} {
		if strings.Contains(capturedRepoPageCypher, forbidden) {
			t.Errorf("repository page cypher still contains %q:\n%s", forbidden, capturedRepoPageCypher)
		}
	}
	if strings.Contains(capturedEdgeCypher, "allowed_repository_ids") {
		t.Errorf("catalog's edge pre-pass must stay unscoped:\n%s", capturedEdgeCypher)
	}

	var envelope querycontract.ResponseEnvelope
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
		markers[querycontract.StringVal(repo, "id")] = querycontract.BoolVal(repo, "is_dependency")
	}
	if !markers["repository:lib"] {
		t.Fatalf("repository:lib is_dependency = false, want true (inbound DEPENDS_ON target)")
	}
	if markers["repository:app"] {
		t.Fatalf("repository:app is_dependency = true, want false (outbound edge only, not a target)")
	}
}
