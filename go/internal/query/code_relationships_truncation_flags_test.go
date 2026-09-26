// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// entityByIDContentStore serves one entity by id and the K8sResource
// candidate scan from the embedded bounded fake, so the content fallback of
// POST /api/v0/code/relationships resolves the entity and then runs the real
// SELECTS candidate scan.
type entityByIDContentStore struct {
	boundedK8sFakeContentStore
	entity EntityContent
}

func (s entityByIDContentStore) GetEntityContent(_ context.Context, entityID string) (*EntityContent, error) {
	if entityID != s.entity.EntityID {
		return nil, nil
	}
	found := s.entity
	return &found, nil
}

func contentFallbackRelationships(t *testing.T, store ContentStore, body string) map[string]any {
	t.Helper()
	handler := &CodeHandler{Content: store, ContentRelationships: ContentIndexRelationshipBuilder{}}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/relationships", strings.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp
}

func requireContentTruncationFlags(t *testing.T, resp map[string]any, wantOutgoing, wantIncoming bool) {
	t.Helper()
	for key, want := range map[string]bool{"outgoing_truncated": wantOutgoing, "incoming_truncated": wantIncoming} {
		got, ok := resp[key].(bool)
		if !ok {
			t.Fatalf("%s missing or not a boolean in response: %#v", key, resp[key])
		}
		if got != want {
			t.Fatalf("%s = %v, want %v", key, got, want)
		}
	}
}

func k8sServiceEntity() EntityContent {
	return EntityContent{
		EntityID: "service-1", RepoID: "repo-1", RelativePath: "deploy/service.yaml",
		EntityType: "K8sResource", EntityName: "web",
		Metadata: map[string]any{
			"kind": "Service", "namespace": "prod",
			"qualified_name": "prod/Service/web", "selector": "app=frontend",
		},
	}
}

// TestHandleRelationshipsContentFallbackReturnsTruncationFlags proves the
// content fallback reports the clipping it knows about: a Service whose k8s
// SELECTS candidate scan overran repositorySemanticEntityLimit has clipped
// OUTGOING neighbours, and nothing is clipped on the incoming side (#7151).
func TestHandleRelationshipsContentFallbackReturnsTruncationFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                       string
		candidates                 int
		body                       string
		wantOutgoing, wantIncoming bool
	}{
		{"scan over the ceiling clips outgoing", repositorySemanticEntityLimit + 1, `{"entity_id":"service-1"}`, true, false},
		{"scan at the ceiling is complete", repositorySemanticEntityLimit, `{"entity_id":"service-1"}`, false, false},
		{"direction=incoming hides the clipped outgoing side", repositorySemanticEntityLimit + 1, `{"entity_id":"service-1","direction":"incoming"}`, false, false},
		{"a non-SELECTS type filter is not affected by the SELECTS scan", repositorySemanticEntityLimit + 1, `{"entity_id":"service-1","relationship_type":"CALLS"}`, false, false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := k8sServiceEntity()
			store := entityByIDContentStore{
				boundedK8sFakeContentStore: boundedK8sFakeContentStore{t: t, rows: k8sResourceFillerEntities(tt.candidates)},
				entity:                     service,
			}
			resp := contentFallbackRelationships(t, store, tt.body)
			requireContentTruncationFlags(t, resp, tt.wantOutgoing, tt.wantIncoming)
		})
	}
}

// TestHandleRelationshipsContentFallbackWithoutScanReturnsFalseFlags: an
// entity whose content relationships are metadata-derived (no row ceiling)
// still returns both advertised flags, false.
func TestHandleRelationshipsContentFallbackWithoutScanReturnsFalseFlags(t *testing.T) {
	t.Parallel()

	store := entityByIDContentStore{
		boundedK8sFakeContentStore: boundedK8sFakeContentStore{t: t},
		entity: EntityContent{
			EntityID: "fn-1", RepoID: "repo-1", RelativePath: "src/a.go",
			EntityType: "Function", EntityName: "a",
		},
	}
	resp := contentFallbackRelationships(t, store, `{"entity_id":"fn-1"}`)
	requireContentTruncationFlags(t, resp, false, false)
}
