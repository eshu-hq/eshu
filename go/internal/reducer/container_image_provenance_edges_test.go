// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"
)

func TestContainerImageBuiltFromRowsAdmitsExactDigestOnly(t *testing.T) {
	t.Parallel()

	decisions := []ContainerImageIdentityDecision{
		{
			Digest:              "sha256:exact",
			SourceRepositoryIDs: []string{"repo-1"},
			Outcome:             ContainerImageIdentityExactDigest,
		},
		{
			Digest:              "sha256:tagresolved",
			SourceRepositoryIDs: []string{"repo-2"},
			Outcome:             ContainerImageIdentityTagResolved,
		},
		{
			Digest:              "sha256:ambiguous",
			SourceRepositoryIDs: []string{"repo-3"},
			Outcome:             ContainerImageIdentityAmbiguousTag,
		},
		{
			Digest:              "sha256:unresolved",
			SourceRepositoryIDs: []string{"repo-4"},
			Outcome:             ContainerImageIdentityUnresolved,
		},
		{
			Digest:              "sha256:stale",
			SourceRepositoryIDs: []string{"repo-5"},
			Outcome:             ContainerImageIdentityStaleTag,
		},
		{
			// exact_digest but no source repository resolved -- must never
			// fabricate a row.
			Digest:              "sha256:noreporesolved",
			SourceRepositoryIDs: nil,
			Outcome:             ContainerImageIdentityExactDigest,
		},
	}

	rows := containerImageBuiltFromRows(decisions)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 (exact_digest with a resolved source repo only): %#v", len(rows), rows)
	}
	if rows[0]["digest"] != "sha256:exact" || rows[0]["repository_id"] != "repo-1" {
		t.Fatalf("row = %#v, want digest=sha256:exact repository_id=repo-1", rows[0])
	}
}

func TestContainerImageBuiltFromRowsFansOutMultipleSourceRepositories(t *testing.T) {
	t.Parallel()

	decisions := []ContainerImageIdentityDecision{
		{
			Digest:              "sha256:multi",
			SourceRepositoryIDs: []string{"repo-b", "repo-a", "repo-a"},
			Outcome:             ContainerImageIdentityExactDigest,
		},
	}

	rows := containerImageBuiltFromRows(decisions)
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2 (one edge per distinct source repository): %#v", len(rows), rows)
	}
	seen := map[string]bool{}
	for _, row := range rows {
		if row["digest"] != "sha256:multi" {
			t.Fatalf("row digest = %v, want sha256:multi", row["digest"])
		}
		seen[row["repository_id"].(string)] = true
	}
	if !seen["repo-a"] || !seen["repo-b"] {
		t.Fatalf("expected rows for both repo-a and repo-b, got %#v", rows)
	}
}

func TestContainerImageBuiltFromRowsRejectsBlankDigest(t *testing.T) {
	t.Parallel()

	decisions := []ContainerImageIdentityDecision{
		{Digest: "  ", SourceRepositoryIDs: []string{"repo-1"}, Outcome: ContainerImageIdentityExactDigest},
	}

	rows := containerImageBuiltFromRows(decisions)
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0 for a blank digest: %#v", len(rows), rows)
	}
}

type recordingContainerImageProvenanceEdgeWriter struct {
	retractCalls []string
	writeRows    [][]map[string]any
	writeErr     error
	retractErr   error
}

func (w *recordingContainerImageProvenanceEdgeWriter) WriteBuiltFromEdges(
	_ context.Context, rows []map[string]any, _ string, _ string, evidenceSource string,
) error {
	w.writeRows = append(w.writeRows, rows)
	_ = evidenceSource
	return w.writeErr
}

func (w *recordingContainerImageProvenanceEdgeWriter) RetractBuiltFromEdges(
	_ context.Context, _ string, _ string, evidenceSource string,
) error {
	w.retractCalls = append(w.retractCalls, evidenceSource)
	return w.retractErr
}

func TestProjectContainerImageBuiltFromEdgesNoOpWithoutWriter(t *testing.T) {
	t.Parallel()

	h := ContainerImageIdentityHandler{}
	if err := h.projectContainerImageBuiltFromEdges(context.Background(), Intent{ScopeID: "scope-1", GenerationID: "gen-1"}, nil); err != nil {
		t.Fatalf("projectContainerImageBuiltFromEdges returned error with no writer: %v", err)
	}
}

func TestProjectContainerImageBuiltFromEdgesRetractsFirstThenWrites(t *testing.T) {
	t.Parallel()

	writer := &recordingContainerImageProvenanceEdgeWriter{}
	h := ContainerImageIdentityHandler{ProvenanceEdgeWriter: writer}
	decisions := []ContainerImageIdentityDecision{
		{Digest: "sha256:exact", SourceRepositoryIDs: []string{"repo-1"}, Outcome: ContainerImageIdentityExactDigest},
	}

	if err := h.projectContainerImageBuiltFromEdges(context.Background(), Intent{ScopeID: "scope-1", GenerationID: "gen-1"}, decisions); err != nil {
		t.Fatalf("projectContainerImageBuiltFromEdges returned error: %v", err)
	}
	if len(writer.retractCalls) != 1 {
		t.Fatalf("retractCalls = %d, want 1", len(writer.retractCalls))
	}
	if writer.retractCalls[0] != containerImageBuiltFromProvenanceEvidenceSource {
		t.Fatalf("retract evidence_source = %q, want %q", writer.retractCalls[0], containerImageBuiltFromProvenanceEvidenceSource)
	}
	if len(writer.writeRows) != 1 || len(writer.writeRows[0]) != 1 {
		t.Fatalf("writeRows = %#v, want one write of one row", writer.writeRows)
	}
}

func TestProjectContainerImageBuiltFromEdgesRetractsEvenWhenNoRowsToWrite(t *testing.T) {
	t.Parallel()

	writer := &recordingContainerImageProvenanceEdgeWriter{}
	h := ContainerImageIdentityHandler{ProvenanceEdgeWriter: writer}

	if err := h.projectContainerImageBuiltFromEdges(context.Background(), Intent{ScopeID: "scope-1", GenerationID: "gen-2"}, nil); err != nil {
		t.Fatalf("projectContainerImageBuiltFromEdges returned error: %v", err)
	}
	if len(writer.retractCalls) != 1 {
		t.Fatalf("retractCalls = %d, want 1 even with nothing to write", len(writer.retractCalls))
	}
	if len(writer.writeRows) != 0 {
		t.Fatalf("writeRows = %#v, want none for an empty projection", writer.writeRows)
	}
}

func TestProjectContainerImageBuiltFromEdgesPropagatesWriterError(t *testing.T) {
	t.Parallel()

	writer := &recordingContainerImageProvenanceEdgeWriter{writeErr: errors.New("boom")}
	h := ContainerImageIdentityHandler{ProvenanceEdgeWriter: writer}
	decisions := []ContainerImageIdentityDecision{
		{Digest: "sha256:exact", SourceRepositoryIDs: []string{"repo-1"}, Outcome: ContainerImageIdentityExactDigest},
	}

	if err := h.projectContainerImageBuiltFromEdges(context.Background(), Intent{ScopeID: "scope-1", GenerationID: "gen-1"}, decisions); err == nil {
		t.Fatal("expected an error when the writer fails")
	}
}
