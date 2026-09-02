// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicdrun

import (
	"context"
	"errors"
	"testing"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// recordingContainerImageProvenanceEdgeWriter is a fake
// reducercontract.ContainerImageProvenanceEdgeWriter that records its calls
// instead of writing anything, for asserting retract-then-write ordering and
// propagating a configured error. It mirrors the equivalent fixture in the
// container_image_identity family's own test suite
// (go/internal/reducer/container_image_provenance_edges_test.go): the two
// families cannot share one package-private type across the root/cicdrun
// seam (issue #6061), so each keeps its own copy of this trivial recorder.
type recordingContainerImageProvenanceEdgeWriter struct {
	retractCalls []string
	writeRows    [][]map[string]any
	writeSources []string
	writeErr     error
	retractErr   error
}

func (w *recordingContainerImageProvenanceEdgeWriter) WriteBuiltFromEdges(
	_ context.Context, rows []map[string]any, _ string, _ string, evidenceSource string,
) error {
	w.writeRows = append(w.writeRows, rows)
	w.writeSources = append(w.writeSources, evidenceSource)
	return w.writeErr
}

func (w *recordingContainerImageProvenanceEdgeWriter) RetractBuiltFromEdges(
	_ context.Context, _ string, _ string, evidenceSource string,
) error {
	w.retractCalls = append(w.retractCalls, evidenceSource)
	return w.retractErr
}

func TestCICDWorkflowImageBuiltFromRowsAdmitsExactProducedOnly(t *testing.T) {
	t.Parallel()

	decisions := []CICDRunCorrelationDecision{
		{
			Provider:        "github_actions",
			RepositoryID:    "repository:producer",
			ArtifactDigest:  "sha256:produced",
			Outcome:         CICDRunCorrelationExact,
			CanonicalTarget: "container_image",
			CorrelationKind: "workflow_image",
		},
		{
			Provider:        "gitlab_ci",
			RepositoryID:    "repository:wrong-provider",
			ArtifactDigest:  "sha256:wrong-provider",
			Outcome:         CICDRunCorrelationExact,
			CanonicalTarget: "container_image",
			CorrelationKind: "workflow_image",
		},
		{
			Provider:        "github_actions",
			RepositoryID:    "repository:consumer",
			ArtifactDigest:  "sha256:consumed",
			Outcome:         CICDRunCorrelationDerived,
			CanonicalTarget: "container_image",
			CorrelationKind: "workflow_image",
		},
		{
			Provider:        "github_actions",
			RepositoryID:    "repository:artifact-only",
			ArtifactDigest:  "sha256:artifact-only",
			Outcome:         CICDRunCorrelationExact,
			CanonicalTarget: "container_image",
			CorrelationKind: "artifact_image",
		},
		{
			Provider:        "github_actions",
			RepositoryID:    "repository:no-target",
			ArtifactDigest:  "sha256:no-target",
			Outcome:         CICDRunCorrelationExact,
			CorrelationKind: "workflow_image",
		},
		{
			Provider:        "github_actions",
			RepositoryID:    " ",
			ArtifactDigest:  "sha256:no-repository",
			Outcome:         CICDRunCorrelationExact,
			CanonicalTarget: "container_image",
			CorrelationKind: "workflow_image",
		},
		{
			Provider:        "github_actions",
			RepositoryID:    "repository:no-digest",
			ArtifactDigest:  " ",
			Outcome:         CICDRunCorrelationExact,
			CanonicalTarget: "container_image",
			CorrelationKind: "workflow_image",
		},
	}

	rows := cicdWorkflowImageBuiltFromRows(decisions)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 exact produced workflow image: %#v", len(rows), rows)
	}
	if rows[0]["digest"] != "sha256:produced" || rows[0]["repository_id"] != "repository:producer" {
		t.Fatalf("row = %#v, want produced digest and repository", rows[0])
	}
}

func TestCICDWorkflowImageBuiltFromRowsDeduplicatesSameAssertion(t *testing.T) {
	t.Parallel()

	decision := CICDRunCorrelationDecision{
		Provider:        "github_actions",
		RepositoryID:    "repository:producer",
		ArtifactDigest:  "sha256:produced",
		Outcome:         CICDRunCorrelationExact,
		CanonicalTarget: "container_image",
		CorrelationKind: "workflow_image",
	}
	rows := cicdWorkflowImageBuiltFromRows([]CICDRunCorrelationDecision{decision, decision})
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want one idempotent assertion: %#v", len(rows), rows)
	}
}

func TestProjectCICDWorkflowImageBuiltFromEdgesRetractsThenWrites(t *testing.T) {
	t.Parallel()

	writer := &recordingContainerImageProvenanceEdgeWriter{}
	handler := CICDRunCorrelationHandler{ProvenanceEdgeWriter: writer}
	decisions := []CICDRunCorrelationDecision{{
		Provider:        "github_actions",
		RepositoryID:    "repository:producer",
		ArtifactDigest:  "sha256:produced",
		Outcome:         CICDRunCorrelationExact,
		CanonicalTarget: "container_image",
		CorrelationKind: "workflow_image",
	}}

	err := handler.ProjectCICDWorkflowImageBuiltFromEdges(
		context.Background(),
		reducercontract.Intent{ScopeID: "ci-run-scope", GenerationID: "generation-1"},
		decisions,
	)
	if err != nil {
		t.Fatalf("ProjectCICDWorkflowImageBuiltFromEdges() error = %v", err)
	}
	if len(writer.retractCalls) != 1 || writer.retractCalls[0] != CICDWorkflowImageBuiltFromEvidenceSource {
		t.Fatalf("retract calls = %#v, want dedicated workflow-image evidence source", writer.retractCalls)
	}
	if len(writer.writeRows) != 1 || len(writer.writeRows[0]) != 1 {
		t.Fatalf("write rows = %#v, want one write containing one row", writer.writeRows)
	}
	if len(writer.writeSources) != 1 || writer.writeSources[0] != CICDWorkflowImageBuiltFromEvidenceSource {
		t.Fatalf("write evidence sources = %#v, want dedicated workflow-image evidence source", writer.writeSources)
	}
}

func TestProjectCICDWorkflowImageBuiltFromEdgesRetractsEmptyAndPropagatesErrors(t *testing.T) {
	t.Parallel()

	intent := reducercontract.Intent{ScopeID: "ci-run-scope", GenerationID: "generation-2"}
	emptyWriter := &recordingContainerImageProvenanceEdgeWriter{}
	if err := (CICDRunCorrelationHandler{ProvenanceEdgeWriter: emptyWriter}).ProjectCICDWorkflowImageBuiltFromEdges(
		context.Background(), intent, nil,
	); err != nil {
		t.Fatalf("empty projection error = %v", err)
	}
	if len(emptyWriter.retractCalls) != 1 || len(emptyWriter.writeRows) != 0 {
		t.Fatalf("empty projection calls = retract %#v write %#v, want retract-only", emptyWriter.retractCalls, emptyWriter.writeRows)
	}

	retractWriter := &recordingContainerImageProvenanceEdgeWriter{retractErr: errors.New("retract failed")}
	if err := (CICDRunCorrelationHandler{ProvenanceEdgeWriter: retractWriter}).ProjectCICDWorkflowImageBuiltFromEdges(
		context.Background(), intent, nil,
	); err == nil {
		t.Fatal("retract error was not propagated")
	}

	writeWriter := &recordingContainerImageProvenanceEdgeWriter{writeErr: errors.New("write failed")}
	decision := CICDRunCorrelationDecision{
		Provider:        "github_actions",
		RepositoryID:    "repository:producer",
		ArtifactDigest:  "sha256:produced",
		Outcome:         CICDRunCorrelationExact,
		CanonicalTarget: "container_image",
		CorrelationKind: "workflow_image",
	}
	if err := (CICDRunCorrelationHandler{ProvenanceEdgeWriter: writeWriter}).ProjectCICDWorkflowImageBuiltFromEdges(
		context.Background(), intent, []CICDRunCorrelationDecision{decision},
	); err == nil {
		t.Fatal("write error was not propagated")
	}
}
