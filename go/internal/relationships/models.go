// Package relationships implements evidence-backed relationship resolution
// for cross-repository and cross-entity dependency discovery.
package relationships

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"
)

// EvidenceKind classifies the origin of one piece of relationship evidence.
type EvidenceKind string

const (
	// EvidenceKindTerraformAppRepo is a Terraform app_repo reference.
	EvidenceKindTerraformAppRepo EvidenceKind = "TERRAFORM_APP_REPO"
	// EvidenceKindTerraformAppName is a Terraform app_name reference.
	EvidenceKindTerraformAppName EvidenceKind = "TERRAFORM_APP_NAME"
	// EvidenceKindTerraformGitHubRepo is a Terraform GitHub repository reference.
	EvidenceKindTerraformGitHubRepo EvidenceKind = "TERRAFORM_GITHUB_REPOSITORY"
	// EvidenceKindTerraformGitHubActions is a Terraform GitHub Actions subject reference.
	EvidenceKindTerraformGitHubActions EvidenceKind = "TERRAFORM_GITHUB_ACTIONS_REPOSITORY"
	// EvidenceKindTerraformConfigPath is a Terraform config path reference.
	EvidenceKindTerraformConfigPath EvidenceKind = "TERRAFORM_CONFIG_PATH"
	// EvidenceKindTerraformIAMPermission is a Terraform IAM permission reference.
	EvidenceKindTerraformIAMPermission EvidenceKind = "TERRAFORM_IAM_PERMISSION"
	// EvidenceKindTerraformModuleSource is a Terraform or Terragrunt module source reference.
	EvidenceKindTerraformModuleSource EvidenceKind = "TERRAFORM_MODULE_SOURCE"
	// EvidenceKindTerragruntDependencyConfigPath is a Terragrunt dependency config_path reference.
	EvidenceKindTerragruntDependencyConfigPath EvidenceKind = "TERRAGRUNT_DEPENDENCY_CONFIG_PATH"
	// EvidenceKindTerragruntConfigAssetPath is a Terragrunt helper or local config asset reference.
	EvidenceKindTerragruntConfigAssetPath EvidenceKind = "TERRAGRUNT_CONFIG_ASSET_PATH"
	// EvidenceKindHelmChart is a Helm Chart.yaml reference.
	EvidenceKindHelmChart EvidenceKind = "HELM_CHART_REFERENCE"
	// EvidenceKindHelmValues is a Helm values reference.
	EvidenceKindHelmValues EvidenceKind = "HELM_VALUES_REFERENCE"
	// EvidenceKindArgoCDAppSource is an ArgoCD Application source reference.
	EvidenceKindArgoCDAppSource EvidenceKind = "ARGOCD_APPLICATION_SOURCE"
	// EvidenceKindArgoCDApplicationSetDiscovery is an ApplicationSet discovery reference.
	EvidenceKindArgoCDApplicationSetDiscovery EvidenceKind = "ARGOCD_APPLICATIONSET_DISCOVERY"
	// EvidenceKindArgoCDApplicationSetDeploySource is an ApplicationSet deploy-source reference.
	EvidenceKindArgoCDApplicationSetDeploySource EvidenceKind = "ARGOCD_APPLICATIONSET_DEPLOY_SOURCE"
	// EvidenceKindArgoCDDestinationPlatform is an ApplicationSet destination-platform reference.
	EvidenceKindArgoCDDestinationPlatform EvidenceKind = "ARGOCD_DESTINATION_PLATFORM"
	// EvidenceKindGitHubActionsReusableWorkflow is a reusable workflow repo reference.
	EvidenceKindGitHubActionsReusableWorkflow EvidenceKind = "GITHUB_ACTIONS_REUSABLE_WORKFLOW"
	// EvidenceKindGitHubActionsCheckoutRepository is an explicit checkout repo reference.
	EvidenceKindGitHubActionsCheckoutRepository EvidenceKind = "GITHUB_ACTIONS_CHECKOUT_REPOSITORY"
	// EvidenceKindGitHubActionsWorkflowInputRepository is an explicit repo-bearing workflow input reference.
	EvidenceKindGitHubActionsWorkflowInputRepository EvidenceKind = "GITHUB_ACTIONS_WORKFLOW_INPUT_REPOSITORY"
	// EvidenceKindGitHubActionsActionRepository is a step-level GitHub Action repository reference.
	EvidenceKindGitHubActionsActionRepository EvidenceKind = "GITHUB_ACTIONS_ACTION_REPOSITORY"
	// EvidenceKindGitHubActionsLocalReusableWorkflow is a repo-local reusable workflow reference.
	EvidenceKindGitHubActionsLocalReusableWorkflow EvidenceKind = "GITHUB_ACTIONS_LOCAL_REUSABLE_WORKFLOW"
	// EvidenceKindJenkinsSharedLibrary is a Jenkins shared library reference.
	EvidenceKindJenkinsSharedLibrary EvidenceKind = "JENKINS_SHARED_LIBRARY"
	// EvidenceKindJenkinsGitHubRepository is an explicit GitHub repo URL inside Jenkins automation.
	EvidenceKindJenkinsGitHubRepository EvidenceKind = "JENKINS_GITHUB_REPOSITORY"
	// EvidenceKindDockerComposeBuildContext is a Compose build context repo reference.
	EvidenceKindDockerComposeBuildContext EvidenceKind = "DOCKER_COMPOSE_BUILD_CONTEXT"
	// EvidenceKindDockerComposeImage is a Compose image repo reference.
	EvidenceKindDockerComposeImage EvidenceKind = "DOCKER_COMPOSE_IMAGE"
	// EvidenceKindDockerComposeDependsOn is a Compose service dependency reference.
	EvidenceKindDockerComposeDependsOn EvidenceKind = "DOCKER_COMPOSE_DEPENDS_ON"
	// EvidenceKindDockerfileSourceLabel is an explicit repo-bearing Dockerfile source label reference.
	EvidenceKindDockerfileSourceLabel EvidenceKind = "DOCKERFILE_SOURCE_LABEL"
	// EvidenceKindKustomizeResource is a Kustomize resource reference.
	EvidenceKindKustomizeResource EvidenceKind = "KUSTOMIZE_RESOURCE_REFERENCE"
	// EvidenceKindKustomizeHelmChart is a Kustomize Helm chart reference.
	EvidenceKindKustomizeHelmChart EvidenceKind = "KUSTOMIZE_HELM_CHART_REFERENCE"
	// EvidenceKindKustomizeImage is a Kustomize image reference.
	EvidenceKindKustomizeImage EvidenceKind = "KUSTOMIZE_IMAGE_REFERENCE"
	// EvidenceKindAnsibleRoleReference is an Ansible playbook role reference.
	EvidenceKindAnsibleRoleReference EvidenceKind = "ANSIBLE_ROLE_REFERENCE"
)

// RelationshipType classifies the kind of link between two entities.
type RelationshipType string

const (
	// RelDeploysFrom indicates the source deploys artifacts from the target.
	RelDeploysFrom RelationshipType = "DEPLOYS_FROM"
	// RelDiscoversConfigIn indicates the source discovers config in the target.
	RelDiscoversConfigIn RelationshipType = "DISCOVERS_CONFIG_IN"
	// RelRunsOn indicates the source runs on the target platform.
	RelRunsOn RelationshipType = "RUNS_ON"
	// RelProvisionsDependencyFor indicates the source provisions infra for the target.
	RelProvisionsDependencyFor RelationshipType = "PROVISIONS_DEPENDENCY_FOR"
	// RelDependsOn is a generic dependency relationship.
	RelDependsOn RelationshipType = "DEPENDS_ON"
	// RelUsesModule indicates the source consumes a target module repository.
	RelUsesModule RelationshipType = "USES_MODULE"
	// RelReadsConfigFrom indicates the source is granted read access to target config.
	RelReadsConfigFrom RelationshipType = "READS_CONFIG_FROM"
)

// ResolutionSource classifies how a resolved relationship was determined.
type ResolutionSource string

const (
	// ResolutionSourceInferred means the relationship was resolved from evidence.
	ResolutionSourceInferred ResolutionSource = "inferred"
	// ResolutionSourceAssertion means the relationship was explicitly asserted.
	ResolutionSourceAssertion ResolutionSource = "assertion"
)

// EvidenceFact is one raw observed fact supporting a relationship candidate.
type EvidenceFact struct {
	EvidenceKind     EvidenceKind
	RelationshipType RelationshipType
	SourceRepoID     string
	TargetRepoID     string
	SourceEntityID   string
	TargetEntityID   string
	Confidence       float64
	Rationale        string
	Details          map[string]any
}

// Assertion is one explicit human or control-plane assertion about a relationship.
type Assertion struct {
	SourceRepoID     string
	TargetRepoID     string
	SourceEntityID   string
	TargetEntityID   string
	RelationshipType RelationshipType
	Decision         string // "assert" or "reject"
	Reason           string
	Actor            string
}

// Candidate is one machine-generated relationship candidate built from evidence.
type Candidate struct {
	SourceRepoID     string
	TargetRepoID     string
	SourceEntityID   string
	TargetEntityID   string
	RelationshipType RelationshipType
	Confidence       float64
	EvidenceCount    int
	Rationale        string
	Details          map[string]any
}

// ResolvedRelationship is one canonical relationship emitted by the resolver.
type ResolvedRelationship struct {
	SourceRepoID     string
	TargetRepoID     string
	SourceEntityID   string
	TargetEntityID   string
	RelationshipType RelationshipType
	Confidence       float64
	EvidenceCount    int
	Rationale        string
	ResolutionSource ResolutionSource
	Details          map[string]any
}

// ResolvedRelationshipID builds the durable Postgres identity for a resolved
// relationship in a generation. The ordinal preserves the current storage
// contract while still giving graph edges a stable pointer back to Postgres.
func ResolvedRelationshipID(generationID string, r ResolvedRelationship, ordinal int) string {
	return relationshipDigest("resolved", generationID,
		r.SourceEntityID, r.TargetEntityID,
		string(r.RelationshipType), fmt.Sprintf("%d", ordinal),
	)
}

// Generation tracks one resolution generation lifecycle.
type Generation struct {
	GenerationID string
	Scope        string
	RunID        string
	Status       string
}

// entityIdentity returns an entity identity from entity ID or repository ID.
func entityIdentity(entityID, repoID string) string {
	if entityID != "" {
		return entityID
	}
	return repoID
}

// relationshipDigest mirrors the storage-layer digest contract for relationship
// identifiers that need to be generated before persistence.
func relationshipDigest(prefix string, parts ...string) string {
	normalized := make([]string, len(parts))
	for i, part := range parts {
		if part == "" {
			normalized[i] = "<none>"
		} else {
			normalized[i] = part
		}
	}
	h := sha1.New()
	h.Write([]byte(strings.Join(normalized, "\n")))
	digest := hex.EncodeToString(h.Sum(nil))[:16]
	return fmt.Sprintf("%s_%s", prefix, digest)
}
