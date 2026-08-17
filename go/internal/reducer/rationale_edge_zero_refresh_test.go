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

// TestRationaleHandlerDeltaSkipsRepositoryWithNoQualifiedPaths pins the
// per-repo half of the delta decision. hasDelta is a SCOPE-wide flag, so one
// repository with qualified changed paths used to stamp delta_projection=true
// onto every repository in the scope -- including ones whose own path list is
// empty. That payload is unroutable: collectDeltaFilePaths
// (edge_writer_retract_scope.go) rejects delta_projection with no
// delta_file_paths, the partition fails, and the intent dead-letters. The
// repository simply has nothing changed in this generation, which is a
// repo-wide refresh, not a delta.
func TestRationaleHandlerDeltaSkipsRepositoryWithNoQualifiedPaths(t *testing.T) {
	t.Parallel()
	changed := rationaleRepositoryContextFact("run-delta-mixed")
	changed.Payload["repo_id"] = "repo-changed"
	changed.Payload["delta_generation"] = true
	changed.Payload["delta_deleted_relative_paths"] = []string{"src/deleted.go"}

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

	for _, row := range writer.refreshRows() {
		repoID, _ := row.Payload["repo_id"].(string)
		paths, _ := row.Payload["delta_file_paths"].([]string)
		isDelta, _ := row.Payload["delta_projection"].(bool)
		switch repoID {
		case "repo-changed":
			if !isDelta || len(paths) == 0 {
				t.Errorf("repo-changed: delta_projection=%v paths=%#v, want a real delta", isDelta, paths)
			}
		case "repo-untouched":
			if isDelta {
				t.Errorf("repo-untouched carries delta_projection=true with paths=%#v; an empty delta payload is unroutable and dead-letters, so this repository must emit a repo-wide refresh", paths)
			}
			if _, present := row.Payload["delta_file_paths"]; present {
				t.Errorf("repo-untouched carries delta_file_paths=%#v; a repo-wide refresh must not carry delta keys at all", paths)
			}
		}
	}
}
