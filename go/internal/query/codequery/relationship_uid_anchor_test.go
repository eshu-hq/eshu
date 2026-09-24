// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil/graph"
)

// neo4jEntityAnchorShape matches the indexed Neo4j entity anchor for one alias
// (issue #7057): a scoped CALL () subquery whose first branch seeks the uid
// uniqueness constraint across a label disjunction that includes Function,
// whose second branch seeks the uid RANGE index on the uid-keyed labels that
// carry no constraint (DocumentationSection, Rationale -- their uids reach
// callers as incoming source_id values on the relationships row), and whose
// third branch seeks the id uniqueness constraint across a disjunction that
// includes Repository and Workload.
func neo4jEntityAnchorShape(alias string) *regexp.Regexp {
	return regexp.MustCompile(`(?s)CALL \(\) \{\s*MATCH \(` + alias + `:[A-Za-z0-9|]*\bFunction\b[A-Za-z0-9|]* \{uid: \$entity_id\}\)\s*RETURN ` + alias +
		`\s*UNION\s*MATCH \(` + alias + `:DocumentationSection\|Rationale \{uid: \$entity_id\}\)\s*RETURN ` + alias +
		`\s*UNION\s*MATCH \(` + alias + `:[A-Za-z0-9|]*\bRepository\b[A-Za-z0-9|]*\bWorkload\b[A-Za-z0-9|]* \{id: \$entity_id\}\)\s*RETURN ` + alias + `\s*\}`)
}

// assertNoIDOrUIDScan rejects the unindexed id-OR-uid predicate the Neo4j
// planner turns into an AllNodesScan.
func assertNoIDOrUIDScan(t *testing.T, cypher string) {
	t.Helper()
	if regexp.MustCompile(`\.id = \$entity_id OR \w+\.uid = \$entity_id`).MatchString(cypher) {
		t.Fatalf("Neo4j read still renders the id-OR-uid anchor:\n%s", cypher)
	}
}

// TestNeo4jRelationshipsGraphRowAnchorsOnIndexedEntityID pins the entity-id
// branch of relationshipsGraphRow (POST /api/v0/code/relationships and the
// MCP relationship tools) to the indexed CALL-UNION anchor instead of
// MATCH (e) WHERE (e.id = $entity_id OR e.uid = $entity_id), which planned as
// an AllNodesScan on Neo4j.
func TestNeo4jRelationshipsGraphRowAnchorsOnIndexedEntityID(t *testing.T) {
	t.Parallel()

	var captured string
	reader := graph.FakeGraphReaderWithSingle{
		RunSingleFn: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
			captured = cypher
			return map[string]any{"id": "content-entity:fn1"}, nil
		},
	}
	handler := &CodeHandler{Neo4j: reader, Profile: ProfileLocalAuthoritative}
	if _, err := handler.relationshipsGraphRow(context.Background(), "content-entity:fn1", "", "", "", ""); err != nil {
		t.Fatalf("relationshipsGraphRow() error = %v", err)
	}
	assertNoIDOrUIDScan(t, captured)
	if !neo4jEntityAnchorShape("e").MatchString(captured) {
		t.Fatalf("relationshipsGraphRow entity-id branch is not anchored on the indexed uid/id seek:\n%s", captured)
	}
	graph.AssertCypherHasNoBrokenAndOr(t, captured)
}

// TestNeo4jRelationshipStoryReadsAnchorOnIndexedEntityID pins the Neo4j
// relationship-story class-methods, inheritance-depth and direct-graph reads
// to indexed anchors (issue #7057).
func TestNeo4jRelationshipStoryReadsAnchorOnIndexedEntityID(t *testing.T) {
	t.Parallel()

	entity := &EntityContent{EntityID: "content-entity:cl1"}
	req := codemodel.RelationshipStoryRequest{EntityID: "content-entity:cl1", Limit: 10}
	cases := []struct {
		name  string
		read  func(h *CodeHandler) error
		check func(t *testing.T, cypher string)
	}{
		{
			name: "class methods",
			read: func(h *CodeHandler) error {
				_, err := h.relationshipStoryClassMethods(context.Background(), req, entity)
				return err
			},
			check: func(t *testing.T, cypher string) {
				if !neo4jEntityAnchorShape("class").MatchString(cypher) {
					t.Fatalf("class-methods read is not anchored on the indexed seek:\n%s", cypher)
				}
			},
		},
		{
			name: "inheritance outgoing",
			read: func(h *CodeHandler) error {
				_, _, err := h.relationshipStoryInheritanceDepthRows(context.Background(), req, entity, "outgoing")
				return err
			},
			check: func(t *testing.T, cypher string) {
				if !strings.Contains(cypher, "(source:Class {uid: $entity_id})") {
					t.Fatalf("outgoing inheritance walk is not anchored on Class uid:\n%s", cypher)
				}
			},
		},
		{
			name: "inheritance incoming",
			read: func(h *CodeHandler) error {
				_, _, err := h.relationshipStoryInheritanceDepthRows(context.Background(), req, entity, "incoming")
				return err
			},
			check: func(t *testing.T, cypher string) {
				if !strings.Contains(cypher, "(target:Class {uid: $entity_id})") {
					t.Fatalf("incoming inheritance walk is not anchored on Class uid:\n%s", cypher)
				}
			},
		},
		{
			name: "graph outgoing",
			read: func(h *CodeHandler) error {
				_, err := h.relationshipStoryGraphRowsForDirection(context.Background(), req, entity, "outgoing")
				return err
			},
			check: func(t *testing.T, cypher string) {
				if !neo4jEntityAnchorShape("source").MatchString(cypher) {
					t.Fatalf("outgoing story read is not anchored on the indexed seek:\n%s", cypher)
				}
			},
		},
		{
			name: "graph incoming",
			read: func(h *CodeHandler) error {
				_, err := h.relationshipStoryGraphRowsForDirection(context.Background(), req, entity, "incoming")
				return err
			},
			check: func(t *testing.T, cypher string) {
				if !neo4jEntityAnchorShape("target").MatchString(cypher) {
					t.Fatalf("incoming story read is not anchored on the indexed seek:\n%s", cypher)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var captured string
			reader := graph.FakeGraphReaderWithSingle{
				RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
					captured = cypher
					return nil, nil
				},
			}
			if err := tc.read(&CodeHandler{Neo4j: reader, Profile: ProfileLocalAuthoritative}); err != nil {
				t.Fatalf("read error = %v", err)
			}
			assertNoIDOrUIDScan(t, captured)
			tc.check(t, captured)
			graph.AssertCypherHasNoBrokenAndOr(t, captured)
		})
	}
}
