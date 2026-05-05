package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

func TestApplyResolvedDeploymentSourcesReclassifiesControllerCandidateAsService(t *testing.T) {
	t.Parallel()

	candidates := []WorkloadCandidate{
		{
			RepoID:         "repo-api",
			RepoName:       "api-service",
			Classification: "utility",
			Confidence:     0.42,
			Provenance:     []string{"jenkins_pipeline"},
		},
	}
	resolved := []relationships.ResolvedRelationship{
		{
			SourceRepoID:     "repo-deploy",
			TargetRepoID:     "repo-api",
			RelationshipType: relationships.RelDeploysFrom,
			Confidence:       0.9,
			ResolutionSource: "test",
			Details: map[string]any{
				"evidence_kinds": []string{string(relationships.EvidenceKindKustomizeResource)},
			},
		},
	}

	enriched := applyResolvedDeploymentSources(candidates, resolved)
	if len(enriched) != 1 {
		t.Fatalf("len(enriched) = %d, want 1", len(enriched))
	}
	candidate := enriched[0]
	if got, want := candidate.DeploymentRepoID, "repo-deploy"; got != want {
		t.Fatalf("DeploymentRepoID = %q, want %q", got, want)
	}
	if got, want := candidate.Classification, "service"; got != want {
		t.Fatalf("Classification = %q, want %q after deployment evidence enrichment", got, want)
	}
	if got, want := candidate.Confidence, 0.9; got != want {
		t.Fatalf("Confidence = %f, want %f", got, want)
	}
	if !hasProvenance(candidate.Provenance, "kustomize_resource") {
		t.Fatalf("Provenance = %v, want kustomize_resource", candidate.Provenance)
	}
}

func TestApplyResolvedDeploymentSourcesPreservesMultipleDeploymentRepos(t *testing.T) {
	t.Parallel()

	candidates := []WorkloadCandidate{
		{
			RepoID:         "repo-api",
			RepoName:       "api-service",
			Classification: "service",
			Confidence:     0.84,
			Provenance:     []string{"dockerfile_runtime"},
		},
	}
	resolved := []relationships.ResolvedRelationship{
		{
			SourceRepoID:     "repo-current-deploy",
			TargetRepoID:     "repo-api",
			RelationshipType: relationships.RelDeploysFrom,
			Confidence:       0.90,
			Details: map[string]any{
				"evidence_kinds": []string{string(relationships.EvidenceKindHelmValues)},
			},
		},
		{
			SourceRepoID:     "repo-api",
			TargetRepoID:     "repo-next-deploy",
			RelationshipType: relationships.RelDeploysFrom,
			Confidence:       0.93,
			Details: map[string]any{
				"evidence_kinds": []string{string(relationships.EvidenceKindArgoCDApplicationSetDeploySource)},
			},
		},
	}

	enriched := applyResolvedDeploymentSources(candidates, resolved)
	if len(enriched) != 1 {
		t.Fatalf("len(enriched) = %d, want 1", len(enriched))
	}
	candidate := enriched[0]
	if got, want := candidate.DeploymentRepoID, "repo-next-deploy"; got != want {
		t.Fatalf("DeploymentRepoID = %q, want highest-confidence primary %q", got, want)
	}
	if got, want := candidate.DeploymentRepoIDs, []string{"repo-current-deploy", "repo-next-deploy"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("DeploymentRepoIDs = %#v, want %#v", got, want)
	}
	if got, want := candidate.Confidence, 0.93; got != want {
		t.Fatalf("Confidence = %f, want %f", got, want)
	}
	if !hasProvenance(candidate.Provenance, "helm_deployment") {
		t.Fatalf("Provenance = %v, want helm_deployment", candidate.Provenance)
	}
	if !hasProvenance(candidate.Provenance, "argocd_applicationset_deploy_source") {
		t.Fatalf("Provenance = %v, want argocd_applicationset_deploy_source", candidate.Provenance)
	}
}
