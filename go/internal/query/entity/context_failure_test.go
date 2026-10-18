// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

const entityContextFailurePath = "/api/v0/entities/function:handler/context"

// entityContextFailureRow is the anchor row for entityContextFailurePath.
// withRepo decides whether it carries its repository identity: without it,
// the route hydrates the repository from content before enriching.
func entityContextFailureRow(withRepo bool) map[string]any {
	row := map[string]any{
		"id": "function:handler", "labels": []any{"Function"}, "name": "handler",
		"file_path": "src/handler.go", "start_line": 3, "end_line": 9,
	}
	if withRepo {
		row["repo_id"] = "repo://example/api"
		row["repo_name"] = "api"
	}
	return row
}

func entityContextFailureGraph(row map[string]any, err error) graph.FakeGraphReader {
	return graph.FakeGraphReader{RunSingleFn: func(context.Context, string, map[string]any) (map[string]any, error) {
		return row, err
	}}
}

// TestEntityContextServerFailureAnswersFixedMessage is the #7674 regression
// for every server failure on GET /api/v0/entities/{entity_id}/context. Each
// step answered "<prefix>: <backend error>", and the content steps also turned
// a reader fence into a 500. Each must answer its own fixed message with the
// error on the span, map a reader fence to the retryable 503, and answer a
// client cancel with 499 and no span error.
func TestEntityContextServerFailureAnswersFixedMessage(t *testing.T) {
	t.Parallel()

	runEntityFailureRoutes(t, []failure.Route{
		entityFailureRoute("anchor query", http.MethodGet, entityContextFailurePath, "", entityContextQueryFailedMessage,
			func(err error) *Handler {
				return &Handler{Neo4j: entityContextFailureGraph(nil, err), Content: routeFailureContent{}}
			}),
		entityFailureRoute("content fallback", http.MethodGet, entityContextFailurePath, "", entityContextContentFallbackFailedMessage,
			func(err error) *Handler {
				return &Handler{Neo4j: entityContextFailureGraph(nil, nil), Content: routeFailureContent{getEntityErr: err}}
			}),
		entityFailureRoute("repo identity hydration", http.MethodGet, entityContextFailurePath, "", entityContextRepoIdentityFailedMessage,
			func(err error) *Handler {
				return &Handler{Neo4j: entityContextFailureGraph(entityContextFailureRow(false), nil), Content: routeFailureContent{getEntityErr: err}}
			}),
		entityFailureRoute("content enrichment", http.MethodGet, entityContextFailurePath, "", entityContextEnrichmentFailedMessage,
			func(err error) *Handler {
				return &Handler{Neo4j: entityContextFailureGraph(entityContextFailureRow(true), nil), Content: routeFailureContent{byIDsErr: err}}
			}),
	})
}

// TestEntityContextRelationshipBuilderSentinelAnswersSentinelText is the
// #7674 regression for the content-fallback 503 when no relationship builder
// is wired: it must answer 503 with only the sentinel's own text and mark the
// span.
func TestEntityContextRelationshipBuilderSentinelAnswersSentinelText(t *testing.T) {
	t.Parallel()

	checkEntitySentinel(t, entitySentinelRoute("relationship builder",
		http.MethodGet, entityContextFailurePath, "", errContentRelationshipBuilderNotConfigured,
		func() *Handler {
			return &Handler{
				Neo4j:   entityContextFailureGraph(nil, nil),
				Content: routeFailureContent{entity: &querycontract.EntityContent{EntityID: "function:handler", RepoID: "repo://example/api"}},
			}
		}), entityBareSentinelCase(errContentRelationshipBuilderNotConfigured))
}

// TestWorkloadRoutesServerFailureAnswersFixedMessage is the #7674 regression
// for the workload context and story routes: the query and enrichment steps
// answered "<prefix>: <backend error>". Each must answer its own fixed
// message, map a reader fence to the retryable 503, and answer a client
// cancel with 499.
func TestWorkloadRoutesServerFailureAnswersFixedMessage(t *testing.T) {
	t.Parallel()

	sites := []struct {
		route   string
		stage   string
		message string
	}{
		{"/api/v0/workloads/workload:api/context", serviceFailQuery, workloadContextQueryFailedMessage},
		{"/api/v0/workloads/workload:api/context", serviceFailEnrichment, workloadContextEnrichmentFailedMessage},
		{"/api/v0/workloads/workload:api/story", serviceFailQuery, workloadStoryQueryFailedMessage},
		{"/api/v0/workloads/workload:api/story", serviceFailEnrichment, workloadStoryEnrichmentFailedMessage},
	}
	routes := make([]failure.Route, 0, len(sites))
	for _, site := range sites {
		routes = append(routes, entityFailureRoute(site.route+"/"+site.stage, http.MethodGet, site.route, "", site.message,
			func(err error) *Handler { return serviceFailureHandler(site.stage, err) }))
	}
	runEntityFailureRoutes(t, routes)
}

// TestEnrichmentSubstringIndexSentinelAnswersSentinelText is the #7674
// regression for the content-substring-index 503 on every route that runs
// service enrichment. The 503 echoed the wrapped error, so wrapper text
// reached the body; it must answer only the sentinel's own text. The service
// story seam builds that 503 as an envelope for its in-process callers too.
func TestEnrichmentSubstringIndexSentinelAnswersSentinelText(t *testing.T) {
	t.Parallel()

	routes := []string{
		"/api/v0/workloads/workload:api/context",
		"/api/v0/workloads/workload:api/story",
		"/api/v0/services/api/context",
		"/api/v0/investigations/services/api",
		"/api/v0/services/api/story",
	}
	tc := entitySentinelCase(querycontract.ErrContentSubstringIndexesNotReady)
	for _, route := range routes {
		checkEntitySentinel(t, entitySentinelRoute("enrichment substring index",
			http.MethodGet, route, "", querycontract.ErrContentSubstringIndexesNotReady,
			func() *Handler { return serviceFailureHandler(serviceFailEnrichment, tc.Err) }), tc)
	}
}
