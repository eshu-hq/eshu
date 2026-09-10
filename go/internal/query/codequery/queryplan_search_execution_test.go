// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

type queryplanCapturedRun struct {
	cypher string
	params map[string]any
}

func TestSearchGraphEntitiesExecutesBuilderBytes(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		repoID     string
		language   string
		exact      bool
		auth       *AuthContext
		wantSHA256 string
	}{
		{
			name: "repository anchored", repoID: "repository:r_proof", exact: true,
			body:       `{"query":"proof","repo_id":"repository:r_proof","limit":9,"exact":true}`,
			wantSHA256: "428464ccf4de18918b814cf137ad4bb330f1bfda643801bf29ffa0ad593e59f3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var captured queryplanCapturedRun
			graph := &captureGraphQuery{RunFn: func(
				_ context.Context,
				cypher string,
				params map[string]any,
			) ([]map[string]any, error) {
				captured = queryplanCapturedRun{cypher: cypher, params: params}
				// One graph row keeps handleSearch on the graph path: empty
				// results would fall through to the content fallback, which
				// this byte-baseline test does not wire.
				return []map[string]any{{"id": "entity:proof"}}, nil
			}}
			handler := &CodeHandler{Neo4j: graph}
			request := httptest.NewRequest(http.MethodPost, "/api/v0/code/search", bytes.NewBufferString(tt.body))
			if tt.auth != nil {
				request = request.WithContext(queryauth.ContextWithAuthContext(request.Context(), *tt.auth))
			}
			response := httptest.NewRecorder()

			handler.handleSearch(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d body=%s", response.Code, http.StatusOK, response.Body.String())
			}
			// handleSearch probes with limit+1, so body limit 9 drives the
			// helper with limit 10, matching the pre-move direct-call baseline.
			access := querycontract.RepositoryAccessFilterFromContext(request.Context())
			wantCypher, wantParams := codemodel.BuildSearchGraphEntitiesQuery(tt.repoID, "proof", tt.language, 10, tt.exact, access)
			assertQueryplanCapturedRun(t, captured, wantCypher, wantParams)
			assertQueryplanBaselineSHA256(t, captured.cypher, tt.wantSHA256)
		})
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
