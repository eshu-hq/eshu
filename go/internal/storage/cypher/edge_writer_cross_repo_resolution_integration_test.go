package cypher

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

type fakeCrossRepoEvidenceLoader struct {
	facts []relationships.EvidenceFact
}

func (f *fakeCrossRepoEvidenceLoader) ListEvidenceFacts(
	_ context.Context,
	_ string,
) ([]relationships.EvidenceFact, error) {
	return f.facts, nil
}

type recordingIntentWriter struct {
	rows [][]reducer.SharedProjectionIntentRow
}

func (r *recordingIntentWriter) UpsertIntents(
	_ context.Context,
	rows []reducer.SharedProjectionIntentRow,
) error {
	copied := make([]reducer.SharedProjectionIntentRow, len(rows))
	copy(copied, rows)
	r.rows = append(r.rows, copied)
	return nil
}

func TestCrossRepoResolutionDispatchesTypedRelationshipsIntoNeo4jWrites(t *testing.T) {
	t.Parallel()

	evidence := []relationships.EvidenceFact{
		{
			EvidenceKind:     relationships.EvidenceKindTerraformAppRepo,
			RelationshipType: relationships.RelProvisionsDependencyFor,
			SourceRepoID:     "infra-repo",
			TargetRepoID:     "app-repo",
			Confidence:       0.99,
		},
		{
			EvidenceKind:     relationships.EvidenceKindHelmChart,
			RelationshipType: relationships.RelDeploysFrom,
			SourceRepoID:     "deploy-repo",
			TargetRepoID:     "config-repo",
			Confidence:       0.97,
		},
		{
			EvidenceKind:     relationships.EvidenceKindArgoCDApplicationSetDiscovery,
			RelationshipType: relationships.RelDiscoversConfigIn,
			SourceRepoID:     "control-repo",
			TargetRepoID:     "shared-config-repo",
			Confidence:       0.96,
		},
		{
			EvidenceKind:     relationships.EvidenceKindArgoCDDestinationPlatform,
			RelationshipType: relationships.RelRunsOn,
			SourceRepoID:     "service-repo",
			TargetEntityID:   "platform:eks:aws:cluster-1:prod:us-east-1",
			Confidence:       0.98,
		},
	}

	intentWriter := &recordingIntentWriter{}
	executor := &recordingExecutor{}
	handler := reducer.CrossRepoRelationshipHandler{
		EvidenceLoader: &fakeCrossRepoEvidenceLoader{facts: evidence},
		IntentWriter:   intentWriter,
	}

	count, err := handler.Resolve(context.Background(), "scope-1", "gen-1")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if count != 4 {
		t.Fatalf("Resolve() = %d, want 4", count)
	}

	if got, want := len(intentWriter.rows), 1; got != want {
		t.Fatalf("intent writes = %d, want %d", got, want)
	}
	intents := intentWriter.rows[0]
	if got, want := len(intents), 4; got != want {
		t.Fatalf("intent row count = %d, want %d", got, want)
	}

	writer := NewEdgeWriter(executor, 0)
	if err := writer.WriteEdges(
		context.Background(),
		reducer.DomainRepoDependency,
		intents,
		"resolver/cross-repo",
	); err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}

	if got, want := len(executor.calls), 4; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}

	typedRepoWrites := map[string]*Statement{}
	var runsOnWrite *Statement
	for i := range executor.calls {
		call := &executor.calls[i]
		switch {
		case strings.Contains(call.Cypher, "MERGE (i)-[rel:RUNS_ON]->(p)"):
			runsOnWrite = call
		case strings.Contains(call.Cypher, "MERGE (source_repo)-[rel:DEPLOYS_FROM]->(target_repo)"):
			typedRepoWrites[string(relationships.RelDeploysFrom)] = call
		case strings.Contains(call.Cypher, "MERGE (source_repo)-[rel:DISCOVERS_CONFIG_IN]->(target_repo)"):
			typedRepoWrites[string(relationships.RelDiscoversConfigIn)] = call
		case strings.Contains(call.Cypher, "MERGE (source_repo)-[rel:PROVISIONS_DEPENDENCY_FOR]->(target_repo)"):
			typedRepoWrites[string(relationships.RelProvisionsDependencyFor)] = call
		}
	}

	gotEvidenceTypes := map[string]bool{}
	for _, want := range []string{
		string(relationships.RelProvisionsDependencyFor),
		string(relationships.RelDeploysFrom),
		string(relationships.RelDiscoversConfigIn),
	} {
		typedRepoWrite := typedRepoWrites[want]
		if typedRepoWrite == nil {
			t.Fatalf("typed repo write call not found for %q", want)
		}
		if typedRepoWrite.Operation != OperationCanonicalUpsert {
			t.Fatalf("typed repo write operation = %q, want %q", typedRepoWrite.Operation, OperationCanonicalUpsert)
		}
		if strings.Contains(typedRepoWrite.Cypher, "FOREACH") {
			t.Fatalf("typed repo write must use direct MERGE, got FOREACH: %s", typedRepoWrite.Cypher)
		}
		typedRows, ok := typedRepoWrite.Parameters["rows"].([]map[string]any)
		if !ok {
			t.Fatalf("typed repo rows type = %T, want []map[string]any", typedRepoWrite.Parameters["rows"])
		}
		if got, wantRows := len(typedRows), 1; got != wantRows {
			t.Fatalf("len(typed repo rows) = %d, want %d for %q", got, wantRows, want)
		}
		relType, _ := typedRows[0]["relationship_type"].(string)
		if relType != want {
			t.Fatalf("typed repo row relationship_type = %q, want %q", relType, want)
		}
		evidenceType, _ := typedRows[0]["evidence_type"].(string)
		if evidenceType != "" {
			gotEvidenceTypes[evidenceType] = true
		}
	}
	for _, want := range []string{"terraform_app_repo", "helm_chart_reference", "argocd_applicationset_discovery"} {
		if !gotEvidenceTypes[want] {
			t.Fatalf("typed repo rows missing evidence_type %q", want)
		}
	}

	if runsOnWrite == nil {
		t.Fatal("runs_on write call not found")
	}
	if runsOnWrite.Operation != OperationCanonicalUpsert {
		t.Fatalf("runs_on write operation = %q, want %q", runsOnWrite.Operation, OperationCanonicalUpsert)
	}
	if !strings.Contains(runsOnWrite.Cypher, "MERGE (i)-[rel:RUNS_ON]->(p)") {
		t.Fatalf("runs_on cypher missing RUNS_ON merge: %s", runsOnWrite.Cypher)
	}
	if !strings.Contains(runsOnWrite.Cypher, "WorkloadInstance") {
		t.Fatalf("runs_on cypher missing WorkloadInstance match: %s", runsOnWrite.Cypher)
	}

	runsOnRows, ok := runsOnWrite.Parameters["rows"].([]map[string]any)
	if !ok {
		t.Fatalf("runs_on rows type = %T, want []map[string]any", runsOnWrite.Parameters["rows"])
	}
	if got, want := len(runsOnRows), 1; got != want {
		t.Fatalf("len(runs_on rows) = %d, want %d", got, want)
	}
	if got, want := runsOnRows[0]["repo_id"], "service-repo"; got != want {
		t.Fatalf("runs_on repo_id = %#v, want %#v", got, want)
	}
	if got, want := runsOnRows[0]["platform_id"], "platform:eks:aws:cluster-1:prod:us-east-1"; got != want {
		t.Fatalf("runs_on platform_id = %#v, want %#v", got, want)
	}
}
