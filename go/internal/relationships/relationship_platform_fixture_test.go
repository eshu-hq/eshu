package relationships

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestRelationshipPlatformFixtureWorkflowEmitsReusableWorkflowEvidence(t *testing.T) {
	t.Parallel()

	content := readRelationshipPlatformFixture(
		t,
		"service-edge-api",
		".github",
		"workflows",
		"deploy-legacy.yml",
	)

	evidence := DiscoverEvidence(
		[]facts.Envelope{
			{
				ScopeID: "service-edge-api",
				Payload: map[string]any{
					"artifact_type": "github_actions_workflow",
					"relative_path": ".github/workflows/deploy-legacy.yml",
					"content":       content,
				},
			},
		},
		[]CatalogEntry{
			{RepoID: "delivery-legacy-automation", Aliases: []string{"delivery-legacy-automation"}},
		},
	)

	workflowEvidence, ok := findEvidenceByKind(evidence, EvidenceKindGitHubActionsReusableWorkflow)
	if !ok {
		t.Fatal("missing reusable workflow evidence for relationship-platform fixture")
	}
	if got, want := workflowEvidence.TargetRepoID, "delivery-legacy-automation"; got != want {
		t.Fatalf("workflow target repo = %q, want %q", got, want)
	}
	if got, want := workflowEvidence.RelationshipType, RelDeploysFrom; got != want {
		t.Fatalf("workflow relationship type = %q, want %q", got, want)
	}
}

func TestRelationshipPlatformFixtureComposeEmitsCrossRepoEvidence(t *testing.T) {
	t.Parallel()

	content := readRelationshipPlatformFixture(t, "service-edge-api", "docker-compose.yaml")

	evidence := DiscoverEvidence(
		[]facts.Envelope{
			{
				ScopeID: "service-edge-api",
				Payload: map[string]any{
					"artifact_type": "docker_compose",
					"relative_path": "docker-compose.yaml",
					"content":       content,
				},
			},
		},
		[]CatalogEntry{
			{RepoID: "service-worker-jobs", Aliases: []string{"service-worker-jobs"}},
		},
	)

	buildContextEvidence, ok := findEvidenceByKind(evidence, EvidenceKindDockerComposeBuildContext)
	if !ok {
		t.Fatal("missing docker compose build-context evidence for relationship-platform fixture")
	}
	if got, want := buildContextEvidence.TargetRepoID, "service-worker-jobs"; got != want {
		t.Fatalf("build-context target repo = %q, want %q", got, want)
	}
	if got, want := buildContextEvidence.RelationshipType, RelDeploysFrom; got != want {
		t.Fatalf("build-context relationship type = %q, want %q", got, want)
	}

	imageEvidence, ok := findEvidenceByKind(evidence, EvidenceKindDockerComposeImage)
	if !ok {
		t.Fatal("missing docker compose image evidence for relationship-platform fixture")
	}
	if got, want := imageEvidence.TargetRepoID, "service-worker-jobs"; got != want {
		t.Fatalf("image target repo = %q, want %q", got, want)
	}
	if got, want := imageEvidence.RelationshipType, RelDeploysFrom; got != want {
		t.Fatalf("image relationship type = %q, want %q", got, want)
	}

	dependsOnEvidence, ok := findEvidenceByKind(evidence, EvidenceKindDockerComposeDependsOn)
	if !ok {
		t.Fatal("missing docker compose depends_on evidence for relationship-platform fixture")
	}
	if got, want := dependsOnEvidence.TargetRepoID, "service-worker-jobs"; got != want {
		t.Fatalf("depends_on target repo = %q, want %q", got, want)
	}
	if got, want := dependsOnEvidence.RelationshipType, RelDependsOn; got != want {
		t.Fatalf("depends_on relationship type = %q, want %q", got, want)
	}
}

func TestRelationshipPlatformFixtureApplicationSetEmitsGitOpsDiscoveryEvidence(t *testing.T) {
	t.Parallel()

	content := readRelationshipPlatformFixture(
		t,
		"delivery-argocd",
		"applicationsets",
		"platform-apps.yaml",
	)

	evidence := DiscoverEvidence(
		[]facts.Envelope{
			{
				ScopeID: "delivery-argocd",
				Payload: map[string]any{
					"artifact_type": "yaml",
					"relative_path": "applicationsets/platform-apps.yaml",
					"content":       content,
				},
			},
		},
		[]CatalogEntry{
			{RepoID: "deployment-kustomize", Aliases: []string{"deployment-kustomize"}},
		},
	)

	appSetEvidence, ok := findEvidenceByKind(evidence, EvidenceKindArgoCDApplicationSetDiscovery)
	if !ok {
		t.Fatal("missing applicationset discovery evidence for relationship-platform fixture")
	}
	if got, want := appSetEvidence.TargetRepoID, "deployment-kustomize"; got != want {
		t.Fatalf("applicationset target repo = %q, want %q", got, want)
	}
	if got, want := appSetEvidence.RelationshipType, RelDiscoversConfigIn; got != want {
		t.Fatalf("applicationset relationship type = %q, want %q", got, want)
	}
}

func TestRelationshipPlatformFixtureTerragruntEmitsProvisioningEvidence(t *testing.T) {
	t.Parallel()

	content := readRelationshipPlatformFixture(t, "infra-runtime-modern", "terragrunt.hcl")

	evidence := DiscoverEvidence(
		[]facts.Envelope{
			{
				ScopeID: "infra-runtime-modern",
				Payload: map[string]any{
					"artifact_type": "terragrunt",
					"relative_path": "terragrunt.hcl",
					"content":       content,
				},
			},
		},
		[]CatalogEntry{
			{RepoID: "infra-modules-shared", Aliases: []string{"infra-modules-shared"}},
		},
	)

	moduleEvidence, ok := findEvidenceByKind(evidence, EvidenceKindTerraformModuleSource)
	if !ok {
		t.Fatal("missing terraform module source evidence for relationship-platform fixture")
	}
	if got, want := moduleEvidence.TargetRepoID, "infra-modules-shared"; got != want {
		t.Fatalf("terragrunt module target repo = %q, want %q", got, want)
	}
	if got, want := moduleEvidence.RelationshipType, RelUsesModule; got != want {
		t.Fatalf("terragrunt module relationship type = %q, want %q", got, want)
	}
}

func TestRelationshipPlatformFixtureLocalWorkflowDoesNotEmitCanonicalRepoEvidence(t *testing.T) {
	t.Parallel()

	content := readRelationshipPlatformFixture(
		t,
		"service-worker-jobs",
		".github",
		"workflows",
		"deploy-modern.yml",
	)

	evidence := DiscoverEvidence(
		[]facts.Envelope{
			{
				ScopeID: "service-worker-jobs",
				Payload: map[string]any{
					"artifact_type": "github_actions_workflow",
					"relative_path": ".github/workflows/deploy-modern.yml",
					"content":       content,
				},
			},
		},
		[]CatalogEntry{
			{RepoID: "delivery-legacy-automation", Aliases: []string{"delivery-legacy-automation"}},
			{RepoID: "service-worker-jobs", Aliases: []string{"service-worker-jobs"}},
		},
	)

	if len(evidence) != 0 {
		t.Fatalf("local workflow evidence = %#v, want none", evidence)
	}
}

func readRelationshipPlatformFixture(t *testing.T, pathParts ...string) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}

	parts := append(
		[]string{filepath.Dir(file), "..", "..", "..", "tests", "fixtures", "relationship_platform"},
		pathParts...,
	)
	path := filepath.Join(parts...)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) error = %v", path, err)
	}
	return string(content)
}
