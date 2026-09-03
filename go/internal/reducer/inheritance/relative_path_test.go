// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inheritance

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/schemadecode"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// TestExtractInheritanceRowsPopulatesChildPathFromRelativePath is the #5996
// regression test. It builds content_entity envelopes shaped exactly like
// production: contentEntityFactEnvelope
// (contentEntityFactEnvelope in go/internal/collector/gitrepo/git_content_fact_envelopes.go) emits
// "relative_path" and never a top-level "path" key. Before the fix,
// declaredInheritanceRow's childPath argument read "path" -- a key absent from
// this fixture, matching every real content_entity fact -- so child_path was
// "" for every inheritance edge in production.
func TestExtractInheritanceRowsPopulatesChildPathFromRelativePath(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: factload.FactKindContentEntity,
			Payload: map[string]any{
				"repo_id":       "repo-1",
				"entity_id":     "content-entity:e_parent",
				"entity_type":   "Class",
				"entity_name":   "ParentClass",
				"relative_path": "src/parent.py",
			},
		},
		{
			FactKind: factload.FactKindContentEntity,
			Payload: map[string]any{
				"repo_id":       "repo-1",
				"entity_id":     "content-entity:e_child",
				"entity_type":   "Class",
				"entity_name":   "ChildClass",
				"relative_path": "src/child.py",
				"entity_metadata": map[string]any{
					"bases": []any{"ParentClass"},
				},
			},
		},
	}

	_, rows := ExtractRows(envelopes)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if got, want := rows[0]["child_path"], "src/child.py"; got != want {
		t.Fatalf("child_path = %#v, want %#v (production content_entity facts carry"+
			" \"relative_path\", never \"path\" -- see contentEntityFactEnvelope in git_content_fact_envelopes.go)", got, want)
	}
}

// TestExtractInheritanceRowsMethodAliasChildPathFromRelativePath covers the
// method-to-method ALIASES derivation path, whose childPath comes from
// buildInheritanceMethodIndex (not the direct declaredInheritanceRow call
// sites covered above). It shares the exact #5996 defect: the method index
// stored path under "path", which no production Function content_entity fact
// carries either.
func TestExtractInheritanceRowsMethodAliasChildPathFromRelativePath(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: factload.FactKindContentEntity,
			Payload: map[string]any{
				"repo_id":       "repo-1",
				"entity_id":     "content-entity:e_logger_trait",
				"entity_type":   "Trait",
				"entity_name":   "LoggerTrait",
				"relative_path": "traits/logger_trait.py",
			},
		},
		{
			FactKind: factload.FactKindContentEntity,
			Payload: map[string]any{
				"repo_id":       "repo-1",
				"entity_id":     "content-entity:e_logger_log",
				"entity_type":   "Function",
				"entity_name":   "log",
				"relative_path": "traits/logger_trait.py",
				"entity_metadata": map[string]any{
					"class_context": "LoggerTrait",
				},
			},
		},
		{
			FactKind: factload.FactKindContentEntity,
			Payload: map[string]any{
				"repo_id":       "repo-1",
				"entity_id":     "content-entity:e_worker",
				"entity_type":   "Class",
				"entity_name":   "Worker",
				"relative_path": "services/worker.py",
				"entity_metadata": map[string]any{
					"trait_adaptations": []any{"LoggerTrait::log as debugLog"},
				},
			},
		},
		{
			FactKind: factload.FactKindContentEntity,
			Payload: map[string]any{
				"repo_id":       "repo-1",
				"entity_id":     "content-entity:e_worker_debug_log",
				"entity_type":   "Function",
				"entity_name":   "debugLog",
				"relative_path": "services/worker.py",
				"entity_metadata": map[string]any{
					"class_context": "Worker",
				},
			},
		},
	}

	_, rows := ExtractRows(envelopes)

	var aliasRow map[string]any
	for _, row := range rows {
		if row["relationship_type"] == "ALIASES" && row["child_entity_id"] == "content-entity:e_worker_debug_log" {
			aliasRow = row
		}
	}
	if aliasRow == nil {
		t.Fatalf("no method-to-method ALIASES row found in %#v", rows)
	}
	if got, want := aliasRow["child_path"], "services/worker.py"; got != want {
		t.Fatalf("child_path = %#v, want %#v", got, want)
	}
}

// TestInheritanceFilePartitionKeyChangesWithProductionChildPath is the #5996
// partition-key regression test. It proves two things against production-
// shaped facts (relative_path, no "path" key):
//
//  1. The per-edge partition key the reducer actually emits today (post-fix)
//     is NOT the key the pre-fix blank-childPath read would have produced --
//     i.e. the bug materially changed the file-scoped hash anchor for every
//     inheritance edge, not merely an unused struct field.
//  2. Per inheritanceFilePartitionKey's own doc (inheritance_intents.go:19-29),
//     collision-freedom comes from the edge identity component
//     (child->parent:relationship_type), not from child_path alone: two edges
//     in different files, even with the SAME blank child_path, already hash to
//     DIFFERENT keys because their edge identities differ. So the pre-fix bug
//     did not collapse distinct edges onto one shared partition key or drop
//     rows via LatestIntentsByRepoAndPartition -- it silently broke the
//     documented "value reads as file-scoped" / re-ingest-stability property
//     of the key (and the child_path provenance field on every edge payload),
//     not partition-key collision-freedom. This test asserts that corrected
//     understanding directly, rather than asserting a collision that does not
//     occur.
func TestInheritanceFilePartitionKeyChangesWithProductionChildPath(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: factload.FactKindRepository,
			ScopeID:  "scope-1",
			Payload: map[string]any{
				"repo_id":       "repo-1",
				"path":          "/repo",
				"source_run_id": "run-1",
			},
		},
		{
			FactKind: factload.FactKindContentEntity,
			ScopeID:  "scope-1",
			Payload: map[string]any{
				"repo_id":       "repo-1",
				"entity_id":     "content-entity:e_parent",
				"entity_type":   "Class",
				"entity_name":   "ParentClass",
				"relative_path": "src/parent.py",
			},
		},
		{
			FactKind: factload.FactKindContentEntity,
			ScopeID:  "scope-1",
			Payload: map[string]any{
				"repo_id":       "repo-1",
				"entity_id":     "content-entity:e_child_a",
				"entity_type":   "Class",
				"entity_name":   "ChildA",
				"relative_path": "src/a.py",
				"entity_metadata": map[string]any{
					"bases": []any{"ParentClass"},
				},
			},
		},
		{
			FactKind: factload.FactKindContentEntity,
			ScopeID:  "scope-1",
			Payload: map[string]any{
				"repo_id":       "repo-1",
				"entity_id":     "content-entity:e_child_b",
				"entity_type":   "Class",
				"entity_name":   "ChildB",
				"relative_path": "src/b.py",
				"entity_metadata": map[string]any{
					"bases": []any{"ParentClass"},
				},
			},
		},
	}

	repoIDs, rows := ExtractRows(envelopes)
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}
	deltaScope := BuildDeltaScope(envelopes)
	contextByRepoID := schemadecode.BuildProjectionContexts(envelopes, "gen-1")
	now := time.Date(2026, time.June, 18, 21, 0, 0, 0, time.UTC)

	intents := BuildSharedIntentRows(rows, deltaScope, repoIDs, contextByRepoID, now)

	perEdge := make(map[string]sharedintent.Row)
	for _, intent := range intents {
		if isRepoRefreshRow(intent) {
			continue
		}
		perEdge[payloadcore.AnyToString(intent.Payload["child_entity_id"])] = intent
	}
	if len(perEdge) != 2 {
		t.Fatalf("per-edge intents = %d, want 2", len(perEdge))
	}

	childA := perEdge["content-entity:e_child_a"]
	childB := perEdge["content-entity:e_child_b"]

	// (1) The emitted key reflects the real, non-blank child_path.
	if got, want := childA.Payload["child_path"], "src/a.py"; got != want {
		t.Fatalf("child A child_path = %#v, want %#v", got, want)
	}
	if got, want := childB.Payload["child_path"], "src/b.py"; got != want {
		t.Fatalf("child B child_path = %#v, want %#v", got, want)
	}

	edgeIdentityA := inheritanceEdgeIdentityKey(childA.Payload)
	edgeIdentityB := inheritanceEdgeIdentityKey(childB.Payload)
	blankKeyA := inheritanceFilePartitionKey("repo-1", "", edgeIdentityA)
	blankKeyB := inheritanceFilePartitionKey("repo-1", "", edgeIdentityB)

	// The bug materially changed the key: today's key differs from what the
	// pre-fix blank-childPath read would have produced for the same edge.
	if childA.PartitionKey == blankKeyA {
		t.Fatalf("child A partition key %q unchanged from the blank-childPath (pre-fix) key -- fix had no effect", childA.PartitionKey)
	}
	if childB.PartitionKey == blankKeyB {
		t.Fatalf("child B partition key %q unchanged from the blank-childPath (pre-fix) key -- fix had no effect", childB.PartitionKey)
	}

	// Distinct files/edges get distinct keys both before and after the fix,
	// because edge identity (not child_path) is what keeps the key
	// collision-free (inheritance_intents.go:19-29) -- confirming the fix
	// closes a provenance/stability gap, not a collision.
	if blankKeyA == blankKeyB {
		t.Fatalf("blank-childPath keys collided across distinct edges (%q); "+
			"expected edge identity alone to keep them apart even pre-fix", blankKeyA)
	}
	if childA.PartitionKey == childB.PartitionKey {
		t.Fatalf("post-fix partition keys collided across distinct files: %q", childA.PartitionKey)
	}
}

// TestInheritanceDeltaRetractTargetIgnoresContentEntityChildPath is the second
// child_path consumer this #5996 fix was asked to cover: the delta-scope
// file-retraction target (the refresh intent's delta_file_paths, which
// BuildRetractInheritanceEdgeStatementsByFilePath binds as the Cypher
// `WHERE child.path IN $file_paths` parameter).
//
// This is what was tried to exercise that consumer directly: build the SAME
// delta generation (one repository fact with delta_generation=true and
// delta_relative_paths) three times, varying ONLY the content_entity facts'
// path-shaped keys -- "path" only, "relative_path" only (production shape),
// and neither -- while keeping every entity_id/base/bases identical. If the
// retraction target consumed child_path (derived from a content_entity fact),
// these three runs would diverge once child_path started resolving instead of
// staying blank. They do not: BuildDeltaScope (the function whose
// output becomes the refresh intent's delta_file_paths, which is the only
// thing BuildRetractInheritanceEdgeStatementsByFilePath's $file_paths
// parameter is built from -- see inheritance_delta_scope.go and
// canonical_inheritance_retract.go) reads delta_relative_paths /
// delta_deleted_relative_paths off the REPOSITORY fact only; it never inspects
// a content_entity fact at all, so it cannot see child_path regardless of
// which key populates it. This test asserts that invariance directly instead
// of asserting a false dependency, and a full-repo grep
// (`rg -n '"child_path"' --type go`, run 2026-08-18) confirms the only
// producer/consumer pair for the child_path payload key in the whole tree is
// declaredInheritanceRow (writes it) and BuildSharedIntentRows'
// inheritanceFilePartitionKey call (reads it) -- covered by the test above.
// No retraction code path reads it, so a test asserting retraction now
// "targets the right file because child_path is populated" would not be
// exercising the fix at all.
func TestInheritanceDeltaRetractTargetIgnoresContentEntityChildPath(t *testing.T) {
	t.Parallel()

	buildDeltaFilePaths := func(t *testing.T, childPathKeys ...string) []string {
		t.Helper()
		repository := facts.Envelope{
			FactKind: factload.FactKindRepository,
			ScopeID:  "scope-1",
			Payload: map[string]any{
				"repo_id":              "repo-1",
				"path":                 "/repo",
				"source_run_id":        "run-1",
				"delta_generation":     true,
				"delta_relative_paths": []string{"src/child.py"},
			},
		}
		childPayload := map[string]any{
			"repo_id":     "repo-1",
			"entity_id":   "content-entity:e_child",
			"entity_type": "Class",
			"entity_name": "ChildClass",
			"entity_metadata": map[string]any{
				"bases": []any{"ParentClass"},
			},
		}
		for _, key := range childPathKeys {
			childPayload[key] = "src/child.py"
		}
		envelopes := []facts.Envelope{
			repository,
			{
				FactKind: factload.FactKindContentEntity,
				ScopeID:  "scope-1",
				Payload: map[string]any{
					"repo_id":     "repo-1",
					"entity_id":   "content-entity:e_parent",
					"entity_type": "Class",
					"entity_name": "ParentClass",
				},
			},
			{FactKind: factload.FactKindContentEntity, ScopeID: "scope-1", Payload: childPayload},
		}

		deltaScope := BuildDeltaScope(envelopes)
		if !deltaScope.HasDelta {
			t.Fatal("fixture must produce a delta scope")
		}
		refresh := BuildRefreshIntents(
			deltaScope, []string{"repo-1"},
			schemadecode.BuildProjectionContexts(envelopes, "gen-1"),
			time.Date(2026, time.June, 18, 22, 0, 0, 0, time.UTC),
		)
		if len(refresh) != 1 {
			t.Fatalf("refresh intents = %d, want 1", len(refresh))
		}
		paths, ok := refresh[0].Payload["delta_file_paths"].([]string)
		if !ok {
			t.Fatalf("delta_file_paths type = %T, want []string", refresh[0].Payload["delta_file_paths"])
		}
		return paths
	}

	withPathKey := buildDeltaFilePaths(t, "path")
	withRelativePathKey := buildDeltaFilePaths(t, "relative_path")
	withNeitherKey := buildDeltaFilePaths(t)

	want := []string{"/repo/src/child.py"}
	for name, got := range map[string][]string{
		"path key":          withPathKey,
		"relative_path key": withRelativePathKey,
		"neither key":       withNeitherKey,
	} {
		if len(got) != len(want) || got[0] != want[0] {
			t.Fatalf("%s: delta_file_paths = %#v, want %#v", name, got, want)
		}
	}
}
