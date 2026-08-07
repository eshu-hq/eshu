// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"
)

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

	err := handler.projectCICDWorkflowImageBuiltFromEdges(
		context.Background(),
		Intent{ScopeID: "ci-run-scope", GenerationID: "generation-1"},
		decisions,
	)
	if err != nil {
		t.Fatalf("projectCICDWorkflowImageBuiltFromEdges() error = %v", err)
	}
	if len(writer.retractCalls) != 1 || writer.retractCalls[0] != cicdWorkflowImageBuiltFromEvidenceSource {
		t.Fatalf("retract calls = %#v, want dedicated workflow-image evidence source", writer.retractCalls)
	}
	if len(writer.writeRows) != 1 || len(writer.writeRows[0]) != 1 {
		t.Fatalf("write rows = %#v, want one write containing one row", writer.writeRows)
	}
	if len(writer.writeSources) != 1 || writer.writeSources[0] != cicdWorkflowImageBuiltFromEvidenceSource {
		t.Fatalf("write evidence sources = %#v, want dedicated workflow-image evidence source", writer.writeSources)
	}
}

func TestProjectCICDWorkflowImageBuiltFromEdgesRetractsEmptyAndPropagatesErrors(t *testing.T) {
	t.Parallel()

	intent := Intent{ScopeID: "ci-run-scope", GenerationID: "generation-2"}
	emptyWriter := &recordingContainerImageProvenanceEdgeWriter{}
	if err := (CICDRunCorrelationHandler{ProvenanceEdgeWriter: emptyWriter}).projectCICDWorkflowImageBuiltFromEdges(
		context.Background(), intent, nil,
	); err != nil {
		t.Fatalf("empty projection error = %v", err)
	}
	if len(emptyWriter.retractCalls) != 1 || len(emptyWriter.writeRows) != 0 {
		t.Fatalf("empty projection calls = retract %#v write %#v, want retract-only", emptyWriter.retractCalls, emptyWriter.writeRows)
	}

	retractWriter := &recordingContainerImageProvenanceEdgeWriter{retractErr: errors.New("retract failed")}
	if err := (CICDRunCorrelationHandler{ProvenanceEdgeWriter: retractWriter}).projectCICDWorkflowImageBuiltFromEdges(
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
	if err := (CICDRunCorrelationHandler{ProvenanceEdgeWriter: writeWriter}).projectCICDWorkflowImageBuiltFromEdges(
		context.Background(), intent, []CICDRunCorrelationDecision{decision},
	); err == nil {
		t.Fatal("write error was not propagated")
	}
}
