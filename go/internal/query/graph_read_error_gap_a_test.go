// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHandleRelationshipStoryDefaultBranchMapsGraphReadAvailabilityErrors
// proves the default target/entity_id lookup branch of handleRelationshipStory
// (code_relationship_story.go, reached via relationshipStoryRelationships ->
// relationshipStoryGraphRowsForDirection) maps the shared Neo4jReader sentinels
// to 503/504 instead of a generic 500. This is distinct from the already
// guarded query_type=overrides branch.
func TestHandleRelationshipStoryDefaultBranchMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &CodeHandler{Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, test.err
			}}}
			req := httptest.NewRequest(http.MethodPost, "/api/v0/code/relationships/story", bytes.NewBufferString(`{"entity_id":"entity-1"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.handleRelationshipStory(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestHandleRelationshipStoryClassHierarchyMapsGraphReadAvailabilityErrors
// proves the class_hierarchy branch of handleRelationshipStory
// (code_relationship_story.go, reached via relationshipStoryClassHierarchy ->
// relationshipStoryInheritanceDepthRows in code_relationship_story_class.go)
// maps the shared Neo4jReader sentinels to 503/504. The primary relationship
// lookup and the class-methods lookup succeed with empty rows so only the
// inheritance-depth traversal (the bounded "INHERITS*1.." query) fails.
func TestHandleRelationshipStoryClassHierarchyMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &CodeHandler{Neo4j: fakeGraphReader{run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				if strings.Contains(cypher, "INHERITS*1..") {
					return nil, test.err
				}
				return []map[string]any{}, nil
			}}}
			req := httptest.NewRequest(
				http.MethodPost,
				"/api/v0/code/relationships/story",
				bytes.NewBufferString(`{"entity_id":"entity-1","query_type":"class_hierarchy"}`),
			)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.handleRelationshipStory(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestHandleDeadCodeMapsGraphReadAvailabilityErrors proves handleDeadCode
// (code_dead_code.go, error from scanDeadCodeCandidates -> deadCodeCandidateRows
// -> h.Neo4j.Run in code_dead_code_scan.go) maps the shared Neo4jReader
// sentinels to 503/504.
func TestHandleDeadCodeMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &CodeHandler{Profile: ProfileLocalAuthoritative, Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, test.err
			}}}
			req := httptest.NewRequest(http.MethodPost, "/api/v0/code/dead-code", bytes.NewBufferString(`{}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.handleDeadCode(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestHandleCrossRepoDeadCodeMapsGraphReadAvailabilityErrors proves
// handleCrossRepoDeadCode (code_dead_code_cross_repo.go, error from
// scanCrossRepoDeadCodeCandidates -> deadCodeCandidateRows -> h.Neo4j.Run) maps
// the shared Neo4jReader sentinels to 503/504.
func TestHandleCrossRepoDeadCodeMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &CodeHandler{Profile: ProfileLocalAuthoritative, Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, test.err
			}}}
			req := httptest.NewRequest(
				http.MethodPost,
				"/api/v0/code/dead-code/cross-repo",
				bytes.NewBufferString(`{"repo_id":"repo-1"}`),
			)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.handleCrossRepoDeadCode(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestHandleDeadCodeInvestigationMapsGraphReadAvailabilityErrors proves
// handleDeadCodeInvestigation (code_dead_code_investigation.go, error from
// scanDeadCodeInvestigation -> deadCodeCandidateRows -> h.Neo4j.Run) maps the
// shared Neo4jReader sentinels to 503/504.
func TestHandleDeadCodeInvestigationMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &CodeHandler{Profile: ProfileLocalAuthoritative, Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, test.err
			}}}
			req := httptest.NewRequest(
				http.MethodPost,
				"/api/v0/code/dead-code/investigate",
				bytes.NewBufferString(`{}`),
			)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.handleDeadCodeInvestigation(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}
