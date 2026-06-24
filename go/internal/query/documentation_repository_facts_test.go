// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDocumentationHandlerListsRepositoryScopedFactsWithTruncation(t *testing.T) {
	t.Parallel()

	var captured documentationFactFilter
	handler := &DocumentationHandler{
		Content: fakePortContentStore{
			documentationFactsFilter: &captured,
			documentationFactsModel: documentationFactListReadModel{
				Facts: []map[string]any{{
					"fact_id":   "fact:doc:repo-readme",
					"fact_kind": "documentation_document",
					"payload": map[string]any{
						"document_id": "doc:git:repository:r_12345678:README.md",
						"title":       "Payment Service",
						"linked_entities": []any{map[string]any{
							"entity_type": "repository",
							"entity_id":   "repository:r_12345678",
						}},
					},
				}},
				NextCursor: "1",
			},
		},
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/documentation/facts?repo=repository:r_12345678&fact_kind=document&limit=1",
		nil,
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := captured.Repository, "repository:r_12345678"; got != want {
		t.Fatalf("Repository = %q, want %q", got, want)
	}
	if got, want := captured.FactKind, "documentation_document"; got != want {
		t.Fatalf("FactKind = %q, want %q", got, want)
	}
	var resp ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := resp.Data.(map[string]any)
	if got, want := data["truncated"], true; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
	if got, want := data["next_cursor"], "1"; got != want {
		t.Fatalf("next_cursor = %#v, want %#v", got, want)
	}
}

func TestBuildDocumentationFactsSQLMatchesRepositoryLinkedEntities(t *testing.T) {
	t.Parallel()

	query, args := buildDocumentationFactsSQL(documentationFactFilter{
		FactKind:   "documentation_document",
		Repository: "repository:r_12345678",
		Limit:      10,
	})

	if !strings.Contains(query, "fact_records.payload @>") {
		t.Fatalf("documentation facts SQL missing linked target predicate: %s", query)
	}
	joinedArgs := documentationArgsString(args)
	for _, fragment := range []string{"linked_entities", "repository", "repository:r_12345678"} {
		if !strings.Contains(joinedArgs, fragment) {
			t.Fatalf("documentation facts SQL args missing fragment %q: %#v", fragment, args)
		}
	}
}
