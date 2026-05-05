package reducer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

func TestCrossRepoResolutionPreservesTypedRelationshipFamilies(t *testing.T) {
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
			Confidence:       0.95,
		},
		{
			EvidenceKind:     relationships.EvidenceKindArgoCDApplicationSetDiscovery,
			RelationshipType: relationships.RelDiscoversConfigIn,
			SourceRepoID:     "control-repo",
			TargetRepoID:     "config-repo",
			Confidence:       0.97,
		},
		{
			EvidenceKind:     relationships.EvidenceKindArgoCDDestinationPlatform,
			RelationshipType: relationships.RelRunsOn,
			SourceRepoID:     "service-repo",
			TargetEntityID:   "platform:eks:aws:cluster-1:prod:us-east-1",
			Confidence:       0.98,
		},
		{
			EvidenceKind:     relationships.EvidenceKindTerraformIAMPermission,
			RelationshipType: relationships.RelReadsConfigFrom,
			SourceRepoID:     "consumer-repo",
			TargetRepoID:     "config-owner-repo",
			Confidence:       0.92,
		},
	}

	intentWriter := &recordingRepoDependencyIntentWriter{}

	handler := CrossRepoRelationshipHandler{
		EvidenceLoader: &fakeEvidenceFactLoader{facts: evidence},
		IntentWriter:   intentWriter,
	}

	count, err := handler.Resolve(context.Background(), "scope-1", "gen-1")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if count != 5 {
		t.Fatalf("Resolve() = %d, want 5", count)
	}

	if len(intentWriter.rows) != 1 {
		t.Fatalf("expected 1 intent write, got %d", len(intentWriter.rows))
	}
	rows := intentWriter.rows[0]
	if len(rows) != 5 {
		t.Fatalf("expected 5 write rows, got %d", len(rows))
	}

	gotTypes := make(map[string]bool, len(rows))
	for _, row := range rows {
		gotTypes[stringValue(row.Payload["relationship_type"])] = true
	}
	for _, want := range []string{
		string(relationships.RelProvisionsDependencyFor),
		string(relationships.RelDeploysFrom),
		string(relationships.RelDiscoversConfigIn),
		string(relationships.RelRunsOn),
		string(relationships.RelReadsConfigFrom),
	} {
		if !gotTypes[want] {
			t.Fatalf("missing relationship_type %q in write rows", want)
		}
	}

	for _, row := range rows {
		if stringValue(row.Payload["relationship_type"]) != string(relationships.RelReadsConfigFrom) {
			continue
		}
		if got, want := stringValue(row.Payload["evidence_type"]), "terraform_iam_permission"; got != want {
			t.Fatalf("READS_CONFIG_FROM evidence_type = %q, want %q", got, want)
		}
	}

	var runsOnRow SharedProjectionIntentRow
	for _, row := range rows {
		if stringValue(row.Payload["relationship_type"]) == string(relationships.RelRunsOn) {
			runsOnRow = row
			break
		}
	}
	if got := stringValue(runsOnRow.Payload["repo_id"]); got != "service-repo" {
		t.Fatalf("runs_on repo_id = %q, want %q", got, "service-repo")
	}
	if got := stringValue(runsOnRow.Payload["platform_id"]); got != "platform:eks:aws:cluster-1:prod:us-east-1" {
		t.Fatalf("runs_on platform_id = %q, want %q", got, "platform:eks:aws:cluster-1:prod:us-east-1")
	}
	if got := stringValue(runsOnRow.Payload["target_repo_id"]); got != "" {
		t.Fatalf("runs_on target_repo_id = %q, want empty", got)
	}
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}
