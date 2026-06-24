// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

type stubDeployableUnitFactLoader struct {
	envelopes []facts.Envelope
	err       error
	calls     int
}

func (f *stubDeployableUnitFactLoader) ListFacts(
	_ context.Context,
	_,
	_ string,
) ([]facts.Envelope, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.envelopes, nil
}

type stubDeployableUnitResolvedLoader struct {
	resolved []relationships.ResolvedRelationship
	err      error
	calls    int
}

func (f *stubDeployableUnitResolvedLoader) GetResolvedRelationships(
	_ context.Context,
	_ string,
) ([]relationships.ResolvedRelationship, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.resolved, nil
}

func TestDeployableUnitCorrelationHandleRejectsWrongDomain(t *testing.T) {
	t.Parallel()

	handler := DeployableUnitCorrelationHandler{
		FactLoader: &stubDeployableUnitFactLoader{},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:        "intent-1",
		ScopeID:         "repository:service-gha",
		GenerationID:    "generation-1",
		SourceSystem:    "git",
		Domain:          DomainDeploymentMapping,
		Cause:           "facts committed",
		EntityKeys:      []string{"service-gha"},
		RelatedScopeIDs: []string{"repository:deploy-repo"},
	})
	if err == nil {
		t.Fatal("Handle() error = nil, want non-nil")
	}
}

func TestDeployableUnitModelCandidateKeepsMultipleDeploymentRepos(t *testing.T) {
	t.Parallel()

	intent := deployableUnitIntent("api-service")
	candidate := WorkloadCandidate{
		RepoID:            "repo-api",
		RepoName:          "api-service",
		DeploymentRepoIDs: []string{"repo-current-deploy", "repo-next-deploy"},
		Classification:    "service",
		Confidence:        0.96,
		Provenance:        []string{"argocd_applicationset_deploy_source"},
	}

	model := deployableUnitModelCandidate(intent, candidate, "api-service", false)

	var got []string
	for _, atom := range model.Evidence {
		if atom.Key == "deployment_repo_id" {
			got = append(got, atom.Value)
		}
	}
	if want := []string{"repo-current-deploy", "repo-next-deploy"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("deployment_repo_id evidence = %#v, want %#v", got, want)
	}
}

func TestDeployableUnitCorrelationHandleRequiresFactLoader(t *testing.T) {
	t.Parallel()

	handler := DeployableUnitCorrelationHandler{}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:        "intent-1",
		ScopeID:         "repository:service-gha",
		GenerationID:    "generation-1",
		SourceSystem:    "git",
		Domain:          DomainDeployableUnitCorrelation,
		Cause:           "facts committed",
		EntityKeys:      []string{"service-gha"},
		RelatedScopeIDs: []string{"repository:deploy-repo"},
	})
	if err == nil {
		t.Fatal("Handle() error = nil, want non-nil")
	}
}

func TestDeployableUnitCorrelationHandleRequiresEntityKeys(t *testing.T) {
	t.Parallel()

	handler := DeployableUnitCorrelationHandler{
		FactLoader: &stubDeployableUnitFactLoader{},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:        "intent-1",
		ScopeID:         "repository:service-gha",
		GenerationID:    "generation-1",
		SourceSystem:    "git",
		Domain:          DomainDeployableUnitCorrelation,
		Cause:           "facts committed",
		RelatedScopeIDs: []string{"repository:deploy-repo"},
	})
	if err == nil {
		t.Fatal("Handle() error = nil, want non-nil")
	}
}

func TestDeployableUnitCorrelationHandleReturnsNoCandidates(t *testing.T) {
	t.Parallel()

	publisher := &recordingGraphProjectionPhasePublisher{}
	handler := DeployableUnitCorrelationHandler{
		PhasePublisher: publisher,
		FactLoader: &stubDeployableUnitFactLoader{
			envelopes: deployableUnitCorrelationEnvelopes(
				"repo-docs",
				"documentation",
				nil,
			),
		},
	}

	got, err := handler.Handle(context.Background(), deployableUnitIntent("documentation"))
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if got.Status != ResultStatusSucceeded {
		t.Fatalf("Handle().Status = %q, want %q", got.Status, ResultStatusSucceeded)
	}
	if got.EvidenceSummary != "no deployable unit candidates found" {
		t.Fatalf("Handle().EvidenceSummary = %q, want no-candidates summary", got.EvidenceSummary)
	}
	if got, want := len(publisher.calls), 1; got != want {
		t.Fatalf("publisher calls = %d, want %d", got, want)
	}
	if got, want := publisher.calls[0][0].Key.Keyspace, GraphProjectionKeyspaceDeployableUnitUID; got != want {
		t.Fatalf("published keyspace = %q, want %q", got, want)
	}
	if got, want := publisher.calls[0][0].Phase, GraphProjectionPhaseDeployableUnitCorrelation; got != want {
		t.Fatalf("published phase = %q, want %q", got, want)
	}
}

func TestDeployableUnitCorrelationHandleRejectsDockerfileOnlyCandidate(t *testing.T) {
	t.Parallel()

	handler := DeployableUnitCorrelationHandler{
		FactLoader: &stubDeployableUnitFactLoader{
			envelopes: deployableUnitCorrelationEnvelopes(
				"repo-edge-api",
				"edge-api",
				[]map[string]any{
					{
						"repo_id":       "repo-edge-api",
						"language":      "dockerfile",
						"relative_path": "Dockerfile",
						"parsed_file_data": map[string]any{
							"dockerfile_stages": []any{
								map[string]any{"name": "runtime"},
							},
						},
					},
				},
			),
		},
	}

	got, err := handler.Handle(context.Background(), deployableUnitIntent("edge-api"))
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if !strings.Contains(got.EvidenceSummary, "admitted=0") {
		t.Fatalf("Handle().EvidenceSummary = %q, want admitted=0", got.EvidenceSummary)
	}
	if !strings.Contains(got.EvidenceSummary, "rejected=1") {
		t.Fatalf("Handle().EvidenceSummary = %q, want rejected=1", got.EvidenceSummary)
	}
}

func TestDeployableUnitCorrelationHandleAdmitsResolvedDeploymentEvidence(t *testing.T) {
	t.Parallel()

	factLoader := &stubDeployableUnitFactLoader{
		envelopes: deployableUnitCorrelationEnvelopes(
			"repo-edge-api",
			"edge-api",
			[]map[string]any{
				{
					"repo_id":       "repo-edge-api",
					"language":      "dockerfile",
					"relative_path": "Dockerfile",
					"parsed_file_data": map[string]any{
						"dockerfile_stages": []any{
							map[string]any{"name": "runtime"},
						},
					},
				},
			},
		),
	}
	resolvedLoader := &stubDeployableUnitResolvedLoader{
		resolved: []relationships.ResolvedRelationship{
			{
				SourceRepoID:     "repo-edge-api",
				TargetRepoID:     "repo-deployments",
				RelationshipType: relationships.RelDeploysFrom,
				Confidence:       0.94,
				Details: map[string]any{
					"evidence_kinds": []string{
						string(relationships.EvidenceKindArgoCDAppSource),
					},
				},
			},
		},
	}

	handler := DeployableUnitCorrelationHandler{
		FactLoader:     factLoader,
		ResolvedLoader: resolvedLoader,
		PhasePublisher: &recordingGraphProjectionPhasePublisher{},
	}

	got, err := handler.Handle(context.Background(), deployableUnitIntent("edge-api"))
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if factLoader.calls != 1 {
		t.Fatalf("FactLoader calls = %d, want 1", factLoader.calls)
	}
	if resolvedLoader.calls != 1 {
		t.Fatalf("ResolvedLoader calls = %d, want 1", resolvedLoader.calls)
	}
	if !strings.Contains(got.EvidenceSummary, "admitted=1") {
		t.Fatalf("Handle().EvidenceSummary = %q, want admitted=1", got.EvidenceSummary)
	}
	if !strings.Contains(got.EvidenceSummary, "rejected=0") {
		t.Fatalf("Handle().EvidenceSummary = %q, want rejected=0", got.EvidenceSummary)
	}
}

func TestDeployableUnitCorrelationHandlePublishesPhaseForAdmittedCandidate(t *testing.T) {
	t.Parallel()

	publisher := &recordingGraphProjectionPhasePublisher{}
	handler := DeployableUnitCorrelationHandler{
		PhasePublisher: publisher,
		FactLoader: &stubDeployableUnitFactLoader{
			envelopes: deployableUnitCorrelationEnvelopes(
				"repo-service-jenkins",
				"service-jenkins",
				[]map[string]any{
					{
						"repo_id":       "repo-service-jenkins",
						"language":      "dockerfile",
						"relative_path": "Dockerfile",
						"parsed_file_data": map[string]any{
							"dockerfile_stages": []any{
								map[string]any{"name": "runtime"},
							},
						},
					},
					{
						"repo_id":       "repo-service-jenkins",
						"language":      "groovy",
						"relative_path": "Jenkinsfile",
						"parsed_file_data": map[string]any{
							"jenkins_pipeline_calls": []any{"deployShared"},
						},
					},
				},
			),
		},
	}

	_, err := handler.Handle(context.Background(), deployableUnitIntent("service-jenkins"))
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if got, want := len(publisher.calls), 1; got != want {
		t.Fatalf("publisher calls = %d, want %d", got, want)
	}
	if got, want := publisher.calls[0][0].Key.Keyspace, GraphProjectionKeyspaceDeployableUnitUID; got != want {
		t.Fatalf("published keyspace = %q, want %q", got, want)
	}
	if got, want := publisher.calls[0][0].Phase, GraphProjectionPhaseDeployableUnitCorrelation; got != want {
		t.Fatalf("published phase = %q, want %q", got, want)
	}
	if got, want := publisher.calls[0][0].Key.AcceptanceUnitID, "service-jenkins"; got != want {
		t.Fatalf("acceptance unit id = %q, want %q", got, want)
	}
}

func TestDeployableUnitCorrelationHandleFiltersByEntityKeys(t *testing.T) {
	t.Parallel()

	handler := DeployableUnitCorrelationHandler{
		FactLoader: &stubDeployableUnitFactLoader{
			envelopes: append(
				deployableUnitCorrelationEnvelopes(
					"repo-edge-api",
					"edge-api",
					[]map[string]any{
						{
							"repo_id":       "repo-edge-api",
							"language":      "dockerfile",
							"relative_path": "Dockerfile",
							"parsed_file_data": map[string]any{
								"dockerfile_stages": []any{
									map[string]any{"name": "runtime"},
								},
							},
						},
					},
				),
				deployableUnitCorrelationEnvelopes(
					"repo-worker",
					"background-worker",
					[]map[string]any{
						{
							"repo_id": "repo-worker",
							"parsed_file_data": map[string]any{
								"argocd_applications": []any{
									map[string]any{"name": "background-worker"},
								},
							},
						},
					},
				)...,
			),
		},
	}

	got, err := handler.Handle(context.Background(), deployableUnitIntent("edge-api"))
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if !strings.Contains(got.EvidenceSummary, "evaluated 1 deployable unit candidate") {
		t.Fatalf("Handle().EvidenceSummary = %q, want one evaluated candidate", got.EvidenceSummary)
	}
}

func TestDeployableUnitCorrelationHandleAcceptsWorkloadPrefixedEntityKey(t *testing.T) {
	t.Parallel()

	factLoader := &stubDeployableUnitFactLoader{
		envelopes: deployableUnitCorrelationEnvelopes(
			"repo-edge-api",
			"edge-api",
			[]map[string]any{
				{
					"repo_id":       "repo-edge-api",
					"language":      "dockerfile",
					"relative_path": "Dockerfile",
					"parsed_file_data": map[string]any{
						"dockerfile_stages": []any{
							map[string]any{"name": "runtime"},
						},
					},
				},
			},
		),
	}
	resolvedLoader := &stubDeployableUnitResolvedLoader{
		resolved: []relationships.ResolvedRelationship{
			{
				SourceRepoID:     "repo-edge-api",
				TargetRepoID:     "repo-deployments",
				RelationshipType: relationships.RelDeploysFrom,
				Confidence:       0.94,
				Details: map[string]any{
					"evidence_kinds": []string{
						string(relationships.EvidenceKindArgoCDAppSource),
					},
				},
			},
		},
	}

	handler := DeployableUnitCorrelationHandler{
		FactLoader:     factLoader,
		ResolvedLoader: resolvedLoader,
	}

	got, err := handler.Handle(context.Background(), deployableUnitIntent("workload:edge-api"))
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if !strings.Contains(got.EvidenceSummary, "admitted=1") {
		t.Fatalf("Handle().EvidenceSummary = %q, want admitted=1", got.EvidenceSummary)
	}
}

func TestDeployableUnitCorrelationHandleFactLoaderError(t *testing.T) {
	t.Parallel()

	handler := DeployableUnitCorrelationHandler{
		FactLoader: &stubDeployableUnitFactLoader{err: fmt.Errorf("facts unavailable")},
	}

	_, err := handler.Handle(context.Background(), deployableUnitIntent("edge-api"))
	if err == nil {
		t.Fatal("Handle() error = nil, want non-nil")
	}
}

func deployableUnitIntent(entityKeys ...string) Intent {
	return Intent{
		IntentID:        "intent-1",
		ScopeID:         "repository:test-scope",
		GenerationID:    "generation-1",
		SourceSystem:    "git",
		Domain:          DomainDeployableUnitCorrelation,
		Cause:           "facts committed",
		EntityKeys:      entityKeys,
		RelatedScopeIDs: []string{"repository:deploy-repo"},
	}
}

func deployableUnitCorrelationEnvelopes(
	repoID string,
	repoName string,
	filePayloads []map[string]any,
) []facts.Envelope {
	now := time.Now().UTC()
	envelopes := []facts.Envelope{
		{
			FactID:   "fact-repo-" + repoID,
			FactKind: "repository",
			Payload: map[string]any{
				"graph_id": repoID,
				"name":     repoName,
			},
			ObservedAt: now,
		},
	}

	for idx, payload := range filePayloads {
		envelopes = append(envelopes, facts.Envelope{
			FactID:     fmt.Sprintf("fact-file-%s-%d", repoID, idx),
			FactKind:   "file",
			Payload:    payload,
			ObservedAt: now,
		})
	}

	return envelopes
}
