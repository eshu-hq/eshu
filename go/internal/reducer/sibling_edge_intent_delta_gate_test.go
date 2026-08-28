// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// deltaRepositoryFactWithoutCheckoutPath returns the repository fact a delta
// generation emits when the collector had no local checkout path to record.
//
// It is the production shape, not a contrived one. repositoryFactEnvelope
// (collector/gitrepo/git_content_fact_envelopes.go) writes delta_generation and
// both delta path slices unconditionally once the snapshot is a delta, but
// writes local_path only when repositoryidentity resolved one -- and it never
// writes "path" at all. The reducer's delta-scope builders qualify every
// relative path against that checkout path (semanticQualifyDeltaPath returns ""
// for an empty repoPath), so this fact yields hasDelta=true with NO qualified
// path for its own repository.
//
// The same empty-path outcome is reachable a second way, without touching
// local_path: relativePathsForSnapshotTargets (collector/gitrepo/
// git_snapshot_delta.go) resolves each changed target through EvalSymlinks but
// leaves the repo root unresolved in git mode, so on a symlinked repos root
// every target relativizes to a "../"-prefixed path that
// normalizeSnapshotRelativePaths drops -- while repository.Delta stays true,
// because git_selection_native.go set it from the pre-relativization delta.
func deltaRepositoryFactWithoutCheckoutPath(repoID, scopeID string) facts.Envelope {
	return facts.Envelope{
		FactKind: factKindRepository,
		ScopeID:  scopeID,
		Payload: map[string]any{
			"graph_id":                     repoID,
			"graph_kind":                   "repository",
			"repo_id":                      repoID,
			"name":                         repoID,
			"source_run_id":                "run-1",
			"delta_generation":             true,
			"delta_relative_paths":         []any{"src/a.py"},
			"delta_deleted_relative_paths": []any{},
		},
	}
}

// TestSiblingRefreshIntentsGateDeltaProjectionOnRepositoryOwnPaths pins the
// #6216 gate for the three fenced repo-wide-retract domains whose refresh
// builders still read the SCOPE-wide hasDelta flag: inheritance, SQL
// relationships and shell exec. The rationale sibling already gates on the
// repository's own path list; these three did not.
//
// The consequence of the scope-wide gate is not a wrong DELETE, it is no
// DELETE at all: the refresh intent goes out carrying delta_projection:true
// with an EMPTY delta_file_paths, and collectDeltaFilePaths
// (storage/cypher/edge_writer_retract_scope.go) rejects exactly that shape --
// "delta retract requires delta_file_paths" -- so the partition fails and the
// intent dead-letters instead of degrading to the repo-wide retract it should
// have asked for. See
// TestEdgeWriterRetractEdgesSiblingEmptyDeltaFilePathsFailsThePartition in
// storage/cypher for that half.
//
// Each subtest runs the real delta-scope builder first and asserts the input
// invariant it depends on (hasDelta true, no qualified path for this
// repository), so a future change that stops producing that shape turns this
// test red rather than leaving it silently vacuous.
func TestSiblingRefreshIntentsGateDeltaProjectionOnRepositoryOwnPaths(t *testing.T) {
	t.Parallel()

	const (
		repoID  = "repo-1"
		scopeID = "scope-1"
		genID   = "gen-1"
	)
	createdAt := time.Date(2026, time.August, 28, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name  string
		build func(envelopes []facts.Envelope, contexts map[string]ProjectionContext) ([]SharedProjectionIntentRow, bool, int)
	}{
		{
			name: "inheritance",
			build: func(envelopes []facts.Envelope, contexts map[string]ProjectionContext) ([]SharedProjectionIntentRow, bool, int) {
				scope := buildInheritanceDeltaScope(envelopes)
				return buildInheritanceRefreshIntents(scope, []string{repoID}, contexts, createdAt),
					scope.hasDelta, len(scope.filePathsByRepoID[repoID])
			},
		},
		{
			name: "sql_relationships",
			build: func(envelopes []facts.Envelope, contexts map[string]ProjectionContext) ([]SharedProjectionIntentRow, bool, int) {
				scope := buildSQLRelationshipDeltaScope(envelopes)
				return buildSQLRelationshipRefreshIntents(scope, []string{repoID}, contexts, createdAt),
					scope.hasDelta, len(scope.filePathsByRepoID[repoID])
			},
		},
		{
			name: "shell_exec",
			build: func(envelopes []facts.Envelope, contexts map[string]ProjectionContext) ([]SharedProjectionIntentRow, bool, int) {
				// Shell exec reuses the SQL-relationship delta scope
				// (shell_exec_materialization.go), so it inherits the same gate.
				scope := buildSQLRelationshipDeltaScope(envelopes)
				return buildShellExecRefreshIntents(scope, []string{repoID}, contexts, createdAt),
					scope.hasDelta, len(scope.filePathsByRepoID[repoID])
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			envelopes := []facts.Envelope{deltaRepositoryFactWithoutCheckoutPath(repoID, scopeID)}
			contexts := buildCodeCallProjectionContexts(envelopes, genID)
			if _, ok := contexts[repoID]; !ok {
				t.Fatalf("no projection context built for %q; the fixture repository fact is malformed", repoID)
			}

			intents, hasDelta, qualifiedPaths := tc.build(envelopes, contexts)

			// Input invariant: the shape under test must actually be the one
			// the delta-scope builder produces, or the assertion below proves
			// nothing.
			if !hasDelta {
				t.Fatal("delta scope reported hasDelta=false; the fixture no longer reaches the delta gate")
			}
			if qualifiedPaths != 0 {
				t.Fatalf("delta scope qualified %d path(s) for %q, want 0", qualifiedPaths, repoID)
			}

			if len(intents) != 1 {
				t.Fatalf("len(intents) = %d, want 1", len(intents))
			}
			payload := intents[0].Payload
			if _, ok := payload["delta_projection"]; ok {
				t.Fatalf("refresh intent for %q carries delta_projection with delta_file_paths=%v; "+
					"collectDeltaFilePaths rejects an empty list, so this intent dead-letters instead of "+
					"falling back to the repo-wide retract", repoID, payload["delta_file_paths"])
			}
			if _, ok := payload["delta_file_paths"]; ok {
				t.Fatalf("refresh intent for %q carries delta_file_paths=%v without delta_projection",
					repoID, payload["delta_file_paths"])
			}
			if payload["intent_type"] != RepoRefreshIntentType {
				t.Fatalf("refresh intent intent_type = %v, want %q", payload["intent_type"], RepoRefreshIntentType)
			}
		})
	}
}
