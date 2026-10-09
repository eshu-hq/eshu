// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"strings"
	"testing"
)

// TestImpactScopedOwnershipChecksAnswerFixedServerFailures is the #7674
// regression for the two failure sites only a scoped caller reaches: the
// ownership check that filters resource-to-code paths, and the one that
// filters the explain-dependency-path result. Each must answer its fixed
// message, 499 for a client cancel, and the retryable 503 for a stale reader.
// Not parallel: it swaps queryHandlerTracer.
func TestImpactScopedOwnershipChecksAnswerFixedServerFailures(t *testing.T) {
	a := tenantAAuth()
	failAfter := func(traversal string, fixture *twoTenantGraph) func(error) *Handler {
		return func(err error) *Handler {
			h := newTwoTenantHandler(fixture)
			h.Neo4j = &ownershipAfterTraversalGraph{inner: fixture, traversal: traversal, err: err}
			return h
		}
	}
	runImpactFailureRoutes(t, []impactFailureRoute{
		{
			name: "scoped resource to code ownership", path: traceRoute,
			body: `{"start":"cr-a"}`, message: resourceToCodeOwnershipFailedMessage,
			handler: func(err error) *Handler {
				return failAfter("(repo:Repository)", &twoTenantGraph{tracePaths: traceFixturePaths()})(err)
			},
			authCtx: &a,
		},
		{
			name: "scoped dependency path ownership", path: explainRoute,
			body: `{"source":"cr-a","target":"repo-a"}`, message: dependencyPathOwnershipFailedMessage,
			handler: func(err error) *Handler {
				return failAfter("shortestPath", &twoTenantGraph{shortest: []string{"cr-a", "wi-a", "repo-a"}})(err)
			},
			authCtx: &a,
		},
	})
}

// ownershipAfterTraversalGraph delegates to the two-tenant fixture and fails
// every ownership read ($uids) issued after the route's traversal ran, so the
// scoped anchor or endpoint check before it still succeeds. One instance
// serves one request.
type ownershipAfterTraversalGraph struct {
	inner     *twoTenantGraph
	traversal string
	err       error
	traversed bool
}

func (g *ownershipAfterTraversalGraph) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	if strings.Contains(cypher, g.traversal) {
		g.traversed = true
	}
	if g.traversed && strings.Contains(cypher, "$uids") {
		return nil, g.err
	}
	return g.inner.Run(ctx, cypher, params)
}

func (g *ownershipAfterTraversalGraph) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	rows, err := g.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}
