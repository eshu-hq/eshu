package cypher

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestEdgeWriterWriteEdgesTypedRepoRelationshipDispatch(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "i1",
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"repo_id":           "repo-a",
				"target_repo_id":    "repo-b",
				"relationship_type": "DEPLOYS_FROM",
				"evidence_type":     "argocd_application_source",
				"resolved_id":       "resolved-1",
				"generation_id":     "gen-1",
				"evidence_count":    3,
				"evidence_kinds":    []string{"ARGOCD_APPLICATION_SOURCE", "HELM_VALUES_REFERENCE"},
				"resolution_source": "inferred",
				"confidence":        0.93,
				"rationale":         "deployment config references service repository",
			},
		},
		{
			IntentID:     "i2",
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"repo_id":           "repo-a",
				"target_repo_id":    "repo-c",
				"relationship_type": "DISCOVERS_CONFIG_IN",
				"evidence_type":     "github_actions_reusable_workflow_ref",
			},
		},
		{
			IntentID:     "i3",
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"repo_id":           "repo-a",
				"target_repo_id":    "repo-d",
				"relationship_type": "PROVISIONS_DEPENDENCY_FOR",
				"evidence_type":     "terraform_module_source",
			},
		},
		{
			IntentID:     "i4",
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"repo_id":           "repo-a",
				"target_repo_id":    "repo-e",
				"relationship_type": "USES_MODULE",
				"evidence_type":     "terraform_module_source",
			},
		},
		{
			IntentID:     "i5",
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"repo_id":           "repo-a",
				"target_repo_id":    "repo-f",
				"relationship_type": "READS_CONFIG_FROM",
				"evidence_type":     "terraform_iam_permission",
			},
		},
	}

	err := writer.WriteEdges(context.Background(), reducer.DomainRepoDependency, rows, "resolver/cross-repo")
	if err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 5; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	wantByType := map[string]string{
		"DEPLOYS_FROM":              "MERGE (source_repo)-[rel:DEPLOYS_FROM]->(target_repo)",
		"DISCOVERS_CONFIG_IN":       "MERGE (source_repo)-[rel:DISCOVERS_CONFIG_IN]->(target_repo)",
		"PROVISIONS_DEPENDENCY_FOR": "MERGE (source_repo)-[rel:PROVISIONS_DEPENDENCY_FOR]->(target_repo)",
		"USES_MODULE":               "MERGE (source_repo)-[rel:USES_MODULE]->(target_repo)",
		"READS_CONFIG_FROM":         "MERGE (source_repo)-[rel:READS_CONFIG_FROM]->(target_repo)",
	}
	seen := make(map[string]bool)
	for _, call := range executor.calls {
		if strings.Contains(call.Cypher, "FOREACH") {
			t.Fatalf("typed repo relationship write must not rely on FOREACH routing: %s", call.Cypher)
		}
		rowsOut, ok := call.Parameters["rows"].([]map[string]any)
		if !ok {
			t.Fatalf("rows type = %T, want []map[string]any", call.Parameters["rows"])
		}
		if got, want := len(rowsOut), 1; got != want {
			t.Fatalf("len(rows) = %d, want %d", got, want)
		}
		relType, _ := rowsOut[0]["relationship_type"].(string)
		wantFragment, ok := wantByType[relType]
		if !ok {
			t.Fatalf("unexpected relationship_type row: %#v", rowsOut[0])
		}
		if !strings.Contains(call.Cypher, wantFragment) {
			t.Fatalf("cypher for %s missing direct MERGE %q: %s", relType, wantFragment, call.Cypher)
		}
		if rowsOut[0]["evidence_type"] == nil || rowsOut[0]["evidence_type"] == "" {
			t.Fatalf("row missing evidence_type: %#v", rowsOut[0])
		}
		if relType == "DEPLOYS_FROM" {
			for key, want := range map[string]any{
				"resolved_id":       "resolved-1",
				"generation_id":     "gen-1",
				"evidence_count":    3,
				"resolution_source": "inferred",
				"confidence":        0.93,
				"rationale":         "deployment config references service repository",
			} {
				if got := rowsOut[0][key]; got != want {
					t.Fatalf("row %s = %#v, want %#v", key, got, want)
				}
			}
			wantKinds := []string{"ARGOCD_APPLICATION_SOURCE", "HELM_VALUES_REFERENCE"}
			if got := rowsOut[0]["evidence_kinds"]; !reflect.DeepEqual(got, wantKinds) {
				t.Fatalf("row evidence_kinds = %#v, want %#v", got, wantKinds)
			}
		}
		seen[relType] = true
	}
	for relType := range wantByType {
		if !seen[relType] {
			t.Fatalf("missing write route for %s", relType)
		}
	}
}

func TestEdgeWriterWriteEdgesMaterializesRepoEvidenceArtifacts(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "i1",
			RepositoryID: "repo-deploy",
			GenerationID: "gen-1",
			Payload: map[string]any{
				"repo_id":           "repo-deploy",
				"target_repo_id":    "repo-service",
				"relationship_type": "DEPLOYS_FROM",
				"resolved_id":       "resolved-1",
				"generation_id":     "gen-1",
				"evidence_artifacts": []map[string]any{
					{
						"evidence_kind":   "HELM_VALUES_REFERENCE",
						"artifact_family": "helm",
						"path":            "argocd/service-api/overlays/prod/values.yaml",
						"extractor":       "helm",
						"environment":     "prod",
						"matched_alias":   "service-api",
						"matched_value":   "registry.example.test/service-api",
						"confidence":      0.84,
					},
				},
			},
		},
	}

	err := writer.WriteEdges(context.Background(), reducer.DomainRepoDependency, rows, "resolver/cross-repo")
	if err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 2; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}

	var artifactCall *Statement
	for i := range executor.calls {
		if strings.Contains(executor.calls[i].Cypher, "HAS_DEPLOYMENT_EVIDENCE") {
			artifactCall = &executor.calls[i]
			break
		}
	}
	if artifactCall == nil {
		t.Fatalf("missing evidence artifact write call: %#v", executor.calls)
	}
	for _, fragment := range []string{
		"MERGE (artifact:EvidenceArtifact {id: row.artifact_id})",
		"MERGE (source_repo)-[source_rel:HAS_DEPLOYMENT_EVIDENCE]->(artifact)",
		"MERGE (artifact)-[target_rel:EVIDENCES_REPOSITORY_RELATIONSHIP]->(target_repo)",
		"MERGE (env:Environment {name: row.environment})",
		"MERGE (artifact)-[env_rel:TARGETS_ENVIRONMENT]->(env)",
	} {
		if !strings.Contains(artifactCall.Cypher, fragment) {
			t.Fatalf("artifact cypher missing %q: %s", fragment, artifactCall.Cypher)
		}
	}

	rowsOut, ok := artifactCall.Parameters["rows"].([]map[string]any)
	if !ok {
		t.Fatalf("rows type = %T, want []map[string]any", artifactCall.Parameters["rows"])
	}
	if got, want := len(rowsOut), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	artifact := rowsOut[0]
	for key, want := range map[string]any{
		"repo_id":           "repo-deploy",
		"target_repo_id":    "repo-service",
		"relationship_type": "DEPLOYS_FROM",
		"resolved_id":       "resolved-1",
		"generation_id":     "gen-1",
		"evidence_kind":     "HELM_VALUES_REFERENCE",
		"artifact_family":   "helm",
		"path":              "argocd/service-api/overlays/prod/values.yaml",
		"extractor":         "helm",
		"environment":       "prod",
		"matched_alias":     "service-api",
		"matched_value":     "registry.example.test/service-api",
		"confidence":        0.84,
		"evidence_source":   "resolver/cross-repo",
	} {
		if got := artifact[key]; got != want {
			t.Fatalf("artifact[%s] = %#v, want %#v; artifact=%#v", key, got, want, artifact)
		}
	}
	if got, ok := artifact["artifact_id"].(string); !ok || !strings.HasPrefix(got, "evidence-artifact:") {
		t.Fatalf("artifact_id = %#v, want evidence-artifact:*", artifact["artifact_id"])
	}
}

func TestEdgeWriterWriteEdgesRunsOnDispatchUsesWorkloadInstanceShape(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "i1",
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"repo_id":           "repo-a",
				"platform_id":       "platform:eks:aws:cluster-1:prod:us-east-1",
				"relationship_type": "RUNS_ON",
			},
		},
	}

	err := writer.WriteEdges(context.Background(), reducer.DomainRepoDependency, rows, "resolver/cross-repo")
	if err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	cypher := executor.calls[0].Cypher
	if !strings.Contains(cypher, "WorkloadInstance") {
		t.Fatalf("cypher missing WorkloadInstance match: %s", cypher)
	}
	if !strings.Contains(cypher, "MERGE (i)-[rel:RUNS_ON]->(p)") {
		t.Fatalf("cypher missing RUNS_ON merge: %s", cypher)
	}
}
