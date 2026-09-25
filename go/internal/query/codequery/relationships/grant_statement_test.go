// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships_test

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codequery/relationships"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// Hermetic #5167 grant-statement proofs for the NornicDB one-hop read and its
// far-endpoint enrichment. The live two-tenant proof
// (codequery/relationships_grant_live_test.go) establishes that this clause
// position decides row membership on the pinned backend; these tests run in
// every PR and fail if the grant text leaves that position, if its arrays are
// not bound, or if an unscoped caller starts rendering it.

const grantStmtRepo = "repo://tenant-a/granted-service"

func grantStmtScoped() querycontract.RepositoryAccessFilter {
	return querycontract.RepositoryAccessFilter{
		AllowedRepositoryIDs: []string{grantStmtRepo},
		AllowedScopeIDs:      []string{"scope-a"},
	}
}

func grantStmtUnscoped() querycontract.RepositoryAccessFilter {
	return querycontract.RepositoryAccessFilter{AllScopes: true}
}

// grantStmtCondition is the exact grant text a statement must carry on alias.
func grantStmtCondition(alias string) string {
	return "(" + alias + ".repo_id IN $allowed_repository_ids OR " + alias + ".repo_id IN $allowed_scope_ids)"
}

// assertGrantClause asserts that anchor is immediately followed by
// "WHERE <grant on alias>" and that the WHERE precedes RETURN and LIMIT.
func assertGrantClause(t *testing.T, name, cypher, anchor, alias string) {
	t.Helper()
	norm := querycontract.NormalizeCypherWhitespace(cypher)
	clause := anchor + " WHERE " + grantStmtCondition(alias)
	at := strings.Index(norm, clause)
	if at < 0 {
		t.Fatalf("%s: grant clause %q not in the anchoring MATCH:\n%s", name, clause, norm)
	}
	ret, limit := strings.Index(norm, " RETURN "), strings.Index(norm, " LIMIT ")
	if ret < at || limit < at {
		t.Fatalf("%s: grant clause at %d must precede RETURN (%d) and LIMIT (%d):\n%s", name, at, ret, limit, norm)
	}
}

func assertGrantBound(t *testing.T, name string, params map[string]any) {
	t.Helper()
	if got := params["allowed_repository_ids"]; !reflect.DeepEqual(got, []string{grantStmtRepo}) {
		t.Fatalf("%s: $allowed_repository_ids = %#v, want [%s]", name, got, grantStmtRepo)
	}
	if got := params["allowed_scope_ids"]; !reflect.DeepEqual(got, []string{"scope-a"}) {
		t.Fatalf("%s: $allowed_scope_ids = %#v, want [scope-a]", name, got)
	}
}

func assertNoGrant(t *testing.T, name, cypher string, params map[string]any) {
	t.Helper()
	if strings.Contains(cypher, "allowed_") {
		t.Fatalf("%s: unscoped statement renders grant text:\n%s", name, cypher)
	}
	for key := range params {
		if strings.HasPrefix(key, "allowed_") {
			t.Fatalf("%s: unscoped statement binds %s", name, key)
		}
	}
}

// grantStmtGraph records every statement and its parameters. The core
// one-hop read returns one row carrying endpoint uids so EnrichRows runs its
// four enrichment reads.
type grantStmtGraph struct {
	mu    sync.Mutex
	calls []grantStmtCall
}

type grantStmtCall struct {
	cypher string
	params map[string]any
}

func (g *grantStmtGraph) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	g.mu.Lock()
	g.calls = append(g.calls, grantStmtCall{cypher: cypher, params: params})
	g.mu.Unlock()
	if strings.Contains(cypher, "'outgoing' as direction") || strings.Contains(cypher, "'incoming' as direction") {
		return []map[string]any{{
			"type": "CALLS", "source_entity_uid": "fn:anchor", "target_entity_uid": "fn:neighbour",
			"source_id": "fn:anchor", "target_id": "fn:neighbour",
		}}, nil
	}
	return nil, nil
}

func (g *grantStmtGraph) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	rows, err := g.Run(ctx, cypher, params)
	if len(rows) == 0 {
		return nil, err
	}
	return rows[0], err
}

// TestOneHopAndEnrichmentStatementsBindTheNeighbourGrant drives the shipped
// OneHopRelationships for both directions and checks the three statements that
// reach the neighbour: the core read and the far-file and far-repo enrichment.
func TestOneHopAndEnrichmentStatementsBindTheNeighbourGrant(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		direction, core, far, alias string
	}{
		{"outgoing", "MATCH (e:Function {uid: $entity_id})-[rel:CALLS]->(target)", "MATCH (e:Function {uid: $entity_id})-[:CALLS]->(enrichNode)", "target"},
		{"incoming", "MATCH (e:Function {uid: $entity_id})<-[rel:CALLS]-(source)", "MATCH (e:Function {uid: $entity_id})<-[:CALLS]-(enrichNode)", "source"},
	} {
		t.Run(tc.direction, func(t *testing.T) {
			t.Parallel()
			graph := &grantStmtGraph{}
			if _, _, err := relationships.OneHopRelationships(context.Background(), graph, "fn:anchor", tc.direction, "CALLS", "Function", grantStmtScoped()); err != nil {
				t.Fatalf("OneHopRelationships() error = %v", err)
			}
			if len(graph.calls) != 5 {
				t.Fatalf("issued %d statements, want the core read plus four enrichment reads", len(graph.calls))
			}
			core, farFile, farRepo := graph.calls[0], graph.calls[1], graph.calls[2]
			assertGrantClause(t, "core", core.cypher, tc.core, tc.alias)
			assertGrantBound(t, "core", core.params)
			assertGrantClause(t, "far file", farFile.cypher, tc.far+"<-[:CONTAINS]-(enrichFile:File)", "enrichNode")
			assertGrantBound(t, "far file", farFile.params)
			assertGrantClause(t, "far repo", farRepo.cypher, tc.far+"<-[:CONTAINS]-(enrichFile:File)<-[:REPO_CONTAINS]-(enrichRepo:Repository)", "enrichNode")
			assertGrantBound(t, "far repo", farRepo.params)

			unscoped := &grantStmtGraph{}
			if _, _, err := relationships.OneHopRelationships(context.Background(), unscoped, "fn:anchor", tc.direction, "CALLS", "Function", grantStmtUnscoped()); err != nil {
				t.Fatalf("OneHopRelationships(unscoped) error = %v", err)
			}
			for i, call := range unscoped.calls {
				assertNoGrant(t, "unscoped statement "+string(rune('0'+i)), call.cypher, call.params)
			}
		})
	}
}
