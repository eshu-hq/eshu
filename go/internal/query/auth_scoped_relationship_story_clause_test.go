// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
)

// Shipped-text pins for #5167 batch 2b. Each asserts WHERE the grant sits, not
// merely that it is present: storyClausePredicates separates the predicates
// attached to the anchoring MATCH from the ones attached to an OPTIONAL MATCH,
// and only the first list decides row membership on the pinned backend.

func storyScopedAccess() repositoryAccessFilter {
	return repositoryAccessFilter{AllowedRepositoryIDs: []string{codeGrantGrantedRepo}}
}

func assertGrantIsAnchoring(t *testing.T, label, cypher string, aliases ...string) {
	t.Helper()
	anchoring, stranded := storyClausePredicates(cypher)
	for _, alias := range aliases {
		condition := alias + ".repo_id IN $allowed_repository_ids"
		if slices.ContainsFunc(stranded, func(p string) bool { return strings.Contains(p, condition) }) {
			t.Fatalf("%s: the grant on %q sits after an OPTIONAL MATCH, where it filters nothing:\n%s", label, alias, cypher)
		}
		if !slices.ContainsFunc(anchoring, func(p string) bool { return strings.Contains(p, condition) }) {
			t.Fatalf("%s: the anchoring MATCH does not bind the grant on %q:\n%s", label, alias, cypher)
		}
	}
}

// TestRelationshipStoryBuildersBindTheGrantInTheAnchoringMatch covers every
// statement the route can issue.
func TestRelationshipStoryBuildersBindTheGrantInTheAnchoringMatch(t *testing.T) {
	t.Parallel()

	access := storyScopedAccess()
	req := relationshipStoryRequest{EntityID: storyGrantedAnchor, RelationshipType: "CALLS", Limit: 50}

	for _, tc := range []struct {
		name    string
		cypher  string
		params  map[string]any
		aliases []string
	}{
		{
			name: "nornicdb_outgoing",
			cypher: firstOf(nornicDBRelationshipStoryGraphCypher(
				req, storyGrantedAnchor, "Function", "uid", "outgoing", access)),
			aliases: []string{"anchor", "target"},
		},
		{
			name: "nornicdb_incoming",
			cypher: firstOf(nornicDBRelationshipStoryGraphCypher(
				req, storyGrantedAnchor, "Function", "uid", "incoming", access)),
			aliases: []string{"source", "anchor"},
		},
		{
			name: "compat_outgoing",
			cypher: firstOf(relationshipStoryGraphCypher(
				req, nil, "outgoing", graphEntityIDPredicate, access)),
			aliases: []string{"source", "target"},
		},
		{
			name: "compat_incoming",
			cypher: firstOf(relationshipStoryGraphCypher(
				req, nil, "incoming", graphEntityIDPredicate, access)),
			aliases: []string{"source", "target"},
		},
		{
			name: "nornicdb_class_methods",
			cypher: firstOf(nornicDBRelationshipStoryClassMethodsCypher(
				req, storyGrantedAnchor, "uid", access)),
			aliases: []string{"class", "method"},
		},
		{
			name: "compat_class_methods",
			cypher: firstOf(relationshipStoryClassMethodsCypher(
				req, storyGrantedAnchor, graphEntityIDPredicate, access)),
			aliases: []string{"class", "method"},
		},
		{
			name: "nornicdb_inheritance_outgoing",
			cypher: firstOf(nornicDBRelationshipStoryInheritanceDepthCypher(
				req, storyGrantedAnchor, "outgoing", "uid", access)),
			aliases: []string{"anchor", "target"},
		},
		{
			name: "nornicdb_inheritance_incoming",
			cypher: firstOf(nornicDBRelationshipStoryInheritanceDepthCypher(
				req, storyGrantedAnchor, "incoming", "uid", access)),
			aliases: []string{"source", "anchor"},
		},
		{
			name: "compat_inheritance",
			cypher: firstOf(relationshipStoryInheritanceDepthCypher(
				req, storyGrantedAnchor, "outgoing", graphEntityIDPredicate, access)),
			aliases: []string{"source", "target"},
		},
		{
			name: "override_rows",
			cypher: firstOf(relationshipStoryOverrideRowsCypher(
				relationshipStoryRequest{QueryType: "overrides", RepoID: codeGrantGrantedRepo, Limit: 50}, access)),
			aliases: []string{"source", "target"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertGrantIsAnchoring(t, tc.name, tc.cypher, tc.aliases...)
		})
	}
}

// TestRelationshipStoryBuildersCarryNoGrantForAnUnscopedCaller is the other
// half: the grant renders nothing at all for a shared-key caller.
func TestRelationshipStoryBuildersCarryNoGrantForAnUnscopedCaller(t *testing.T) {
	t.Parallel()

	access := repositoryAccessFilter{AllScopes: true}
	req := relationshipStoryRequest{EntityID: storyGrantedAnchor, RelationshipType: "CALLS", Limit: 50}
	for name, cypher := range map[string]string{
		"nornicdb_outgoing":      firstOf(nornicDBRelationshipStoryGraphCypher(req, storyGrantedAnchor, "Function", "uid", "outgoing", access)),
		"nornicdb_incoming":      firstOf(nornicDBRelationshipStoryGraphCypher(req, storyGrantedAnchor, "Function", "uid", "incoming", access)),
		"compat_outgoing":        firstOf(relationshipStoryGraphCypher(req, nil, "outgoing", graphEntityIDPredicate, access)),
		"nornicdb_class_methods": firstOf(nornicDBRelationshipStoryClassMethodsCypher(req, storyGrantedAnchor, "uid", access)),
		"nornicdb_inheritance":   firstOf(nornicDBRelationshipStoryInheritanceDepthCypher(req, storyGrantedAnchor, "outgoing", "uid", access)),
		"compat_inheritance":     firstOf(relationshipStoryInheritanceDepthCypher(req, storyGrantedAnchor, "outgoing", graphEntityIDPredicate, access)),
		"override_rows":          firstOf(relationshipStoryOverrideRowsCypher(relationshipStoryRequest{QueryType: "overrides", RepoID: codeGrantGrantedRepo, Limit: 50}, access)),
	} {
		if strings.Contains(cypher, "$allowed_repository_ids") || strings.Contains(cypher, "$allowed_scope_ids") {
			t.Fatalf("%s rendered a grant array for an unscoped caller:\n%s", name, cypher)
		}
	}
}

// TestRelationshipStoryClassHierarchyStaysInGrant drives the class_hierarchy
// query type end to end. Its two enrichment reads -- class methods and the
// inheritance walk -- carried no repository binding at all before this batch.
func TestRelationshipStoryClassHierarchyStaysInGrant(t *testing.T) {
	t.Parallel()

	content := storyGrantContent()
	content.entities["entity:story-granted-class"] = EntityContent{
		EntityID:   "entity:story-granted-class",
		EntityName: "StoryAnchorClass",
		EntityType: "Class",
		RepoID:     codeGrantGrantedRepo,
		Language:   "go",
	}
	graph := &storyClauseGraph{
		optionalColumns: []string{"source_repo_fallback_id", "target_repo_fallback_id"},
		seeds: []storyGrantSeed{
			{
				repoByAlias: map[string]string{
					"class": codeGrantGrantedRepo, "method": codeGrantGrantedRepo,
					"anchor": codeGrantGrantedRepo, "source": codeGrantGrantedRepo, "target": codeGrantGrantedRepo,
				},
				row: map[string]any{
					"method_uid": "entity:story-granted-method", "method_name": storyGrantedNeighbour,
					"direction": "outgoing", "type": "INHERITS",
					"source_uid": "entity:story-granted-class", "source_name": "StoryAnchorClass",
					"target_uid": "entity:story-granted-parent", "target_name": storyGrantedNeighbour,
					"depth": 1,
				},
			},
			{
				repoByAlias: map[string]string{
					"class": codeGrantGrantedRepo, "method": codeGrantOtherRepo,
					"anchor": codeGrantGrantedRepo, "source": codeGrantOtherRepo, "target": codeGrantOtherRepo,
				},
				row: map[string]any{
					"method_uid": storyUngrantedEntity, "method_name": storyUngrantedTarget,
					"direction": "outgoing", "type": "INHERITS",
					"source_uid": "entity:story-granted-class", "source_name": "StoryAnchorClass",
					"target_uid": storyUngrantedEntity, "target_name": storyUngrantedTarget,
					"depth": 1,
				},
			},
		},
	}
	auth := codeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
	rec := runStoryRequest(t, storyGrantHandler(GraphBackendNornicDB, graph, content), map[string]any{
		"entity_id":  "entity:story-granted-class",
		"query_type": "class_hierarchy",
		"limit":      50,
	}, &auth)
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	body := rec.Body.String()
	for _, leaked := range []string{storyUngrantedTarget, storyUngrantedEntity, codeGrantOtherRepo} {
		if strings.Contains(body, leaked) {
			t.Fatalf("the scoped class hierarchy leaked %q: %s", leaked, body)
		}
	}
}

// TestRelationshipStoryOverrideRowsStayInGrant covers the repo-scoped override
// branch. Its source is already anchored through the granted repository's own
// File; the OVERRIDES target was the open end.
func TestRelationshipStoryOverrideRowsStayInGrant(t *testing.T) {
	t.Parallel()

	graph := &storyClauseGraph{
		seeds: []storyGrantSeed{
			{
				repoByAlias: map[string]string{"source": codeGrantGrantedRepo, "target": codeGrantGrantedRepo},
				row: map[string]any{
					"direction": "outgoing", "type": "OVERRIDES",
					"source_id": "entity:story-granted-override", "source_name": "StoryOverrideSource",
					"target_id": "entity:story-granted-base", "target_name": storyGrantedNeighbour,
				},
			},
			{
				repoByAlias: map[string]string{"source": codeGrantGrantedRepo, "target": codeGrantOtherRepo},
				row: map[string]any{
					"direction": "outgoing", "type": "OVERRIDES",
					"source_id": "entity:story-granted-override", "source_name": "StoryOverrideSource",
					"target_id": storyUngrantedEntity, "target_name": storyUngrantedTarget,
				},
			},
		},
	}
	auth := codeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
	rec := runStoryRequest(t, storyGrantHandler(GraphBackendNornicDB, graph, storyGrantContent()), map[string]any{
		"query_type": "overrides",
		"repo_id":    codeGrantGrantedRepo,
		"limit":      50,
	}, &auth)
	body := rec.Body.String()
	if !strings.Contains(body, storyGrantedNeighbour) {
		t.Fatalf("the granted override target is missing: %s", body)
	}
	for _, leaked := range []string{storyUngrantedTarget, storyUngrantedEntity, codeGrantOtherRepo} {
		if strings.Contains(body, leaked) {
			t.Fatalf("the scoped override story leaked %q: %s", leaked, body)
		}
	}
}

// TestStoryClauseGraphKeepsOptionalMatchRows proves the fake can still fail.
// A grant written after an OPTIONAL MATCH must leave the out-of-grant row in
// the result with only the optional columns nulled -- the behaviour measured on
// the pinned backend, and the reason a substring assertion proves nothing here.
func TestStoryClauseGraphKeepsOptionalMatchRows(t *testing.T) {
	t.Parallel()

	graph := &storyClauseGraph{
		seeds:           storyNornicDBSeeds(),
		optionalColumns: []string{"target_repo_fallback_id"},
	}
	stranded := `
		MATCH (anchor:Function {uid: $entity_id})-[rel:CALLS]->(target)
		OPTIONAL MATCH (targetRepo:Repository)-[:REPO_CONTAINS]->(targetFile)
		WHERE (target.repo_id IN $allowed_repository_ids OR target.repo_id IN $allowed_scope_ids)
		RETURN target.name as target_name
	`
	params := storyScopedAccess().GraphParams(map[string]any{"entity_id": storyGrantedAnchor})
	rows, err := graph.Run(t.Context(), stranded, params)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2; a grant stranded on an OPTIONAL MATCH drops nothing", len(rows))
	}
	if rows[1]["target_repo_fallback_id"] != nil {
		t.Fatalf("the stranded predicate did not null the optional column: %#v", rows[1])
	}
	if got, want := StringVal(rows[1], "target_name"), storyUngrantedTarget; got != want {
		t.Fatalf("out-of-grant row = %q, want %q kept by the optional pattern", got, want)
	}
}

// TestStoryClauseGraphRecordsParallelRuns pins the fake against the handler's
// own fan-out. relationshipStoryGraphRows issues the incoming and outgoing
// reads from two goroutines when direction is "both", and the class-hierarchy
// story issues three reads at once, so one request calls Run concurrently.
// Drop the lock in Run and this test trips the race detector, which fails every
// other test in the package binary along with it.
func TestStoryClauseGraphRecordsParallelRuns(t *testing.T) {
	t.Parallel()

	graph := &storyClauseGraph{
		seeds:           storyNornicDBSeeds(),
		optionalColumns: []string{"target_repo_fallback_id"},
	}
	ctx := t.Context()
	params := storyScopedAccess().GraphParams(map[string]any{"entity_id": storyGrantedAnchor})
	const readers = 8
	var wg sync.WaitGroup
	wg.Add(readers)
	for range readers {
		go func() {
			defer wg.Done()
			if _, err := graph.Run(ctx, "MATCH (anchor)-[rel:CALLS]->(target) RETURN target.name as target_name", params); err != nil {
				t.Errorf("Run() error = %v", err)
			}
		}()
	}
	wg.Wait()
	if got := len(graph.recordedStatements()); got != readers {
		t.Fatalf("recorded statements = %d, want %d", got, readers)
	}
}

func firstOf(cypher string, _ map[string]any) string { return cypher }
