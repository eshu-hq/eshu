// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	entitycontract "github.com/eshu-hq/eshu/go/internal/query/querycontract/entity"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

const resolveFailurePath = "/api/v0/entities/resolve"

// resolveFailureRow is a repo-anchored graph hit with no content metadata and
// no repo identity, so the route enriches it from content and then hydrates
// its repository from content.
func resolveFailureRow() map[string]any {
	return map[string]any{
		"id": "function:handler", "labels": []any{"Function"}, "name": "handler",
		"file_path": "src/handler.go", "start_line": 3, "end_line": 9,
	}
}

func resolveFailureGraph(rows []map[string]any, err error) graph.FakeGraphReader {
	return graph.FakeGraphReader{RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
		return rows, err
	}}
}

// TestResolveEntityServerFailureAnswersFixedMessage is the #7674 regression
// for every server failure on POST /api/v0/entities/resolve. Each step
// answered "<prefix>: <backend error>" (or, for the content steps, skipped the
// reader fence and the client cancel). Each must now answer its own fixed
// message with the error on the span, map a reader fence to the retryable 503,
// and answer a client cancel with 499 and no span error.
func TestResolveEntityServerFailureAnswersFixedMessage(t *testing.T) {
	t.Parallel()

	const anchored = `{"name":"handler","type":"function","repo_id":"repo://example/api"}`
	steps := []struct {
		name    string
		body    string
		message string
		handler func(err error) *Handler
	}{
		{"graph query", anchored, resolveEntityQueryFailedMessage, func(err error) *Handler {
			return &Handler{Neo4j: resolveFailureGraph(nil, err), Content: routeFailureContent{}}
		}},
		{"content enrichment", anchored, resolveEntityEnrichmentFailedMessage, func(err error) *Handler {
			return &Handler{
				Neo4j:   resolveFailureGraph([]map[string]any{resolveFailureRow()}, nil),
				Content: routeFailureContent{byIDsErr: err},
			}
		}},
		{"repo identity hydration", anchored, resolveEntityRepoIdentityFailedMessage, func(err error) *Handler {
			return &Handler{
				Neo4j:   resolveFailureGraph([]map[string]any{resolveFailureRow()}, nil),
				Content: routeFailureContent{getEntityErr: err},
			}
		}},
		{"content fallback", anchored, resolveEntityContentFallbackFailedMessage, func(err error) *Handler {
			return &Handler{Neo4j: resolveFailureGraph(nil, nil), Content: routeFailureContent{byNameErr: err}}
		}},
		{"global content", `{"name":"handler","type":"function"}`, resolveGlobalContentEntitiesFailedMessage, func(err error) *Handler {
			return &Handler{Content: routeFailureContent{nameSearchErr: err}}
		}},
		{"canonical content", `{"name":"content-entity:e1"}`, resolveCanonicalContentEntityFailedMessage, func(err error) *Handler {
			return &Handler{Content: routeFailureContent{getEntityErr: err}}
		}},
		{"workload", `{"name":"api","type":"workload"}`, resolveWorkloadEntitiesFailedMessage, func(err error) *Handler {
			return &Handler{Neo4j: resolveFailureGraph(nil, err)}
		}},
	}
	for _, step := range steps {
		for _, tc := range routeFailureCases(step.message) {
			t.Run(step.name+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				serveRouteFailure(t, step.handler(tc.err), http.MethodPost, resolveFailurePath, step.body, tc)
			})
		}
	}
}

// TestResolveEntityUnavailableSentinelsAnswerSentinelText is the #7674
// regression for the configuration-state 503s on the resolve route. Each
// answered 503 with err.Error(), so a sentinel wrapped by backend text leaked
// that text. Each must answer 503 with only the sentinel's own text.
func TestResolveEntityUnavailableSentinelsAnswerSentinelText(t *testing.T) {
	t.Parallel()

	const global = `{"name":"handler","type":"function"}`
	cases := []struct {
		name    string
		body    string
		handler *Handler
		tc      routeFailureCase
	}{
		{
			name:    "global name search wrapped",
			body:    global,
			handler: &Handler{Content: routeFailureContent{nameSearchErr: sentinelFailureCase(entitycontract.ErrEntityNameSearchUnavailable).err}},
			tc:      sentinelFailureCase(entitycontract.ErrEntityNameSearchUnavailable),
		},
		{
			name:    "global name search not wired",
			body:    global,
			handler: &Handler{Content: content.FakePortContentStore{}},
			tc:      bareSentinelCase(entitycontract.ErrEntityNameSearchUnavailable),
		},
		{
			name:    "global substring indexes not ready",
			body:    global,
			handler: &Handler{Content: routeFailureContent{nameSearchErr: sentinelFailureCase(querycontract.ErrContentSubstringIndexesNotReady).err}},
			tc:      sentinelFailureCase(querycontract.ErrContentSubstringIndexesNotReady),
		},
		{
			name:    "workload graph not wired",
			body:    `{"name":"api","type":"workload"}`,
			handler: &Handler{},
			tc:      bareSentinelCase(errWorkloadGraphUnavailable),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			serveRouteFailure(t, tc.handler, http.MethodPost, resolveFailurePath, tc.body, tc.tc)
		})
	}
}

// bareSentinelCase is a configuration-state 503 the route raises itself, with
// no wrapper text.
func bareSentinelCase(sentinel error) routeFailureCase {
	tc := sentinelFailureCase(sentinel)
	tc.err = sentinel
	return tc
}
