// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestContainerImageBuiltFromRowsAdmitsExactDigestOnly(t *testing.T) {
	t.Parallel()

	decisions := []ContainerImageIdentityDecision{
		{
			Digest:                       "sha256:exact",
			SourceRepositoryIDs:          []string{"repo-1"},
			BuildProvenanceRepositoryIDs: []string{"repo-1"},
			Outcome:                      ContainerImageIdentityExactDigest,
		},
		{
			Digest:                       "sha256:tagresolved",
			SourceRepositoryIDs:          []string{"repo-2"},
			BuildProvenanceRepositoryIDs: []string{"repo-2"},
			Outcome:                      ContainerImageIdentityTagResolved,
		},
		{
			Digest:                       "sha256:ambiguous",
			SourceRepositoryIDs:          []string{"repo-3"},
			BuildProvenanceRepositoryIDs: []string{"repo-3"},
			Outcome:                      ContainerImageIdentityAmbiguousTag,
		},
		{
			Digest:                       "sha256:unresolved",
			SourceRepositoryIDs:          []string{"repo-4"},
			BuildProvenanceRepositoryIDs: []string{"repo-4"},
			Outcome:                      ContainerImageIdentityUnresolved,
		},
		{
			Digest:                       "sha256:stale",
			SourceRepositoryIDs:          []string{"repo-5"},
			BuildProvenanceRepositoryIDs: []string{"repo-5"},
			Outcome:                      ContainerImageIdentityStaleTag,
		},
		{
			// exact_digest but no build-provenance repository resolved -- must
			// never fabricate a row.
			Digest:                       "sha256:noreporesolved",
			BuildProvenanceRepositoryIDs: nil,
			Outcome:                      ContainerImageIdentityExactDigest,
		},
	}

	rows := containerImageBuiltFromRows(decisions)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 (exact_digest with a build-provenance repo only): %#v", len(rows), rows)
	}
	if rows[0]["digest"] != "sha256:exact" || rows[0]["repository_id"] != "repo-1" {
		t.Fatalf("row = %#v, want digest=sha256:exact repository_id=repo-1", rows[0])
	}
}

// TestContainerImageBuiltFromRowsExcludesRuntimeReferenceOnlyRepository is the
// #5796 regression: a repository whose only anchor to the image is a
// runtime/deploy reference (SourceRepositoryIDs set from the referencing
// repository's own scope) must never be attributed as the image's builder.
// Only BuildProvenanceRepositoryIDs -- populated by genuine build evidence --
// may produce a BUILT_FROM row.
func TestContainerImageBuiltFromRowsExcludesRuntimeReferenceOnlyRepository(t *testing.T) {
	t.Parallel()

	decisions := []ContainerImageIdentityDecision{
		{
			Digest: "sha256:deployreferenceonly",
			// A Kubernetes manifest in "repository:deploy-only" merely
			// references this digest-pinned third-party image; that anchors
			// SourceRepositoryIDs but is not build evidence.
			SourceRepositoryIDs:          []string{"repository:deploy-only"},
			BuildProvenanceRepositoryIDs: nil,
			Outcome:                      ContainerImageIdentityExactDigest,
		},
	}

	rows := containerImageBuiltFromRows(decisions)
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0: a deploy-only reference must not emit a BUILT_FROM row: %#v", len(rows), rows)
	}
}

func TestContainerImageBuiltFromRowsFansOutMultipleBuildProvenanceRepositories(t *testing.T) {
	t.Parallel()

	decisions := []ContainerImageIdentityDecision{
		{
			Digest: "sha256:multi",
			// SourceRepositoryIDs deliberately differs from
			// BuildProvenanceRepositoryIDs to prove the row builder fans out
			// over the latter, not the former.
			SourceRepositoryIDs:          []string{"repo-deploy-only"},
			BuildProvenanceRepositoryIDs: []string{"repo-b", "repo-a", "repo-a"},
			Outcome:                      ContainerImageIdentityExactDigest,
		},
	}

	rows := containerImageBuiltFromRows(decisions)
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2 (one edge per distinct build-provenance repository): %#v", len(rows), rows)
	}
	seen := map[string]bool{}
	for _, row := range rows {
		if row["digest"] != "sha256:multi" {
			t.Fatalf("row digest = %v, want sha256:multi", row["digest"])
		}
		if row["repository_id"] == "repo-deploy-only" {
			t.Fatalf("row attributed the deploy-only reference repository, want only build-provenance repos: %#v", rows)
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
		{Digest: "  ", BuildProvenanceRepositoryIDs: []string{"repo-1"}, Outcome: ContainerImageIdentityExactDigest},
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
		{Digest: "sha256:exact", SourceRepositoryIDs: []string{"repo-1"}, BuildProvenanceRepositoryIDs: []string{"repo-1"}, Outcome: ContainerImageIdentityExactDigest},
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
		{Digest: "sha256:exact", SourceRepositoryIDs: []string{"repo-1"}, BuildProvenanceRepositoryIDs: []string{"repo-1"}, Outcome: ContainerImageIdentityExactDigest},
	}

	if err := h.projectContainerImageBuiltFromEdges(context.Background(), Intent{ScopeID: "scope-1", GenerationID: "gen-1"}, decisions); err == nil {
		t.Fatal("expected an error when the writer fails")
	}
}

// TestContainerImageBuiltFromRowsFromCICDRunBuildProvenanceEndToEnd proves the
// #5796 fix's other required direction, end to end through the real
// evidence-extraction pipeline rather than a hand-built decision: a CI run
// that reported producing this artifact digest (ci.run.repository_id, joined
// via ci.artifact.artifact_digest in addCICDArtifactImageReference) IS build
// evidence, so it must still reach BuildProvenanceRepositoryIDs and still
// produce a BUILT_FROM row after the gate narrowed off SourceRepositoryIDs.
//
// This is the exact evidence shape the B-7 golden-corpus rc-165 assertion
// depends on (the cicdrun cassette's ci.run carries repository_id
// repository:r_69256c06, joined to the ociregistry cassette's manifest
// digest) -- this test is the unit-level proof that rc-165 survives the
// narrowing without needing the live golden-corpus gate. Neither
// container_image_build_provenance_test.go (which covers the OCI
// config-source-label path and its negative) nor
// container_image_derived_from_edges_test.go (DERIVED_FROM-specific) cover
// this CI-run wiring reaching a decision through the public
// BuildContainerImageIdentityDecisions entrypoint.
func TestContainerImageBuiltFromRowsFromCICDRunBuildProvenanceEndToEnd(t *testing.T) {
	t.Parallel()

	decisions := BuildContainerImageIdentityDecisions([]facts.Envelope{
		ciRunFact("run-image", "github_actions", "repo-api", "abc123"),
		ciArtifactFact("artifact-image", "run-image", testContainerDigest),
		ociManifestFact("oci-manifest", testContainerDigest),
	})

	got := decisionsByRef(decisions)["registry.example.com/team/api@"+testContainerDigest]
	assertContainerImageDecision(t, got, ContainerImageIdentityExactDigest, testContainerDigest, 1)
	if !stringSliceContains(got.BuildProvenanceRepositoryIDs, "repo-api") {
		t.Fatalf("BuildProvenanceRepositoryIDs = %#v, want repo-api: a CI run reporting it produced this digest is build evidence", got.BuildProvenanceRepositoryIDs)
	}

	rows := containerImageBuiltFromRows(decisions)
	if len(rows) != 1 || rows[0]["repository_id"] != "repo-api" {
		t.Fatalf("containerImageBuiltFromRows = %#v, want one row for repo-api", rows)
	}
}
