// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationshiptools

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/contract/route"
)

// relationshipsRouteQueryTypes are the analyze_code_relationships query types
// that share the fall-through dispatch to POST /api/v0/code/relationships
// (#7216).
var relationshipsRouteQueryTypes = []string{
	"who_modifies",
	"module_deps",
	"variable_scope",
	"find_complexity",
	"find_functions_by_argument",
	"find_functions_by_decorator",
}

// TestCodeRouteRelationshipsQueryTypesForwardRepoIDAndNameTarget pins the
// request each fall-through query type sends: the caller's repo_id reaches the
// route (so its grant selector can reject an ungranted one), and target is
// sent as the name to resolve, the way the relationship-story query types
// treat it, not as an entity id (#7216).
func TestCodeRouteRelationshipsQueryTypesForwardRepoIDAndNameTarget(t *testing.T) {
	t.Parallel()

	for _, queryType := range relationshipsRouteQueryTypes {
		t.Run(queryType, func(t *testing.T) {
			t.Parallel()
			request, handled, err := CodeRoute("analyze_code_relationships", routecontract.Arguments{
				"query_type": queryType,
				"target":     "checkout",
				"repo_id":    "repo-1",
				"limit":      float64(10),
			})
			if err != nil || !handled {
				t.Fatalf("CodeRoute() = (_, %v, %v), want handled without error", handled, err)
			}
			if got, want := request.Path, "/api/v0/code/relationships"; got != want {
				t.Fatalf("Path = %q, want %q", got, want)
			}
			want := map[string]any{
				"name":       "checkout",
				"entity_id":  "",
				"repo_id":    "repo-1",
				"query_type": queryType,
			}
			if got := requireRequestBody(t, request); !reflect.DeepEqual(got, want) {
				t.Fatalf("Body = %#v, want %#v", got, want)
			}
		})
	}
}

// TestCodeRouteRelationshipsQueryTypesPreferExplicitEntityID: an exact
// entity_id argument is forwarded as entity_id and wins over the target name,
// so a caller can still anchor on an exact entity (#7216).
func TestCodeRouteRelationshipsQueryTypesPreferExplicitEntityID(t *testing.T) {
	t.Parallel()

	for _, queryType := range relationshipsRouteQueryTypes {
		t.Run(queryType, func(t *testing.T) {
			t.Parallel()
			request, _, err := CodeRoute("analyze_code_relationships", routecontract.Arguments{
				"query_type": queryType,
				"entity_id":  "entity:checkout",
				"repo_id":    "repo-1",
			})
			if err != nil {
				t.Fatalf("CodeRoute() error = %v, want nil", err)
			}
			body := requireRequestBody(t, request)
			if got, want := body["entity_id"], "entity:checkout"; got != want {
				t.Errorf("body[entity_id] = %#v, want %#v", got, want)
			}
			if got, want := body["repo_id"], "repo-1"; got != want {
				t.Errorf("body[repo_id] = %#v, want %#v", got, want)
			}
		})
	}
}
