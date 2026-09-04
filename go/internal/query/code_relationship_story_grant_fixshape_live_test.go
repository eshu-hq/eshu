// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_relationship_story

// Full-projection fix-shape probes for #5167 batch 2b, question 3.
//
// The reduced-projection candidate in the sibling file proves the predicate
// position. This file proves the shape the fix actually shipped: the complete
// nornicDBRelationshipStoryGraphCypher projection, ORDER BY, SKIP and LIMIT,
// with the grant moved off the trailing OPTIONAL MATCH-attached WHERE and onto
// the anchoring MATCH's own WHERE against the entity nodes' repo_id -- the
// property the canonical node writer already persists.
//
// It matters that the full projection is measured and not only the predicate:
// the pinned build corrupts projections in some multi-clause shapes, so a
// rewrite that filters correctly can still return literal expression text.
package query

import (
	"context"
	"strings"
	"testing"
	"time"
)

// storyFullProjectionCandidate is the outgoing statement with the grant moved.
// Only the WHERE moved; every RETURN column, the ORDER BY and the paging are
// character-for-character the shipped text.
const storyFullProjectionCandidate = `
		MATCH (anchor:Function {uid: $entity_id})-[rel:CALLS]->(target)
		WHERE anchor.repo_id IN $relationship_repo_ids
		  AND target.repo_id IN $relationship_repo_ids
		OPTIONAL MATCH (anchor)<-[:CONTAINS]-(sourceFile:File)
		OPTIONAL MATCH (sourceRepo:Repository)-[:REPO_CONTAINS]->(sourceFile)
		OPTIONAL MATCH (target)<-[:CONTAINS]-(targetFile:File)
		OPTIONAL MATCH (targetRepo:Repository)-[:REPO_CONTAINS]->(targetFile)
		RETURN 'outgoing' as direction,
		       'CALLS' as type,
		       'direct_code_edge' as edge_origin,
		       rel.call_kind as call_kind,
		       rel.reason as reason,
		       rel.confidence as confidence,
		       rel.resolution_method as resolution_method,
		       rel.evidence_source as evidence_source,
		       rel.why_trail_json as why_trail_json,
		       rel.why_trail_truncated as why_trail_truncated,
		       anchor.id as source_legacy_id,
		       anchor.uid as source_uid,
		       anchor.name as source_name,
		       anchor.repo_id as source_node_repo_id,
		       sourceRepo.id as source_repo_fallback_id,
		       sourceRepo.name as source_repo_name,
		       sourceFile.relative_path as source_file_path,
		       anchor.language as source_language_value,
		       anchor.lang as source_lang_value,
		       sourceFile.language as source_file_language,
		       target.id as target_legacy_id,
		       target.uid as target_uid,
		       target.name as target_name,
		       target.repo_id as target_node_repo_id,
		       targetRepo.id as target_repo_fallback_id,
		       targetRepo.name as target_repo_name,
		       targetFile.relative_path as target_file_path,
		       target.language as target_language_value,
		       target.lang as target_lang_value,
		       targetFile.language as target_file_language
		ORDER BY target.name, target.id, target.uid
		SKIP $offset
		LIMIT $limit
	`

// TestLiveNornicDBRelationshipStoryFullProjectionFixShape proves the moved
// predicate filters AND that the 30-column projection survives the move.
func TestLiveNornicDBRelationshipStoryFullProjectionFixShape(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	reader, closeDriver := newLiveStoryReader(ctx, t)
	defer closeDriver()

	rows, err := reader.Run(ctx, storyFullProjectionCandidate, map[string]any{
		"entity_id":             liveClauseAnchorUID,
		"limit":                 50,
		"offset":                0,
		"relationship_repo_ids": []string{codeGrantGrantedRepo},
	})
	if err != nil {
		t.Fatalf("run full-projection candidate: %v", err)
	}
	normalized := normalizeNornicDBRelationshipStoryRows(rows)
	t.Logf("full-projection candidate returned %d rows", len(normalized))
	for _, row := range normalized {
		t.Logf("  source_id=%v source_repo_id=%v target_id=%v target_name=%v target_repo_id=%v target_file_path=%v type=%v confidence=%v",
			row["source_id"], row["source_repo_id"], row["target_id"], row["target_name"],
			row["target_repo_id"], row["target_file_path"], row["type"], row["confidence"])
	}
	if len(normalized) != 1 {
		t.Fatalf("row count = %d, want 1 (the granted callee only)", len(normalized))
	}
	row := normalized[0]
	for key, want := range map[string]string{
		"target_name":      liveClauseGrantedCallee,
		"target_repo_id":   codeGrantGrantedRepo,
		"source_repo_id":   codeGrantGrantedRepo,
		"type":             "CALLS",
		"direction":        "outgoing",
		"target_file_path": "internal/neighbor.go",
	} {
		if got := strings.TrimSpace(StringVal(row, key)); got != want {
			t.Fatalf("%s = %q, want %q; the moved predicate corrupted the projection: %#v", key, got, want, row)
		}
	}
}

// TestLiveNornicDBRelationshipStoryFullProjectionUnscopedIsUnchanged pins the
// other direction. With no grant the fix must render no predicate at all, so
// the caller keeps the shipped statement and the shipped row set.
func TestLiveNornicDBRelationshipStoryFullProjectionUnscopedIsUnchanged(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	reader, closeDriver := newLiveStoryReader(ctx, t)
	defer closeDriver()

	cypher, params := nornicDBRelationshipStoryGraphCypher(
		storyProbeRequest(),
		liveClauseAnchorUID,
		"Function",
		"uid",
		"outgoing",
		repositoryAccessFilter{AllScopes: true},
	)
	if strings.Contains(cypher, "relationship_repo_ids") {
		t.Fatalf("an unscoped caller still rendered a grant array:\n%s", cypher)
	}
	rows, err := reader.Run(ctx, cypher, params)
	if err != nil {
		t.Fatalf("run unscoped story statement: %v", err)
	}
	names := liveClauseRowNames(normalizeNornicDBRelationshipStoryRows(rows), "target_name")
	t.Logf("unscoped shipped statement returned %d rows: %v", len(rows), names)
	for _, want := range []string{liveClauseGrantedCallee, liveClauseUngrantedCallee, liveClauseOrphanCallee} {
		if !liveClauseContainsName(names, want) {
			t.Fatalf("the unscoped answer lost %q: %v", want, names)
		}
	}
}

// TestLiveNornicDBRelationshipStoryClassMethodsFixShape measures a binding for
// the class-methods read, which carries none today. The class and the method
// both have to sit in grant: a class can contain a method the projector
// attributed to another repository.
func TestLiveNornicDBRelationshipStoryClassMethodsFixShape(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	reader, closeDriver := newLiveStoryReader(ctx, t)
	defer closeDriver()

	for _, tc := range []struct {
		name      string
		anchorUID string
		wantCount int
		wantName  string
	}{
		{name: "out_of_grant_class_returns_nothing", anchorUID: liveClauseUngrantedOwnerUID, wantCount: 0},
		{name: "in_grant_class_keeps_its_methods", anchorUID: liveClauseAnchorClassUID, wantCount: 1, wantName: liveClauseGrantedMethod},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := reader.Run(ctx, `
				MATCH (class:Class {uid: $entity_id})-[:CONTAINS]->(method:Function)
				WHERE class.repo_id IN $relationship_repo_ids
				  AND method.repo_id IN $relationship_repo_ids
				RETURN method.id as method_legacy_id,
				       method.uid as method_uid,
				       method.name as method_name,
				       method.path as file_path,
				       method.start_line as start_line,
				       method.end_line as end_line
				ORDER BY method.name, method.id, method.uid
				SKIP $offset
				LIMIT $limit
			`, map[string]any{
				"entity_id":             tc.anchorUID,
				"limit":                 50,
				"offset":                0,
				"relationship_repo_ids": []string{codeGrantGrantedRepo},
			})
			if err != nil {
				t.Fatalf("run candidate class-methods statement: %v", err)
			}
			names := liveClauseRowNames(rows, "method_name")
			t.Logf("%s returned %d rows: %v", tc.name, len(rows), names)
			if len(rows) != tc.wantCount {
				t.Fatalf("row count = %d, want %d: %v", len(rows), tc.wantCount, names)
			}
			if tc.wantName != "" && !liveClauseContainsName(names, tc.wantName) {
				t.Fatalf("candidate statement lost %q: %v", tc.wantName, names)
			}
		})
	}
}

// TestLiveNornicDBRelationshipStoryInheritanceDepthFixShape measures a binding
// for the inheritance walk. Bounding only the far endpoint leaves a gap in
// principle -- an intermediate ancestor in another repository could still join
// two in-grant classes -- but the obvious closure, all(node IN nodes(path)
// WHERE node.repo_id IN $ids), does not filter on the pinned build. That is
// measured next door in TestLiveNornicDBPathListPredicateBehaviour, which pins
// every list form as inert and only the single scalar equality as evaluated.
//
// So the endpoint bound is what the fix can actually rely on in Cypher here,
// and the intermediate-hop gap has to close in Go or by rendering a scalar
// equality when the grant resolves to exactly one repository.
func TestLiveNornicDBRelationshipStoryInheritanceDepthFixShape(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	reader, closeDriver := newLiveStoryReader(ctx, t)
	defer closeDriver()

	for _, tc := range []struct {
		name     string
		cypher   string
		wantRows int
	}{
		{
			name: "endpoint_bound_excludes_the_out_of_grant_ancestor",
			cypher: `MATCH path = (anchor:Class {uid: $entity_id})-[:INHERITS*1..5]->(target:Class)
				WHERE anchor.repo_id IN $relationship_repo_ids
				  AND target.repo_id IN $relationship_repo_ids
				RETURN target.name as target_name, length(path) as depth
				ORDER BY depth DESC
				LIMIT $limit`,
			wantRows: 0,
		},
		{
			name: "single_repository_grant_renders_a_scalar_equality",
			cypher: `MATCH path = (anchor:Class {uid: $entity_id})-[:INHERITS*1..5]->(target:Class)
				WHERE all(node IN nodes(path) WHERE coalesce(node.repo_id, '') = $single_repo_id)
				RETURN target.name as target_name, length(path) as depth
				ORDER BY depth DESC
				LIMIT $limit`,
			wantRows: 0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := reader.Run(ctx, tc.cypher, map[string]any{
				"entity_id":             liveClauseAnchorClassUID,
				"limit":                 50,
				"single_repo_id":        codeGrantGrantedRepo,
				"relationship_repo_ids": []string{codeGrantGrantedRepo},
			})
			if err != nil {
				t.Fatalf("run candidate inheritance statement: %v", err)
			}
			names := liveClauseRowNames(rows, "target_name")
			t.Logf("%s returned %d rows: %v", tc.name, len(rows), names)
			if liveClauseContainsName(names, liveClauseUngrantedParent) {
				t.Fatalf("candidate inheritance statement still crossed into the out-of-grant repository: %v", names)
			}
			if len(rows) != tc.wantRows {
				t.Fatalf("row count = %d, want %d: %v", len(rows), tc.wantRows, names)
			}
		})
	}
}
