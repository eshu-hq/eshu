// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// The three CodeHandler cases of root's graph_read_error_entity_service_test.go
// moved here at the #6060 CodeHandler move: they drive unexported
// handleCodeQualityInspection, handleSearchBundles, and handleRelationshipStory
// directly, which cannot be called from another package. The
// CodeownersOwnershipHandler and EntityHandler cases stay in root.
// graphReadSweepCases/assertGraphReadSweepResponse
// (graph_read_error_sweep_shared_test.go) are exported from there.
func TestHandleCodeQualityInspectionMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &CodeHandler{Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, test.err
			}}}
			req := httptest.NewRequest(http.MethodPost, "/api/v0/code/quality", bytes.NewBufferString(`{"check":"function_length"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.handleCodeQualityInspection(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

func TestHandleSearchBundlesMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &CodeHandler{Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, test.err
			}}}
			req := httptest.NewRequest(http.MethodPost, "/api/v0/code/registry/bundles/search", bytes.NewBufferString(`{"query":"left-pad"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.handleSearchBundles(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

func TestHandleRelationshipStoryRepoScopedOverridesMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &CodeHandler{Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, test.err
			}}}
			req := httptest.NewRequest(http.MethodPost, "/api/v0/code/relationships/story", bytes.NewBufferString(`{"query_type":"overrides","repo_id":"repo-1"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.handleRelationshipStory(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}
