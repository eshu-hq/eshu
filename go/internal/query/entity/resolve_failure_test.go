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
	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
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
	runEntityFailureRoutes(t, []failure.Route{
		entityFailureRoute("graph query", http.MethodPost, resolveFailurePath, anchored, resolveEntityQueryFailedMessage,
			func(err error) *Handler {
				return &Handler{Neo4j: resolveFailureGraph(nil, err), Content: routeFailureContent{}}
			}),
		entityFailureRoute("content enrichment", http.MethodPost, resolveFailurePath, anchored, resolveEntityEnrichmentFailedMessage,
			func(err error) *Handler {
				return &Handler{
					Neo4j:   resolveFailureGraph([]map[string]any{resolveFailureRow()}, nil),
					Content: routeFailureContent{byIDsErr: err},
				}
			}),
		entityFailureRoute("repo identity hydration", http.MethodPost, resolveFailurePath, anchored, resolveEntityRepoIdentityFailedMessage,
			func(err error) *Handler {
				return &Handler{
					Neo4j:   resolveFailureGraph([]map[string]any{resolveFailureRow()}, nil),
					Content: routeFailureContent{getEntityErr: err},
				}
			}),
		entityFailureRoute("content fallback", http.MethodPost, resolveFailurePath, anchored, resolveEntityContentFallbackFailedMessage,
			func(err error) *Handler {
				return &Handler{Neo4j: resolveFailureGraph(nil, nil), Content: routeFailureContent{byNameErr: err}}
			}),
		entityFailureRoute("global content", http.MethodPost, resolveFailurePath, `{"name":"handler","type":"function"}`, resolveGlobalContentEntitiesFailedMessage,
			func(err error) *Handler {
				return &Handler{Content: routeFailureContent{nameSearchErr: err}}
			}),
		entityFailureRoute("canonical content", http.MethodPost, resolveFailurePath, `{"name":"content-entity:e1"}`, resolveCanonicalContentEntityFailedMessage,
			func(err error) *Handler {
				return &Handler{Content: routeFailureContent{getEntityErr: err}}
			}),
		entityFailureRoute("workload", http.MethodPost, resolveFailurePath, `{"name":"api","type":"workload"}`, resolveWorkloadEntitiesFailedMessage,
			func(err error) *Handler {
				return &Handler{Neo4j: resolveFailureGraph(nil, err)}
			}),
	})
}

// TestResolveEntityUnavailableSentinelsAnswerSentinelText is the #7674
// regression for the configuration-state 503s on the resolve route. Each
// answered 503 with err.Error(), so a sentinel wrapped by backend text leaked
// that text. Each must answer 503 with only the sentinel's own text.
func TestResolveEntityUnavailableSentinelsAnswerSentinelText(t *testing.T) {
	t.Parallel()

	const global = `{"name":"handler","type":"function"}`
	globalSearch := func(searchErr error) func() *Handler {
		return func() *Handler {
			return &Handler{Content: routeFailureContent{nameSearchErr: searchErr}}
		}
	}
	checkEntitySentinel(t, entitySentinelRoute("global name search wrapped",
		http.MethodPost, resolveFailurePath, global, entitycontract.ErrEntityNameSearchUnavailable,
		globalSearch(entitySentinelCase(entitycontract.ErrEntityNameSearchUnavailable).Err)),
		entitySentinelCase(entitycontract.ErrEntityNameSearchUnavailable))
	checkEntitySentinel(t, entitySentinelRoute("global name search not wired",
		http.MethodPost, resolveFailurePath, global, entitycontract.ErrEntityNameSearchUnavailable,
		func() *Handler { return &Handler{Content: content.FakePortContentStore{}} }),
		entityBareSentinelCase(entitycontract.ErrEntityNameSearchUnavailable))
	checkEntitySentinel(t, entitySentinelRoute("global substring indexes not ready",
		http.MethodPost, resolveFailurePath, global, querycontract.ErrContentSubstringIndexesNotReady,
		globalSearch(entitySentinelCase(querycontract.ErrContentSubstringIndexesNotReady).Err)),
		entitySentinelCase(querycontract.ErrContentSubstringIndexesNotReady))
	checkEntitySentinel(t, entitySentinelRoute("workload graph not wired",
		http.MethodPost, resolveFailurePath, `{"name":"api","type":"workload"}`, errWorkloadGraphUnavailable,
		func() *Handler { return &Handler{} }),
		entityBareSentinelCase(errWorkloadGraphUnavailable))
}
