// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"net/http"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/contract/route"
	relationshiptools "github.com/eshu-hq/eshu/go/internal/mcp/relationships"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

// TestCodeRelationshipsMCPDispatchRejectsUngrantedRepoSelector composes the
// analyze_code_relationships dispatch with the route (#7216). The six query
// types that fall through to POST /api/v0/code/relationships must carry the
// caller's repo_id, so a scoped token naming an ungranted repository gets the
// same 400 as the relationship-story query types, with no backend read and no
// leak of the other tenant's entity.
func TestCodeRelationshipsMCPDispatchRejectsUngrantedRepoSelector(t *testing.T) {
	t.Parallel()
	for _, queryType := range []string{
		"who_modifies",
		"module_deps",
		"variable_scope",
		"find_complexity",
		"find_functions_by_argument",
		"find_functions_by_decorator",
	} {
		for _, backend := range relGrantBackends() {
			t.Run(queryType+"/"+string(backend), func(t *testing.T) {
				t.Parallel()
				request, handled, err := relationshiptools.CodeRoute("analyze_code_relationships", routecontract.Arguments{
					"query_type": queryType,
					"target":     relGrantOtherName,
					"repo_id":    codeGrantOtherRepo,
				})
				if err != nil || !handled {
					t.Fatalf("CodeRoute() = (_, %v, %v), want handled without error", handled, err)
				}
				if got, want := request.Path, "/api/v0/code/relationships"; got != want {
					t.Fatalf("dispatch Path = %q, want %q", got, want)
				}
				auth := testutil.CodeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
				fixture := newRelGrantFixture(backend)
				body, ok := request.Body.(map[string]any)
				if !ok {
					t.Fatalf("dispatch Body = %T, want map[string]any", request.Body)
				}
				rec := fixture.serve(t, body, &auth)
				if got, want := rec.Code, http.StatusBadRequest; got != want {
					t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
				}
				if strings.Contains(rec.Body.String(), relGrantOtherID) {
					t.Fatalf("rejection leaked the entity: %s", rec.Body.String())
				}
				if n := fixture.graph.count() + fixture.content.reads; n != 0 {
					t.Fatalf("ungranted selector issued %d backend read(s)", n)
				}
			})
		}
	}
}
