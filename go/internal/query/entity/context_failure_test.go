// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
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

	steps := []struct {
		name    string
		message string
		handler func(err error) *Handler
	}{
		{"anchor query", entityContextQueryFailedMessage, func(err error) *Handler {
			return &Handler{Neo4j: entityContextFailureGraph(nil, err), Content: routeFailureContent{}}
		}},
		{"content fallback", entityContextContentFallbackFailedMessage, func(err error) *Handler {
			return &Handler{Neo4j: entityContextFailureGraph(nil, nil), Content: routeFailureContent{getEntityErr: err}}
		}},
		{"repo identity hydration", entityContextRepoIdentityFailedMessage, func(err error) *Handler {
			return &Handler{Neo4j: entityContextFailureGraph(entityContextFailureRow(false), nil), Content: routeFailureContent{getEntityErr: err}}
		}},
		{"content enrichment", entityContextEnrichmentFailedMessage, func(err error) *Handler {
			return &Handler{Neo4j: entityContextFailureGraph(entityContextFailureRow(true), nil), Content: routeFailureContent{byIDsErr: err}}
		}},
	}
	for _, step := range steps {
		for _, tc := range routeFailureCases(step.message) {
			t.Run(step.name+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				serveRouteFailure(t, step.handler(tc.err), http.MethodGet, entityContextFailurePath, "", tc)
			})
		}
	}
}

// TestEntityContextRelationshipBuilderSentinelAnswersSentinelText is the
// #7674 regression for the content-fallback 503 when no relationship builder
// is wired: it must answer 503 with only the sentinel's own text and mark the
// span.
func TestEntityContextRelationshipBuilderSentinelAnswersSentinelText(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		Neo4j:   entityContextFailureGraph(nil, nil),
		Content: routeFailureContent{entity: &querycontract.EntityContent{EntityID: "function:handler", RepoID: "repo://example/api"}},
	}
	serveRouteFailure(t, handler, http.MethodGet, entityContextFailurePath, "", bareSentinelCase(errContentRelationshipBuilderNotConfigured))
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
	for _, site := range sites {
		for _, tc := range routeFailureCases(site.message) {
			t.Run(site.route+"/"+site.stage+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				serveRouteFailure(t, serviceFailureHandler(site.stage, tc.err), http.MethodGet, site.route, "", tc)
			})
		}
	}
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
	tc := sentinelFailureCase(querycontract.ErrContentSubstringIndexesNotReady)
	for _, route := range routes {
		t.Run(route, func(t *testing.T) {
			t.Parallel()
			serveRouteFailure(t, serviceFailureHandler(serviceFailEnrichment, tc.err), http.MethodGet, route, "", tc)
		})
	}
}
