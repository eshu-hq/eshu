// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
)

// Global-graph fail-closed builder proof that lives in package query: it
// calls the root-owned buildResolveEntityGraphQuery builder and the
// queryplan-scoped test helper directly, which codequery cannot name without
// importing the root back (#6060). Split from
// codequery/global_graph_builder_fail_closed_test.go at the lane-A move; the
// direct-search guard stays there.

func TestGlobalGraphBuildersFailClosed(t *testing.T) {
	t.Parallel()
	accesses := []repositoryAccessFilter{{AllScopes: true}, queryplanScopedRepositoryAccess()}
	for _, access := range accesses {
		if cypher, params := buildResolveEntityGraphQuery(resolveEntityRequest{Name: "proof", Type: "function"}, 10, access); cypher != "" || params != nil {
			t.Fatalf("global entity graph builder = %q/%#v, want fail-closed empty", cypher, params)
		}
		if cypher, params := codemodel.BuildSearchGraphEntitiesQuery("", "proof", "go", 10, true, access); cypher != "" || params != nil {
			t.Fatalf("global code graph builder = %q/%#v, want fail-closed empty", cypher, params)
		}
	}
}
