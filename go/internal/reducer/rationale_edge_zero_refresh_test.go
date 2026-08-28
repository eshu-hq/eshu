// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestRationaleHandlerRefreshesValidFullRepositoryWithNoEdges(t *testing.T) {
	t.Parallel()
	writer := &recordingRationaleIntentWriter{}
	handler := RationaleEdgeMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			rationaleRepositoryContextFact("run-full-zero"),
			{
				FactKind: factKindContentEntity,
				Payload: map[string]any{
					"repo_id":       "repo-123",
					"entity_id":     "content-entity:plain",
					"entity_type":   "Function",
					"entity_name":   "Plain",
					"relative_path": "src/plain.go",
				},
			},
		}},
		IntentWriter: writer,
	}

	result, err := handler.Handle(context.Background(), rationaleMaterializationIntent())
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if got := len(writer.refreshRows()); got != 1 {
		t.Fatalf("full zero-edge refresh intents = %d, want 1", got)
	}
	if got := len(writer.edgeRows()); got != 0 {
		t.Fatalf("full zero-edge write intents = %d, want 0", got)
	}
	if result.CanonicalWrites != 1 {
		t.Fatalf("CanonicalWrites = %d, want 1 repo-wide refresh", result.CanonicalWrites)
	}

	// Model the next full generation after a prior EXPLAINS edge existed. The
	// zero-edge refresh must retract it even though there are no replacement
	// edge intents to write.
	edges := newRationaleStateModelingEdgeWriter()
	prior := []SharedProjectionIntentRow{{
		ProjectionDomain: DomainRationaleEdges,
		RepositoryID:     "repo-123",
		Payload: map[string]any{
			"repo_id": "repo-123", "rationale_uid": "rationale:prior",
			"target_entity_id": "content-entity:prior", "target_path": "/repo/src/prior.go",
		},
	}}
	if _, err := edges.WriteEdges(context.Background(), DomainRationaleEdges, prior, rationaleEvidenceSource); err != nil {
		t.Fatalf("seed prior rationale edge: %v", err)
	}
	if err := edges.RetractEdges(context.Background(), DomainRationaleEdges, writer.refreshRows(), rationaleEvidenceSource); err != nil {
		t.Fatalf("apply zero-edge full refresh: %v", err)
	}
	if got := len(edges.edgeKeys("repo-123")); got != 0 {
		t.Fatalf("prior rationale edges after zero-comment full refresh = %d, want 0", got)
	}
}

func TestRationaleHandlerDoesNotRefreshRepositoryWithoutSourceRun(t *testing.T) {
	t.Parallel()
	repository := rationaleRepositoryContextFact("")
	writer := &recordingRationaleIntentWriter{}
	handler := RationaleEdgeMaterializationHandler{
		FactLoader:   &stubFactLoader{envelopes: []facts.Envelope{repository}},
		IntentWriter: writer,
	}

	result, err := handler.Handle(context.Background(), rationaleMaterializationIntent())
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if len(writer.rows) != 0 {
		t.Fatalf("missing-source-run repository emitted %d intents, want 0", len(writer.rows))
	}
	if result.CanonicalWrites != 0 {
		t.Fatalf("CanonicalWrites = %d, want 0 without projection context", result.CanonicalWrites)
	}
}

func TestRationaleHandlerDeletionOnlyDeltaEmitsFileScopedRefresh(t *testing.T) {
	t.Parallel()
	repository := rationaleRepositoryContextFact("run-delta-delete")
	repository.Payload["delta_generation"] = true
	repository.Payload["delta_deleted_relative_paths"] = []string{"src/deleted.go"}
	writer := &recordingRationaleIntentWriter{}
	handler := RationaleEdgeMaterializationHandler{
		FactLoader:   &stubFactLoader{envelopes: []facts.Envelope{repository}},
		IntentWriter: writer,
	}

	result, err := handler.Handle(context.Background(), rationaleMaterializationIntent())
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	refreshes := writer.refreshRows()
	if len(refreshes) != 1 {
		t.Fatalf("deletion-only delta refresh intents = %d, want 1", len(refreshes))
	}
	if got := len(writer.edgeRows()); got != 0 {
		t.Fatalf("deletion-only delta edge intents = %d, want 0", got)
	}
	if got, want := refreshes[0].Payload["delta_projection"], true; got != want {
		t.Errorf("delta_projection = %#v, want %#v", got, want)
	}
	paths, _ := refreshes[0].Payload["delta_file_paths"].([]string)
	if len(paths) != 1 || paths[0] != "/repo/src/deleted.go" {
		t.Errorf("delta_file_paths = %#v, want [/repo/src/deleted.go]", paths)
	}
	if result.CanonicalWrites != 1 {
		t.Fatalf("CanonicalWrites = %d, want 1 refresh", result.CanonicalWrites)
	}
}

func rationaleRepositoryContextFact(sourceRunID string) facts.Envelope {
	payload := map[string]any{"repo_id": "repo-123", "local_path": "/repo"}
	if sourceRunID != "" {
		payload["source_run_id"] = sourceRunID
	}
	return facts.Envelope{FactKind: factKindRepository, ScopeID: "scope-code", Payload: payload}
}

func rationaleMaterializationIntent() Intent {
	now := time.Date(2026, time.August, 15, 10, 0, 0, 0, time.UTC)
	return Intent{
		IntentID: "intent-rationale-zero", ScopeID: "scope-code", GenerationID: "gen-2",
		SourceSystem: "git", Domain: DomainRationaleMaterialization,
		EnqueuedAt: now, AvailableAt: now, Status: IntentStatusPending,
	}
}

// TestRationaleHandlerDeltaKeepsUnqualifiedRepositoryFailClosed pins the
// per-repo half of the delta decision, and it REPLACES an earlier test that
// pinned the opposite (#6216).
//
// The earlier test read this fixture as "the repository simply has nothing
// changed in this generation, which is a repo-wide refresh, not a delta", and
// asserted repo-untouched got no delta keys. That reading does not survive the
// collector. A repository is marked Delta only when its git delta is non-empty
// (buildSelectedRepositories, collector/gitrepo/git_selection_native.go, guards
// on GitSyncDelta.IsEmpty), so "delta generation, no changed paths" is never
// emitted for a repository that genuinely had no changes. What DOES emit this
// exact payload is a delta whose changed paths could not be expressed: on a
// symlinked repos root in git mode every target relativizes to a "../"-prefixed
// path that normalizeSnapshotRelativePaths drops, leaving delta_generation=true
// with both path slices empty.
//
// The two are indistinguishable in the repository fact, and only one of them is
// safe to widen. A delta generation carries content-entity facts for the CHANGED
// files only, so a repo-wide retract for such a repository deletes every
// unchanged file's EXPLAINS edge with nothing left to re-create it -- silent
// wrong graph, no error, no dead letter. So the repository stays delta-scoped
// with an empty path list, which collectDeltaFilePaths
// (storage/cypher/edge_writer_retract_scope.go) rejects before any statement
// runs. The partition fails and the intent dead-letters, which an operator can
// see and act on.
//
// A silent third option -- emit no retract at all for this repository -- was
// rejected: it also avoids the over-delete, but it hides the broken delta and
// lets stale edges accumulate with no signal.
func TestRationaleHandlerDeltaKeepsUnqualifiedRepositoryFailClosed(t *testing.T) {
	t.Parallel()
	changed := rationaleRepositoryContextFact("run-delta-mixed")
	changed.Payload["repo_id"] = "repo-changed"
	changed.Payload["delta_generation"] = true
	changed.Payload["delta_deleted_relative_paths"] = []string{"src/deleted.go"}

	// delta_generation with BOTH path slices absent, while local_path is
	// present: the shape a delta whose changed paths could not be expressed
	// produces, and the one a repository that genuinely had no changes never
	// reaches (see this test's doc).
	untouched := rationaleRepositoryContextFact("run-delta-mixed")
	untouched.Payload["repo_id"] = "repo-untouched"
	untouched.Payload["delta_generation"] = true

	writer := &recordingRationaleIntentWriter{}
	handler := RationaleEdgeMaterializationHandler{
		FactLoader:   &stubFactLoader{envelopes: []facts.Envelope{changed, untouched}},
		IntentWriter: writer,
	}
	if _, err := handler.Handle(context.Background(), rationaleMaterializationIntent()); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}

	seen := map[string]bool{}
	for _, row := range writer.refreshRows() {
		repoID, _ := row.Payload["repo_id"].(string)
		paths, _ := row.Payload["delta_file_paths"].([]string)
		isDelta, _ := row.Payload["delta_projection"].(bool)
		seen[repoID] = true
		switch repoID {
		case "repo-changed":
			if !isDelta || len(paths) == 0 {
				t.Errorf("repo-changed: delta_projection=%v paths=%#v, want a real delta", isDelta, paths)
			}
		case "repo-untouched":
			if !isDelta {
				t.Errorf("repo-untouched: delta_projection=%v, want true; dropping it widens the retract to "+
					"the whole repository and deletes every unchanged file's EXPLAINS edge, which a delta "+
					"generation's changed-files-only facts cannot re-create", isDelta)
			}
			if _, present := row.Payload["delta_file_paths"]; !present {
				t.Error("repo-untouched carries no delta_file_paths key; the retract dispatch needs the empty " +
					"list to reject the intent instead of running a repo-wide DELETE")
			}
			if len(paths) != 0 {
				t.Errorf("repo-untouched: delta_file_paths=%#v, want empty", paths)
			}
		}
	}
	for _, repoID := range []string{"repo-changed", "repo-untouched"} {
		if !seen[repoID] {
			t.Fatalf("no refresh intent emitted for %q; the fixture no longer reaches the gate", repoID)
		}
	}
}
