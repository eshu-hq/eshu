// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

func TestGoldenDeployableFixtureMaterializesTwoCanonicalEnvironments(t *testing.T) {
	t.Parallel()

	fixtureRoot := filepath.Join("..", "..", "..", "tests", "fixtures", "ecosystems")
	deployRoot := filepath.Join(fixtureRoot, "deployable-config")
	sourceRoot := filepath.Join(fixtureRoot, "deployable-source")
	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v", err)
	}

	parse := func(repoRoot, relativePath string) map[string]any {
		t.Helper()
		payload, err := engine.ParsePath(repoRoot, filepath.Join(repoRoot, relativePath), false, parser.Options{})
		if err != nil {
			t.Fatalf("ParsePath(%q) error = %v", relativePath, err)
		}
		serialized, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("json.Marshal(ParsePath(%q)) error = %v", relativePath, err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(serialized, &decoded); err != nil {
			t.Fatalf("json.Unmarshal(ParsePath(%q)) error = %v", relativePath, err)
		}
		return decoded
	}

	now := time.Now().UTC()
	envelopes := []facts.Envelope{
		{
			FactID:   "golden-repo-deployable-source",
			FactKind: "repository",
			Payload: map[string]any{
				"graph_id": "repo-deployable-source",
				"name":     "deployable-source",
			},
			ObservedAt: now,
		},
		{
			FactID:   "golden-repo-deployable-config",
			FactKind: "repository",
			Payload: map[string]any{
				"graph_id": "repo-deployable-config",
				"name":     "deployable-config",
			},
			ObservedAt: now,
		},
		goldenFixtureFileEnvelope(
			"golden-source-dockerfile",
			"repo-deployable-source",
			"Dockerfile",
			"dockerfile",
			parse(sourceRoot, "Dockerfile"),
			now,
		),
		goldenFixtureFileEnvelope(
			"golden-config-production",
			"repo-deployable-config",
			"application.yaml",
			"yaml",
			parse(deployRoot, "application.yaml"),
			now,
		),
		goldenFixtureFileEnvelope(
			"golden-config-stage",
			"repo-deployable-config",
			"application-stage.yaml",
			"yaml",
			parse(deployRoot, "application-stage.yaml"),
			now,
		),
		{
			FactID:   "golden-config-adjacent-unknown",
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-deployable-config",
				"relative_path": "application-adjacent.yaml",
				"parsed_file_data": map[string]any{
					"argocd_applications": []any{map[string]any{
						"name":           "deployable-source-adjacent",
						"dest_namespace": "payments-team",
					}},
				},
			},
			ObservedAt: now,
		},
	}

	candidates, deploymentEnvironments := ExtractWorkloadCandidates(envelopes)
	if got, want := deploymentEnvironments["repo-deployable-config"], []string{"prod", "stage"}; !slices.Equal(got, want) {
		t.Fatalf("deployment environments = %v, want %v", got, want)
	}

	resolved := []relationships.ResolvedRelationship{{
		// Production Argo Application evidence originates in the repository
		// containing the Application and points at the deployed repository.
		SourceRepoID:     "repo-deployable-config",
		TargetRepoID:     "repo-deployable-source",
		RelationshipType: relationships.RelDeploysFrom,
		Confidence:       0.96,
		Details: map[string]any{
			"evidence_kinds": []any{string(relationships.EvidenceKindArgoCDAppSource)},
		},
	}}
	candidates = applyResolvedDeploymentSources(candidates, resolved)
	var sourceCandidate WorkloadCandidate
	for _, candidate := range candidates {
		if candidate.RepoID == "repo-deployable-source" {
			sourceCandidate = candidate
			break
		}
	}
	if sourceCandidate.RepoID == "" {
		t.Fatal("deployable-source candidate was not admitted")
	}
	if got, want := sourceCandidate.Classification, "service"; got != want {
		t.Fatalf("candidate classification = %q, want %q", got, want)
	}
	if got, want := sourceCandidate.DeploymentRepoID, "repo-deployable-config"; got != want {
		t.Fatalf("candidate deployment repo = %q, want %q", got, want)
	}

	projection := BuildProjectionRows([]WorkloadCandidate{sourceCandidate}, deploymentEnvironments)
	if got, want := len(projection.InstanceRows), 2; got != want {
		t.Fatalf("instance rows = %d, want %d", got, want)
	}
	instanceIDs := []string{projection.InstanceRows[0].InstanceID, projection.InstanceRows[1].InstanceID}
	slices.Sort(instanceIDs)
	if want := []string{
		"workload-instance:deployable-source:prod",
		"workload-instance:deployable-source:stage",
	}; !slices.Equal(instanceIDs, want) {
		t.Fatalf("instance IDs = %v, want %v", instanceIDs, want)
	}

	executor := &fakeNeo4jExecutor{}
	materialized, err := NewWorkloadMaterializer(executor).Materialize(context.Background(), projection)
	if err != nil {
		t.Fatalf("Materialize() error = %v", err)
	}
	if got, want := materialized.InstancesWritten, 2; got != want {
		t.Fatalf("InstancesWritten = %d, want %d", got, want)
	}
	for environment, instanceID := range map[string]string{
		"prod":  "workload-instance:deployable-source:prod",
		"stage": "workload-instance:deployable-source:stage",
	} {
		if !fakeCallContainsRow(executor.calls, batchWorkloadInstanceNodeUpsertCypher, map[string]any{
			"instance_id": instanceID,
			"workload_id": "workload:deployable-source",
			"environment": environment,
		}) {
			t.Errorf("WorkloadInstance graph upsert missing %q in %q", instanceID, environment)
		}
		if !fakeCallContainsRow(executor.calls, batchWorkloadInstanceOfEdgeUpsertCypher, map[string]any{
			"instance_id": instanceID,
			"workload_id": "workload:deployable-source",
		}) {
			t.Errorf("INSTANCE_OF graph upsert missing %q -> workload:deployable-source", instanceID)
		}
		if !fakeCallContainsRow(executor.calls, batchDeploymentSourceUpsertCypher, map[string]any{
			"instance_id":        instanceID,
			"deployment_repo_id": "repo-deployable-config",
		}) {
			t.Errorf("DEPLOYMENT_SOURCE graph upsert missing %q -> repo-deployable-config", instanceID)
		}
	}
}

func goldenFixtureFileEnvelope(
	factID string,
	repoID string,
	relativePath string,
	language string,
	parsed map[string]any,
	observedAt time.Time,
) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: "file",
		Payload: map[string]any{
			"repo_id":          repoID,
			"relative_path":    relativePath,
			"language":         language,
			"parsed_file_data": parsed,
		},
		ObservedAt: observedAt,
	}
}

func fakeCallContainsRow(calls []fakeExecutorCall, cypher string, want map[string]any) bool {
	for _, call := range calls {
		if call.Cypher != cypher {
			continue
		}
		rows, _ := call.Parameters["rows"].([]map[string]any)
		for _, row := range rows {
			matches := true
			for key, value := range want {
				if row[key] != value {
					matches = false
					break
				}
			}
			if matches {
				return true
			}
		}
	}
	return false
}
