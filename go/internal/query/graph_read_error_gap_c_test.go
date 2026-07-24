// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestGetRepositoryBranchesMapsGraphReadAvailabilityErrors covers GET
// /api/v0/repositories/{repo_id}/branches: repositoryStatsRepositoryRef
// reads Neo4j directly, and the pre-fix handler wrote an unguarded 500 for
// any error it returned, including the bounded graph-read sentinels.
func TestGetRepositoryBranchesMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &RepositoryHandler{Neo4j: fakeGraphReader{runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
				return nil, test.err
			}}}
			req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-1/branches", nil)
			req.SetPathValue("repo_id", "repo-1")
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.getRepositoryBranches(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestGetRepositoryContentMapsGraphReadAvailabilityErrors covers GET
// /api/v0/repositories/{repo_id}/content, which shares the same
// repositoryStatsRepositoryRef graph read.
func TestGetRepositoryContentMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &RepositoryHandler{Neo4j: fakeGraphReader{runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
				return nil, test.err
			}}}
			req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-1/content?path=README.md", nil)
			req.SetPathValue("repo_id", "repo-1")
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.getRepositoryContent(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestGetRepositoryFreshnessMapsGraphReadAvailabilityErrors covers GET
// /api/v0/repositories/{repo_id}/freshness. The Postgres-backed
// Freshness.ReadRepositoryFreshness read must succeed so the handler
// reaches the second, graph-backed repositoryStatsRepositoryRef call.
func TestGetRepositoryFreshnessMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &RepositoryHandler{
				Neo4j: fakeGraphReader{runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
					return nil, test.err
				}},
				Freshness: &fakeRepositoryFreshnessReader{},
			}
			req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-1/freshness", nil)
			req.SetPathValue("repo_id", "repo-1")
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.getRepositoryFreshness(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestGetRepositoryTreeMapsGraphReadAvailabilityErrors covers GET
// /api/v0/repositories/{repo_id}/tree, which shares the same
// repositoryStatsRepositoryRef graph read.
func TestGetRepositoryTreeMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &RepositoryHandler{Neo4j: fakeGraphReader{runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
				return nil, test.err
			}}}
			req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-1/tree", nil)
			req.SetPathValue("repo_id", "repo-1")
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.getRepositoryTree(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}
