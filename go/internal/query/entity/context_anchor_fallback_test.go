// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// entityContextUnlabeledAnchor is the pre-#7006 anchor. Any node carrying an
// id property matches it, whatever its label, so it is the exact-answer
// fallback after the fast per-label reads all miss.
const entityContextUnlabeledAnchor = "MATCH (e) WHERE e.id = $entity_id"

// TestGetEntityContextFallsBackToUnlabeledAnchorAfterLabelMisses pins answer
// parity with the pre-#7006 read: the old `MATCH (e) WHERE e.id = $entity_id`
// resolved an id on ANY label, while EntityContextAnchorLabels is a short
// fast-path list. An id whose label is outside that list (a CloudResource, a
// K8sResource, a TerraformResource, ...) must still resolve through a final
// unlabeled read instead of silently answering 404 (#7006 live-backend
// regression).
func TestGetEntityContextFallsBackToUnlabeledAnchorAfterLabelMisses(t *testing.T) {
	t.Parallel()

	var calls []string
	reader := graph.FakeGraphReader{
		RunSingleFn: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
			calls = append(calls, cypher)
			if !strings.Contains(cypher, entityContextUnlabeledAnchor) {
				return nil, nil // every fast-path label misses
			}
			return map[string]any{
				"id":            "cr-1",
				"labels":        []any{"CloudResource"},
				"name":          "cr-1",
				"relationships": []any{},
			}, nil
		},
	}
	handler := &Handler{Neo4j: reader, Profile: querycontract.ProfileLocalAuthoritative}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/cr-1/context", nil)
	req.SetPathValue("entity_id", "cr-1")
	rec := httptest.NewRecorder()

	handler.GetEntityContext(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if got, want := len(calls), len(EntityContextAnchorLabels)+1; got != want {
		t.Fatalf("graph reads = %d, want %d (every fast-path label, then one unlabeled fallback)", got, want)
	}
	if !strings.Contains(calls[len(calls)-1], entityContextUnlabeledAnchor) {
		t.Fatalf("last read = %s, want the unlabeled fallback anchor", calls[len(calls)-1])
	}
	for _, cypher := range calls[:len(calls)-1] {
		if strings.Contains(cypher, entityContextUnlabeledAnchor) {
			t.Fatalf("unlabeled anchor ran before the fast-path labels were exhausted:\n%s", cypher)
		}
	}
}

// TestGetEntityContextRepoIdentityComesFromRepositoryHop pins the columns the
// pre-#7006 read returned: repo_id and repo_name come from the Repository
// that REPO_CONTAINS the entity's File, not from a repo_id property that
// fixtures and older graphs do not carry. The #7006 rewrite read
// coalesce(e.repo_id, f.repo_id) and dropped repo_name, which lost both
// columns on NornicDB and Neo4j and, for a scoped caller, turned an in-grant
// entity into a 404 because its repo_id came back empty.
func TestGetEntityContextRepoIdentityComesFromRepositoryHop(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		scoped bool
	}{
		{name: "unscoped"},
		{name: "scoped", scoped: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var calls []string
			reader := graph.FakeGraphReader{
				RunSingleFn: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
					calls = append(calls, cypher)
					return map[string]any{
						"id":            "fn-1",
						"labels":        []any{"Function"},
						"name":          "Main",
						"file_path":     "a.go",
						"repo_id":       "repo-a",
						"repo_name":     "repo-a-name",
						"relationships": []any{},
					}, nil
				},
			}
			handler := &Handler{Neo4j: reader, Profile: querycontract.ProfileLocalAuthoritative}
			req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/fn-1/context", nil)
			if tc.scoped {
				req = req.WithContext(auth.ContextWithAuthContext(req.Context(), auth.AuthContext{
					Mode:                 auth.AuthModeScoped,
					AllowedRepositoryIDs: []string{"repo-a"},
				}))
			}
			req.SetPathValue("entity_id", "fn-1")
			rec := httptest.NewRecorder()

			handler.GetEntityContext(rec, req)

			if got, want := rec.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
			if len(calls) != 1 {
				t.Fatalf("graph reads = %d, want 1 (the first label hits)", len(calls))
			}
			cypher := calls[0]
			for _, want := range []string{
				"OPTIONAL MATCH (e)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(r:Repository)",
				"r.id as repo_id",
				"r.name as repo_name",
			} {
				if !strings.Contains(cypher, want) {
					t.Fatalf("cypher missing %q:\n%s", want, cypher)
				}
			}
			if strings.Contains(cypher, "e.repo_id") || strings.Contains(cypher, "f.repo_id") {
				t.Fatalf("cypher reads repo_id from a node property instead of the Repository hop:\n%s", cypher)
			}
			access := querycontract.RepositoryAccessFilterFromContext(req.Context())
			if tc.scoped && !strings.Contains(cypher, "WHERE "+access.GraphCondition("r")) {
				t.Fatalf("scoped cypher missing the Repository grant predicate:\n%s", cypher)
			}
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body["repo_id"] != "repo-a" || body["repo_name"] != "repo-a-name" {
				t.Fatalf("repo identity = (%v, %v), want (repo-a, repo-a-name)", body["repo_id"], body["repo_name"])
			}
		})
	}
}
