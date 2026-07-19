// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

type queryplanCapturedRun struct {
	cypher string
	params map[string]any
}

func TestResolveEntityExecutesBuilderBytes(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		auth       *AuthContext
		req        resolveEntityRequest
		wantSHA256 string
	}{
		{
			name:       "repository anchored",
			body:       `{"name":"proof","repo_id":"repository:r_proof","limit":10}`,
			req:        resolveEntityRequest{Name: "proof", RepoID: "repository:r_proof", Limit: 10},
			wantSHA256: "ee51f2a612461a5e5bba6d28fc1e11e0259f44cd653819ca41380cee8985afc2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var captured []queryplanCapturedRun
			graph := &captureGraphQuery{runFn: func(
				_ context.Context,
				cypher string,
				params map[string]any,
			) ([]map[string]any, error) {
				captured = append(captured, queryplanCapturedRun{cypher: cypher, params: params})
				return nil, nil
			}}
			handler := &EntityHandler{Neo4j: graph}
			request := httptest.NewRequest(http.MethodPost, "/api/v0/entities/resolve", bytes.NewBufferString(tt.body))
			if tt.auth != nil {
				request = request.WithContext(ContextWithAuthContext(request.Context(), *tt.auth))
			}
			response := httptest.NewRecorder()

			handler.resolveEntity(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d body=%s", response.Code, http.StatusOK, response.Body.String())
			}
			if len(captured) != 1 {
				t.Fatalf("graph calls = %d, want 1", len(captured))
			}
			access := repositoryAccessFilterFromContext(request.Context())
			wantCypher, wantParams := buildResolveEntityGraphQuery(tt.req, normalizeResolveEntityLimit(tt.req.Limit), access)
			assertQueryplanCapturedRun(t, captured[0], wantCypher, wantParams)
			assertQueryplanBaselineSHA256(t, captured[0].cypher, tt.wantSHA256)
		})
	}
}

func TestSearchGraphEntitiesExecutesBuilderBytes(t *testing.T) {
	tests := []struct {
		name       string
		repoID     string
		language   string
		exact      bool
		auth       *AuthContext
		wantSHA256 string
	}{
		{
			name: "repository anchored", repoID: "repository:r_proof", exact: true,
			wantSHA256: "428464ccf4de18918b814cf137ad4bb330f1bfda643801bf29ffa0ad593e59f3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var captured queryplanCapturedRun
			graph := &captureGraphQuery{runFn: func(
				_ context.Context,
				cypher string,
				params map[string]any,
			) ([]map[string]any, error) {
				captured = queryplanCapturedRun{cypher: cypher, params: params}
				return nil, nil
			}}
			handler := &CodeHandler{Neo4j: graph}
			ctx := context.Background()
			if tt.auth != nil {
				ctx = ContextWithAuthContext(ctx, *tt.auth)
			}

			if _, err := handler.searchGraphEntitiesWithExact(ctx, tt.repoID, "proof", tt.language, 10, tt.exact); err != nil {
				t.Fatalf("searchGraphEntitiesWithExact() error = %v", err)
			}
			access := repositoryAccessFilterFromContext(ctx)
			wantCypher, wantParams := buildSearchGraphEntitiesQuery(tt.repoID, "proof", tt.language, 10, tt.exact, access)
			assertQueryplanCapturedRun(t, captured, wantCypher, wantParams)
			assertQueryplanBaselineSHA256(t, captured.cypher, tt.wantSHA256)
		})
	}
}

func TestResolveWorkloadEntitiesExecutesBuilderBytes(t *testing.T) {
	tests := []struct {
		name                   string
		repoID                 string
		auth                   *AuthContext
		wantPropertySHA256     string
		wantRelationshipSHA256 string
	}{
		{
			name:                   "repository anchored",
			repoID:                 "repository:r_proof",
			wantPropertySHA256:     "72a03ec3f654f82a76641fc7126a30be39b2b6d36c4ac3a74bd55c4726a92ae1",
			wantRelationshipSHA256: "230a12016a8d10d5063cd2c38a4205378b900ec6952d97ccfe7ebba6ccbcde10",
		},
		{
			name:                   "all scopes",
			wantPropertySHA256:     "488098fce2bffef4913aff9b0b73cc276a4cda6a6d1b512604b3d34ef726d2b1",
			wantRelationshipSHA256: "c049b400c72bc558538c8dc12127b39fb80fdd0d4556ddf8902fcfe5a77813f3",
		},
		{
			name: "scoped",
			auth: &AuthContext{
				Mode:                 AuthModeScoped,
				AllowedRepositoryIDs: []string{"repository:r_proof"},
			},
			wantPropertySHA256:     "e3568d5b2c0ef13d18edef8242b2a3fe23c96a8803f0d7c914475e2d4ad8e3e0",
			wantRelationshipSHA256: "336ad00b7a3a6a0396b4b8b20a11a03d1f102360d4102e180466a3e034634ea9",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var captured []queryplanCapturedRun
			graph := &captureGraphQuery{runFn: func(
				_ context.Context,
				cypher string,
				params map[string]any,
			) ([]map[string]any, error) {
				captured = append(captured, queryplanCapturedRun{cypher: cypher, params: params})
				return nil, nil
			}}
			handler := &EntityHandler{Neo4j: graph}
			ctx := context.Background()
			if tt.auth != nil {
				ctx = ContextWithAuthContext(ctx, *tt.auth)
			}

			if _, err := handler.resolveWorkloadEntities(ctx, "proof", tt.repoID, 10); err != nil {
				t.Fatalf("resolveWorkloadEntities() error = %v", err)
			}
			if len(captured) != 2 {
				t.Fatalf("graph calls = %d, want 2", len(captured))
			}
			access := repositoryAccessFilterFromContext(ctx)
			propertyCypher, relationshipCypher, wantParams := buildResolveWorkloadQueries(
				"proof",
				tt.repoID,
				10,
				access,
			)
			assertQueryplanCapturedRun(t, captured[0], propertyCypher, wantParams)
			assertQueryplanCapturedRun(t, captured[1], relationshipCypher, wantParams)
			assertQueryplanBaselineSHA256(t, captured[0].cypher, tt.wantPropertySHA256)
			assertQueryplanBaselineSHA256(t, captured[1].cypher, tt.wantRelationshipSHA256)
		})
	}
}

func TestHydrateResolvedWorkloadRepoNamesExecutesBuilderBytes(t *testing.T) {
	tests := []struct {
		name       string
		auth       *AuthContext
		wantSHA256 string
	}{
		{name: "all scopes", wantSHA256: "07ec02c66c38c50c13c852294a48043ca90eba8e95504be63f447c4955194402"},
		{
			name: "scoped",
			auth: &AuthContext{
				Mode:                 AuthModeScoped,
				AllowedRepositoryIDs: []string{"repository:r_proof"},
			},
			wantSHA256: "c9a4534995480419be4469752bf7dc2cd1823e0a94820d971ae6b00f6201bbeb",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var captured queryplanCapturedRun
			graph := &captureGraphQuery{runFn: func(
				_ context.Context,
				cypher string,
				params map[string]any,
			) ([]map[string]any, error) {
				captured = queryplanCapturedRun{cypher: cypher, params: params}
				return nil, nil
			}}
			handler := &EntityHandler{Neo4j: graph}
			ctx := context.Background()
			if tt.auth != nil {
				ctx = ContextWithAuthContext(ctx, *tt.auth)
			}
			entities := []map[string]any{{"id": "workload:proof", "repo_id": "repository:r_proof"}}

			if err := handler.hydrateResolvedWorkloadRepoNames(ctx, entities); err != nil {
				t.Fatalf("hydrateResolvedWorkloadRepoNames() error = %v", err)
			}
			access := repositoryAccessFilterFromContext(ctx)
			wantCypher, wantParams := buildHydrateResolvedWorkloadRepoNamesQuery(
				[]string{"repository:r_proof"},
				access,
			)
			assertQueryplanCapturedRun(t, captured, wantCypher, wantParams)
			assertQueryplanBaselineSHA256(t, captured.cypher, tt.wantSHA256)
		})
	}
}

func TestResourceInvestigationExecutesBuilderBytes(t *testing.T) {
	var captured []queryplanCapturedRun
	graph := &captureGraphQuery{runFn: func(
		_ context.Context,
		cypher string,
		params map[string]any,
	) ([]map[string]any, error) {
		captured = append(captured, queryplanCapturedRun{cypher: cypher, params: params})
		return nil, nil
	}}
	handler := &ImpactHandler{Neo4j: graph}
	selected := &resourceInvestigationCandidate{ID: "proof-resource", Labels: []string{"CloudResource"}}
	req := resourceInvestigationRequest{Environment: "prod", MaxDepth: 3, Limit: 10}

	if _, _, err := handler.resourceInvestigationWorkloads(context.Background(), req, selected, repositoryAccessFilter{allScopes: true}); err != nil {
		t.Fatalf("resourceInvestigationWorkloads() error = %v", err)
	}
	if _, err := handler.resourceInvestigationInstanceWorkloads(context.Background(), []map[string]any{{"instance_id": "proof-instance"}}); err != nil {
		t.Fatalf("resourceInvestigationInstanceWorkloads() error = %v", err)
	}
	if _, _, err := handler.resourceInvestigationRepoPaths(context.Background(), req, selected, "outgoing", repositoryAccessFilter{allScopes: true}); err != nil {
		t.Fatalf("resourceInvestigationRepoPaths() error = %v", err)
	}
	if _, _, err := handler.resourceInvestigationRepoPaths(context.Background(), req, selected, "incoming", repositoryAccessFilter{allScopes: true}); err != nil {
		t.Fatalf("resourceInvestigationRepoPaths(incoming) error = %v", err)
	}
	if len(captured) != 4 {
		t.Fatalf("graph calls = %d, want 4", len(captured))
	}
	if captured[0].cypher != resourceInvestigationWorkloadsCypher(selected) {
		t.Fatal("resource workload query bytes differ from production builder")
	}
	if captured[1].cypher != resourceInvestigationInstanceWorkloadsCypher() {
		t.Fatal("instance workload query bytes differ from production builder")
	}
	if captured[2].cypher != resourceInvestigationRepoPathsCypher(req, selected, "outgoing") {
		t.Fatal("resource repository-path query bytes differ from production builder")
	}
	if captured[3].cypher != resourceInvestigationRepoPathsCypher(req, selected, "incoming") {
		t.Fatal("incoming resource repository-path query bytes differ from production builder")
	}
	for index, wantSHA256 := range []string{
		"e100f5b99cf9be76a0bfa62a92c405f069c571cd686a999c596efd0fbc1e3a32",
		// #5167 W3: resourceInvestigationInstanceWorkloadsCypher gained
		// `workload.repo_id AS workload_repo_id` so resourceInvestigationWorkloads
		// can bind each dependent workload to the caller's grant
		// (impact_resource_investigation_reads.go); this baseline is the
		// post-change production digest.
		"2463d998139bb2f60b10bdac64b62cc655d1e67d5ad9862c450ef317f6ae557e",
		"4868f98cf10731ff8781ed9ba394da16d97c623c5cdd7d74217257bb5b641565",
		"ecabb2254490134516441574ffd8a3e8edac0bcf227a37d5ec25495be610c4fc",
	} {
		assertQueryplanBaselineSHA256(t, captured[index].cypher, wantSHA256)
	}
}

func assertQueryplanBaselineSHA256(t *testing.T, cypher string, want string) {
	t.Helper()
	got := fmt.Sprintf("%x", sha256.Sum256([]byte(cypher)))
	if got != want {
		t.Fatalf("production Cypher SHA-256 = %s, want immutable pre-extraction digest %s\nquery=%q", got, want, cypher)
	}
}

func assertQueryplanCapturedRun(
	t *testing.T,
	got queryplanCapturedRun,
	wantCypher string,
	wantParams map[string]any,
) {
	t.Helper()
	if got.cypher != wantCypher {
		t.Fatalf("executed Cypher differs from production builder\ngot:  %q\nwant: %q", got.cypher, wantCypher)
	}
	if !reflect.DeepEqual(got.params, wantParams) {
		t.Fatalf("executed params = %#v, want %#v", got.params, wantParams)
	}
}
