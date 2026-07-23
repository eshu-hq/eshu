// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"
)

func TestPackageOwnershipPublishesRowsAdmitsExactAndDerivedOnly(t *testing.T) {
	t.Parallel()

	decisions := []PackageSourceCorrelationDecision{
		{PackageID: "pkg-exact", RepositoryID: "repo-1", Outcome: PackageSourceCorrelationExact},
		{PackageID: "pkg-derived", RepositoryID: "repo-2", Outcome: PackageSourceCorrelationDerived},
		{PackageID: "pkg-ambiguous", RepositoryID: "repo-3", Outcome: PackageSourceCorrelationAmbiguous},
		{PackageID: "pkg-unresolved", RepositoryID: "repo-4", Outcome: PackageSourceCorrelationUnresolved},
		{PackageID: "pkg-stale", RepositoryID: "repo-5", Outcome: PackageSourceCorrelationStale},
		{PackageID: "pkg-rejected", RepositoryID: "repo-6", Outcome: PackageSourceCorrelationRejected},
		// exact but no repository resolved -- must never fabricate a row.
		{PackageID: "pkg-no-repo", RepositoryID: "", Outcome: PackageSourceCorrelationExact},
	}

	rows := packageOwnershipPublishesRows(decisions)
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2 (exact + derived only): %#v", len(rows), rows)
	}
	for _, row := range rows {
		if row["version_id"] != nil && row["version_id"] != "" {
			t.Fatalf("package-level ownership row must not carry a non-empty version_id: %#v", row)
		}
	}
}

func TestPackageOwnershipPublishesRowsTargetsVersionWhenPresent(t *testing.T) {
	t.Parallel()

	decisions := []PackageSourceCorrelationDecision{
		{PackageID: "pkg-1", VersionID: "ver-1", RepositoryID: "repo-1", Outcome: PackageSourceCorrelationExact},
	}

	rows := packageOwnershipPublishesRows(decisions)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	row := rows[0]
	if row["version_id"] != "ver-1" {
		t.Fatalf("row version_id = %v, want ver-1", row["version_id"])
	}
	if _, ok := row["package_id"]; ok {
		t.Fatalf("version-targeted row must not also carry package_id (ambiguous MATCH target): %#v", row)
	}
	if row["repository_id"] != "repo-1" {
		t.Fatalf("row repository_id = %v, want repo-1", row["repository_id"])
	}
}

func TestPackagePublicationPublishesRowsAdmitsExactAndDerivedOnly(t *testing.T) {
	t.Parallel()

	decisions := []PackagePublicationDecision{
		{PackageID: "pkg-1", VersionID: "ver-1", RepositoryID: "repo-1", Outcome: PackageSourceCorrelationExact},
		{PackageID: "pkg-2", VersionID: "ver-2", RepositoryID: "repo-2", Outcome: PackageSourceCorrelationDerived},
		{PackageID: "pkg-3", VersionID: "ver-3", RepositoryID: "repo-3", Outcome: PackageSourceCorrelationAmbiguous},
		{PackageID: "pkg-4", VersionID: "ver-4", RepositoryID: "", Outcome: PackageSourceCorrelationExact},
	}

	rows := packagePublicationPublishesRows(decisions)
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2 (exact + derived, non-empty repository only): %#v", len(rows), rows)
	}
	for _, row := range rows {
		if row["version_id"] == "" || row["version_id"] == nil {
			t.Fatalf("publication row must always target a PackageVersion: %#v", row)
		}
	}
}

// recordingPackageProvenanceEdgeWriter is a fake PackageProvenanceEdgeWriter
// used to prove projectPackageProvenanceEdges retracts before writing, calls
// the writer once per distinct evidence_source, and is a safe no-op when
// unwired.
type recordingPackageProvenanceEdgeWriter struct {
	retractCalls []string
	writeCalls   []recordedPublishesWrite
	writeErr     error
	retractErr   error
}

type recordedPublishesWrite struct {
	rows           []map[string]any
	evidenceSource string
}

func (w *recordingPackageProvenanceEdgeWriter) WritePublishesEdges(
	_ context.Context, rows []map[string]any, _ string, _ string, evidenceSource string,
) error {
	w.writeCalls = append(w.writeCalls, recordedPublishesWrite{rows: rows, evidenceSource: evidenceSource})
	return w.writeErr
}

func (w *recordingPackageProvenanceEdgeWriter) RetractPublishesEdges(
	_ context.Context, _ string, _ string, evidenceSource string,
) error {
	w.retractCalls = append(w.retractCalls, evidenceSource)
	return w.retractErr
}

func TestProjectPackageProvenanceEdgesNoOpWithoutWriter(t *testing.T) {
	t.Parallel()

	h := PackageSourceCorrelationHandler{}
	err := h.projectPackageProvenanceEdges(context.Background(), Intent{ScopeID: "scope-1", GenerationID: "gen-1"}, nil, nil)
	if err != nil {
		t.Fatalf("projectPackageProvenanceEdges returned error with no writer: %v", err)
	}
}

func TestProjectPackageProvenanceEdgesRetractsFirstThenWritesBothEvidenceSources(t *testing.T) {
	t.Parallel()

	writer := &recordingPackageProvenanceEdgeWriter{}
	h := PackageSourceCorrelationHandler{ProvenanceEdgeWriter: writer}
	ownership := []PackageSourceCorrelationDecision{
		{PackageID: "pkg-1", RepositoryID: "repo-1", Outcome: PackageSourceCorrelationExact},
	}
	publication := []PackagePublicationDecision{
		{PackageID: "pkg-2", VersionID: "ver-2", RepositoryID: "repo-2", Outcome: PackageSourceCorrelationExact},
	}

	if err := h.projectPackageProvenanceEdges(context.Background(), Intent{ScopeID: "scope-1", GenerationID: "gen-1"}, ownership, publication); err != nil {
		t.Fatalf("projectPackageProvenanceEdges returned error: %v", err)
	}

	if len(writer.retractCalls) != 2 {
		t.Fatalf("retractCalls = %d, want 2 (one per evidence_source)", len(writer.retractCalls))
	}
	if len(writer.writeCalls) != 2 {
		t.Fatalf("writeCalls = %d, want 2", len(writer.writeCalls))
	}

	sawOwnership, sawPublication := false, false
	for _, call := range writer.writeCalls {
		switch call.evidenceSource {
		case packageOwnershipProvenanceEvidenceSource:
			sawOwnership = true
		case packagePublicationProvenanceEvidenceSource:
			sawPublication = true
		}
	}
	if !sawOwnership || !sawPublication {
		t.Fatalf("expected one write per evidence_source, got %#v", writer.writeCalls)
	}
}

func TestProjectPackageProvenanceEdgesRetractsEvenWhenNoRowsToWrite(t *testing.T) {
	t.Parallel()

	writer := &recordingPackageProvenanceEdgeWriter{}
	h := PackageSourceCorrelationHandler{ProvenanceEdgeWriter: writer}

	// No admitted decisions this generation -- retract must still run so a
	// dropped decision's stale edge from a prior generation is removed.
	if err := h.projectPackageProvenanceEdges(context.Background(), Intent{ScopeID: "scope-1", GenerationID: "gen-2"}, nil, nil); err != nil {
		t.Fatalf("projectPackageProvenanceEdges returned error: %v", err)
	}
	if len(writer.retractCalls) != 2 {
		t.Fatalf("retractCalls = %d, want 2 even with nothing to write", len(writer.retractCalls))
	}
	if len(writer.writeCalls) != 0 {
		t.Fatalf("writeCalls = %d, want 0 for an empty projection", len(writer.writeCalls))
	}
}

func TestProjectPackageProvenanceEdgesPropagatesWriterError(t *testing.T) {
	t.Parallel()

	writer := &recordingPackageProvenanceEdgeWriter{writeErr: errors.New("boom")}
	h := PackageSourceCorrelationHandler{ProvenanceEdgeWriter: writer}
	ownership := []PackageSourceCorrelationDecision{
		{PackageID: "pkg-1", RepositoryID: "repo-1", Outcome: PackageSourceCorrelationExact},
	}

	err := h.projectPackageProvenanceEdges(context.Background(), Intent{ScopeID: "scope-1", GenerationID: "gen-1"}, ownership, nil)
	if err == nil {
		t.Fatal("expected an error when the writer fails")
	}
}
