// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	codeownersv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codeowners/v1"
)

// recordingCodeownersOwnershipEdgeWriter is a SharedProjectionEdgeWriter fake
// that records every retract/write call, mirroring
// recordingDocumentationEdgeWriter (documentation_edge_materialization_test.go).
type recordingCodeownersOwnershipEdgeWriter struct {
	retractDomain string
	retractRows   []SharedProjectionIntentRow
	writeDomain   string
	writeRows     []SharedProjectionIntentRow
}

func (r *recordingCodeownersOwnershipEdgeWriter) RetractEdges(
	_ context.Context,
	domain string,
	rows []SharedProjectionIntentRow,
	_ string,
) error {
	r.retractDomain = domain
	r.retractRows = append(r.retractRows, rows...)
	return nil
}

func (r *recordingCodeownersOwnershipEdgeWriter) WriteEdges(
	_ context.Context,
	domain string,
	rows []SharedProjectionIntentRow,
	_ string,
) error {
	r.writeDomain = domain
	r.writeRows = append(r.writeRows, rows...)
	return nil
}

func codeownersOwnershipEnvelope(repoID, sourcePath, pattern string, owners []string, orderIndex int) facts.Envelope {
	payload, err := factschema.EncodeCodeownersOwnership(codeownersv1.Ownership{
		RepoID:     repoID,
		SourcePath: sourcePath,
		Pattern:    pattern,
		Owners:     owners,
		OrderIndex: orderIndex,
	})
	if err != nil {
		panic(err)
	}
	return facts.Envelope{
		FactKind: factKindCodeownersOwnership,
		Payload:  payload,
	}
}

func codeownersOwnershipIntent(scopeID, generationID string) Intent {
	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	return Intent{
		IntentID:     "intent-codeowners-" + scopeID + "-" + generationID,
		ScopeID:      scopeID,
		GenerationID: generationID,
		SourceSystem: "git",
		Domain:       DomainCodeownersOwnership,
		EnqueuedAt:   now,
		AvailableAt:  now,
		Status:       IntentStatusPending,
	}
}

func TestCodeownersOwnershipHandlerBuildsOneEdgePerPatternOwner(t *testing.T) {
	t.Parallel()

	writer := &recordingCodeownersOwnershipEdgeWriter{}
	handler := CodeownersOwnershipEdgeMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			codeownersOwnershipEnvelope("repo-1", ".github/CODEOWNERS", "*.go", []string{"@org/backend", "@org/platform"}, 0),
			codeownersOwnershipEnvelope("repo-1", ".github/CODEOWNERS", "*.md", []string{"@org/docs"}, 1),
		}},
		EdgeWriter: writer,
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) {
			return true, nil
		},
	}

	result, err := handler.Handle(context.Background(), codeownersOwnershipIntent("scope-1", "gen-1"))
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if result.CanonicalWrites != 3 {
		t.Fatalf("CanonicalWrites = %d, want 3 (2 owners + 1 owner)", result.CanonicalWrites)
	}
	if len(writer.writeRows) != 3 {
		t.Fatalf("writeRows len = %d, want 3", len(writer.writeRows))
	}

	wantOwnerRefs := map[string]bool{"@org/backend": false, "@org/platform": false, "@org/docs": false}
	for _, row := range writer.writeRows {
		if row.ProjectionDomain != DomainCodeownersOwnershipEdges {
			t.Errorf("ProjectionDomain = %q, want %q", row.ProjectionDomain, DomainCodeownersOwnershipEdges)
		}
		if row.RepositoryID != "repo-1" {
			t.Errorf("RepositoryID = %q, want repo-1", row.RepositoryID)
		}
		ownerRef, _ := row.Payload["owner_ref"].(string)
		if _, ok := wantOwnerRefs[ownerRef]; !ok {
			t.Fatalf("unexpected owner_ref %q in row %#v", ownerRef, row.Payload)
		}
		wantOwnerRefs[ownerRef] = true
		if row.Payload["source_path"] != ".github/CODEOWNERS" {
			t.Errorf("source_path = %#v, want .github/CODEOWNERS", row.Payload["source_path"])
		}
		if row.Payload["generation_id"] != "gen-1" {
			t.Errorf("generation_id = %#v, want gen-1", row.Payload["generation_id"])
		}
	}
	for ownerRef, seen := range wantOwnerRefs {
		if !seen {
			t.Errorf("owner_ref %q never produced a row", ownerRef)
		}
	}

	// Whole-generation (no delta) retract: a repo-scoped retract row, no
	// delta_projection payload.
	if len(writer.retractRows) != 1 {
		t.Fatalf("retractRows len = %d, want 1", len(writer.retractRows))
	}
	if writer.retractRows[0].RepositoryID != "repo-1" {
		t.Errorf("retract RepositoryID = %q, want repo-1", writer.retractRows[0].RepositoryID)
	}
	if writer.retractRows[0].Payload != nil {
		t.Errorf("whole-scope retract Payload = %#v, want nil", writer.retractRows[0].Payload)
	}
}

// TestCodeownersOwnershipHandlerDeletedFileRetractsWithoutWrites is the
// mandatory delta-retract proof: when a CODEOWNERS file is removed (the
// repository delta reports it deleted, and no codeowners.ownership fact for
// it survives into the new generation), the handler MUST still sweep the
// stale DECLARES_CODEOWNER edges even though it writes zero new edges. This is
// the same leak class as the bug already fixed for documentation edges: a
// materialization handler that only retracts when it also writes would leave
// orphaned edges behind forever once a source file disappears.
//
// The retract MUST be whole-repository, not scoped to the deleted path
// (issue #5419 P1, Bug 2): CODEOWNERS winner-resolution is whole-repo, so
// deleting the current winner can hand the win to a different candidate
// location entirely, and a retract scoped only to the deleted path would
// leave the graph with zero edges instead of falling back to whichever
// candidate now wins (the empty-graph transition).
func TestCodeownersOwnershipHandlerDeletedFileRetractsWithoutWrites(t *testing.T) {
	t.Parallel()

	writer := &recordingCodeownersOwnershipEdgeWriter{}
	handler := CodeownersOwnershipEdgeMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			{
				FactKind: factKindRepository,
				Payload: map[string]any{
					"repo_id":                      "repo-1",
					"local_path":                   "/repo",
					"delta_generation":             true,
					"delta_deleted_relative_paths": []string{".github/CODEOWNERS"},
				},
			},
			// No codeowners.ownership facts survive: the file was removed
			// and, in this scenario, no other candidate exists to fall back
			// to.
		}},
		EdgeWriter: writer,
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) {
			return true, nil
		},
	}

	result, err := handler.Handle(context.Background(), codeownersOwnershipIntent("scope-1", "gen-2"))
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if result.CanonicalWrites != 0 {
		t.Fatalf("CanonicalWrites = %d, want 0", result.CanonicalWrites)
	}
	if len(writer.writeRows) != 0 {
		t.Fatalf("writeRows len = %d, want 0", len(writer.writeRows))
	}
	if writer.retractDomain != DomainCodeownersOwnershipEdges {
		t.Fatalf("retractDomain = %q, want %q", writer.retractDomain, DomainCodeownersOwnershipEdges)
	}
	if len(writer.retractRows) != 1 {
		t.Fatalf("retractRows len = %d, want 1", len(writer.retractRows))
	}
	if writer.retractRows[0].Payload != nil {
		t.Fatalf("retract Payload = %#v, want nil (whole-repository retract)", writer.retractRows[0].Payload)
	}
	if writer.retractRows[0].RepositoryID != "repo-1" {
		t.Fatalf("retract RepositoryID = %q, want repo-1", writer.retractRows[0].RepositoryID)
	}
}

// TestCodeownersOwnershipHandlerChangedCandidateForcesWholeRepoRetract proves
// the companion case for issue #5419 P1 Bug 2: a changed (not deleted)
// CODEOWNERS candidate file forces a whole-repository retract before
// rewriting the current generation's rules, rather than a retract scoped only
// to its own source path. A path-scoped retract here would leave a
// now-losing candidate's previously-projected edges behind (the union
// transition) whenever the changed file causes the winner to switch between
// candidate locations.
func TestCodeownersOwnershipHandlerChangedCandidateForcesWholeRepoRetract(t *testing.T) {
	t.Parallel()

	writer := &recordingCodeownersOwnershipEdgeWriter{}
	handler := CodeownersOwnershipEdgeMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			{
				FactKind: factKindRepository,
				Payload: map[string]any{
					"repo_id":              "repo-1",
					"local_path":           "/repo",
					"delta_generation":     true,
					"delta_relative_paths": []string{".github/CODEOWNERS"},
				},
			},
			codeownersOwnershipEnvelope("repo-1", ".github/CODEOWNERS", "*.go", []string{"@org/backend"}, 0),
		}},
		EdgeWriter: writer,
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) {
			return true, nil
		},
	}

	result, err := handler.Handle(context.Background(), codeownersOwnershipIntent("scope-1", "gen-3"))
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
	if writer.retractRows[0].RepositoryID != "repo-1" {
		t.Fatalf("retract RepositoryID = %q, want repo-1", writer.retractRows[0].RepositoryID)
	}
}

// TestCodeownersOwnershipHandlerNonCandidateDeltaKeepsPathScopedRetract proves
// the fix stays narrow: a delta whose changed paths do NOT touch any of the
// three recognized CODEOWNERS locations keeps the ordinary path-scoped delta
// retract (the pre-existing, harmless no-op mechanism against
// DECLARES_CODEOWNER edges), rather than escalating every unrelated file
// change in the repo to a whole-repository codeowners retract.
func TestCodeownersOwnershipHandlerNonCandidateDeltaKeepsPathScopedRetract(t *testing.T) {
	t.Parallel()

	writer := &recordingCodeownersOwnershipEdgeWriter{}
	handler := CodeownersOwnershipEdgeMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			{
				FactKind: factKindRepository,
				Payload: map[string]any{
					"repo_id":              "repo-1",
					"local_path":           "/repo",
					"delta_generation":     true,
					"delta_relative_paths": []string{"main.go"},
				},
			},
			// No codeowners.ownership facts: CODEOWNERS itself did not
			// change this generation.
		}},
		EdgeWriter: writer,
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) {
			return true, nil
		},
	}

	result, err := handler.Handle(context.Background(), codeownersOwnershipIntent("scope-1", "gen-4"))
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if result.CanonicalWrites != 0 {
		t.Fatalf("CanonicalWrites = %d, want 0", result.CanonicalWrites)
	}
	if len(writer.retractRows) != 1 {
		t.Fatalf("retractRows len = %d, want 1", len(writer.retractRows))
	}
	gotPaths, ok := writer.retractRows[0].Payload["delta_file_paths"].([]string)
	if !ok {
		t.Fatalf("delta_file_paths type = %T, want []string", writer.retractRows[0].Payload["delta_file_paths"])
	}
	wantPaths := []string{"main.go"}
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("delta_file_paths = %#v, want %#v", gotPaths, wantPaths)
	}
}

func TestExtractCodeownersOwnershipEdgeRowsFansOutOwnersPerRule(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		codeownersOwnershipEnvelope("repo-1", "CODEOWNERS", "*", []string{"@org/a", "@org/b"}, 0),
	}
	rows, quarantined, err := ExtractCodeownersOwnershipEdgeRowsWithQuarantine(envelopes, "gen-1")
	if err != nil {
		t.Fatalf("ExtractCodeownersOwnershipEdgeRowsWithQuarantine() error = %v", err)
	}
	if len(quarantined) != 0 {
		t.Fatalf("quarantined = %#v, want empty", quarantined)
	}
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}
	for _, row := range rows {
		if row["pattern"] != "*" {
			t.Errorf("pattern = %#v, want *", row["pattern"])
		}
		if row["order_index"] != 0 {
			t.Errorf("order_index = %#v, want 0", row["order_index"])
		}
		if row["action"] != IntentActionUpsert {
			t.Errorf("action = %#v, want %q", row["action"], IntentActionUpsert)
		}
	}
}

// TestExtractCodeownersOwnershipEdgeRowsKeepsLastMatchOrdinalOnRepeatedRule
// proves the last-match-wins ordinal fix (issue #5419 P1): GitHub CODEOWNERS
// resolution is last-match-wins, so when the same (pattern, owner) pair
// repeats across multiple rule lines, the surviving edge MUST carry the
// highest (latest) order_index, not the first occurrence's. Before the fix,
// a duplicate (repo, path, pattern, owner) key was skipped outright, freezing
// the FIRST occurrence's order_index — for `*.go @team-a`(0), `*.go
// @team-b`(1), `*.go @team-a`(2), the @team-a edge kept order_index 0, so the
// precedence resolver (highest surviving ordinal wins) picked @team-b (1)
// instead of the true last-match winner @team-a (2).
func TestExtractCodeownersOwnershipEdgeRowsKeepsLastMatchOrdinalOnRepeatedRule(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		codeownersOwnershipEnvelope("repo-1", "CODEOWNERS", "*.go", []string{"@team-a"}, 0),
		codeownersOwnershipEnvelope("repo-1", "CODEOWNERS", "*.go", []string{"@team-b"}, 1),
		codeownersOwnershipEnvelope("repo-1", "CODEOWNERS", "*.go", []string{"@team-a"}, 2),
	}
	rows, quarantined, err := ExtractCodeownersOwnershipEdgeRowsWithQuarantine(envelopes, "gen-1")
	if err != nil {
		t.Fatalf("ExtractCodeownersOwnershipEdgeRowsWithQuarantine() error = %v", err)
	}
	if len(quarantined) != 0 {
		t.Fatalf("quarantined = %#v, want empty", quarantined)
	}
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2 (one row per owner, @team-a deduped)", len(rows))
	}

	gotOrderIndex := make(map[string]int)
	for _, row := range rows {
		ownerRef, _ := row["owner_ref"].(string)
		orderIndex, _ := row["order_index"].(int)
		if _, dup := gotOrderIndex[ownerRef]; dup {
			t.Fatalf("owner_ref %q produced more than one row: %#v", ownerRef, rows)
		}
		gotOrderIndex[ownerRef] = orderIndex
	}
	if gotOrderIndex["@team-a"] != 2 {
		t.Errorf("@team-a order_index = %d, want 2 (last-match ordinal)", gotOrderIndex["@team-a"])
	}
	if gotOrderIndex["@team-b"] != 1 {
		t.Errorf("@team-b order_index = %d, want 1", gotOrderIndex["@team-b"])
	}
}

func TestExtractCodeownersOwnershipEdgeRowsQuarantinesMissingRequiredField(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: factKindCodeownersOwnership,
			FactID:   "fact-bad-1",
			Payload: map[string]any{
				"source_path": "CODEOWNERS",
				"pattern":     "*",
				"owners":      []string{"@org/a"},
				"order_index": 0,
				// repo_id deliberately missing.
			},
		},
	}
	rows, quarantined, err := ExtractCodeownersOwnershipEdgeRowsWithQuarantine(envelopes, "gen-1")
	if err != nil {
		t.Fatalf("ExtractCodeownersOwnershipEdgeRowsWithQuarantine() error = %v, want nil (quarantinable)", err)
	}
	if len(rows) != 0 {
		t.Fatalf("rows = %#v, want empty", rows)
	}
	if len(quarantined) != 1 {
		t.Fatalf("quarantined len = %d, want 1", len(quarantined))
	}
	if quarantined[0].factID != "fact-bad-1" {
		t.Errorf("quarantined factID = %q, want fact-bad-1", quarantined[0].factID)
	}
}
