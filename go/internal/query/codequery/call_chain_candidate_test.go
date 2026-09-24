// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// TestCallChainCandidateOneHopRowsNeo4jSeeksUID pins the Neo4j one-hop read
// the call-chain name resolver walks (issue #7057). The source is anchored on
// the uid uniqueness constraints of the code-call endpoint labels. The old
// unlabeled MATCH (source)-[:CALLS]->(target) WHERE (source.id = x OR
// source.uid = x) planned as a scan of every CALLS relationship per hop. The
// traversal and grant predicates must still bind the target.
func TestCallChainCandidateOneHopRowsNeo4jSeeksUID(t *testing.T) {
	t.Parallel()

	const anchor = "MATCH (source:Function|Class|Struct|Interface|TypeAlias|File {uid: $source_id})-[:CALLS]->(target)"
	cases := []struct {
		name      string
		req       callChainRequest
		ctx       context.Context
		wantWhere []string
	}{
		{name: "unbounded", req: callChainRequest{}, ctx: context.Background()},
		{
			name:      "repo bound",
			req:       callChainRequest{RepoID: "repo-1"},
			ctx:       context.Background(),
			wantWhere: []string{"coalesce(target.repo_id, '') IN $traversal_repo_ids"},
		},
		{
			name: "grant scoped",
			req:  callChainRequest{},
			ctx: auth.ContextWithAuthContext(context.Background(),
				auth.AuthContext{Mode: auth.AuthModeScoped, AllowedRepositoryIDs: []string{"repo-1"}}),
			wantWhere: []string{"target.repo_id IN $allowed_repository_ids"},
		},
	}
	for _, tc := range cases {
		var got string
		h := &CodeHandler{Neo4j: graph.FakeGraphReader{RunFn: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
			got = cypher
			if params["source_id"] != "fn-1" {
				t.Errorf("%s: params[source_id] = %#v, want fn-1", tc.name, params["source_id"])
			}
			return nil, nil
		}}}
		req := tc.req
		if _, err := h.callChainCandidateOneHopRows(tc.ctx, &req, "fn-1", "Function"); err != nil {
			t.Fatalf("%s: callChainCandidateOneHopRows() error = %v", tc.name, err)
		}
		if !strings.Contains(got, anchor) {
			t.Errorf("%s: anchor missing %q:\n%s", tc.name, anchor, got)
		}
		if strings.Contains(got, ".id =") {
			t.Errorf("%s: anchor still uses the unindexed id predicate:\n%s", tc.name, got)
		}
		for _, want := range tc.wantWhere {
			if !strings.Contains(got, want) {
				t.Errorf("%s: missing target predicate %q:\n%s", tc.name, want, got)
			}
		}
		if len(tc.wantWhere) == 0 && strings.Contains(got, "WHERE") {
			t.Errorf("%s: unbounded read must not render an empty WHERE:\n%s", tc.name, got)
		}
	}
}
