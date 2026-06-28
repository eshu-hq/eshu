// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships //nolint:filelength // 624 lines: flat golden calibration corpus — one fixed labeled goldenCase literal per evidence kind (positives, negatives, ambiguous), reviewed as a single immutable dataset. The golangci filelength cap already exempts _test.go; splitting the linear table across files would scatter the per-kind rows the calibration gate reads together.

// confidence_calibration_corpus_test.go holds the labeled golden corpus and the
// P/R sweep helpers for the DefaultConfidenceRegistry calibration gate
// (issue #3510). The test bodies live in confidence_calibration_test.go.
//
// # Independent golden truth (review fix for #3657)
//
// Each golden case carries a GoldenConfidence: a FIXED, measured score that is
// independent of the registry value under test. The score is the real
// per-extractor confidence that the matching extractor emits for a clean match
// of that kind, captured as a literal here so the calibration gate has an
// external labeled truth to compare the registry against.
//
// This is the whole point of the gate: if a future edit moves a prior in
// confidence.go away from the golden optimum by more than the tolerance, the
// gate FAILS, because the golden corpus does not move with the registry. The
// earlier harness derived every case score from DefaultConfidenceRegistry, so
// any drift moved the "calibrated optimum" along with the value under test and
// could only ever be logged as tier tension — it could never fail. The fixed
// GoldenConfidence values decouple the truth from the value being tested.
//
// # Score model
//
// For each kind the golden corpus encodes three labeled bands relative to the
// real extractor prior p for that kind:
//   - positive:  p          — a clean full-confidence match (relationship real)
//   - negative:  p × 0.88    — a marginal false positive (name collision, stale
//     binding, partial path) just below the prior
//   - ambiguous: p × 0.80    — a degraded partial match that must not resolve
//     alone (corroboration required)
//
// The literals below are those products computed once, at the priors measured
// when #3490 priors were found calibration-consistent, and frozen. They are NOT
// recomputed from DefaultConfidenceRegistry at test time.

// goldenLabel classifies one case in the golden set.
type goldenLabel string

const (
	// goldenPositive: extractor emits a clean full-confidence match; relationship is real.
	goldenPositive goldenLabel = "positive"
	// goldenNegative: extractor runs on a false positive; emits a marginal score below the prior.
	goldenNegative goldenLabel = "negative"
	// goldenAmbiguous: extractor emits a degraded match; a single such fact must
	// not cross DefaultConfidenceThreshold.
	goldenAmbiguous goldenLabel = "ambiguous"
)

// goldenCase is one labeled correlation case with an independent golden score.
type goldenCase struct {
	// ID is a stable identifier for diagnostic output.
	ID string
	// Kind is the EvidenceKind under test.
	Kind EvidenceKind
	// Label is the ground-truth classification.
	Label goldenLabel
	// GoldenConfidence is the FIXED labeled score for this case. It is measured
	// independently of DefaultConfidenceRegistry and frozen as a literal so the
	// calibration gate compares the registry priors against external truth, not
	// against a value derived from the registry itself.
	GoldenConfidence float64
	// Rationale is human-readable provenance for the label.
	Rationale string
}

// goldenSet is the complete labeled golden corpus with FIXED scores.
//
// Construction rules:
//  1. Every registered EvidenceKind has at least two positive cases and one
//     negative case. New EvidenceKind constants added to models.go require new
//     golden entries; TestGoldenSetCoverage enforces this.
//  2. Negative cases represent known failure modes: name collision, partial path
//     match, shared CI library, orphaned config, wildcard principals. Their
//     GoldenConfidence is the prior × 0.88 product, frozen as a literal.
//  3. Ambiguous cases represent degraded matches (partial name, short alias)
//     that need corroboration. Per TestAmbiguousCasesRequireCorroboration, the
//     score (prior × 0.80) must be below DefaultConfidenceThreshold. For
//     high-confidence kinds (prior ≥ 0.94) even 0.80 × prior ≥ 0.75, so these
//     kinds have no ambiguous cases — any match of that kind resolves alone.
//
// The GoldenConfidence literals are independent of DefaultConfidenceRegistry.
// TestGoldenScoresAreCalibrationConsistent asserts the current registry priors
// still match these golden positives within tolerance; a drift in confidence.go
// fails that gate.
var goldenSet = []goldenCase{
	// ---- TierDirectBinding (priors 0.95–0.99) ----
	// All direct-binding priors × 0.80 ≥ 0.76 > threshold, so no ambiguous cases.

	// TERRAFORM_APP_REPO (prior 0.99; neg 0.99×0.88=0.8712)
	{
		ID: "tf-app-repo-pos-1", Kind: EvidenceKindTerraformAppRepo, Label: goldenPositive, GoldenConfidence: 0.99,
		Rationale: "app_repo = 'payments-service' in main.tf directly names the deploy target",
	},
	{
		ID: "tf-app-repo-pos-2", Kind: EvidenceKindTerraformAppRepo, Label: goldenPositive, GoldenConfidence: 0.99,
		Rationale: "app_repo = 'auth-service' in a module call directly names the deploy target",
	},
	{
		ID: "tf-app-repo-pos-3", Kind: EvidenceKindTerraformAppRepo, Label: goldenPositive, GoldenConfidence: 0.99,
		Rationale: "app_repo = 'data-pipeline' in service module directly names the deploy target",
	},
	{
		ID: "tf-app-repo-neg-1", Kind: EvidenceKindTerraformAppRepo, Label: goldenNegative, GoldenConfidence: 0.8712,
		Rationale: "app_repo = 'payments-service' in an abandoned plan; target repo was renamed — stale binding",
	},

	// TERRAFORM_GITHUB_REPOSITORY (prior 0.98; neg 0.8624)
	{
		ID: "tf-gh-repo-pos-1", Kind: EvidenceKindTerraformGitHubRepo, Label: goldenPositive, GoldenConfidence: 0.98,
		Rationale: "repository = 'github.com/org/payments-service' names exactly one catalog repo",
	},
	{
		ID: "tf-gh-repo-pos-2", Kind: EvidenceKindTerraformGitHubRepo, Label: goldenPositive, GoldenConfidence: 0.98,
		Rationale: "repository = 'github.com/org/infra-platform' names exactly one catalog repo",
	},
	{
		ID: "tf-gh-repo-neg-1", Kind: EvidenceKindTerraformGitHubRepo, Label: goldenNegative, GoldenConfidence: 0.8624,
		Rationale: "repository = 'github.com/org/DELETED' — repo no longer exists in the catalog",
	},

	// TERRAFORM_MODULE_SOURCE (prior 0.98; neg 0.8624)
	{
		ID: "tf-mod-src-pos-1", Kind: EvidenceKindTerraformModuleSource, Label: goldenPositive, GoldenConfidence: 0.98,
		Rationale: "source = 'git::github.com/org/tf-modules//ecs-service' binds to one module repo",
	},
	{
		ID: "tf-mod-src-pos-2", Kind: EvidenceKindTerraformModuleSource, Label: goldenPositive, GoldenConfidence: 0.98,
		Rationale: "source = 'git::github.com/org/tf-modules//rds' binds to one module repo",
	},
	{
		ID: "tf-mod-src-neg-1", Kind: EvidenceKindTerraformModuleSource, Label: goldenNegative, GoldenConfidence: 0.8624,
		Rationale: "source is a Terraform registry path (hashicorp/consul/aws) with no catalog match",
	},

	// TERRAFORM_GITHUB_ACTIONS_REPOSITORY (prior 0.97; neg 0.8536)
	{
		ID: "tf-gha-repo-pos-1", Kind: EvidenceKindTerraformGitHubActions, Label: goldenPositive, GoldenConfidence: 0.97,
		Rationale: "oidc subject repo:org/payments-service:ref:refs/heads/main names the repo unambiguously",
	},
	{
		ID: "tf-gha-repo-pos-2", Kind: EvidenceKindTerraformGitHubActions, Label: goldenPositive, GoldenConfidence: 0.97,
		Rationale: "oidc subject repo:org/infra-deploy:environment:production names the deploy repo",
	},
	{
		ID: "tf-gha-repo-neg-1", Kind: EvidenceKindTerraformGitHubActions, Label: goldenNegative, GoldenConfidence: 0.8536,
		Rationale: "oidc subject uses a wildcard (*) that matches multiple catalog repos — ambiguous principal",
	},

	// ARGOCD_APPLICATION_SOURCE (prior 0.95; neg 0.836)
	{
		ID: "argocd-app-src-pos-1", Kind: EvidenceKindArgoCDAppSource, Label: goldenPositive, GoldenConfidence: 0.95,
		Rationale: "Application.spec.source.repoURL = 'https://github.com/org/helm-charts' is explicit",
	},
	{
		ID: "argocd-app-src-pos-2", Kind: EvidenceKindArgoCDAppSource, Label: goldenPositive, GoldenConfidence: 0.95,
		Rationale: "Application.spec.source.repoURL = 'ssh://github.com/org/config-repo' is explicit",
	},
	{
		ID: "argocd-app-src-neg-1", Kind: EvidenceKindArgoCDAppSource, Label: goldenNegative, GoldenConfidence: 0.836,
		Rationale: "repoURL is a template variable not resolved at parse time — no concrete catalog target",
	},

	// ARGOCD_APPLICATIONSET_DISCOVERY (prior 0.99; neg 0.8712)
	{
		ID: "appset-disco-pos-1", Kind: EvidenceKindArgoCDApplicationSetDiscovery, Label: goldenPositive, GoldenConfidence: 0.99,
		Rationale: "git generator enumerates concrete source repos — each resolved entry is authoritative",
	},
	{
		ID: "appset-disco-pos-2", Kind: EvidenceKindArgoCDApplicationSetDiscovery, Label: goldenPositive, GoldenConfidence: 0.99,
		Rationale: "list generator with explicit repoURL values enumerates concrete source repos",
	},
	{
		ID: "appset-disco-neg-1", Kind: EvidenceKindArgoCDApplicationSetDiscovery, Label: goldenNegative, GoldenConfidence: 0.8712,
		Rationale: "git generator pattern matches many repos but the catalog has only unrelated names",
	},

	// ARGOCD_APPLICATIONSET_DEPLOY_SOURCE (prior 0.99; neg 0.8712)
	{
		ID: "appset-deploy-pos-1", Kind: EvidenceKindArgoCDApplicationSetDeploySource, Label: goldenPositive, GoldenConfidence: 0.99,
		Rationale: "template.spec.source.repoURL resolves to one concrete repo per generator entry",
	},
	{
		ID: "appset-deploy-pos-2", Kind: EvidenceKindArgoCDApplicationSetDeploySource, Label: goldenPositive, GoldenConfidence: 0.99,
		Rationale: "template names explicit target per generator value in a multi-source ApplicationSet",
	},
	{
		ID: "appset-deploy-neg-1", Kind: EvidenceKindArgoCDApplicationSetDeploySource, Label: goldenNegative, GoldenConfidence: 0.8712,
		Rationale: "template parameter is a cluster-scoped variable with no catalog mapping",
	},

	// ARGOCD_DESTINATION_PLATFORM (prior 0.97; neg 0.8536)
	{
		ID: "argocd-dest-pos-1", Kind: EvidenceKindArgoCDDestinationPlatform, Label: goldenPositive, GoldenConfidence: 0.97,
		Rationale: "destination.server = 'https://k8s.prod.example.com' maps to one platform repo",
	},
	{
		ID: "argocd-dest-pos-2", Kind: EvidenceKindArgoCDDestinationPlatform, Label: goldenPositive, GoldenConfidence: 0.97,
		Rationale: "destination.name = 'prod-cluster' maps to one well-known platform in the catalog",
	},
	{
		ID: "argocd-dest-neg-1", Kind: EvidenceKindArgoCDDestinationPlatform, Label: goldenNegative, GoldenConfidence: 0.8536,
		Rationale: "destination.server is localhost — local dev only, not a catalog entry",
	},

	// ---- TierStrongReference (priors 0.90–0.94) ----
	// 0.90 × 0.80 = 0.72 < 0.75 threshold, so ambiguous cases are valid for 0.90 kinds.
	// 0.94 × 0.80 = 0.752 > 0.75, so TERRAFORM_APP_NAME (0.94) has no ambiguous cases.

	// TERRAFORM_APP_NAME (prior 0.94; neg 0.8272; no ambiguous: 0.94×0.80=0.752 > threshold)
	{
		ID: "tf-app-name-pos-1", Kind: EvidenceKindTerraformAppName, Label: goldenPositive, GoldenConfidence: 0.94,
		Rationale: "app_name = 'payments' resolves to 'payments-service' via catalog alias; unambiguous",
	},
	{
		ID: "tf-app-name-pos-2", Kind: EvidenceKindTerraformAppName, Label: goldenPositive, GoldenConfidence: 0.94,
		Rationale: "app_name = 'auth-worker' resolves to 'auth-service' worker component via alias",
	},
	{
		ID: "tf-app-name-neg-1", Kind: EvidenceKindTerraformAppName, Label: goldenNegative, GoldenConfidence: 0.8272,
		Rationale: "app_name = 'common' matches multiple catalog repos — too generic",
	},

	// DOCKERFILE_SOURCE_LABEL (prior 0.93; neg 0.8184)
	{
		ID: "dockerfile-label-pos-1", Kind: EvidenceKindDockerfileSourceLabel, Label: goldenPositive, GoldenConfidence: 0.93,
		Rationale: "LABEL org.opencontainers.image.source = 'https://github.com/org/payments-service'",
	},
	{
		ID: "dockerfile-label-pos-2", Kind: EvidenceKindDockerfileSourceLabel, Label: goldenPositive, GoldenConfidence: 0.93,
		Rationale: "LABEL source.repository = 'github.com/org/auth-service'",
	},
	{
		ID: "dockerfile-label-neg-1", Kind: EvidenceKindDockerfileSourceLabel, Label: goldenNegative, GoldenConfidence: 0.8184,
		Rationale: "LABEL source is set to a fork URL that does not exist in the catalog",
	},

	// GITHUB_ACTIONS_REUSABLE_WORKFLOW (prior 0.93; neg 0.8184)
	{
		ID: "gha-reusable-pos-1", Kind: EvidenceKindGitHubActionsReusableWorkflow, Label: goldenPositive, GoldenConfidence: 0.93,
		Rationale: "uses: org/deploy-workflows/.github/workflows/deploy.yml@main names the automation repo",
	},
	{
		ID: "gha-reusable-pos-2", Kind: EvidenceKindGitHubActionsReusableWorkflow, Label: goldenPositive, GoldenConfidence: 0.93,
		Rationale: "uses: org/release-workflows/.github/workflows/release.yml@v2 names the release repo",
	},
	{
		ID: "gha-reusable-neg-1", Kind: EvidenceKindGitHubActionsReusableWorkflow, Label: goldenNegative, GoldenConfidence: 0.8184,
		Rationale: "uses: org/archived-workflows/.github/workflows/old.yml — repo is archived, not in catalog",
	},

	// TERRAFORM_IAM_PERMISSION (prior 0.92; neg 0.8096)
	{
		ID: "tf-iam-pos-1", Kind: EvidenceKindTerraformIAMPermission, Label: goldenPositive, GoldenConfidence: 0.92,
		Rationale: "principal ARN arn:aws:iam::123:role/payments-service-deploy ties provisioning to one repo",
	},
	{
		ID: "tf-iam-pos-2", Kind: EvidenceKindTerraformIAMPermission, Label: goldenPositive, GoldenConfidence: 0.92,
		Rationale: "workload identity 'projects/123/serviceAccounts/auth-service@...' maps to one repo",
	},
	{
		ID: "tf-iam-neg-1", Kind: EvidenceKindTerraformIAMPermission, Label: goldenNegative, GoldenConfidence: 0.8096,
		Rationale: "principal is '*' wildcard — cannot bind to any specific catalog repo",
	},

	// JENKINS_GITHUB_REPOSITORY (prior 0.92; neg 0.8096)
	{
		ID: "jenkins-gh-repo-pos-1", Kind: EvidenceKindJenkinsGitHubRepository, Label: goldenPositive, GoldenConfidence: 0.92,
		Rationale: "checkout scm url 'https://github.com/org/payments-service' explicitly names the repo",
	},
	{
		ID: "jenkins-gh-repo-pos-2", Kind: EvidenceKindJenkinsGitHubRepository, Label: goldenPositive, GoldenConfidence: 0.92,
		Rationale: "git url 'git@github.com:org/infra-scripts.git' in Jenkinsfile names the repo",
	},
	{
		ID: "jenkins-gh-repo-neg-1", Kind: EvidenceKindJenkinsGitHubRepository, Label: goldenNegative, GoldenConfidence: 0.8096,
		Rationale: "URL is a mirror host (git.company.internal/...) with no catalog match",
	},

	// ANSIBLE_ROLE_REFERENCE (prior 0.92; neg 0.8096)
	{
		ID: "ansible-role-pos-1", Kind: EvidenceKindAnsibleRoleReference, Label: goldenPositive, GoldenConfidence: 0.92,
		Rationale: "role: org.payments-deploy references the deploy role owned by payments-service repo",
	},
	{
		ID: "ansible-role-pos-2", Kind: EvidenceKindAnsibleRoleReference, Label: goldenPositive, GoldenConfidence: 0.92,
		Rationale: "role: org.auth-setup references the auth-service setup automation",
	},
	{
		ID: "ansible-role-neg-1", Kind: EvidenceKindAnsibleRoleReference, Label: goldenNegative, GoldenConfidence: 0.8096,
		Rationale: "role name is a built-in community role (geerlingguy.docker) with no catalog match",
	},
	// Puppet module references. Positive golden score == the registry prior
	// (0.90); negative == prior × 0.88 (0.792), mirroring the kustomize/ansible
	// rows. A Puppetfile mod with an explicit git source resolves to the
	// module-owning repository; a forge slug or commented source does not.
	{
		ID: "puppet-module-pos-1", Kind: EvidenceKindPuppetModuleReference, Label: goldenPositive, GoldenConfidence: 0.90,
		Rationale: "mod 'acme-base', :git => github.com/acme/deployable-source resolves to the in-corpus module repo",
	},
	{
		ID: "puppet-module-pos-2", Kind: EvidenceKindPuppetModuleReference, Label: goldenPositive, GoldenConfidence: 0.90,
		Rationale: "mod 'acme-network', git: github.com/acme/network-modules resolves to the network module repo",
	},
	{
		ID: "puppet-module-neg-1", Kind: EvidenceKindPuppetModuleReference, Label: goldenNegative, GoldenConfidence: 0.792,
		Rationale: "forge-only mod 'puppetlabs-stdlib', '9.4.0' has no git source and no catalog match",
	},
	// Chef cookbook dependencies. Positive golden score == the registry prior
	// (0.90); negative == prior x 0.88 (0.792), mirroring the puppet/ansible
	// rows. A Berksfile cookbook with an explicit git source resolves to the
	// cookbook-owning repository; a Supermarket version constraint or commented
	// source does not.
	{
		ID: "chef-cookbook-pos-1", Kind: EvidenceKindChefCookbookDependency, Label: goldenPositive, GoldenConfidence: 0.90,
		Rationale: "cookbook 'acme-base', git: github.com/acme/deployable-source resolves to the in-corpus cookbook repo",
	},
	{
		ID: "chef-cookbook-pos-2", Kind: EvidenceKindChefCookbookDependency, Label: goldenPositive, GoldenConfidence: 0.90,
		Rationale: "cookbook 'acme-network', :git => github.com/acme/network-cookbooks resolves to the network cookbook repo",
	},
	{
		ID: "chef-cookbook-neg-1", Kind: EvidenceKindChefCookbookDependency, Label: goldenNegative, GoldenConfidence: 0.792,
		Rationale: "supermarket-only cookbook 'nginx', '~> 12.0' has no git source and no catalog match",
	},
	// Salt formula references. Positive golden score == the registry prior
	// (0.90); negative == prior x 0.88 (0.792), mirroring the puppet/chef rows. A
	// Salt config whose gitfs_remotes lists a formula git repository resolves to
	// that formula-owning repository; a non-gitfs config or unmatched remote does
	// not.
	{
		ID: "salt-formula-pos-1", Kind: EvidenceKindSaltFormulaReference, Label: goldenPositive, GoldenConfidence: 0.90,
		Rationale: "gitfs_remotes lists github.com/acme/deployable-source which resolves to the in-corpus formula repo",
	},
	{
		ID: "salt-formula-pos-2", Kind: EvidenceKindSaltFormulaReference, Label: goldenPositive, GoldenConfidence: 0.90,
		Rationale: "gitfs_remotes single-key map github.com/acme/network-formulas resolves to the network formula repo",
	},
	{
		ID: "salt-formula-neg-1", Kind: EvidenceKindSaltFormulaReference, Label: goldenNegative, GoldenConfidence: 0.792,
		Rationale: "a Salt config with no gitfs_remotes formula source has no repository identity and no catalog match",
	},
	// Helm template-value references. Positive golden score == the registry prior
	// (0.90); negative == prior x 0.88 (0.792), mirroring the puppet/chef rows. A
	// chart template `{{ .Values.<path> }}` whose dotted path matches a leaf key in
	// the same chart's values.yaml resolves to that definition; a path with no
	// matching leaf (or a value read from a parent/global scope) does not.
	{
		ID: "helm-template-value-pos-1", Kind: EvidenceKindHelmTemplateValueReference, Label: goldenPositive, GoldenConfidence: 0.90,
		Rationale: "{{ .Values.image.repository }} resolves to the image.repository leaf in the chart's values.yaml",
	},
	{
		ID: "helm-template-value-pos-2", Kind: EvidenceKindHelmTemplateValueReference, Label: goldenPositive, GoldenConfidence: 0.90,
		Rationale: "{{ .Values.replicaCount }} resolves to the replicaCount leaf in the chart's values.yaml",
	},
	{
		ID: "helm-template-value-neg-1", Kind: EvidenceKindHelmTemplateValueReference, Label: goldenNegative, GoldenConfidence: 0.792,
		Rationale: "{{ .Values.global.unset }} reads a path with no matching leaf in the chart's values.yaml",
	},

	// DOCKER_COMPOSE_BUILD_CONTEXT (prior 0.91; neg 0.8008)
	{
		ID: "compose-build-pos-1", Kind: EvidenceKindDockerComposeBuildContext, Label: goldenPositive, GoldenConfidence: 0.91,
		Rationale: "build.context: '../payments-service' points at the sibling service repo",
	},
	{
		ID: "compose-build-pos-2", Kind: EvidenceKindDockerComposeBuildContext, Label: goldenPositive, GoldenConfidence: 0.91,
		Rationale: "build.context: '../auth-service' points at the auth sibling repo",
	},
	{
		ID: "compose-build-neg-1", Kind: EvidenceKindDockerComposeBuildContext, Label: goldenNegative, GoldenConfidence: 0.8008,
		Rationale: "build.context: '.' refers to the same repo — no cross-repo relationship",
	},

	// GITHUB_ACTIONS_CHECKOUT_REPOSITORY (prior 0.91; neg 0.8008)
	{
		ID: "gha-checkout-pos-1", Kind: EvidenceKindGitHubActionsCheckoutRepository, Label: goldenPositive, GoldenConfidence: 0.91,
		Rationale: "actions/checkout with repository: org/config-repo names the config automation source",
	},
	{
		ID: "gha-checkout-pos-2", Kind: EvidenceKindGitHubActionsCheckoutRepository, Label: goldenPositive, GoldenConfidence: 0.91,
		Rationale: "actions/checkout with repository: org/shared-infra names the infra source repo",
	},
	{
		ID: "gha-checkout-neg-1", Kind: EvidenceKindGitHubActionsCheckoutRepository, Label: goldenNegative, GoldenConfidence: 0.8008,
		Rationale: "actions/checkout with no repository field checks out the same repo — not cross-repo",
	},

	// HELM_CHART_REFERENCE (prior 0.90; neg 0.792; ambiguous 0.72 < 0.75 valid)
	{
		ID: "helm-chart-pos-1", Kind: EvidenceKindHelmChart, Label: goldenPositive, GoldenConfidence: 0.90,
		Rationale: "Chart.yaml name: payments-service resolves to one catalog repo by alias",
	},
	{
		ID: "helm-chart-pos-2", Kind: EvidenceKindHelmChart, Label: goldenPositive, GoldenConfidence: 0.90,
		Rationale: "Chart.yaml name: auth-service resolves to one catalog repo by alias",
	},
	{
		ID: "helm-chart-neg-1", Kind: EvidenceKindHelmChart, Label: goldenNegative, GoldenConfidence: 0.792,
		Rationale: "Chart.yaml name: common matches multiple catalog repos — ambiguous without more context",
	},
	{
		ID: "helm-chart-ambiguous-1", Kind: EvidenceKindHelmChart, Label: goldenAmbiguous, GoldenConfidence: 0.72,
		Rationale: "Chart.yaml name: svc — very short alias; degraded-confidence match needs corroboration",
	},

	// TERRAFORM_CONFIG_PATH (prior 0.90; neg 0.792; ambiguous 0.72)
	{
		ID: "tf-cfg-path-pos-1", Kind: EvidenceKindTerraformConfigPath, Label: goldenPositive, GoldenConfidence: 0.90,
		Rationale: "config_path = '../payments-service/config' contains the target repo name as path segment",
	},
	{
		ID: "tf-cfg-path-pos-2", Kind: EvidenceKindTerraformConfigPath, Label: goldenPositive, GoldenConfidence: 0.90,
		Rationale: "config_path = 'services/auth-service/env/prod' contains exact repo name segment",
	},
	{
		ID: "tf-cfg-path-neg-1", Kind: EvidenceKindTerraformConfigPath, Label: goldenNegative, GoldenConfidence: 0.792,
		Rationale: "config_path segment matches 'service' — too generic, substring of many repo names",
	},
	{
		ID: "tf-cfg-path-ambiguous-1", Kind: EvidenceKindTerraformConfigPath, Label: goldenAmbiguous, GoldenConfidence: 0.72,
		Rationale: "config_path = '../svc/prod' — very short segment; degraded match needs corroboration",
	},

	// TERRAGRUNT_DEPENDENCY_CONFIG_PATH (prior 0.90; neg 0.792; ambiguous 0.72)
	{
		ID: "tg-dep-pos-1", Kind: EvidenceKindTerragruntDependencyConfigPath, Label: goldenPositive, GoldenConfidence: 0.90,
		Rationale: "dependency.config_path = '../payments-rds' resolves to one terragrunt stack",
	},
	{
		ID: "tg-dep-pos-2", Kind: EvidenceKindTerragruntDependencyConfigPath, Label: goldenPositive, GoldenConfidence: 0.90,
		Rationale: "dependency.config_path = '../../vpc/prod' resolves to one infrastructure module",
	},
	{
		ID: "tg-dep-neg-1", Kind: EvidenceKindTerragruntDependencyConfigPath, Label: goldenNegative, GoldenConfidence: 0.792,
		Rationale: "dependency.config_path contains only numeric folder names — no catalog match",
	},
	{
		ID: "tg-dep-ambiguous-1", Kind: EvidenceKindTerragruntDependencyConfigPath, Label: goldenAmbiguous, GoldenConfidence: 0.72,
		Rationale: "dependency.config_path = '../db' — short name; degraded match needs corroboration",
	},

	// KUSTOMIZE_RESOURCE (prior 0.90; neg 0.792; ambiguous 0.72)
	{
		ID: "kust-resource-pos-1", Kind: EvidenceKindKustomizeResource, Label: goldenPositive, GoldenConfidence: 0.90,
		Rationale: "kustomization.yaml resources: [../payments-service/base] names the config repo",
	},
	{
		ID: "kust-resource-pos-2", Kind: EvidenceKindKustomizeResource, Label: goldenPositive, GoldenConfidence: 0.90,
		Rationale: "resources: [github.com/org/cert-manager/config/default] names the source repo",
	},
	{
		ID: "kust-resource-neg-1", Kind: EvidenceKindKustomizeResource, Label: goldenNegative, GoldenConfidence: 0.792,
		Rationale: "resources entry is a local path (./base) — no cross-repo identity",
	},
	{
		ID: "kust-resource-ambiguous-1", Kind: EvidenceKindKustomizeResource, Label: goldenAmbiguous, GoldenConfidence: 0.72,
		Rationale: "resources: [../svc] — short path segment; degraded match needs corroboration",
	},

	// GITHUB_ACTIONS_WORKFLOW_INPUT_REPOSITORY (prior 0.90; neg 0.792; ambiguous 0.72)
	{
		ID: "gha-input-repo-pos-1", Kind: EvidenceKindGitHubActionsWorkflowInputRepository, Label: goldenPositive, GoldenConfidence: 0.90,
		Rationale: "workflow_dispatch input named 'config_repo' passed as 'org/platform-config'",
	},
	{
		ID: "gha-input-repo-pos-2", Kind: EvidenceKindGitHubActionsWorkflowInputRepository, Label: goldenPositive, GoldenConfidence: 0.90,
		Rationale: "workflow_call input named 'infra_repo' bound to 'org/infra-platform' explicitly",
	},
	{
		ID: "gha-input-repo-neg-1", Kind: EvidenceKindGitHubActionsWorkflowInputRepository, Label: goldenNegative, GoldenConfidence: 0.792,
		Rationale: "workflow input named 'repo' has default value '' (empty) — no catalog match possible",
	},
	{
		ID: "gha-input-repo-ambiguous-1", Kind: EvidenceKindGitHubActionsWorkflowInputRepository, Label: goldenAmbiguous, GoldenConfidence: 0.72,
		Rationale: "workflow input named 'repo' with default 'org/svc' — short name; degraded match",
	},

	// ---- TierReference (priors 0.86–0.89) ----
	// 0.86 × 0.80 = 0.688 < 0.75; all ambiguous cases valid.

	// JENKINS_SHARED_LIBRARY (prior 0.89; neg 0.7832; ambiguous 0.712)
	{
		ID: "jenkins-lib-pos-1", Kind: EvidenceKindJenkinsSharedLibrary, Label: goldenPositive, GoldenConfidence: 0.89,
		Rationale: "@Library('deploy-commons') references the shared library repo — unambiguous",
	},
	{
		ID: "jenkins-lib-pos-2", Kind: EvidenceKindJenkinsSharedLibrary, Label: goldenPositive, GoldenConfidence: 0.89,
		Rationale: "@Library('release-pipeline') references the release automation library repo",
	},
	{
		ID: "jenkins-lib-neg-1", Kind: EvidenceKindJenkinsSharedLibrary, Label: goldenNegative, GoldenConfidence: 0.7832,
		Rationale: "@Library('utils') is a generic name matching several catalog repos",
	},
	{
		ID: "jenkins-lib-ambiguous-1", Kind: EvidenceKindJenkinsSharedLibrary, Label: goldenAmbiguous, GoldenConfidence: 0.712,
		Rationale: "@Library('tools') — short name; degraded match needs corroboration",
	},

	// KUSTOMIZE_HELM_CHART_REFERENCE (prior 0.89; neg 0.7832; ambiguous 0.712)
	{
		ID: "kust-helm-pos-1", Kind: EvidenceKindKustomizeHelmChart, Label: goldenPositive, GoldenConfidence: 0.89,
		Rationale: "helmCharts[].name = 'payments-service' matches one catalog repo by alias",
	},
	{
		ID: "kust-helm-pos-2", Kind: EvidenceKindKustomizeHelmChart, Label: goldenPositive, GoldenConfidence: 0.89,
		Rationale: "helmCharts[].name = 'cert-manager' from a catalog-mapped chart repo",
	},
	{
		ID: "kust-helm-neg-1", Kind: EvidenceKindKustomizeHelmChart, Label: goldenNegative, GoldenConfidence: 0.7832,
		Rationale: "helmCharts[].name = 'common' matches multiple repos",
	},
	{
		ID: "kust-helm-ambiguous-1", Kind: EvidenceKindKustomizeHelmChart, Label: goldenAmbiguous, GoldenConfidence: 0.712,
		Rationale: "helmCharts[].name = 'svc' — too short; degraded match needs corroboration",
	},

	// DOCKER_COMPOSE_IMAGE (prior 0.88; neg 0.7744; ambiguous 0.704)
	{
		ID: "compose-image-pos-1", Kind: EvidenceKindDockerComposeImage, Label: goldenPositive, GoldenConfidence: 0.88,
		Rationale: "image: registry.io/org/payments-service:latest maps to the payments-service repo",
	},
	{
		ID: "compose-image-pos-2", Kind: EvidenceKindDockerComposeImage, Label: goldenPositive, GoldenConfidence: 0.88,
		Rationale: "image: ghcr.io/org/auth-service:main maps to auth-service via alias",
	},
	{
		ID: "compose-image-neg-1", Kind: EvidenceKindDockerComposeImage, Label: goldenNegative, GoldenConfidence: 0.7744,
		Rationale: "image: postgres:14 is a third-party image with no catalog match",
	},
	{
		ID: "compose-image-ambiguous-1", Kind: EvidenceKindDockerComposeImage, Label: goldenAmbiguous, GoldenConfidence: 0.704,
		Rationale: "image: api:latest — short name; degraded match could match several repos",
	},

	// GITHUB_ACTIONS_ACTION_REPOSITORY (prior 0.88; neg 0.7744; ambiguous 0.704)
	{
		ID: "gha-action-repo-pos-1", Kind: EvidenceKindGitHubActionsActionRepository, Label: goldenPositive, GoldenConfidence: 0.88,
		Rationale: "uses: org/deploy-action@v3 names the action repo in the catalog",
	},
	{
		ID: "gha-action-repo-pos-2", Kind: EvidenceKindGitHubActionsActionRepository, Label: goldenPositive, GoldenConfidence: 0.88,
		Rationale: "uses: org/setup-tools@main names an internal tool repo in the catalog",
	},
	{
		ID: "gha-action-repo-neg-1", Kind: EvidenceKindGitHubActionsActionRepository, Label: goldenNegative, GoldenConfidence: 0.7744,
		Rationale: "uses: actions/checkout@v4 is a public third-party action — not a deploy relationship",
	},
	{
		ID: "gha-action-repo-ambiguous-1", Kind: EvidenceKindGitHubActionsActionRepository, Label: goldenAmbiguous, GoldenConfidence: 0.704,
		Rationale: "uses: org/tools@v1 — generic tool repo; degraded match needs corroboration",
	},

	// TERRAGRUNT_CONFIG_ASSET_PATH (prior 0.88; neg 0.7744; ambiguous 0.704)
	{
		ID: "tg-asset-pos-1", Kind: EvidenceKindTerragruntConfigAssetPath, Label: goldenPositive, GoldenConfidence: 0.88,
		Rationale: "local.config_path referencing '../platform-config/modules/ecs' names a catalog repo segment",
	},
	{
		ID: "tg-asset-pos-2", Kind: EvidenceKindTerragruntConfigAssetPath, Label: goldenPositive, GoldenConfidence: 0.88,
		Rationale: "find_in_parent_folders path including 'shared-infra' segment names a config repo",
	},
	{
		ID: "tg-asset-neg-1", Kind: EvidenceKindTerragruntConfigAssetPath, Label: goldenNegative, GoldenConfidence: 0.7744,
		Rationale: "path segment is only 'modules' — a generic directory name shared by many repos",
	},
	{
		ID: "tg-asset-ambiguous-1", Kind: EvidenceKindTerragruntConfigAssetPath, Label: goldenAmbiguous, GoldenConfidence: 0.704,
		Rationale: "path = '../cfg' — very short; degraded match needs corroboration",
	},

	// KUSTOMIZE_IMAGE_REFERENCE (prior 0.86; neg 0.7568; ambiguous 0.688)
	{
		ID: "kust-image-pos-1", Kind: EvidenceKindKustomizeImage, Label: goldenPositive, GoldenConfidence: 0.86,
		Rationale: "images[].name = 'payments-service' with newTag maps to one catalog repo",
	},
	{
		ID: "kust-image-pos-2", Kind: EvidenceKindKustomizeImage, Label: goldenPositive, GoldenConfidence: 0.86,
		Rationale: "images[].name = 'auth-worker' resolves to auth-service repo via alias",
	},
	{
		ID: "kust-image-neg-1", Kind: EvidenceKindKustomizeImage, Label: goldenNegative, GoldenConfidence: 0.7568,
		Rationale: "images[].name = 'nginx' is a third-party image with no catalog match",
	},
	{
		ID: "kust-image-ambiguous-1", Kind: EvidenceKindKustomizeImage, Label: goldenAmbiguous, GoldenConfidence: 0.688,
		Rationale: "images[].name = 'backend' — too generic; degraded match could match several repos",
	},

	// GITHUB_ACTIONS_LOCAL_REUSABLE_WORKFLOW (prior 0.86; neg 0.7568; ambiguous 0.688)
	{
		ID: "gha-local-wf-pos-1", Kind: EvidenceKindGitHubActionsLocalReusableWorkflow, Label: goldenPositive, GoldenConfidence: 0.86,
		Rationale: "uses: ./.github/workflows/deploy.yml in a repo that owns the deploy automation",
	},
	{
		ID: "gha-local-wf-pos-2", Kind: EvidenceKindGitHubActionsLocalReusableWorkflow, Label: goldenPositive, GoldenConfidence: 0.86,
		Rationale: "uses: ./.github/workflows/release.yml in the release-automation repo",
	},
	{
		ID: "gha-local-wf-neg-1", Kind: EvidenceKindGitHubActionsLocalReusableWorkflow, Label: goldenNegative, GoldenConfidence: 0.7568,
		Rationale: "same-repo workflow reference in a utility library that owns no deployment",
	},
	{
		ID: "gha-local-wf-ambiguous-1", Kind: EvidenceKindGitHubActionsLocalReusableWorkflow, Label: goldenAmbiguous, GoldenConfidence: 0.688,
		Rationale: "uses: ./.github/workflows/check.yml — only a CI check workflow; deployment unclear",
	},

	// ---- TierWeakReference (priors 0.82–0.84) ----
	// 0.82 × 0.80 = 0.656 < 0.75; all ambiguous cases valid.

	// HELM_VALUES_REFERENCE (prior 0.84; neg 0.7392; ambiguous 0.672)
	{
		ID: "helm-values-pos-1", Kind: EvidenceKindHelmValues, Label: goldenPositive, GoldenConfidence: 0.84,
		Rationale: "values.yaml repository: org/payments-service is an explicit repo reference",
	},
	{
		ID: "helm-values-pos-2", Kind: EvidenceKindHelmValues, Label: goldenPositive, GoldenConfidence: 0.84,
		Rationale: "values.yaml image.repository: ghcr.io/org/auth-service maps to auth-service repo",
	},
	{
		ID: "helm-values-neg-1", Kind: EvidenceKindHelmValues, Label: goldenNegative, GoldenConfidence: 0.7392,
		Rationale: "values.yaml repository key contains a Helm template placeholder — unresolvable at parse time",
	},
	{
		ID: "helm-values-neg-2", Kind: EvidenceKindHelmValues, Label: goldenNegative, GoldenConfidence: 0.7392,
		Rationale: "values.yaml contains string 'service' as repository — too generic",
	},
	{
		ID: "helm-values-ambiguous-1", Kind: EvidenceKindHelmValues, Label: goldenAmbiguous, GoldenConfidence: 0.672,
		Rationale: "values.yaml image.tag points to a SHA from the target repo but repo name absent",
	},

	// DOCKER_COMPOSE_DEPENDS_ON (prior 0.84; neg 0.7392; ambiguous 0.672)
	{
		ID: "compose-depends-pos-1", Kind: EvidenceKindDockerComposeDependsOn, Label: goldenPositive, GoldenConfidence: 0.84,
		Rationale: "depends_on: payments-service where payments-service is a catalog alias",
	},
	{
		ID: "compose-depends-pos-2", Kind: EvidenceKindDockerComposeDependsOn, Label: goldenPositive, GoldenConfidence: 0.84,
		Rationale: "depends_on: auth-service where auth-service is in the catalog",
	},
	{
		ID: "compose-depends-neg-1", Kind: EvidenceKindDockerComposeDependsOn, Label: goldenNegative, GoldenConfidence: 0.7392,
		Rationale: "depends_on: redis — third-party service, not a catalog repo",
	},
	{
		ID: "compose-depends-neg-2", Kind: EvidenceKindDockerComposeDependsOn, Label: goldenNegative, GoldenConfidence: 0.7392,
		Rationale: "depends_on: db — generic alias not resolvable to a specific catalog repo",
	},
	{
		ID: "compose-depends-ambiguous-1", Kind: EvidenceKindDockerComposeDependsOn, Label: goldenAmbiguous, GoldenConfidence: 0.672,
		Rationale: "depends_on: api — could be several catalog repos; needs corroboration",
	},

	// GCP_CLOUD_RELATIONSHIP (prior 0.82; neg 0.7216; ambiguous 0.656)
	{
		ID: "gcp-cloud-pos-1", Kind: EvidenceKindGCPCloudRelationship, Label: goldenPositive, GoldenConfidence: 0.82,
		Rationale: "GCP resource binding both sides resolve uniquely: payments-service → payments-db",
	},
	{
		ID: "gcp-cloud-pos-2", Kind: EvidenceKindGCPCloudRelationship, Label: goldenPositive, GoldenConfidence: 0.82,
		Rationale: "GCP IAM binding project/org/auth-service-sa resolves source to auth-service repo",
	},
	{
		ID: "gcp-cloud-neg-1", Kind: EvidenceKindGCPCloudRelationship, Label: goldenNegative, GoldenConfidence: 0.7216,
		Rationale: "GCP resource references a project ID with no resolvable catalog repo on either side",
	},
	{
		ID: "gcp-cloud-ambiguous-1", Kind: EvidenceKindGCPCloudRelationship, Label: goldenAmbiguous, GoldenConfidence: 0.656,
		Rationale: "GCP binding source is unique but target resolves to two catalog repos — ambiguous",
	},
}
