// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"testing"
)

func TestGlobalGraphBuildersFailClosed(t *testing.T) {
	t.Parallel()
	accesses := []repositoryAccessFilter{{allScopes: true}, queryplanScopedRepositoryAccess()}
	for _, access := range accesses {
		if cypher, params := buildResolveEntityGraphQuery(resolveEntityRequest{Name: "proof", Type: "function"}, 10, access); cypher != "" || params != nil {
			t.Fatalf("global entity graph builder = %q/%#v, want fail-closed empty", cypher, params)
		}
		if cypher, params := buildSearchGraphEntitiesQuery("", "proof", "go", 10, true, access); cypher != "" || params != nil {
			t.Fatalf("global code graph builder = %q/%#v, want fail-closed empty", cypher, params)
		}
	}
}

func TestDirectGlobalGraphSearchDoesNotCallGraph(t *testing.T) {
	t.Parallel()
	graph := &captureGraphQuery{runFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
		t.Fatal("direct global graph search called GraphQuery")
		return nil, nil
	}}
	_, err := (&CodeHandler{Neo4j: graph}).searchGraphEntitiesWithExact(context.Background(), "", "proof", "", 10, true)
	if !errors.Is(err, errGlobalGraphEntitySearchUnsupported) {
		t.Fatalf("error = %v, want fail-closed global graph error", err)
	}
}
