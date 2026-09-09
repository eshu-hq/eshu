// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/queryauth"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// TestCallGraphMetricsDataRefusesARepositoryOutsideTheGrant is the regression
// for the #6060 export: CallGraphMetricsData became callable without the HTTP
// route, and the route is where applyRepositorySelectorForCapability rejects an
// ungranted repo_id. A scoped caller holding a grant for one repository must
// not read another's metrics by naming it directly.
//
// The empty-grant case cannot catch this. The grant here is NON-empty and
// simply excludes the requested repository, which is the case the route used to
// absorb on this method's behalf.
func TestCallGraphMetricsDataRefusesARepositoryOutsideTheGrant(t *testing.T) {
	t.Parallel()

	// The fake returns a row for ANY query. Without the grant check the method
	// runs the Cypher and hands that row back, so this fake is what makes the
	// test able to fail -- an empty fake would pass with or without the guard.
	reached := false
	handler := &CodeHandler{Neo4j: &fakeGraphReader{
		run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			reached = true
			return []map[string]any{{"name": "leaked", "callers": 3}}, nil
		},
	}}
	ctx := queryauth.ContextWithAuthContext(context.Background(), querytestutil.CodeGrantScopedAuthContext([]string{"repo-granted"}))

	got, err := handler.CallGraphMetricsData(ctx, codemodel.CallGraphMetricsRequest{RepoID: "repo-other"})
	if err != nil {
		t.Fatalf("CallGraphMetricsData(ungranted repo) error = %v, want nil", err)
	}
	if reached {
		t.Fatal("CallGraphMetricsData ran the graph query for a repository outside the caller's grant")
	}
	if rows, ok := got["hubs"].([]map[string]any); ok && len(rows) > 0 {
		t.Fatalf("CallGraphMetricsData(ungranted repo) returned %d hub rows, want none", len(rows))
	}
}
