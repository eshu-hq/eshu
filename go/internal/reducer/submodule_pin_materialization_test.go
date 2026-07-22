// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	submodulev1 "github.com/eshu-hq/eshu/sdk/go/factschema/submodule/v1"
)

// recordingSubmodulePinEdgeWriter is a SharedProjectionEdgeWriter fake that
// records every retract/write call, mirroring
// recordingCodeownersOwnershipEdgeWriter (codeowners_ownership_materialization_test.go).
type recordingSubmodulePinEdgeWriter struct {
	retractDomain string
	retractRows   []SharedProjectionIntentRow
	writeDomain   string
	writeRows     []SharedProjectionIntentRow
}

func (r *recordingSubmodulePinEdgeWriter) RetractEdges(
	_ context.Context,
	domain string,
	rows []SharedProjectionIntentRow,
	_ string,
) error {
	r.retractDomain = domain
	r.retractRows = append(r.retractRows, rows...)
	return nil
}

func (r *recordingSubmodulePinEdgeWriter) WriteEdges(
	_ context.Context,
	domain string,
	rows []SharedProjectionIntentRow,
	_ string,
) error {
	r.writeDomain = domain
	r.writeRows = append(r.writeRows, rows...)
	return nil
}

func submodulePinEnvelope(parentRepoID, submodulePath string, resolvedRepoID, pinnedSHA *string) facts.Envelope {
	payload, err := factschema.EncodeSubmodulePin(submodulev1.Pin{
		ParentRepoID:   parentRepoID,
		SubmodulePath:  submodulePath,
		ResolvedRepoID: resolvedRepoID,
		PinnedSHA:      pinnedSHA,
	})
	if err != nil {
		panic(err)
	}
	return facts.Envelope{
		FactKind: factKindSubmodulePin,
		Payload:  payload,
	}
}

func strPtr(s string) *string { return &s }

func submodulePinIntent(scopeID, generationID string) Intent {
	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	return Intent{
		IntentID:     "intent-submodule-" + scopeID + "-" + generationID,
		ScopeID:      scopeID,
		GenerationID: generationID,
		SourceSystem: "git",
		Domain:       DomainSubmodulePin,
		EnqueuedAt:   now,
		AvailableAt:  now,
		Status:       IntentStatusPending,
	}
}

func TestSubmodulePinHandlerBuildsOneEdgePerResolvedFact(t *testing.T) {
	t.Parallel()

	writer := &recordingSubmodulePinEdgeWriter{}
	handler := SubmodulePinEdgeMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			submodulePinEnvelope("repo-parent", "vendor/lib-a", strPtr("repo-lib-a"), strPtr("abc123")),
			submodulePinEnvelope("repo-parent", "vendor/lib-b", strPtr("repo-lib-b"), nil),
		}},
		EdgeWriter: writer,
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) {
			return true, nil
		},
	}

	result, err := handler.Handle(context.Background(), submodulePinIntent("scope-1", "gen-1"))
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if result.CanonicalWrites != 2 {
		t.Fatalf("CanonicalWrites = %d, want 2", result.CanonicalWrites)
	}
	if len(writer.writeRows) != 2 {
		t.Fatalf("writeRows len = %d, want 2", len(writer.writeRows))
	}

	wantPaths := map[string]bool{"vendor/lib-a": false, "vendor/lib-b": false}
	for _, row := range writer.writeRows {
		if row.ProjectionDomain != DomainSubmodulePinEdges {
			t.Errorf("ProjectionDomain = %q, want %q", row.ProjectionDomain, DomainSubmodulePinEdges)
		}
		if row.RepositoryID != "repo-parent" {
			t.Errorf("RepositoryID = %q, want repo-parent", row.RepositoryID)
		}
		path, _ := row.Payload["submodule_path"].(string)
		if _, ok := wantPaths[path]; !ok {
			t.Fatalf("unexpected submodule_path %q in row %#v", path, row.Payload)
		}
		wantPaths[path] = true
		if row.Payload["generation_id"] != "gen-1" {
			t.Errorf("generation_id = %#v, want gen-1", row.Payload["generation_id"])
		}
	}
	for path, seen := range wantPaths {
		if !seen {
			t.Errorf("submodule_path %q never produced a row", path)
		}
	}

	if len(writer.retractRows) != 1 {
		t.Fatalf("retractRows len = %d, want 1", len(writer.retractRows))
	}
	if writer.retractRows[0].RepositoryID != "repo-parent" {
		t.Errorf("retract RepositoryID = %q, want repo-parent", writer.retractRows[0].RepositoryID)
	}
	if writer.retractRows[0].Payload != nil {
		t.Errorf("whole-scope retract Payload = %#v, want nil", writer.retractRows[0].Payload)
	}
}

// TestSubmodulePinHandlerUnresolvedURLProjectsNoEdge proves the #5420 Phase 3
// design contract: a submodule.pin fact whose URL never resolved to a known
// repository (ResolvedRepoID nil) MUST NOT project a PINS_SUBMODULE edge —
// the reducer must never guess a target Repository id.
func TestSubmodulePinHandlerUnresolvedURLProjectsNoEdge(t *testing.T) {
	t.Parallel()

	writer := &recordingSubmodulePinEdgeWriter{}
	handler := SubmodulePinEdgeMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			submodulePinEnvelope("repo-parent", "vendor/unresolved", nil, strPtr("deadbeef")),
		}},
		EdgeWriter: writer,
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) {
			return true, nil
		},
	}

	result, err := handler.Handle(context.Background(), submodulePinIntent("scope-1", "gen-1"))
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if result.CanonicalWrites != 0 {
		t.Fatalf("CanonicalWrites = %d, want 0 (unresolved submodule URL projects no edge)", result.CanonicalWrites)
	}
	if len(writer.writeRows) != 0 {
		t.Fatalf("writeRows len = %d, want 0", len(writer.writeRows))
	}
	// No repositoryIDs are derivable (no written row, no delta scope), so no
	// retract call happens either — nothing to sweep.
	if len(writer.retractRows) != 0 {
		t.Fatalf("retractRows len = %d, want 0", len(writer.retractRows))
	}
}

// TestSubmodulePinHandlerDeletedFileRetractsWithoutWrites is the mandatory
// delta-retract proof: when ".gitmodules" is removed (the repository delta
// reports it deleted, and no submodule.pin fact for it survives into the new
// generation), the handler MUST still sweep the stale PINS_SUBMODULE edges
// even though it writes zero new edges.
func TestSubmodulePinHandlerDeletedFileRetractsWithoutWrites(t *testing.T) {
	t.Parallel()

	writer := &recordingSubmodulePinEdgeWriter{}
	handler := SubmodulePinEdgeMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			{
				FactKind: factKindRepository,
				Payload: map[string]any{
					"repo_id":                      "repo-parent",
					"local_path":                   "/repo",
					"delta_generation":             true,
					"delta_deleted_relative_paths": []string{".gitmodules"},
				},
			},
			// No submodule.pin facts survive: the file was removed.
		}},
		EdgeWriter: writer,
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) {
			return true, nil
		},
	}

	result, err := handler.Handle(context.Background(), submodulePinIntent("scope-1", "gen-2"))
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if result.CanonicalWrites != 0 {
		t.Fatalf("CanonicalWrites = %d, want 0", result.CanonicalWrites)
	}
	if writer.retractDomain != DomainSubmodulePinEdges {
		t.Fatalf("retractDomain = %q, want %q", writer.retractDomain, DomainSubmodulePinEdges)
	}
	if len(writer.retractRows) != 1 {
		t.Fatalf("retractRows len = %d, want 1", len(writer.retractRows))
	}
	if writer.retractRows[0].Payload != nil {
		t.Fatalf("retract Payload = %#v, want nil (whole-repository retract)", writer.retractRows[0].Payload)
	}
	if writer.retractRows[0].RepositoryID != "repo-parent" {
		t.Fatalf("retract RepositoryID = %q, want repo-parent", writer.retractRows[0].RepositoryID)
	}
}

// TestSubmodulePinHandlerNonGitmodulesDeltaSkipsRetract proves the design
// stays narrow: a delta whose changed paths do NOT touch ".gitmodules" skips
// retraction entirely for that repository (submodule.pin has only one
// recognized source location, so an untouched ".gitmodules" provably means
// nothing about this repo's PINS_SUBMODULE edges could have changed) — unlike
// CODEOWNERS, which keeps a harmless no-op path-scoped retract row because it
// has three candidate locations.
func TestSubmodulePinHandlerNonGitmodulesDeltaSkipsRetract(t *testing.T) {
	t.Parallel()

	writer := &recordingSubmodulePinEdgeWriter{}
	handler := SubmodulePinEdgeMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			{
				FactKind: factKindRepository,
				Payload: map[string]any{
					"repo_id":              "repo-parent",
					"local_path":           "/repo",
					"delta_generation":     true,
					"delta_relative_paths": []string{"main.go"},
				},
			},
			// No submodule.pin facts: .gitmodules itself did not change.
		}},
		EdgeWriter: writer,
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) {
			return true, nil
		},
	}

	result, err := handler.Handle(context.Background(), submodulePinIntent("scope-1", "gen-3"))
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if result.CanonicalWrites != 0 {
		t.Fatalf("CanonicalWrites = %d, want 0", result.CanonicalWrites)
	}
	if len(writer.retractRows) != 0 {
		t.Fatalf("retractRows len = %d, want 0 (untouched .gitmodules skips retract)", len(writer.retractRows))
	}
}

// TestSubmodulePinHandlerChangedGitmodulesForcesWholeRepoRetract proves a
// changed (not deleted) ".gitmodules" forces a whole-repository retract
// before rewriting the current generation's pins.
func TestSubmodulePinHandlerChangedGitmodulesForcesWholeRepoRetract(t *testing.T) {
	t.Parallel()

	writer := &recordingSubmodulePinEdgeWriter{}
	handler := SubmodulePinEdgeMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			{
				FactKind: factKindRepository,
				Payload: map[string]any{
					"repo_id":              "repo-parent",
					"local_path":           "/repo",
					"delta_generation":     true,
					"delta_relative_paths": []string{".gitmodules"},
				},
			},
			submodulePinEnvelope("repo-parent", "vendor/lib-a", strPtr("repo-lib-a"), strPtr("abc123")),
		}},
		EdgeWriter: writer,
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) {
			return true, nil
		},
	}

	result, err := handler.Handle(context.Background(), submodulePinIntent("scope-1", "gen-4"))
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if result.CanonicalWrites != 1 {
		t.Fatalf("CanonicalWrites = %d, want 1", result.CanonicalWrites)
	}
	if len(writer.retractRows) != 1 {
		t.Fatalf("retractRows len = %d, want 1", len(writer.retractRows))
	}
	if writer.retractRows[0].Payload != nil {
		t.Fatalf("retract Payload = %#v, want nil (whole-repository retract)", writer.retractRows[0].Payload)
	}
	if writer.retractRows[0].RepositoryID != "repo-parent" {
		t.Fatalf("retract RepositoryID = %q, want repo-parent", writer.retractRows[0].RepositoryID)
	}
}

func TestExtractSubmodulePinEdgeRowsSkipsUnresolvedAndQuarantinesMissingRequiredField(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		submodulePinEnvelope("repo-parent", "vendor/lib-a", strPtr("repo-lib-a"), strPtr("abc123")),
		submodulePinEnvelope("repo-parent", "vendor/unresolved", nil, nil),
		{
			FactKind: factKindSubmodulePin,
			FactID:   "fact-bad-1",
			Payload: map[string]any{
				"submodule_path": "vendor/lib-c",
				// parent_repo_id deliberately missing.
			},
		},
	}
	rows, quarantined, err := ExtractSubmodulePinEdgeRowsWithQuarantine(envelopes, "gen-1")
	if err != nil {
		t.Fatalf("ExtractSubmodulePinEdgeRowsWithQuarantine() error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 (only the resolved fact)", len(rows))
	}
	if rows[0]["resolved_repo_id"] != "repo-lib-a" {
		t.Errorf("resolved_repo_id = %#v, want repo-lib-a", rows[0]["resolved_repo_id"])
	}
	if rows[0]["pinned_sha"] != "abc123" {
		t.Errorf("pinned_sha = %#v, want abc123", rows[0]["pinned_sha"])
	}
	if len(quarantined) != 1 {
		t.Fatalf("quarantined len = %d, want 1", len(quarantined))
	}
	if quarantined[0].factID != "fact-bad-1" {
		t.Errorf("quarantined factID = %q, want fact-bad-1", quarantined[0].factID)
	}
}
