// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// repositoryContextCountCypherFragment uniquely identifies the summary-count
// queries (repository_context_counts.go) among every other graph read issued
// by getRepositoryContext/getRepositoryStory. queryRepositoryFileCount runs
// first in both queryRepositoryContextCounts and
// queryRepositoryStoryGraphSummary and aborts the whole aggregate on error,
// so matching only this fragment -- not every reader.Run call -- is required
// to isolate the summary-count propagation site: a broader match would also
// trip the (already-fixed) deployment_evidence site downstream and pass even
// if the count propagation itself regressed.
const repositoryContextCountCypherFragment = "RETURN count(DISTINCT f) AS count"

// TestGetRepositoryContextSummaryCountsMapsGraphReadAvailabilityErrors covers
// repository_context_counts.go's queryRepositoryContextCount, reached from
// getRepositoryContext's "summary_counts" stage (#5764 site 1). Before the
// fix, a bounded graph-read error there was indistinguishable from a
// genuine-zero count: queryRepositoryContextCount folded `err != nil` into
// the same fallback path as `len(rows) == 0`, so the response silently
// carried fabricated zero counts instead of a 503/504. runSingle succeeds
// (repositoryBaseCypher) so the aux count read is actually reached; Content
// is left nil so no read-model short-circuits it first. The "unavailable"
// sweep case here (see graphReadSweepCases) proves this site's error MAPPING
// is correct for either sentinel, not that ErrGraphUnavailable is reachable
// at this exact position in production: the base RunSingle above already
// succeeded against the same backend, so a genuine outage would already have
// failed that earlier call and short-circuited before this stage runs --
// ErrGraphUnavailable here is only reachable via a transient mid-request
// health flip. ErrGraphReadDeadline has no such caveat: a per-call read
// budget can expire on this stage regardless of the base lookup's outcome.
func TestGetRepositoryContextSummaryCountsMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &RepositoryHandler{Neo4j: fakeGraphReader{
				runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
					return map[string]any{"id": "repo-counts-1", "name": "repo-counts-one"}, nil
				},
				run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
					if strings.Contains(cypher, repositoryContextCountCypherFragment) {
						return nil, test.err
					}
					return nil, nil
				},
			}}
			req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-counts-1/context", nil)
			req.SetPathValue("repo_id", "repo-counts-1")
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.getRepositoryContext(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestGetRepositoryStoryGraphSummaryMapsGraphReadAvailabilityErrors covers
// repository_story_counts.go's queryRepositoryFileCount (via
// queryRepositoryStoryGraphSummary, which runs it first and aborts the whole
// summary on error), reached from getRepositoryStory's "graph_summary" stage
// (#5764 site 2). The fake below matches
// repositoryContextCountCypherFragment -- the file-count query's Cypher, the
// same fragment TestGetRepositoryContextSummaryCountsMapsGraphReadAvailabilityErrors
// above matches -- not queryRepositoryStoryStringRows's workload/platform/
// language row queries; see
// TestGetRepositoryStoryStringRowsMapsGraphReadAvailabilityErrors below for
// that site. Before the fix, a bounded graph-read error there was
// indistinguishable from a genuine-empty workload/platform/language list, and
// the story's headline narrative sentence was built from the fabricated
// empty result instead of the request failing with 503/504.
func TestGetRepositoryStoryGraphSummaryMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &RepositoryHandler{Neo4j: fakeGraphReader{
				runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
					return map[string]any{"id": "repo-story-summary-1", "name": "repo-story-summary-one"}, nil
				},
				run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
					if strings.Contains(cypher, repositoryContextCountCypherFragment) {
						return nil, test.err
					}
					return nil, nil
				},
			}}
			req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-story-summary-1/story", nil)
			req.SetPathValue("repo_id", "repo-story-summary-1")
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.getRepositoryStory(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestGetRepositoryStoryStringRowsMapsGraphReadAvailabilityErrors covers
// repository_story_counts.go's queryRepositoryStoryStringRows itself (#5764
// site 2), distinct from the shared queryRepositoryFileCount path exercised by
// TestGetRepositoryStoryGraphSummaryMapsGraphReadAvailabilityErrors above: the
// file-count query succeeds here, isolating the languages string-rows read
// (queryRepositoryStoryLanguages -> queryRepositoryStoryStringRows) as the
// failing call.
func TestGetRepositoryStoryStringRowsMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &RepositoryHandler{Neo4j: fakeGraphReader{
				runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
					return map[string]any{"id": "repo-story-strings-1", "name": "repo-story-strings-one"}, nil
				},
				run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
					switch {
					case strings.Contains(cypher, repositoryContextCountCypherFragment):
						return []map[string]any{{"count": int64(3)}}, nil
					case strings.Contains(cypher, "RETURN f.language AS language, count(DISTINCT f) AS file_count"):
						return nil, test.err
					default:
						return nil, nil
					}
				},
			}}
			req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-story-strings-1/story", nil)
			req.SetPathValue("repo_id", "repo-story-strings-1")
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.getRepositoryStory(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestGetRepositoryContextDeploymentEvidenceMapsGraphReadAvailabilityErrors
// covers repository_deployment_evidence.go's queryRepoDeploymentEvidenceDirection
// (#5764 site 3), reached from getRepositoryContext's "deployment_evidence"
// stage. Before the fix, queryRepoDeploymentEvidenceDirection always swallowed
// its reader.Run error into a "no truncation, no rows" return, so
// queryRepoDeploymentEvidence never saw the failure and this stage's error
// path -- a bare 500 carrying err.Error() -- was unreachable for a bounded
// graph-read sentinel; the whole class of failure surfaced as a generic 500
// leaking driver text instead of the stable 503/504 contract.
func TestGetRepositoryContextDeploymentEvidenceMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &RepositoryHandler{Neo4j: fakeGraphReader{
				runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
					return map[string]any{"id": "repo-deploy-ev-1", "name": "repo-deploy-ev-one"}, nil
				},
				run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
					if strings.Contains(cypher, "HAS_DEPLOYMENT_EVIDENCE") {
						return nil, test.err
					}
					return nil, nil
				},
			}}
			req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-deploy-ev-1/context", nil)
			req.SetPathValue("repo_id", "repo-deploy-ev-1")
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.getRepositoryContext(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestGetRepositoryStoryDeploymentEvidenceMapsGraphReadAvailabilityErrors
// covers the same queryRepoDeploymentEvidenceDirection propagation reached
// through getRepositoryStory's "deployment_evidence" stage
// (loadRepositoryDeploymentEvidenceForOverview -> queryRepoDeploymentEvidence,
// #5764 site 3). Before the fix this caller also answered a bare 500 with
// err.Error() in the body for a bounded graph-read sentinel.
func TestGetRepositoryStoryDeploymentEvidenceMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &RepositoryHandler{Neo4j: fakeGraphReader{
				runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
					return map[string]any{"id": "repo-story-deploy-ev-1", "name": "repo-story-deploy-ev-one"}, nil
				},
				run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
					if strings.Contains(cypher, "HAS_DEPLOYMENT_EVIDENCE") {
						return nil, test.err
					}
					return nil, nil
				},
			}}
			req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-story-deploy-ev-1/story", nil)
			req.SetPathValue("repo_id", "repo-story-deploy-ev-1")
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.getRepositoryStory(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}
