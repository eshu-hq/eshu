// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/inheritance"
	"github.com/eshu-hq/eshu/go/internal/reducer/sqlrelationship"
)

// deltaRepositoryFactWithoutCheckoutPath returns the repository fact a delta
// generation emits when the reducer cannot qualify any of its changed paths.
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
// leaves the repo root unresolved in git mode (git_selection_native.go resolves
// it only in filesystem mode), so on a symlinked repos root every target
// relativizes to a "../"-prefixed path that normalizeSnapshotRelativePaths
// drops -- while repository.Delta stays true, because it was set from the
// pre-relativization delta.
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

// deltaRepositoryFactWithCheckoutPath is the healthy delta: the same shape, but
// the checkout path is present so every relative path qualifies.
func deltaRepositoryFactWithCheckoutPath(repoID, scopeID string) facts.Envelope {
	env := deltaRepositoryFactWithoutCheckoutPath(repoID, scopeID)
	env.Payload["local_path"] = "/repo"
	return env
}

// fullGenerationRepositoryFact is a repository on a FULL generation: no
// delta_generation key at all. Its edges are re-emitted for every file, so the
// repo-wide retract is the correct scope for it.
func fullGenerationRepositoryFact(repoID, scopeID string) facts.Envelope {
	return facts.Envelope{
		FactKind: factKindRepository,
		ScopeID:  scopeID,
		Payload: map[string]any{
			"graph_id":      repoID,
			"graph_kind":    "repository",
			"repo_id":       repoID,
			"name":          repoID,
			"source_run_id": "run-1",
		},
	}
}

// siblingDeltaGateCase drives one fenced repo-wide-retract domain's real
// delta-scope builder and real refresh-intent builder.
type siblingDeltaGateCase struct {
	name  string
	build func(
		envelopes []facts.Envelope,
		repoIDs []string,
		contexts map[string]ProjectionContext,
		createdAt time.Time,
	) (intents []SharedProjectionIntentRow, deltaRepoIDs []string, qualified map[string][]string)
}

// siblingDeltaGateCases covers all FOUR fenced repo-wide-retract domains.
// Rationale is included deliberately: it is where this gate shape originated,
// and it carried the same defect as the three siblings that copied it (#6216).
func siblingDeltaGateCases() []siblingDeltaGateCase {
	return []siblingDeltaGateCase{
		{
			name: "inheritance",
			build: func(
				envelopes []facts.Envelope,
				repoIDs []string,
				contexts map[string]ProjectionContext,
				createdAt time.Time,
			) ([]SharedProjectionIntentRow, []string, map[string][]string) {
				scope := inheritance.BuildDeltaScope(envelopes)
				return inheritance.BuildRefreshIntents(scope, repoIDs, contexts, createdAt),
					scope.RepositoryIDs, scope.FilePathsByRepoID
			},
		},
		{
			name: "rationale",
			build: func(
				envelopes []facts.Envelope,
				repoIDs []string,
				contexts map[string]ProjectionContext,
				createdAt time.Time,
			) ([]SharedProjectionIntentRow, []string, map[string][]string) {
				scope := buildRationaleDeltaScope(envelopes)
				return buildRationaleRefreshIntents(scope, repoIDs, contexts, createdAt),
					scope.repositoryIDs, scope.filePathsByRepoID
			},
		},
		{
			name: "sql_relationships",
			build: func(
				envelopes []facts.Envelope,
				repoIDs []string,
				contexts map[string]ProjectionContext,
				createdAt time.Time,
			) ([]SharedProjectionIntentRow, []string, map[string][]string) {
				scope := sqlrelationship.BuildDeltaScope(envelopes)
				return sqlrelationship.BuildRefreshIntents(scope, repoIDs, contexts, createdAt),
					scope.RepositoryIDs, scope.FilePathsByRepoID
			},
		},
		{
			name: "shell_exec",
			build: func(
				envelopes []facts.Envelope,
				repoIDs []string,
				contexts map[string]ProjectionContext,
				createdAt time.Time,
			) ([]SharedProjectionIntentRow, []string, map[string][]string) {
				// Shell exec reuses the SQL-relationship delta scope
				// (shell_exec_materialization.go), so it inherits the same gate.
				scope := sqlrelationship.BuildDeltaScope(envelopes)
				return buildShellExecRefreshIntents(scope, repoIDs, contexts, createdAt),
					scope.RepositoryIDs, scope.FilePathsByRepoID
			},
		},
	}
}

func refreshIntentPayloadForRepo(t *testing.T, intents []SharedProjectionIntentRow, repoID string) map[string]any {
	t.Helper()
	for _, intent := range intents {
		if intent.RepositoryID == repoID {
			return intent.Payload
		}
	}
	t.Fatalf("no refresh intent emitted for %q (got %d intents)", repoID, len(intents))
	return nil
}

// TestSiblingRefreshIntentsKeepUnusableDeltaFailClosed is the reducer half of
// the #6216 P0 proof. It pins that a repository ON A DELTA GENERATION always
// gets a delta-scoped refresh intent, even when none of its changed paths could
// be qualified -- so the retract fails closed instead of widening to the whole
// repository.
//
// Why widening is data loss, not a slower correct answer. On a delta
// generation the collector replaces the discovered file set with the changed
// targets alone (resolveNativeSnapshotFileSetForTargets,
// collector/gitrepo/git_snapshot_native.go), so the generation carries
// content-entity facts for the CHANGED files only. The per-edge intents
// therefore re-create only the changed files' edges. A repo-wide
// `DELETE ... WHERE child.repo_id IN $repo_ids` deletes every UNCHANGED file's
// edge too, and nothing in that generation re-creates it: silent wrong graph,
// no error, no dead letter.
//
// So the gate is membership in the delta scope's repositoryIDs -- the
// repositories whose repository fact carried delta_generation -- and NOT "does
// this repository have qualified paths". An empty path list reaches
// collectDeltaFilePaths (storage/cypher/edge_writer_retract_scope.go), which
// rejects it before any statement runs. A dead letter an operator can see beats
// a silently wrong graph.
//
// Each subtest runs the real delta-scope builder first and asserts the input
// invariant it depends on, so a future change that stops producing that shape
// turns this test red rather than leaving it silently vacuous.
func TestSiblingRefreshIntentsKeepUnusableDeltaFailClosed(t *testing.T) {
	t.Parallel()

	const (
		repoID  = "repo-1"
		scopeID = "scope-1"
		genID   = "gen-1"
	)
	createdAt := time.Date(2026, time.August, 28, 12, 0, 0, 0, time.UTC)

	for _, tc := range siblingDeltaGateCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			envelopes := []facts.Envelope{deltaRepositoryFactWithoutCheckoutPath(repoID, scopeID)}
			contexts := buildCodeCallProjectionContexts(envelopes, genID)
			if _, ok := contexts[repoID]; !ok {
				t.Fatalf("no projection context built for %q; the fixture repository fact is malformed", repoID)
			}

			intents, deltaRepoIDs, qualified := tc.build(envelopes, []string{repoID}, contexts, createdAt)

			// Input invariants: the shape under test must actually be the one
			// the delta-scope builder produces, or the assertions below prove
			// nothing.
			if len(deltaRepoIDs) != 1 || deltaRepoIDs[0] != repoID {
				t.Fatalf("delta scope repositoryIDs = %v, want [%q]; the fixture no longer reaches the delta gate",
					deltaRepoIDs, repoID)
			}
			if got := len(qualified[repoID]); got != 0 {
				t.Fatalf("delta scope qualified %d path(s) for %q, want 0", got, repoID)
			}

			payload := refreshIntentPayloadForRepo(t, intents, repoID)
			if payload["delta_projection"] != true {
				t.Fatalf("refresh intent for delta-generation repository %q omits delta_projection (payload=%v); "+
					"the retract widens to the whole repository and deletes every unchanged file's edge, "+
					"which this generation's changed-files-only facts cannot re-create",
					repoID, payload)
			}
			filePaths, ok := payload["delta_file_paths"].([]string)
			if !ok {
				t.Fatalf("delta_file_paths type = %T, want []string", payload["delta_file_paths"])
			}
			if len(filePaths) != 0 {
				t.Fatalf("delta_file_paths = %v, want empty; the fixture qualifies no path", filePaths)
			}
			if payload["intent_type"] != RepoRefreshIntentType {
				t.Fatalf("refresh intent intent_type = %v, want %q", payload["intent_type"], RepoRefreshIntentType)
			}
		})
	}
}

// TestSiblingRefreshIntentsScopeDeltaToItsOwnRepositories pins the other half of
// the gate: hasDelta is SCOPE-wide, so a repository that is NOT on a delta
// generation must keep its repo-wide refresh even when a delta-generation
// repository shares the scope. Its generation re-emits every file, so the
// repo-wide retract is the correct scope for it, and stamping delta_projection
// on it would leave its removed-file edges stale.
//
// This is an invariant guard rather than a reproduced production shape: today a
// git scope is per repository. It is what separates the fix from a plain revert
// to the scope-wide `deltaScope.hasDelta` gate, which would fail this test.
func TestSiblingRefreshIntentsScopeDeltaToItsOwnRepositories(t *testing.T) {
	t.Parallel()

	const (
		deltaRepoID = "repo-delta"
		fullRepoID  = "repo-full"
		scopeID     = "scope-1"
		genID       = "gen-1"
	)
	createdAt := time.Date(2026, time.August, 28, 12, 0, 0, 0, time.UTC)

	for _, tc := range siblingDeltaGateCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			envelopes := []facts.Envelope{
				deltaRepositoryFactWithoutCheckoutPath(deltaRepoID, scopeID),
				fullGenerationRepositoryFact(fullRepoID, scopeID),
			}
			contexts := buildCodeCallProjectionContexts(envelopes, genID)
			if len(contexts) != 2 {
				t.Fatalf("projection contexts = %d, want 2; the fixture repository facts are malformed", len(contexts))
			}

			intents, deltaRepoIDs, _ := tc.build(
				envelopes, []string{deltaRepoID, fullRepoID}, contexts, createdAt)

			if len(deltaRepoIDs) != 1 || deltaRepoIDs[0] != deltaRepoID {
				t.Fatalf("delta scope repositoryIDs = %v, want [%q]", deltaRepoIDs, deltaRepoID)
			}

			deltaPayload := refreshIntentPayloadForRepo(t, intents, deltaRepoID)
			if deltaPayload["delta_projection"] != true {
				t.Fatalf("delta-generation repository %q lost delta_projection (payload=%v)", deltaRepoID, deltaPayload)
			}

			fullPayload := refreshIntentPayloadForRepo(t, intents, fullRepoID)
			if _, ok := fullPayload["delta_projection"]; ok {
				t.Fatalf("full-generation repository %q carries delta_projection=%v; its generation re-emits every "+
					"file, so scoping its retract to a delta sibling's paths leaves its removed-file edges stale",
					fullRepoID, fullPayload["delta_projection"])
			}
			if _, ok := fullPayload["delta_file_paths"]; ok {
				t.Fatalf("full-generation repository %q carries delta_file_paths=%v",
					fullRepoID, fullPayload["delta_file_paths"])
			}
		})
	}
}

// TestSiblingRefreshIntentsCarryQualifiedDeltaPaths is the healthy-delta case:
// the ordinary incremental sync must still get a file-scoped retract. Without
// it the two tests above are satisfied by a builder that stamps
// delta_projection unconditionally and never carries a path.
func TestSiblingRefreshIntentsCarryQualifiedDeltaPaths(t *testing.T) {
	t.Parallel()

	const (
		repoID  = "repo-1"
		scopeID = "scope-1"
		genID   = "gen-1"
	)
	createdAt := time.Date(2026, time.August, 28, 12, 0, 0, 0, time.UTC)

	for _, tc := range siblingDeltaGateCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			envelopes := []facts.Envelope{deltaRepositoryFactWithCheckoutPath(repoID, scopeID)}
			contexts := buildCodeCallProjectionContexts(envelopes, genID)
			intents, _, qualified := tc.build(envelopes, []string{repoID}, contexts, createdAt)

			if got, want := qualified[repoID], "/repo/src/a.py"; len(got) != 1 || got[0] != want {
				t.Fatalf("delta scope qualified %v for %q, want [%v]", got, repoID, want)
			}

			payload := refreshIntentPayloadForRepo(t, intents, repoID)
			if payload["delta_projection"] != true {
				t.Fatalf("healthy delta refresh intent omits delta_projection (payload=%v)", payload)
			}
			filePaths, ok := payload["delta_file_paths"].([]string)
			if !ok {
				t.Fatalf("delta_file_paths type = %T, want []string", payload["delta_file_paths"])
			}
			if len(filePaths) != 1 || filePaths[0] != "/repo/src/a.py" {
				t.Fatalf("delta_file_paths = %v, want [/repo/src/a.py]", filePaths)
			}
		})
	}
}
