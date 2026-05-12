package relationships

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestDiscoverEvidenceEmptyInputs(t *testing.T) {
	t.Parallel()

	if result := DiscoverEvidence(nil, nil); result != nil {
		t.Errorf("got %v, want nil", result)
	}
	if result := DiscoverEvidence([]facts.Envelope{{}}, nil); result != nil {
		t.Errorf("got %v, want nil for empty catalog", result)
	}
}

func TestDiscoverTerraformEvidence(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-infra",
			Payload: map[string]any{
				"artifact_type": "terraform",
				"relative_path": "main.tf",
				"content":       `app_repo = "payments-service"`,
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-payments", Aliases: []string{"payments-service", "payments"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)

	if len(evidence) != 1 {
		t.Fatalf("len = %d, want 1", len(evidence))
	}
	if evidence[0].EvidenceKind != EvidenceKindTerraformAppRepo {
		t.Errorf("kind = %q", evidence[0].EvidenceKind)
	}
	if evidence[0].SourceRepoID != "repo-infra" {
		t.Errorf("source = %q", evidence[0].SourceRepoID)
	}
	if evidence[0].TargetRepoID != "repo-payments" {
		t.Errorf("target = %q", evidence[0].TargetRepoID)
	}
	if evidence[0].Confidence != 0.99 {
		t.Errorf("confidence = %f", evidence[0].Confidence)
	}
}

func TestDiscoverTerraformEvidenceFromTfvarsPath(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-infra",
			Payload: map[string]any{
				"relative_path": "env/prod/terraform.tfvars",
				"content":       `app_repo = "payments-service"`,
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-payments", Aliases: []string{"payments-service", "payments"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	if len(evidence) != 1 {
		t.Fatalf("len = %d, want 1", len(evidence))
	}
	if evidence[0].RelationshipType != RelProvisionsDependencyFor {
		t.Fatalf("type = %q, want %q", evidence[0].RelationshipType, RelProvisionsDependencyFor)
	}
}

func TestDiscoverTerraformGitHubEvidence(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-infra",
			Payload: map[string]any{
				"artifact_type": "terraform",
				"relative_path": "modules/iam.tf",
				"content":       `source = "github.com/myorg/api-gateway.git"`,
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-api-gw", Aliases: []string{"api-gateway"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)

	if len(evidence) != 1 {
		t.Fatalf("len = %d, want 1", len(evidence))
	}
	if evidence[0].EvidenceKind != EvidenceKindTerraformGitHubRepo {
		t.Errorf("kind = %q", evidence[0].EvidenceKind)
	}
}

func TestDiscoverTerraformRuntimeServiceModuleEvidence(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-infra",
			Payload: map[string]any{
				"artifact_type": "terraform_hcl",
				"relative_path": "shared/resources.tf",
				"content": `module "api_service" {
  source = "registry.example.com/platform/ecs-application/aws"

  name = "payments-service"
}`,
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	if !hasEvidenceKind(evidence, EvidenceKind("TERRAFORM_ECS_SERVICE")) {
		t.Fatal("missing TERRAFORM_ECS_SERVICE evidence")
	}
	if !hasRelationshipType(evidence, RelProvisionsDependencyFor) {
		t.Fatalf("missing %q relationship evidence", RelProvisionsDependencyFor)
	}
}

func TestDiscoverTerraformIAMSSMConfigReadEvidence(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-terraform-stack",
			Payload: map[string]any{
				"artifact_type": "terraform_hcl",
				"relative_path": "shared/resources.tf",
				"content": `data "aws_iam_policy_document" "consumer" {
  statement {
    sid = "AllowReadSSMParameters"
    actions = [
      "ssm:GetParameter",
      "ssm:GetParameters",
      "ssm:GetParametersByPath",
    ]
    resources = [
      "arn:aws:ssm:us-east-1:123456789012:parameter/configd/payments-service/client/*",
    ]
  }
}`,
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)

	var configRead *EvidenceFact
	for i := range evidence {
		item := evidence[i]
		if item.TargetRepoID == "repo-payments" && item.RelationshipType == RelReadsConfigFrom {
			configRead = &item
		}
		if item.TargetRepoID == "repo-payments" &&
			item.EvidenceKind == EvidenceKindTerraformConfigPath &&
			item.RelationshipType == RelProvisionsDependencyFor {
			t.Fatalf("IAM SSM config read must not emit provisioning evidence: %#v", item)
		}
	}
	if configRead == nil {
		t.Fatalf("missing %q evidence in %#v", RelReadsConfigFrom, evidence)
	}
	if configRead.EvidenceKind != EvidenceKindTerraformIAMPermission {
		t.Fatalf("kind = %q, want %q", configRead.EvidenceKind, EvidenceKindTerraformIAMPermission)
	}
	if got, want := configRead.Details["permission_family"], "ssm_config_read"; got != want {
		t.Fatalf("permission_family = %#v, want %#v", got, want)
	}
	if got, want := configRead.Details["matched_resource"], "/configd/payments-service/client/*"; got != want {
		t.Fatalf("matched_resource = %#v, want %#v", got, want)
	}
}

func TestDiscoverTerraformConfigPathWithoutIAMReadStillProvisions(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-terraform-stack",
			Payload: map[string]any{
				"artifact_type": "terraform_hcl",
				"relative_path": "shared/resources.tf",
				"content":       `config_path = "/configd/payments-service/runtime.yaml"`,
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)

	if !hasEvidenceKind(evidence, EvidenceKindTerraformConfigPath) {
		t.Fatal("missing Terraform config path evidence")
	}
	if !hasRelationshipType(evidence, RelProvisionsDependencyFor) {
		t.Fatalf("missing %q relationship evidence", RelProvisionsDependencyFor)
	}
	if hasRelationshipType(evidence, RelReadsConfigFrom) {
		t.Fatalf("unexpected %q relationship evidence for a non-IAM config path", RelReadsConfigFrom)
	}
}

func TestDiscoverTerraformModuleSourceEvidence(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-stack",
			Payload: map[string]any{
				"artifact_type": "terraform_hcl",
				"relative_path": "main.tf",
				"content": `module "service" {
  source = "git::https://github.com/example/terraform-modules-shared.git//modules/edge-service?ref=v1.2.3"
}`,
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-shared-modules", Aliases: []string{"terraform-modules-shared"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	if !hasEvidenceKind(evidence, EvidenceKindTerraformModuleSource) {
		t.Fatal("missing TERRAFORM_MODULE_SOURCE evidence")
	}
	if !hasRelationshipType(evidence, RelUsesModule) {
		t.Fatalf("missing %q relationship evidence", RelUsesModule)
	}
}

func TestDiscoverTerragruntModuleSourceEvidence(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-live",
			Payload: map[string]any{
				"artifact_type": "terragrunt",
				"relative_path": "env/dev/terragrunt.hcl",
				"content": `terraform {
  source = "git::git@github.com:example-org/terraform-module-eks.git//wrapper?ref=${feature.version.value}"
}`,
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-terraform-module-eks", Aliases: []string{"terraform-module-eks"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	if !hasEvidenceKind(evidence, EvidenceKindTerraformModuleSource) {
		t.Fatal("missing TERRAFORM_MODULE_SOURCE evidence")
	}
	if !hasRelationshipType(evidence, RelUsesModule) {
		t.Fatalf("missing %q relationship evidence", RelUsesModule)
	}
}

func TestDiscoverTerraformRegistryModuleSourceDoesNotCreateRepoEdge(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-live",
			Payload: map[string]any{
				"artifact_type": "terragrunt",
				"relative_path": "env/dev/terragrunt.hcl",
				"content": `terraform {
  source = "tfr:///terraform-aws-modules/eks/aws?version=19.0.0"
}`,
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-local-eks", Aliases: []string{"eks"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	if hasRelationshipType(evidence, RelUsesModule) {
		t.Fatalf("unexpected %q relationship evidence for registry module source", RelUsesModule)
	}
}

func TestDiscoverHelmChartEvidence(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-deploy",
			Payload: map[string]any{
				"artifact_type": "helm",
				"relative_path": "charts/app/Chart.yaml",
				"content":       "name: my-app\nrepository: payments-service\n",
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)

	if len(evidence) != 1 {
		t.Fatalf("len = %d, want 1", len(evidence))
	}
	if evidence[0].EvidenceKind != EvidenceKindHelmChart {
		t.Errorf("kind = %q", evidence[0].EvidenceKind)
	}
	if evidence[0].RelationshipType != RelDeploysFrom {
		t.Errorf("type = %q", evidence[0].RelationshipType)
	}
}

func TestDiscoverHelmValuesEvidence(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-deploy",
			Payload: map[string]any{
				"relative_path": "charts/app/values.yaml",
				"content":       "image:\n  repository: payments-service\n",
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)

	if len(evidence) != 1 {
		t.Fatalf("len = %d, want 1", len(evidence))
	}
	if evidence[0].EvidenceKind != EvidenceKindHelmValues {
		t.Errorf("kind = %q", evidence[0].EvidenceKind)
	}
}

func TestDiscoverStructuredHelmEvidenceIncludesNormalizedFirstPartyDetails(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-deploy",
			Payload: map[string]any{
				"artifact_type": "helm",
				"relative_path": "charts/app/Chart.yaml",
				"parsed_file_data": map[string]any{
					"helm_charts": []any{
						map[string]any{
							"name":                    "payments",
							"dependencies":            "payments-service",
							"dependency_repositories": "file://../payments-service",
						},
					},
				},
			},
		},
		{
			ScopeID: "repo-deploy",
			Payload: map[string]any{
				"artifact_type": "helm",
				"relative_path": "charts/app/values.yaml",
				"parsed_file_data": map[string]any{
					"helm_values": []any{
						map[string]any{
							"name":               "values",
							"image_repositories": "ghcr.io/example/payments-service:1.2.3",
						},
					},
				},
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	if len(evidence) != 3 {
		t.Fatalf("len(evidence) = %d, want 3", len(evidence))
	}

	var sawDependencyRepository bool
	var sawImageRepository bool
	for _, item := range evidence {
		switch item.Details["first_party_ref_kind"] {
		case "helm_dependency_repository":
			sawDependencyRepository = true
			if got, want := item.Details["first_party_ref_normalized"], "../payments-service"; got != want {
				t.Fatalf("dependency repository normalized = %#v, want %#v", got, want)
			}
			if got, want := item.Details["helm_chart_name"], "payments"; got != want {
				t.Fatalf("helm_chart_name = %#v, want %#v", got, want)
			}
		case "helm_image_repository":
			sawImageRepository = true
			if got, want := item.Details["first_party_ref_normalized"], "ghcr.io/example/payments-service"; got != want {
				t.Fatalf("image repository normalized = %#v, want %#v", got, want)
			}
			if got, want := item.Details["helm_values_name"], "values"; got != want {
				t.Fatalf("helm_values_name = %#v, want %#v", got, want)
			}
		}
	}
	if !sawDependencyRepository {
		t.Fatal("missing normalized Helm dependency repository evidence details")
	}
	if !sawImageRepository {
		t.Fatal("missing normalized Helm image repository evidence details")
	}
}

func TestDiscoverKustomizeEvidence(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-deploy",
			Payload: map[string]any{
				"relative_path": "overlays/prod/kustomization.yaml",
				"content":       "resources:\n  - ../../base\nnamePrefix: payments-service\n",
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)

	if len(evidence) < 1 {
		t.Fatalf("len = %d, want >= 1", len(evidence))
	}
	found := false
	for _, e := range evidence {
		if e.EvidenceKind == EvidenceKindKustomizeResource {
			found = true
		}
	}
	if !found {
		t.Error("expected KUSTOMIZE_RESOURCE_REFERENCE evidence")
	}
}

func TestDiscoverKustomizeEvidenceTypedKinds(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-deploy",
			Payload: map[string]any{
				"relative_path": "overlays/prod/kustomization.yaml",
				"content": `resources:
  - ../../payments-service/base
helmCharts:
  - name: payments-service
    repo: https://github.com/myorg/payments-service
images:
  - name: payments-service
    newName: ghcr.io/myorg/payments-service
`,
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)

	if !hasEvidenceKind(evidence, EvidenceKindKustomizeResource) {
		t.Fatal("missing KUSTOMIZE_RESOURCE_REFERENCE evidence")
	}
	if !hasEvidenceKind(evidence, EvidenceKindKustomizeHelmChart) {
		t.Fatal("missing KUSTOMIZE_HELM_CHART_REFERENCE evidence")
	}
	if !hasEvidenceKind(evidence, EvidenceKindKustomizeImage) {
		t.Fatal("missing KUSTOMIZE_IMAGE_REFERENCE evidence")
	}
}

func TestDiscoverArgoCDEvidence(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-gitops",
			Payload: map[string]any{
				"artifact_type": "argocd",
				"relative_path": "apps/payments.yaml",
				"content": `apiVersion: argoproj.io/v1alpha1
kind: Application
spec:
  source:
    repoURL: 'https://github.com/myorg/payments-service.git'
    targetRevision: HEAD
`,
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)

	if len(evidence) != 1 {
		t.Fatalf("len = %d, want 1", len(evidence))
	}
	if evidence[0].EvidenceKind != EvidenceKindArgoCDAppSource {
		t.Errorf("kind = %q", evidence[0].EvidenceKind)
	}
	if evidence[0].Confidence != 0.95 {
		t.Errorf("confidence = %f", evidence[0].Confidence)
	}
}

func TestDiscoverStructuredArgoCDEvidenceIncludesNormalizedFirstPartyDetails(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-gitops",
			Payload: map[string]any{
				"artifact_type": "argocd",
				"relative_path": "apps/payments.yaml",
				"parsed_file_data": map[string]any{
					"argocd_applications": []any{
						map[string]any{
							"name":            "payments",
							"source_repo":     "https://github.com/myorg/payments-service.git",
							"source_path":     "deploy/overlays/prod",
							"source_root":     "deploy/",
							"source_revision": "main",
							"dest_name":       "prod-cluster",
						},
					},
					"argocd_applicationsets": []any{
						map[string]any{
							"name":                   "payments-appset",
							"generator_source_repos": "https://github.com/myorg/payments-config.git",
							"generator_source_paths": "argocd/payments/*/config.yaml",
							"source_roots":           "argocd/payments/",
							"template_source_repos":  "https://github.com/myorg/payments-service.git",
							"template_source_paths":  "deploy/overlays/prod",
							"template_source_roots":  "deploy/",
							"dest_name":              "prod-cluster",
						},
					},
				},
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-config", Aliases: []string{"payments-config"}},
		{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	if len(evidence) != 4 {
		t.Fatalf("len(evidence) = %d, want 4", len(evidence))
	}

	var sawAppSource bool
	var sawDiscovery bool
	var sawDeploySource bool
	for _, item := range evidence {
		switch item.EvidenceKind {
		case EvidenceKindArgoCDAppSource:
			sawAppSource = true
			if got, want := item.Details["first_party_ref_kind"], "argocd_application_source"; got != want {
				t.Fatalf("application ref kind = %#v, want %#v", got, want)
			}
			if got, want := item.Details["first_party_ref_path"], "deploy/overlays/prod"; got != want {
				t.Fatalf("application ref path = %#v, want %#v", got, want)
			}
			if got, want := item.Details["first_party_ref_root"], "deploy/"; got != want {
				t.Fatalf("application ref root = %#v, want %#v", got, want)
			}
		case EvidenceKindArgoCDApplicationSetDiscovery:
			sawDiscovery = true
			if got, want := item.Details["first_party_ref_kind"], "argocd_applicationset_discovery"; got != want {
				t.Fatalf("appset discovery ref kind = %#v, want %#v", got, want)
			}
			if got, want := item.Details["first_party_ref_root"], "argocd/payments/"; got != want {
				t.Fatalf("appset discovery ref root = %#v, want %#v", got, want)
			}
		case EvidenceKindArgoCDApplicationSetDeploySource:
			sawDeploySource = true
			if got, want := item.Details["first_party_ref_kind"], "argocd_applicationset_template_source"; got != want {
				t.Fatalf("appset deploy ref kind = %#v, want %#v", got, want)
			}
			if got, want := item.Details["first_party_ref_root"], "deploy/"; got != want {
				t.Fatalf("appset deploy ref root = %#v, want %#v", got, want)
			}
		}
	}

	if !sawAppSource {
		t.Fatal("missing structured ArgoCD application evidence")
	}
	if !sawDiscovery {
		t.Fatal("missing structured ArgoCD ApplicationSet discovery evidence")
	}
	if !sawDeploySource {
		t.Fatal("missing structured ArgoCD ApplicationSet deploy evidence")
	}
}

func TestDiscoverStructuredArgoCDEvidenceMatchesApplicationMultiSourceRepos(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-gitops",
			Payload: map[string]any{
				"artifact_type": "argocd",
				"relative_path": "apps/multi-source.yaml",
				"parsed_file_data": map[string]any{
					"argocd_applications": []any{
						map[string]any{
							"name":             "multi-source",
							"source_repos":     "https://github.com/myorg/helm-charts.git,https://github.com/myorg/config-repo.git",
							"source_paths":     "charts/comprehensive-app,",
							"source_revisions": "main,main",
							"source_roots":     "charts/comprehensive-app/,",
						},
					},
				},
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-charts", Aliases: []string{"helm-charts"}},
		{RepoID: "repo-config", Aliases: []string{"config-repo"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	if len(evidence) != 2 {
		t.Fatalf("len(evidence) = %d, want 2: %#v", len(evidence), evidence)
	}

	targets := map[string]bool{}
	for _, item := range evidence {
		if item.EvidenceKind != EvidenceKindArgoCDAppSource {
			t.Fatalf("EvidenceKind = %q, want %q", item.EvidenceKind, EvidenceKindArgoCDAppSource)
		}
		targets[item.TargetRepoID] = true
		switch item.TargetRepoID {
		case "repo-charts":
			if got, want := item.Details["first_party_ref_path"], "charts/comprehensive-app"; got != want {
				t.Fatalf("repo-charts first_party_ref_path = %#v, want %#v", got, want)
			}
			if got, want := item.Details["first_party_ref_root"], "charts/comprehensive-app/"; got != want {
				t.Fatalf("repo-charts first_party_ref_root = %#v, want %#v", got, want)
			}
		case "repo-config":
			if _, ok := item.Details["first_party_ref_path"]; ok {
				t.Fatalf("repo-config must not inherit first_party_ref_path: %#v", item.Details)
			}
			if _, ok := item.Details["first_party_ref_root"]; ok {
				t.Fatalf("repo-config must not inherit first_party_ref_root: %#v", item.Details)
			}
		}
	}
	for _, want := range []string{"repo-charts", "repo-config"} {
		if !targets[want] {
			t.Fatalf("missing ArgoCD Application multi-source evidence for %s in %#v", want, evidence)
		}
	}
}

func TestDiscoverGitHubActionsReusableWorkflowEvidence(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-service",
			Payload: map[string]any{
				"artifact_type": "github_actions_workflow",
				"relative_path": ".github/workflows/deploy.yaml",
				"content": `name: Deploy
jobs:
  deploy:
    uses: myorg/deployment-helm/.github/workflows/deploy.yaml@main
`,
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-deploy", Aliases: []string{"deployment-helm"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	if len(evidence) != 1 {
		t.Fatalf("len = %d, want 1", len(evidence))
	}
	if evidence[0].EvidenceKind != EvidenceKindGitHubActionsReusableWorkflow {
		t.Fatalf("kind = %q, want %q", evidence[0].EvidenceKind, EvidenceKindGitHubActionsReusableWorkflow)
	}
	if evidence[0].RelationshipType != RelDeploysFrom {
		t.Fatalf("type = %q, want %q", evidence[0].RelationshipType, RelDeploysFrom)
	}
	if evidence[0].TargetRepoID != "repo-deploy" {
		t.Fatalf("target = %q, want %q", evidence[0].TargetRepoID, "repo-deploy")
	}
	if got, want := evidence[0].Details["first_party_ref_kind"], "github_actions_reusable_workflow"; got != want {
		t.Fatalf("first_party_ref_kind = %#v, want %#v", got, want)
	}
	if got, want := evidence[0].Details["first_party_ref_path"], ".github/workflows/deploy.yaml"; got != want {
		t.Fatalf("first_party_ref_path = %#v, want %#v", got, want)
	}
	if got, want := evidence[0].Details["first_party_ref_version"], "main"; got != want {
		t.Fatalf("first_party_ref_version = %#v, want %#v", got, want)
	}
}

func TestDiscoverGitHubActionsCheckoutRepositoryEvidence(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-service",
			Payload: map[string]any{
				"artifact_type": "github_actions_workflow",
				"relative_path": ".github/workflows/deploy.yaml",
				"content": `name: Deploy
jobs:
  deploy:
    steps:
      - uses: actions/checkout@v4
        with:
          repository: myorg/deployment-kustomize
`,
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-kustomize", Aliases: []string{"deployment-kustomize"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	if len(evidence) != 1 {
		t.Fatalf("len = %d, want 1", len(evidence))
	}
	if evidence[0].EvidenceKind != EvidenceKindGitHubActionsCheckoutRepository {
		t.Fatalf("kind = %q, want %q", evidence[0].EvidenceKind, EvidenceKindGitHubActionsCheckoutRepository)
	}
	if evidence[0].RelationshipType != RelDiscoversConfigIn {
		t.Fatalf("type = %q, want %q", evidence[0].RelationshipType, RelDiscoversConfigIn)
	}
	if evidence[0].TargetRepoID != "repo-kustomize" {
		t.Fatalf("target = %q, want %q", evidence[0].TargetRepoID, "repo-kustomize")
	}
}

func TestDiscoverGitHubActionsAutomationRepositoryEvidence(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-service",
			Payload: map[string]any{
				"artifact_type": "github_actions_workflow",
				"relative_path": ".github/workflows/pr-command-dispatch.yml",
				"content": `name: 'Pull Request: Command Dispatch'
jobs:
  dispatch-command:
    uses: example-org/shared-automation/.github/workflows/node-api-command-processing.yml@v2
    with:
      automation-repo: 'example-org/shared-automation'
      automation-repo-ref: 'refs/tags/v2'
`,
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-automation", Aliases: []string{"shared-automation", "example-org/shared-automation"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	if len(evidence) != 2 {
		t.Fatalf("len = %d, want 2", len(evidence))
	}
	if !hasEvidenceKind(evidence, EvidenceKindGitHubActionsReusableWorkflow) {
		t.Fatal("missing reusable workflow evidence")
	}
	if !hasEvidenceKind(evidence, EvidenceKindGitHubActionsWorkflowInputRepository) {
		t.Fatal("missing automation repository evidence")
	}
	if !hasRelationshipType(evidence, RelDiscoversConfigIn) {
		t.Fatalf("missing %q evidence", RelDiscoversConfigIn)
	}
}

func TestDiscoverGitHubActionsWorkflowInputRepositoryEvidence(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-service",
			Payload: map[string]any{
				"artifact_type": "github_actions_workflow",
				"relative_path": ".github/workflows/pr-command-dispatch.yml",
				"content": `name: 'Pull Request: Command Dispatch'
jobs:
  dispatch-command:
    uses: example-org/shared-automation/.github/workflows/node-api-command-processing.yml@v2
    with:
      workflow_input_repository: 'example-org/shared-automation'
`,
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-automation", Aliases: []string{"shared-automation", "example-org/shared-automation"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	if len(evidence) != 2 {
		t.Fatalf("len = %d, want 2", len(evidence))
	}
	if !hasEvidenceKind(evidence, EvidenceKindGitHubActionsReusableWorkflow) {
		t.Fatal("missing reusable workflow evidence")
	}
	if !hasEvidenceKind(evidence, EvidenceKindGitHubActionsWorkflowInputRepository) {
		t.Fatal("missing workflow input repository evidence")
	}
	if !hasRelationshipType(evidence, RelDiscoversConfigIn) {
		t.Fatalf("missing %q evidence", RelDiscoversConfigIn)
	}
}

func TestDiscoverGitHubActionsWorkflowInputRepositoriesEvidence(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-app",
			Payload: map[string]any{
				"artifact_type": "github_actions_workflow",
				"relative_path": ".github/workflows/deploy.yaml",
				"content": `jobs:
  deploy:
    uses: example-org/shared-automation/.github/workflows/deploy.yaml@main
    with:
      workflow_input_repositories:
        - example-org/automation-fallback
        - example-org/shared-automation
`,
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-automation-fallback", Aliases: []string{"example-org/automation-fallback", "automation-fallback"}},
		{RepoID: "repo-core-automation", Aliases: []string{"example-org/shared-automation", "shared-automation"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	if len(evidence) != 3 {
		t.Fatalf("len(evidence) = %d, want 3", len(evidence))
	}
	if !hasRelationshipType(evidence, RelDiscoversConfigIn) {
		t.Fatalf("missing %q evidence", RelDiscoversConfigIn)
	}

	targetRepoIDs := make(map[string]struct{}, len(evidence))
	for _, item := range evidence {
		targetRepoIDs[item.TargetRepoID] = struct{}{}
	}
	for _, want := range []string{"repo-automation-fallback", "repo-core-automation"} {
		if _, ok := targetRepoIDs[want]; !ok {
			t.Fatalf("missing target repo %q in %#v", want, targetRepoIDs)
		}
	}
}

func TestDiscoverGitHubActionsActionRepositoryEvidence(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-service",
			Payload: map[string]any{
				"artifact_type": "github_actions_workflow",
				"relative_path": ".github/workflows/update-providers.yml",
				"content": `name: Update providers
jobs:
  update:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: hashicorp/setup-terraform@v3
      - uses: peter-evans/create-pull-request@v5
      - uses: ./.github/actions/local-helper
`,
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-hashicorp-setup-terraform", Aliases: []string{"hashicorp/setup-terraform", "setup-terraform"}},
		{RepoID: "repo-peter-evans-create-pr", Aliases: []string{"peter-evans/create-pull-request", "create-pull-request"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	if len(evidence) != 2 {
		t.Fatalf("len = %d, want 2", len(evidence))
	}
	if !hasEvidenceKind(evidence, EvidenceKindGitHubActionsActionRepository) {
		t.Fatal("missing GitHub Actions action repository evidence")
	}
	if !hasRelationshipType(evidence, RelDependsOn) {
		t.Fatalf("missing %q evidence", RelDependsOn)
	}
	for _, item := range evidence {
		if item.EvidenceKind != EvidenceKindGitHubActionsActionRepository {
			continue
		}
		if got, want := item.Details["first_party_ref_kind"], "github_actions_action"; got != want {
			t.Fatalf("first_party_ref_kind = %#v, want %#v", got, want)
		}
		if got, ok := item.Details["first_party_ref_version"].(string); !ok || got == "" {
			t.Fatalf("first_party_ref_version = %#v, want non-empty string", item.Details["first_party_ref_version"])
		}
	}
}

func TestDiscoverGitHubActionsLocalReusableWorkflowEvidence(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-service",
			Payload: map[string]any{
				"artifact_type": "github_actions_workflow",
				"relative_path": ".github/workflows/deploy-local.yml",
				"content": `name: Deploy local
jobs:
  deploy:
    uses: ./.github/workflows/release.yaml
  verify:
    uses: .github/workflows/verify.yaml@main
`,
			},
		},
	}

	evidence := DiscoverEvidence(envelopes, nil)
	if len(evidence) != 2 {
		t.Fatalf("len = %d, want 2", len(evidence))
	}
	if !hasEvidenceKind(evidence, EvidenceKindGitHubActionsLocalReusableWorkflow) {
		t.Fatal("missing local reusable workflow evidence")
	}
	if !hasRelationshipType(evidence, RelDeploysFrom) {
		t.Fatalf("missing %q evidence", RelDeploysFrom)
	}
	for _, item := range evidence {
		if item.EvidenceKind != EvidenceKindGitHubActionsLocalReusableWorkflow {
			continue
		}
		if item.SourceRepoID != "repo-service" || item.TargetRepoID != "repo-service" {
			t.Fatalf("same-repo workflow evidence = %#v, want repo-service self edge", item)
		}
	}
}

func TestDiscoverDockerComposeEvidence(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-deploy",
			Payload: map[string]any{
				"artifact_type": "docker_compose",
				"relative_path": "docker-compose.yaml",
				"content": `services:
  payments:
    build:
      context: ../payments-service
    depends_on:
      - checkout-service
  checkout:
    image: ghcr.io/myorg/checkout-service:latest
`,
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
		{RepoID: "repo-checkout", Aliases: []string{"checkout-service"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	if len(evidence) != 3 {
		t.Fatalf("len = %d, want 3", len(evidence))
	}
	if !hasEvidenceKind(evidence, EvidenceKindDockerComposeBuildContext) {
		t.Fatal("missing Docker Compose build-context evidence")
	}
	if !hasEvidenceKind(evidence, EvidenceKindDockerComposeImage) {
		t.Fatal("missing Docker Compose image evidence")
	}
	if !hasEvidenceKind(evidence, EvidenceKindDockerComposeDependsOn) {
		t.Fatal("missing Docker Compose depends_on evidence")
	}
}

func TestDiscoverJenkinsSharedLibraryEvidence(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-service",
			Payload: map[string]any{
				"relative_path": "Jenkinsfile",
				"content":       "@Library('pipelines') _\n",
				"parsed_file_data": map[string]any{
					"shared_libraries": []any{"pipelines"},
					"pipeline_calls":   []any{"deployService"},
					"entry_points":     []any{"dist/api.js"},
					"shell_commands":   []any{"ansible-playbook playbooks/deploy.yml -i inventories/prod.yml"},
					"ansible_playbook_hints": []any{
						map[string]any{
							"playbook":  "playbooks/deploy.yml",
							"inventory": "inventories/prod.yml",
							"command":   "ansible-playbook playbooks/deploy.yml -i inventories/prod.yml",
						},
					},
					"use_configd":    true,
					"has_pre_deploy": true,
				},
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-pipelines", Aliases: []string{"pipelines"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	if len(evidence) != 1 {
		t.Fatalf("len = %d, want 1", len(evidence))
	}
	if evidence[0].EvidenceKind != EvidenceKindJenkinsSharedLibrary {
		t.Fatalf("kind = %q, want %q", evidence[0].EvidenceKind, EvidenceKindJenkinsSharedLibrary)
	}
	if evidence[0].RelationshipType != RelDiscoversConfigIn {
		t.Fatalf("type = %q, want %q", evidence[0].RelationshipType, RelDiscoversConfigIn)
	}
	if evidence[0].TargetRepoID != "repo-pipelines" {
		t.Fatalf("target = %q, want %q", evidence[0].TargetRepoID, "repo-pipelines")
	}
	if got, want := evidence[0].Details["pipeline_calls"], []string{"deployService"}; !stringSlicesEqual(got, want) {
		t.Fatalf("pipeline_calls = %#v, want %#v", got, want)
	}
	if got, want := evidence[0].Details["entry_points"], []string{"dist/api.js"}; !stringSlicesEqual(got, want) {
		t.Fatalf("entry_points = %#v, want %#v", got, want)
	}
	if got, want := evidence[0].Details["shell_commands"], []string{"ansible-playbook playbooks/deploy.yml -i inventories/prod.yml"}; !stringSlicesEqual(got, want) {
		t.Fatalf("shell_commands = %#v, want %#v", got, want)
	}
	hints, ok := evidence[0].Details["ansible_playbook_hints"].([]map[string]any)
	if !ok || len(hints) != 1 {
		t.Fatalf("ansible_playbook_hints = %#v, want 1 hint", evidence[0].Details["ansible_playbook_hints"])
	}
	if got, want := hints[0]["playbook"], "playbooks/deploy.yml"; got != want {
		t.Fatalf("ansible_playbook_hints[0].playbook = %#v, want %#v", got, want)
	}
	if got, want := evidence[0].Details["use_configd"], true; got != want {
		t.Fatalf("use_configd = %#v, want %#v", got, want)
	}
	if got, want := evidence[0].Details["has_pre_deploy"], true; got != want {
		t.Fatalf("has_pre_deploy = %#v, want %#v", got, want)
	}
}

func TestDiscoverJenkinsSharedLibraryEvidenceTrimsVersionSuffix(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-service",
			Payload: map[string]any{
				"relative_path": "Jenkinsfile",
				"content":       "@Library('pipelines@v2') _\n",
				"parsed_file_data": map[string]any{
					"shared_libraries": []any{"pipelines@v2", "pipelines"},
				},
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-pipelines", Aliases: []string{"pipelines"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	if len(evidence) != 1 {
		t.Fatalf("len = %d, want 1", len(evidence))
	}
	if evidence[0].EvidenceKind != EvidenceKindJenkinsSharedLibrary {
		t.Fatalf("kind = %q, want %q", evidence[0].EvidenceKind, EvidenceKindJenkinsSharedLibrary)
	}
	if got, want := evidence[0].Details["shared_library"], "pipelines"; got != want {
		t.Fatalf("shared_library = %#v, want %#v", got, want)
	}
	if got, want := evidence[0].Details["first_party_ref_kind"], "jenkins_shared_library"; got != want {
		t.Fatalf("first_party_ref_kind = %#v, want %#v", got, want)
	}
}

func TestDiscoverJenkinsGitHubRepositoryEvidence(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-service",
			Payload: map[string]any{
				"relative_path": "Jenkinsfile",
				"content": `node {
  git url: "https://github.com/example-org/terraform-modules-aws.git"
}`,
				"parsed_file_data": map[string]any{
					"pipeline_calls": []any{"deployService"},
					"entry_points":   []any{"dist/api.js"},
					"shell_commands": []any{
						`git clone https://github.com/example-org/terraform-modules-aws.git`,
					},
					"use_configd": true,
				},
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-modules", Aliases: []string{"terraform-modules-aws"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	if len(evidence) != 1 {
		t.Fatalf("len = %d, want 1", len(evidence))
	}
	if evidence[0].EvidenceKind != EvidenceKindJenkinsGitHubRepository {
		t.Fatalf("kind = %q, want %q", evidence[0].EvidenceKind, EvidenceKindJenkinsGitHubRepository)
	}
	if evidence[0].RelationshipType != RelDiscoversConfigIn {
		t.Fatalf("type = %q, want %q", evidence[0].RelationshipType, RelDiscoversConfigIn)
	}
	if evidence[0].TargetRepoID != "repo-modules" {
		t.Fatalf("target = %q, want %q", evidence[0].TargetRepoID, "repo-modules")
	}
	if got, want := evidence[0].Details["pipeline_calls"], []string{"deployService"}; !stringSlicesEqual(got, want) {
		t.Fatalf("pipeline_calls = %#v, want %#v", got, want)
	}
	if got, want := evidence[0].Details["entry_points"], []string{"dist/api.js"}; !stringSlicesEqual(got, want) {
		t.Fatalf("entry_points = %#v, want %#v", got, want)
	}
	if got, want := evidence[0].Details["use_configd"], true; got != want {
		t.Fatalf("use_configd = %#v, want %#v", got, want)
	}
	if got, want := evidence[0].Details["first_party_ref_kind"], "jenkins_repository"; got != want {
		t.Fatalf("first_party_ref_kind = %#v, want %#v", got, want)
	}
	if got, want := evidence[0].Details["first_party_ref_name"], "terraform-modules-aws"; got != want {
		t.Fatalf("first_party_ref_name = %#v, want %#v", got, want)
	}
}

func TestDiscoverAnsibleRoleReferenceEvidenceIncludesResolvedMetadata(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-ops",
			Payload: map[string]any{
				"artifact_type": "ansible_playbook",
				"relative_path": "playbooks/site.yml",
				"content": `- hosts: all
  roles:
    - role: payments-service
      src: git+https://github.com/myorg/payments-service.git
`,
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	if len(evidence) != 1 {
		t.Fatalf("len = %d, want 1", len(evidence))
	}
	if evidence[0].EvidenceKind != EvidenceKindAnsibleRoleReference {
		t.Fatalf("kind = %q, want %q", evidence[0].EvidenceKind, EvidenceKindAnsibleRoleReference)
	}
	if evidence[0].RelationshipType != RelDependsOn {
		t.Fatalf("type = %q, want %q", evidence[0].RelationshipType, RelDependsOn)
	}
	if evidence[0].TargetRepoID != "repo-payments" {
		t.Fatalf("target = %q, want %q", evidence[0].TargetRepoID, "repo-payments")
	}
	if got, want := evidence[0].Details["role_name"], "payments-service"; got != want {
		t.Fatalf("role_name = %#v, want %#v", got, want)
	}
	if got, want := evidence[0].Details["reference_key"], "roles"; got != want {
		t.Fatalf("reference_key = %#v, want %#v", got, want)
	}
	if got, want := evidence[0].Details["source_ref"], "git+https://github.com/myorg/payments-service.git"; got != want {
		t.Fatalf("source_ref = %#v, want %#v", got, want)
	}
	if got, want := evidence[0].Details["first_party_ref_kind"], "ansible_role_source"; got != want {
		t.Fatalf("first_party_ref_kind = %#v, want %#v", got, want)
	}
	if got, want := evidence[0].Details["first_party_ref_name"], "payments-service"; got != want {
		t.Fatalf("first_party_ref_name = %#v, want %#v", got, want)
	}
}

func TestDiscoverDockerfileSourceLabelEvidence(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-runtime",
			Payload: map[string]any{
				"artifact_type": "dockerfile",
				"relative_path": "Dockerfile",
				"content":       `FROM alpine:3.20`,
				"parsed_file_data": map[string]any{
					"dockerfile_labels": []any{
						map[string]any{
							"name":  "org.opencontainers.image.source",
							"value": "https://github.com/acme/payments-service.git",
						},
					},
				},
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	if len(evidence) != 1 {
		t.Fatalf("len = %d, want 1", len(evidence))
	}
	if got, want := evidence[0].EvidenceKind, EvidenceKindDockerfileSourceLabel; got != want {
		t.Fatalf("kind = %q, want %q", got, want)
	}
	if got, want := evidence[0].RelationshipType, RelDeploysFrom; got != want {
		t.Fatalf("type = %q, want %q", got, want)
	}
	if got, want := evidence[0].TargetRepoID, "repo-payments"; got != want {
		t.Fatalf("target = %q, want %q", got, want)
	}
	if got, want := evidence[0].Details["source_label"], "org.opencontainers.image.source"; got != want {
		t.Fatalf("source_label = %#v, want %#v", got, want)
	}
}

func TestDiscoverDockerfileBaseImageDoesNotEmitRepoEvidence(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-runtime",
			Payload: map[string]any{
				"artifact_type":    "dockerfile",
				"relative_path":    "Dockerfile",
				"content":          `FROM ghcr.io/acme/payments-service:latest`,
				"parsed_file_data": map[string]any{},
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	if len(evidence) != 0 {
		t.Fatalf("len = %d, want 0", len(evidence))
	}
}

func TestDiscoverDockerfileStageAliasDoesNotEmitRepoEvidence(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-runtime",
			Payload: map[string]any{
				"artifact_type": "dockerfile",
				"relative_path": "Dockerfile",
				"content": `FROM golang:1.24 AS builder
COPY --from=builder /out/app /app
FROM alpine:3.20 AS runtime
COPY --from=builder /out/app /app
`,
				"parsed_file_data": map[string]any{
					"dockerfile_stages": []any{
						map[string]any{
							"name":        "builder",
							"base_image":  "golang",
							"copies_from": "",
						},
						map[string]any{
							"name":        "runtime",
							"base_image":  "alpine",
							"copies_from": "builder",
						},
					},
				},
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	if len(evidence) != 0 {
		t.Fatalf("len = %d, want 0 for Dockerfile stage aliases", len(evidence))
	}
}

func stringSlicesEqual(got any, want []string) bool {
	values, ok := got.([]string)
	if !ok {
		return false
	}
	if len(values) != len(want) {
		return false
	}
	for index := range values {
		if values[index] != want[index] {
			return false
		}
	}
	return true
}

func TestIsTerraformArtifactIncludesVariableFiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		artifactType string
		filePath     string
		want         bool
	}{
		{
			name:     "tfvars suffix",
			filePath: "env/prod/terraform.tfvars",
			want:     true,
		},
		{
			name:     "tfvars json suffix",
			filePath: "env/prod/terraform.tfvars.json",
			want:     true,
		},
		{
			name:         "terraform hcl artifact type",
			artifactType: "terraform_hcl",
			filePath:     "env/prod/terraform.tfvars.json",
			want:         true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := isTerraformArtifact(tt.artifactType, tt.filePath); got != tt.want {
				t.Fatalf("isTerraformArtifact(%q, %q) = %t, want %t", tt.artifactType, tt.filePath, got, tt.want)
			}
		})
	}
}

func TestDiscoverArgoCDApplicationSetEvidence(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-gitops",
			Payload: map[string]any{
				"artifact_type": "argocd",
				"relative_path": "apps/applicationset.yaml",
				"content": `apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
spec:
  generators:
    - git:
        repoURL: https://github.com/myorg/payments-config.git
        files:
          - path: argocd/*/config.yaml
  template:
    spec:
      destination:
        name: prod-cluster
      source:
        repoURL: https://github.com/myorg/payments-service.git
`,
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-config", Aliases: []string{"payments-config"}},
		{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)

	if !hasEvidenceKind(evidence, EvidenceKindArgoCDApplicationSetDiscovery) {
		t.Fatal("missing ARGOCD_APPLICATIONSET_DISCOVERY evidence")
	}
	if !hasEvidenceKind(evidence, EvidenceKindArgoCDApplicationSetDeploySource) {
		t.Fatal("missing ARGOCD_APPLICATIONSET_DEPLOY_SOURCE evidence")
	}
	if !hasRelationshipType(evidence, RelDiscoversConfigIn) {
		t.Fatal("missing DISCOVERS_CONFIG_IN relationship evidence")
	}
}

func TestDiscoverArgoCDApplicationSetEvaluatesGitFileGeneratorYAML(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-gitops",
			Payload: map[string]any{
				"artifact_type": "argocd",
				"relative_path": "applicationsets/services.yaml",
				"content": `apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
spec:
  generators:
    - git:
        repoURL: https://github.com/myorg/platform-config.git
        files:
          - path: services/**/service.yaml
  template:
    spec:
      sources:
        - repoURL: "{{ .service.repoURL }}"
          path: "{{ .service.overlayPath }}"
        - repoURL: https://charts.example.invalid
          chart: "{{ .helm.chart }}"
`,
			},
		},
		{
			ScopeID: "repo-config",
			Payload: map[string]any{
				"artifact_type": "yaml",
				"relative_path": "services/prod/payments/service.yaml",
				"content": `service:
  repoURL: https://github.com/myorg/payments-service.git
  overlayPath: deploy/overlays/prod
helm:
  chart: payments
`,
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-config", Aliases: []string{"platform-config"}},
		{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)

	discovery, ok := findEvidenceByKind(evidence, EvidenceKindArgoCDApplicationSetDiscovery)
	if !ok {
		t.Fatal("missing ApplicationSet discovery evidence")
	}
	if got, want := discovery.TargetRepoID, "repo-config"; got != want {
		t.Fatalf("discovery target = %q, want %q", got, want)
	}

	deploySource, ok := findEvidenceByKind(evidence, EvidenceKindArgoCDApplicationSetDeploySource)
	if !ok {
		t.Fatal("missing evaluated ApplicationSet deploy-source evidence")
	}
	if got, want := deploySource.SourceRepoID, "repo-payments"; got != want {
		t.Fatalf("deploy source = %q, want %q", got, want)
	}
	if got, want := deploySource.TargetRepoID, "repo-config"; got != want {
		t.Fatalf("deploy target = %q, want %q", got, want)
	}
	if got, want := deploySource.Details["deploy_repo_url"], "https://github.com/myorg/payments-service.git"; got != want {
		t.Fatalf("deploy repo url = %#v, want %#v", got, want)
	}
}

func TestDiscoverArgoCDApplicationSetSharedChartConfigDoesNotInventDeploySource(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-gitops",
			Payload: map[string]any{
				"artifact_type": "argocd",
				"relative_path": "applicationsets/platform.yaml",
				"content": `apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
spec:
  generators:
    - git:
        repoURL: https://github.com/myorg/platform-config.git
        files:
          - path: platform/*/settings.yaml
  template:
    spec:
      sources:
        - repoURL: "{{ .helm.repoURL }}"
          chart: "{{ .helm.chart }}"
        - repoURL: "{{ .git.repoURL }}"
          path: platform/{{ .environment }}
`,
			},
		},
		{
			ScopeID: "repo-config",
			Payload: map[string]any{
				"artifact_type": "yaml",
				"relative_path": "platform/prod/settings.yaml",
				"content": `environment: prod
helm:
  repoURL: https://charts.example.invalid
  chart: shared-platform
git:
  repoURL: https://github.com/myorg/platform-config.git
client:
  repository: payments-service
`,
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-config", Aliases: []string{"platform-config"}},
		{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)

	if !hasEvidenceKind(evidence, EvidenceKindArgoCDApplicationSetDiscovery) {
		t.Fatal("missing ApplicationSet discovery evidence")
	}
	if hasEvidenceKind(evidence, EvidenceKindArgoCDApplicationSetDeploySource) {
		t.Fatalf("unexpected deploy-source evidence: %#v", evidence)
	}
}

func TestDiscoverArgoCDApplicationSetDeploySourceFromConfigServiceIdentity(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-gitops",
			Payload: map[string]any{
				"artifact_type": "argocd",
				"relative_path": "applicationsets/services.yaml",
				"content": `apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
spec:
  generators:
    - git:
        repoURL: https://github.com/myorg/platform-config.git
        files:
          - path: services/*/config.yaml
  template:
    spec:
      sources:
        - repoURL: "{{ .helm.repoURL }}"
          chart: "{{ .helm.chart }}"
        - repoURL: "{{ .git.repoURL }}"
          path: "{{ .git.overlayPath }}"
`,
			},
		},
		{
			ScopeID: "repo-config",
			Payload: map[string]any{
				"artifact_type": "yaml",
				"relative_path": "services/prod/config.yaml",
				"content": `addon: payments-service
environment: prod
git:
  repoURL: https://github.com/myorg/platform-config.git
  overlayPath: services/prod
helm:
  repoURL: https://charts.example.invalid
  chart: shared-service
  releaseName: payments-service
`,
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-config", Aliases: []string{"platform-config"}},
		{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)

	deploySource, ok := findEvidenceByKind(evidence, EvidenceKindArgoCDApplicationSetDeploySource)
	if !ok {
		t.Fatalf("missing service-identity deploy-source evidence: %#v", evidence)
	}
	if got, want := deploySource.SourceRepoID, "repo-payments"; got != want {
		t.Fatalf("deploy source = %q, want %q", got, want)
	}
	if got, want := deploySource.TargetRepoID, "repo-config"; got != want {
		t.Fatalf("deploy target = %q, want %q", got, want)
	}
}

func TestDiscoverArgoCDApplicationSetUnsupportedTemplateStaysDiscoveryOnly(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-gitops",
			Payload: map[string]any{
				"artifact_type": "argocd",
				"relative_path": "applicationsets/services.yaml",
				"content": `apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
spec:
  generators:
    - git:
        repoURL: https://github.com/myorg/platform-config.git
        files:
          - path: services/*/service.yaml
  template:
    spec:
      source:
        repoURL: "{{ index .service \"repoURL\" }}"
`,
			},
		},
		{
			ScopeID: "repo-config",
			Payload: map[string]any{
				"artifact_type": "yaml",
				"relative_path": "services/payments/service.yaml",
				"content": `service:
  repoURL: https://github.com/myorg/payments-service.git
`,
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-config", Aliases: []string{"platform-config"}},
		{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)

	if !hasEvidenceKind(evidence, EvidenceKindArgoCDApplicationSetDiscovery) {
		t.Fatal("missing ApplicationSet discovery evidence")
	}
	if hasEvidenceKind(evidence, EvidenceKindArgoCDApplicationSetDeploySource) {
		t.Fatalf("unexpected deploy-source evidence: %#v", evidence)
	}
}

func TestDiscoverArgoCDApplicationSetTemplateLiteralDoesNotMatchBroadAlias(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-gitops",
			Payload: map[string]any{
				"artifact_type": "argocd",
				"relative_path": "applicationsets/services.yaml",
				"content": `apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
spec:
  generators:
    - git:
        repoURL: https://github.com/myorg/platform-config.git
        files:
          - path: services/*/service.yaml
  template:
    spec:
      source:
        repoURL: "{{ .service.repoURL }}"
`,
			},
		},
		{
			ScopeID: "repo-config",
			Payload: map[string]any{
				"artifact_type": "yaml",
				"relative_path": "services/payments/service.yaml",
				"content":       `service: {}`,
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-config", Aliases: []string{"platform-config"}},
		{RepoID: "repo-service", Aliases: []string{"service"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)

	if !hasEvidenceKind(evidence, EvidenceKindArgoCDApplicationSetDiscovery) {
		t.Fatal("missing ApplicationSet discovery evidence")
	}
	if hasEvidenceKind(evidence, EvidenceKindArgoCDApplicationSetDeploySource) {
		t.Fatalf("unexpected deploy-source evidence: %#v", evidence)
	}
}

func TestDiscoverEvidenceSelfReferenceSkipped(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-infra",
			Payload: map[string]any{
				"artifact_type": "terraform",
				"relative_path": "main.tf",
				"content":       `app_repo = "infra-repo"`,
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-infra", Aliases: []string{"infra-repo"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)

	if len(evidence) != 0 {
		t.Errorf("len = %d, want 0 (self-reference)", len(evidence))
	}
}

func TestDiscoverEvidenceDeduplication(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-infra",
			Payload: map[string]any{
				"artifact_type": "terraform",
				"relative_path": "main.tf",
				"content":       `app_repo = "payments-service"` + "\n" + `app_repo = "payments-service"`,
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)

	// Even with two identical matches in the same file, dedup by (kind, src, tgt, path).
	if len(evidence) != 1 {
		t.Errorf("len = %d, want 1 (deduped per file)", len(evidence))
	}
}

func TestIsTerraformArtifact(t *testing.T) {
	t.Parallel()

	tests := []struct {
		artifactType string
		filePath     string
		want         bool
	}{
		{"terraform", "main.tf", true},
		{"terragrunt", "terragrunt.hcl", true},
		{"", "modules/vpc.tf", true},
		{"", "config.tf.json", true},
		{"", "data.hcl", true},
		{"", "values.yaml", false},
		{"helm", "Chart.yaml", false},
	}
	for _, tt := range tests {
		got := isTerraformArtifact(tt.artifactType, tt.filePath)
		if got != tt.want {
			t.Errorf("isTerraformArtifact(%q, %q) = %v, want %v",
				tt.artifactType, tt.filePath, got, tt.want)
		}
	}
}

func TestIsHelmArtifact(t *testing.T) {
	t.Parallel()

	tests := []struct {
		artifactType string
		filePath     string
		want         bool
	}{
		{"helm", "Chart.yaml", true},
		{"", "charts/app/Chart.yaml", true},
		{"", "charts/app/Chart.yml", true},
		{"", "values.yaml", true},
		{"", "values-prod.yaml", true},
		{"", "main.tf", false},
	}
	for _, tt := range tests {
		got := isHelmArtifact(tt.artifactType, tt.filePath)
		if got != tt.want {
			t.Errorf("isHelmArtifact(%q, %q) = %v, want %v",
				tt.artifactType, tt.filePath, got, tt.want)
		}
	}
}

func TestIsKustomizeArtifact(t *testing.T) {
	t.Parallel()

	tests := []struct {
		filePath string
		want     bool
	}{
		{"overlays/prod/kustomization.yaml", true},
		{"overlays/prod/kustomization.yml", true},
		{"main.tf", false},
		{"Chart.yaml", false},
	}
	for _, tt := range tests {
		got := isKustomizeArtifact(tt.filePath)
		if got != tt.want {
			t.Errorf("isKustomizeArtifact(%q) = %v, want %v",
				tt.filePath, got, tt.want)
		}
	}
}

func TestIsArgoCDArtifact(t *testing.T) {
	t.Parallel()

	tests := []struct {
		artifactType string
		content      string
		want         bool
	}{
		{"argocd", "", true},
		{"", "kind: Application", true},
		{"", "kind: ApplicationSet", true},
		{"", "kind: Deployment", false},
	}
	for _, tt := range tests {
		got := isArgoCDArtifact(tt.artifactType, tt.content)
		if got != tt.want {
			t.Errorf("isArgoCDArtifact(%q, %q) = %v, want %v",
				tt.artifactType, tt.content, got, tt.want)
		}
	}
}

// TestDiscoverEvidenceFromGoCollectorContentFacts verifies that evidence
// discovery works with the Go collector's content fact payload format, which
// uses content_path/content_body instead of relative_path/content.
func TestDiscoverEvidenceFromGoCollectorContentFacts(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-infra",
			Payload: map[string]any{
				"artifact_type":  "terraform",
				"content_path":   "modules/iam.tf",
				"content_body":   `app_repo = "payments-service"`,
				"content_digest": "sha256:abc123",
				"repo_id":        "repo-infra",
				"language":       "terraform_hcl",
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)

	if len(evidence) != 1 {
		t.Fatalf("len = %d, want 1 (Go collector content fact format)", len(evidence))
	}
	if evidence[0].EvidenceKind != EvidenceKindTerraformAppRepo {
		t.Errorf("kind = %q, want %q", evidence[0].EvidenceKind, EvidenceKindTerraformAppRepo)
	}
	if evidence[0].SourceRepoID != "repo-infra" {
		t.Errorf("source = %q, want repo-infra", evidence[0].SourceRepoID)
	}
	if evidence[0].TargetRepoID != "repo-payments" {
		t.Errorf("target = %q, want repo-payments", evidence[0].TargetRepoID)
	}
}

func TestDiscoverEvidenceNormalizesScopedSourceRepositoryID(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "git-repository-scope:repository:r_infra",
			Payload: map[string]any{
				"artifact_type": "terraform",
				"relative_path": "main.tf",
				"content":       `app_repo = "payments-service"`,
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repository:r_payments", Aliases: []string{"payments-service"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	if len(evidence) != 1 {
		t.Fatalf("len = %d, want 1", len(evidence))
	}
	if got, want := evidence[0].SourceRepoID, "repository:r_infra"; got != want {
		t.Fatalf("source repo = %q, want %q", got, want)
	}
}

// TestDiscoverEvidenceFromGoCollectorHelmFacts verifies Helm evidence
// extraction using Go collector content fact format (content_path/content_body).
func TestDiscoverEvidenceFromGoCollectorHelmFacts(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-deploy",
			Payload: map[string]any{
				"content_path": "charts/app/values.yaml",
				"content_body": "image:\n  repository: payments-service\n",
				"repo_id":      "repo-deploy",
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)

	if len(evidence) != 1 {
		t.Fatalf("len = %d, want 1 (Helm values via content_path/content_body)", len(evidence))
	}
	if evidence[0].EvidenceKind != EvidenceKindHelmValues {
		t.Errorf("kind = %q, want %q", evidence[0].EvidenceKind, EvidenceKindHelmValues)
	}
}

// TestDiscoverEvidenceFromGoCollectorArgoCDFacts verifies ArgoCD evidence
// extraction using Go collector content fact format.
func TestDiscoverEvidenceFromGoCollectorArgoCDFacts(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-gitops",
			Payload: map[string]any{
				"content_path": "apps/payments.yaml",
				"content_body": `apiVersion: argoproj.io/v1alpha1
kind: Application
spec:
  source:
    repoURL: 'https://github.com/myorg/payments-service.git'
`,
				"repo_id": "repo-gitops",
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)

	if len(evidence) != 1 {
		t.Fatalf("len = %d, want 1 (ArgoCD via content_path/content_body)", len(evidence))
	}
	if evidence[0].EvidenceKind != EvidenceKindArgoCDAppSource {
		t.Errorf("kind = %q, want %q", evidence[0].EvidenceKind, EvidenceKindArgoCDAppSource)
	}
}

func TestFileBaseName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path string
		want string
	}{
		{"charts/app/Chart.yaml", "Chart.yaml"},
		{"main.tf", "main.tf"},
		{"a/b/c/d.hcl", "d.hcl"},
	}
	for _, tt := range tests {
		got := fileBaseName(tt.path)
		if got != tt.want {
			t.Errorf("fileBaseName(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestMatchesEntry(t *testing.T) {
	t.Parallel()

	entry := CatalogEntry{RepoID: "repo-1", Aliases: []string{"payments-service", "payments"}}

	if got := matchesEntry("payments-service", entry); got != "payments-service" {
		t.Errorf("got %q, want payments-service", got)
	}
	if got := matchesEntry("PAYMENTS-SERVICE", entry); got != "payments-service" {
		t.Errorf("got %q for case-insensitive match", got)
	}
	if got := matchesEntry("unrelated-repo", entry); got != "" {
		t.Errorf("got %q, want empty for no match", got)
	}
}

func hasEvidenceKind(evidence []EvidenceFact, want EvidenceKind) bool {
	for _, fact := range evidence {
		if fact.EvidenceKind == want {
			return true
		}
	}
	return false
}

func hasRelationshipType(evidence []EvidenceFact, want RelationshipType) bool {
	for _, fact := range evidence {
		if fact.RelationshipType == want {
			return true
		}
	}
	return false
}
