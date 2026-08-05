// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

type workflowBridgeCICDRunFactLoader struct {
	scopeFacts        []facts.Envelope
	workflowFacts     []facts.Envelope
	identityFact      facts.Envelope
	repositoryIDCalls [][]string
	identityImageRefs [][]string
}

func (l *workflowBridgeCICDRunFactLoader) ListFacts(context.Context, string, string) ([]facts.Envelope, error) {
	return append([]facts.Envelope(nil), l.scopeFacts...), nil
}

func (l *workflowBridgeCICDRunFactLoader) ListFactsByKind(
	_ context.Context,
	_ string,
	_ string,
	_ []string,
) ([]facts.Envelope, error) {
	return append([]facts.Envelope(nil), l.scopeFacts...), nil
}

func (l *workflowBridgeCICDRunFactLoader) ListActiveCICDWorkflowImageFacts(
	_ context.Context,
	repositoryIDs []string,
) ([]facts.Envelope, error) {
	l.repositoryIDCalls = append(l.repositoryIDCalls, append([]string(nil), repositoryIDs...))
	return append([]facts.Envelope(nil), l.workflowFacts...), nil
}

func (l *workflowBridgeCICDRunFactLoader) ListActiveCICDRunCorrelationFacts(
	_ context.Context,
	_ []string,
	imageRefs []string,
) ([]facts.Envelope, error) {
	l.identityImageRefs = append(l.identityImageRefs, append([]string(nil), imageRefs...))
	return []facts.Envelope{l.identityFact}, nil
}

func TestCICDRunCorrelationHandlerBridgesGitWorkflowImagesByRunRepository(t *testing.T) {
	t.Parallel()

	const imageRef = "registry.example.com/team/api:prod"
	malformedWorkflow := commitScopedWorkflowImageFact(
		"workflow-malformed",
		"repository:api",
		"commit-api",
		"registry.example.com/malformed:latest",
	)
	delete(malformedWorkflow.Payload, "repository_id")
	loader := &workflowBridgeCICDRunFactLoader{
		scopeFacts: []facts.Envelope{
			ciRunFact("run-api", "github_actions", "repository:api", "commit-api"),
			ciRunFact("run-api-retry", "github_actions", "repository:api", "commit-api"),
			ciRunFact("run-worker", "github_actions", "repository:worker", "commit-worker"),
			ciRunFact("run-malformed", "github_actions", "", "commit-missing-owner"),
		},
		workflowFacts: []facts.Envelope{
			commitScopedWorkflowImageFact("workflow-api", "repository:api", "commit-api", imageRef),
			commitScopedWorkflowImageFact("workflow-foreign", "repository:foreign", "commit-api", "registry.example.com/foreign:latest"),
			malformedWorkflow,
		},
		identityFact: containerImageIdentityFact("identity-api", "repository:api", imageRef, testCICDDigest),
	}
	writer := &recordingCICDRunCorrelationWriter{}
	edgeWriter := &recordingContainerImageProvenanceEdgeWriter{}
	handler := CICDRunCorrelationHandler{
		FactLoader:           loader,
		Writer:               writer,
		ProvenanceEdgeWriter: edgeWriter,
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-workflow-bridge",
		ScopeID:      "ci_cd_run:github_actions:team-api",
		GenerationID: "generation-ci",
		SourceSystem: "ci_cd_run",
		Domain:       DomainCICDRunCorrelation,
		Cause:        "ci run observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if got, want := loader.repositoryIDCalls, [][]string{{"repository:api", "repository:worker"}}; !slices.EqualFunc(got, want, slices.Equal[[]string]) {
		t.Fatalf("ListActiveCICDWorkflowImageFacts() repository IDs = %#v, want %#v", got, want)
	}
	if got, want := loader.identityImageRefs, [][]string{{imageRef}}; !slices.EqualFunc(got, want, slices.Equal[[]string]) {
		t.Fatalf("ListActiveCICDRunCorrelationFacts() image refs = %#v, want %#v", got, want)
	}
	byRun := cicdDecisionsByRun(writer.write.Decisions)
	decision := byRun["github_actions:run-api:1"]
	if decision.CorrelationKind != "workflow_image" || decision.ImageRef != imageRef {
		t.Fatalf("run-api decision = %#v, want workflow_image for %q", decision, imageRef)
	}
	for _, factID := range []string{"workflow-foreign", "workflow-malformed"} {
		if stringSliceContains(decision.EvidenceFactIDs, factID) {
			t.Fatalf("run-api EvidenceFactIDs = %#v, must defense-filter %q", decision.EvidenceFactIDs, factID)
		}
	}
	if got := result.SubSignals["input_invalid_facts"]; got != 1 {
		t.Fatalf("input_invalid_facts = %v, want 1 because malformed bridge rows must reach quarantine", got)
	}
	if _, ok := result.SubDurations["workflow_image_bridge_load"]; !ok {
		t.Fatalf("SubDurations = %#v, want workflow_image_bridge_load timing", result.SubDurations)
	}
	if len(edgeWriter.writeRows) != 1 || len(edgeWriter.writeRows[0]) != 1 {
		t.Fatalf("workflow-image BUILT_FROM rows = %#v, want one exact produced-image assertion", edgeWriter.writeRows)
	}
	if got := edgeWriter.writeRows[0][0]; got["digest"] != testCICDDigest || got["repository_id"] != "repository:api" {
		t.Fatalf("workflow-image BUILT_FROM row = %#v, want exact digest and run repository", got)
	}
}
