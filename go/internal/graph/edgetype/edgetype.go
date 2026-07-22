// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package edgetype

// EdgeType is a Cypher graph relationship (edge) type. Its string value is the
// exact relationship type emitted in Cypher and MUST remain byte-identical to
// the historical literal it replaces.
type EdgeType string

// String returns the raw Cypher relationship type string.
func (e EdgeType) String() string { return string(e) }

const (
	// Aliases is the "ALIASES" graph relationship type.
	Aliases EdgeType = "ALIASES"
	// AllowsEgress is the "ALLOWS_EGRESS" graph relationship type.
	AllowsEgress EdgeType = "ALLOWS_EGRESS"
	// AllowsIngress is the "ALLOWS_INGRESS" graph relationship type.
	AllowsIngress EdgeType = "ALLOWS_INGRESS"
	// AtlantisDependsOn is the "ATLANTIS_DEPENDS_ON" graph relationship type (an
	// Atlantis project to a sibling project it declares in depends_on). It is
	// deliberately distinct from DependsOn so Atlantis apply-ordering is not
	// conflated with repository/package dependency edges by label-agnostic
	// DEPENDS_ON traversals.
	AtlantisDependsOn EdgeType = "ATLANTIS_DEPENDS_ON"
	// Calls is the "CALLS" graph relationship type.
	Calls EdgeType = "CALLS"
	// CanAssume is the "CAN_ASSUME" graph relationship type.
	CanAssume EdgeType = "CAN_ASSUME"
	// CanEscalateTo is the "CAN_ESCALATE_TO" graph relationship type.
	CanEscalateTo EdgeType = "CAN_ESCALATE_TO"
	// CanPerform is the "CAN_PERFORM" graph relationship type.
	CanPerform EdgeType = "CAN_PERFORM"
	// Contains is the "CONTAINS" graph relationship type.
	Contains EdgeType = "CONTAINS"
	// CorrelatesDeployableUnit is the "CORRELATES_DEPLOYABLE_UNIT" graph relationship type.
	CorrelatesDeployableUnit EdgeType = "CORRELATES_DEPLOYABLE_UNIT"
	// DeclaresCodeowner is the "DECLARES_CODEOWNER" graph relationship type (a
	// Repository to the CodeownerTeam a CODEOWNERS rule pattern names as
	// owner, issue #5419 Phase 3).
	DeclaresCodeowner EdgeType = "DECLARES_CODEOWNER"
	// DeclaresDependency is the "DECLARES_DEPENDENCY" graph relationship type.
	DeclaresDependency EdgeType = "DECLARES_DEPENDENCY"
	// Defines is the "DEFINES" graph relationship type.
	Defines EdgeType = "DEFINES"
	// DefinesJob is the "DEFINES_JOB" graph relationship type (a GitLab CI
	// pipeline to a job it declares in .gitlab-ci.yml). It is deliberately
	// distinct from the code-symbol-scoped Defines so a pipeline-to-job edge is
	// never conflated with a code DEFINES traversal.
	DefinesJob EdgeType = "DEFINES_JOB"
	// DependsOn is the "DEPENDS_ON" graph relationship type.
	DependsOn EdgeType = "DEPENDS_ON"
	// DependsOnPackage is the "DEPENDS_ON_PACKAGE" graph relationship type.
	DependsOnPackage EdgeType = "DEPENDS_ON_PACKAGE"
	// DeploymentSource is the "DEPLOYMENT_SOURCE" graph relationship type.
	DeploymentSource EdgeType = "DEPLOYMENT_SOURCE"
	// DeploysFrom is the "DEPLOYS_FROM" graph relationship type.
	DeploysFrom EdgeType = "DEPLOYS_FROM"
	// DiscoversConfigIn is the "DISCOVERS_CONFIG_IN" graph relationship type.
	DiscoversConfigIn EdgeType = "DISCOVERS_CONFIG_IN"
	// Documents is the "DOCUMENTS" graph relationship type.
	Documents EdgeType = "DOCUMENTS"
	// EvidencesRepositoryRelationship is the "EVIDENCES_REPOSITORY_RELATIONSHIP" graph relationship type.
	EvidencesRepositoryRelationship EdgeType = "EVIDENCES_REPOSITORY_RELATIONSHIP"
	// Executes is the "EXECUTES" graph relationship type.
	Executes EdgeType = "EXECUTES"
	// ExecutesShell is the "EXECUTES_SHELL" graph relationship type.
	ExecutesShell EdgeType = "EXECUTES_SHELL"
	// Explains is the "EXPLAINS" graph relationship type.
	Explains EdgeType = "EXPLAINS"
	// ExposesEndpoint is the "EXPOSES_ENDPOINT" graph relationship type.
	ExposesEndpoint EdgeType = "EXPOSES_ENDPOINT"
	// ExtendsBase is the "EXTENDS_BASE" graph relationship type (issue #5445
	// slice 3: a Kustomize overlay's KustomizeOverlay node to the
	// KustomizeOverlay node of a local (same-repo, non-remote) base directory
	// it declares via the deprecated `bases:` field or a directory-shaped
	// `resources:` entry). Same-repo only: a remote Kustomize base resolves
	// through the existing cross-repo DEPLOYS_FROM evidence path instead
	// (go/internal/relationships/kustomize_yaml_evidence.go), never this edge.
	// Many-to-many and cycle-tolerant by construction (Kustomize does not
	// forbid a base cycle at parse time), so any future [:EXTENDS_BASE*]
	// variable-length traversal MUST bound its depth explicitly; an
	// unbounded traversal over a cyclic overlay graph does not terminate.
	ExtendsBase EdgeType = "EXTENDS_BASE"
	// GrantsAccessTo is the "GRANTS_ACCESS_TO" graph relationship type.
	GrantsAccessTo EdgeType = "GRANTS_ACCESS_TO"
	// HandlesRoute is the "HANDLES_ROUTE" graph relationship type.
	HandlesRoute EdgeType = "HANDLES_ROUTE"
	// HasAppliedRouting is the "HAS_APPLIED_ROUTING" graph relationship type.
	HasAppliedRouting EdgeType = "HAS_APPLIED_ROUTING"
	// HasColumn is the "HAS_COLUMN" graph relationship type.
	HasColumn EdgeType = "HAS_COLUMN"
	// HasDeploymentEvidence is the "HAS_DEPLOYMENT_EVIDENCE" graph relationship type.
	HasDeploymentEvidence EdgeType = "HAS_DEPLOYMENT_EVIDENCE"
	// HasIntendedRouting is the "HAS_INTENDED_ROUTING" graph relationship type.
	HasIntendedRouting EdgeType = "HAS_INTENDED_ROUTING"
	// HasLiveRouting is the "HAS_LIVE_ROUTING" graph relationship type.
	HasLiveRouting EdgeType = "HAS_LIVE_ROUTING"
	// HasParameter is the "HAS_PARAMETER" graph relationship type.
	HasParameter EdgeType = "HAS_PARAMETER"
	// HasRole is the "HAS_ROLE" graph relationship type.
	HasRole EdgeType = "HAS_ROLE"
	// HasTaintEvidence is the "HAS_TAINT_EVIDENCE" graph relationship type.
	HasTaintEvidence EdgeType = "HAS_TAINT_EVIDENCE"
	// HasVersion is the "HAS_VERSION" graph relationship type.
	HasVersion EdgeType = "HAS_VERSION"
	// HelmValueReference is the "HELM_VALUE_REFERENCE" graph relationship type:
	// a Helm chart template `.Values.<path>` usage references the matching
	// values.yaml leaf definition. Dedicated (not the shared REFERENCES type) so
	// the full-refresh retract's delete-index stays small (#4476).
	HelmValueReference EdgeType = "HELM_VALUE_REFERENCE"
	// Implements is the "IMPLEMENTS" graph relationship type.
	Implements EdgeType = "IMPLEMENTS"
	// Imports is the "IMPORTS" graph relationship type.
	Imports EdgeType = "IMPORTS"
	// Indexes is the "INDEXES" graph relationship type.
	Indexes EdgeType = "INDEXES"
	// Inherits is the "INHERITS" graph relationship type.
	Inherits EdgeType = "INHERITS"
	// InstanceOf is the "INSTANCE_OF" graph relationship type.
	InstanceOf EdgeType = "INSTANCE_OF"
	// Instantiates is the "INSTANTIATES" graph relationship type.
	Instantiates EdgeType = "INSTANTIATES"
	// InvokesCloudAction is the "INVOKES_CLOUD_ACTION" graph relationship type.
	InvokesCloudAction EdgeType = "INVOKES_CLOUD_ACTION"
	// LogsTo is the "LOGS_TO" graph relationship type.
	LogsTo EdgeType = "LOGS_TO"
	// Manages is the "MANAGES" graph relationship type (an Atlantis project to the
	// Terraform Directory it plans/applies).
	Manages EdgeType = "MANAGES"
	// MapsToTable is the "MAPS_TO_TABLE" graph relationship type.
	MapsToTable EdgeType = "MAPS_TO_TABLE"
	// MatchesState is the "MATCHES_STATE" graph relationship type (#5443: a
	// config-declared TerraformResource to the TerraformStateResource it
	// matches by exact address equality).
	MatchesState EdgeType = "MATCHES_STATE"
	// Needs is the "NEEDS" graph relationship type (a GitLab CI job to a sibling
	// job it declares in needs/dependencies, resolved within the same
	// .gitlab-ci.yml). It is distinct from DependsOn so CI job ordering is never
	// conflated with repository/package dependency edges.
	Needs EdgeType = "NEEDS"
	// Migrates is the "MIGRATES" graph relationship type.
	Migrates EdgeType = "MIGRATES"
	// Overrides is the "OVERRIDES" graph relationship type.
	Overrides EdgeType = "OVERRIDES"
	// PinsSubmodule is the "PINS_SUBMODULE" graph relationship type (a parent
	// Repository to the Repository it pins as a git submodule at a specific
	// commit SHA, issue #5420).
	PinsSubmodule EdgeType = "PINS_SUBMODULE"
	// ProvisionsDependencyFor is the "PROVISIONS_DEPENDENCY_FOR" graph relationship type.
	ProvisionsDependencyFor EdgeType = "PROVISIONS_DEPENDENCY_FOR"
	// ProvisionsPlatform is the "PROVISIONS_PLATFORM" graph relationship type.
	ProvisionsPlatform EdgeType = "PROVISIONS_PLATFORM"
	// QueriesTable is the "QUERIES_TABLE" graph relationship type.
	QueriesTable EdgeType = "QUERIES_TABLE"
	// ReadsConfigFrom is the "READS_CONFIG_FROM" graph relationship type.
	ReadsConfigFrom EdgeType = "READS_CONFIG_FROM"
	// ReadsFrom is the "READS_FROM" graph relationship type.
	ReadsFrom EdgeType = "READS_FROM"
	// ReconcilesFrom is the "RECONCILES_FROM" graph relationship type (a Flux
	// Kustomization to the GitRepository/OCIRepository/Bucket source CR its
	// spec.sourceRef resolves to, issue #5360 PR B).
	ReconcilesFrom EdgeType = "RECONCILES_FROM"
	// References is the "REFERENCES" graph relationship type.
	References EdgeType = "REFERENCES"
	// ReferencesTable is the "REFERENCES_TABLE" graph relationship type.
	ReferencesTable EdgeType = "REFERENCES_TABLE"
	// RepoContains is the "REPO_CONTAINS" graph relationship type.
	RepoContains EdgeType = "REPO_CONTAINS"
	// RunsImage is the "RUNS_IMAGE" graph relationship type.
	RunsImage EdgeType = "RUNS_IMAGE"
	// RunsIn is the "RUNS_IN" graph relationship type.
	RunsIn EdgeType = "RUNS_IN"
	// RunsOn is the "RUNS_ON" graph relationship type.
	RunsOn EdgeType = "RUNS_ON"
	// SatisfiedBy is the "SATISFIED_BY" graph relationship type.
	SatisfiedBy EdgeType = "SATISFIED_BY"
	// SecretsIamAssumesIamRole is the "SECRETS_IAM_ASSUMES_IAM_ROLE" graph relationship type.
	SecretsIamAssumesIamRole EdgeType = "SECRETS_IAM_ASSUMES_IAM_ROLE" // #nosec G101 -- graph edge-type label constant, not a credential
	// SecretsIamAuthenticatesToVaultRole is the "SECRETS_IAM_AUTHENTICATES_TO_VAULT_ROLE" graph relationship type.
	SecretsIamAuthenticatesToVaultRole EdgeType = "SECRETS_IAM_AUTHENTICATES_TO_VAULT_ROLE" // #nosec G101 -- graph edge-type label constant, not a credential
	// SecretsIamGrantsSecretRead is the "SECRETS_IAM_GRANTS_SECRET_READ" graph relationship type.
	SecretsIamGrantsSecretRead EdgeType = "SECRETS_IAM_GRANTS_SECRET_READ"
	// SecretsIamUsesServiceAccount is the "SECRETS_IAM_USES_SERVICE_ACCOUNT" graph relationship type.
	SecretsIamUsesServiceAccount EdgeType = "SECRETS_IAM_USES_SERVICE_ACCOUNT"
	// SecretsIamUsesVaultPolicy is the "SECRETS_IAM_USES_VAULT_POLICY" graph relationship type.
	SecretsIamUsesVaultPolicy EdgeType = "SECRETS_IAM_USES_VAULT_POLICY"
	// TaintFlowsTo is the "TAINT_FLOWS_TO" graph relationship type.
	TaintFlowsTo EdgeType = "TAINT_FLOWS_TO"
	// TargetsEnvironment is the "TARGETS_ENVIRONMENT" graph relationship type.
	TargetsEnvironment EdgeType = "TARGETS_ENVIRONMENT"
	// To is the "TO" graph relationship type.
	To EdgeType = "TO"
	// Triggers is the "TRIGGERS" graph relationship type.
	Triggers EdgeType = "TRIGGERS"
	// TriggersOn is the "TRIGGERS_ON" graph relationship type.
	TriggersOn EdgeType = "TRIGGERS_ON"
	// WritesTo is the "WRITES_TO" graph relationship type.
	WritesTo EdgeType = "WRITES_TO"
	// Uses is the "USES" graph relationship type.
	Uses EdgeType = "USES"
	// UsesMetaclass is the "USES_METACLASS" graph relationship type.
	UsesMetaclass EdgeType = "USES_METACLASS"
	// UsesModule is the "USES_MODULE" graph relationship type.
	UsesModule EdgeType = "USES_MODULE"
	// UsesProfile is the "USES_PROFILE" graph relationship type.
	UsesProfile EdgeType = "USES_PROFILE"
	// UsesWorkflow is the "USES_WORKFLOW" graph relationship type (an Atlantis
	// project to the custom workflow it names).
	UsesWorkflow EdgeType = "USES_WORKFLOW"
)

// registered lists every edge type known to the registry. Order is not
// significant; All returns a defensive copy.
var registered = []EdgeType{
	Aliases, AllowsEgress, AllowsIngress, AtlantisDependsOn, Calls,
	CanAssume, CanEscalateTo, CanPerform, Contains,
	CorrelatesDeployableUnit, DeclaresCodeowner, DeclaresDependency, Defines, DefinesJob, DependsOn,
	DependsOnPackage, DeploymentSource, DeploysFrom, DiscoversConfigIn,
	Documents, EvidencesRepositoryRelationship, Executes, ExecutesShell,
	Explains, ExposesEndpoint, ExtendsBase, GrantsAccessTo, HandlesRoute,
	HasAppliedRouting, HasColumn, HasDeploymentEvidence, HasIntendedRouting,
	HasLiveRouting, HasParameter, HasRole, HasTaintEvidence,
	HasVersion, HelmValueReference, Implements, Imports, Indexes,
	Inherits, InstanceOf, Instantiates, InvokesCloudAction,
	LogsTo, Manages, MapsToTable, MatchesState, Migrates, Needs, Overrides,
	PinsSubmodule,
	ProvisionsDependencyFor, ProvisionsPlatform, QueriesTable, ReadsConfigFrom,
	ReadsFrom, ReconcilesFrom, References, ReferencesTable, RepoContains,
	RunsImage, RunsIn, RunsOn, SatisfiedBy,
	SecretsIamAssumesIamRole, SecretsIamAuthenticatesToVaultRole, SecretsIamGrantsSecretRead, SecretsIamUsesServiceAccount,
	SecretsIamUsesVaultPolicy, TaintFlowsTo, TargetsEnvironment, To,
	Triggers, TriggersOn, WritesTo, Uses, UsesMetaclass,
	UsesModule, UsesProfile, UsesWorkflow,
}

// set indexes registered edge-type strings for O(1) membership checks.
var set = func() map[string]struct{} {
	m := make(map[string]struct{}, len(registered))
	for _, e := range registered {
		m[string(e)] = struct{}{}
	}
	return m
}()

// All returns a defensive copy of every registered edge type.
func All() []EdgeType {
	out := make([]EdgeType, len(registered))
	copy(out, registered)
	return out
}

// IsRegistered reports whether s is a known graph edge-type string.
func IsRegistered(s string) bool {
	_, ok := set[s]
	return ok
}
