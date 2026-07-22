// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package reducer defines the durable cross-source and cross-scope reducer
// substrate used by the Go data plane.
package reducer

import "errors"

// Domain identifies a canonical shared-truth reducer domain.
type Domain string

const (
	// DomainWorkloadIdentity resolves canonical workload identity.
	DomainWorkloadIdentity Domain = "workload_identity"
	// DomainDeployableUnitCorrelation correlates cross-source deployable-unit
	// evidence before workload admission and materialization.
	DomainDeployableUnitCorrelation Domain = "deployable_unit_correlation"
	// DomainCloudAssetResolution resolves canonical cloud asset identity.
	DomainCloudAssetResolution Domain = "cloud_asset_resolution"
	// DomainDeploymentMapping resolves deployment relationships.
	DomainDeploymentMapping Domain = "deployment_mapping"
	// DomainDataLineage resolves lineage across sources and scopes.
	DomainDataLineage Domain = "data_lineage"
	// DomainCodeTaintEvidence projects value-flow taint findings into graph
	// evidence nodes attached to their Function.
	DomainCodeTaintEvidence Domain = "code_taint_evidence"
	// DomainCodeInterprocEvidence projects cross-function value-flow findings into
	// TAINT_FLOWS_TO edges between the source and sink Function nodes.
	DomainCodeInterprocEvidence Domain = "code_interproc_evidence"
	// DomainCodeFunctionSummary persists each function's durable value-flow
	// summary (its structural Effects) to the function-summary store so the
	// interprocedural fixpoint can recompose summaries across runs and repos.
	DomainCodeFunctionSummary Domain = "code_function_summary"
	// DomainOwnership resolves ownership and responsibility records.
	DomainOwnership Domain = "ownership"
	// DomainGovernance resolves governance and policy attribution.
	DomainGovernance Domain = "governance"
	// DomainWorkloadMaterialization materializes canonical workload graph nodes.
	DomainWorkloadMaterialization Domain = "workload_materialization"
	// DomainCodeCallMaterialization materializes canonical code call edges.
	DomainCodeCallMaterialization Domain = "code_call_materialization"
	// DomainPlatformInfraMaterialization extracts Terraform/terragrunt IaC
	// platform-provisioning signals from a repository's facts and emits
	// platform_infra shared-projection intents, which the shared worker writes as
	// Repository-[:PROVISIONS_PLATFORM]->Platform edges. It owns the
	// infrastructure-provisioning verb on its own dedicated trigger rather than
	// riding the deployment_mapping handler as a side-effect.
	DomainPlatformInfraMaterialization Domain = "platform_infra_materialization"
	// DomainSemanticEntityMaterialization materializes Annotation, Typedef,
	// TypeAlias, and Component semantic nodes.
	DomainSemanticEntityMaterialization Domain = "semantic_entity_materialization"
	// DomainSQLRelationshipMaterialization materializes canonical SQL
	// relationship edges (READS_FROM, HAS_COLUMN, TRIGGERS).
	DomainSQLRelationshipMaterialization Domain = "sql_relationship_materialization"
	// DomainShellExecMaterialization materializes parser command-execution call
	// evidence into canonical shell-exec graph edges.
	DomainShellExecMaterialization Domain = "shell_exec_materialization"
	// DomainInheritanceMaterialization materializes canonical inheritance,
	// override, and alias edges from parser entity bases and trait adaptation
	// metadata.
	DomainInheritanceMaterialization Domain = "inheritance_materialization"
	// DomainDocumentationMaterialization materializes canonical DOCUMENTS edges
	// from exact documentation entity mentions to the code entities or workloads
	// they resolve to.
	DomainDocumentationMaterialization Domain = "documentation_materialization"
	// DomainRationaleMaterialization materializes canonical EXPLAINS edges from
	// intent-comment rationale to the code entities they precede.
	DomainRationaleMaterialization Domain = "rationale_materialization"
	// DomainCodeownersOwnership materializes canonical DECLARES_CODEOWNER edges
	// from directly-emitted codeowners.ownership facts to the CodeownerTeam a
	// CODEOWNERS rule pattern names (issue #5419 Phase 3). One rule with N
	// owners projects N edges (owners are per-rule), riding the shared-projection
	// intent-queue path the same way DomainDocumentationMaterialization does.
	DomainCodeownersOwnership Domain = "codeowners_ownership"
	// DomainConfigStateDrift correlates Terraform config (parsed HCL) against
	// Terraform state to detect five drift kinds. Cross-source, cross-scope,
	// non-canonical-write — counters and structured logs are the v1 surface.
	// Current proof gates are documented in docs/public/reference/local-testing.md
	// under "Terraform Config-vs-State Drift Compose Proofs".
	DomainConfigStateDrift Domain = "config_state_drift"
	// DomainPackageSourceCorrelation classifies package-registry source hints
	// against active repository remotes without promoting package ownership.
	DomainPackageSourceCorrelation Domain = "package_source_correlation"
	// DomainCodeImportRepoEdge projects repo-to-repo DEPENDS_ON edges from
	// per-file external import sources correlated to package-registry ownership.
	// It runs in the git-repository scope so the per-file import facts are
	// scope-local, and resolves owners from cross-scope package-registry facts
	// through the same (ecosystem, name) join the package-consumption path uses
	// (issue #3642).
	DomainCodeImportRepoEdge Domain = "code_import_repo_edge"
	// DomainContainerImageIdentity joins Git, registry, and runtime image
	// evidence into digest-keyed container image identity decisions.
	DomainContainerImageIdentity Domain = "container_image_identity"
	// DomainCICDRunCorrelation correlates provider CI/CD runs, artifacts, and
	// environment observations with reducer-owned artifact identity evidence.
	DomainCICDRunCorrelation Domain = "ci_cd_run_correlation"
	// DomainServiceCatalogCorrelation correlates service-catalog entity
	// declarations with repository and ownership evidence without letting
	// catalog names create workloads.
	DomainServiceCatalogCorrelation Domain = "service_catalog_correlation"
	// DomainSBOMAttestationAttachment attaches SBOM and attestation evidence to
	// image digests only when subject evidence is explicit.
	DomainSBOMAttestationAttachment Domain = "sbom_attestation_attachment"
	// DomainSupplyChainImpact publishes reducer-owned vulnerability impact
	// findings only when vulnerability, package, SBOM, image, or repository
	// evidence forms an explicit path.
	DomainSupplyChainImpact Domain = "supply_chain_impact"
	// DomainSecurityAlertReconciliation compares provider-reported repository
	// security alerts against Eshu-owned dependency and impact evidence without
	// promoting provider alerts into impact truth.
	DomainSecurityAlertReconciliation Domain = "security_alert_reconciliation"
	// DomainSecretsIAMTrustChain builds reducer-owned secrets/IAM read models
	// from AWS IAM, Kubernetes ServiceAccount/RBAC, and Vault metadata source
	// facts. It writes durable reducer facts only: no graph labels, edges, or DDL
	// are part of this domain.
	DomainSecretsIAMTrustChain Domain = "secrets_iam_trust_chain" // #nosec G101 -- domain name identifier, not a credential
	// DomainAWSCloudRuntimeDrift publishes admitted AWS runtime-vs-IaC drift
	// findings as canonical reducer facts. The domain stays graph-neutral until
	// the drift node and query shape are frozen.
	DomainAWSCloudRuntimeDrift Domain = "aws_cloud_runtime_drift"
	// DomainMultiCloudRuntimeDrift publishes admitted provider-neutral
	// runtime-vs-IaC drift findings keyed on canonical cloud_resource_uid for
	// AWS, GCP, and Azure (issues #1997, #1998). It mirrors
	// DomainAWSCloudRuntimeDrift but joins on the shared identity keyspace so the
	// orphaned/unmanaged/ambiguous/unknown vocabulary is shared across providers.
	// The domain stays graph-neutral until the drift node and query shape freeze.
	DomainMultiCloudRuntimeDrift Domain = "multi_cloud_runtime_drift"
	// DomainAWSResourceMaterialization materializes aws_resource facts into
	// canonical CloudResource graph nodes. It is the node substrate the AWS
	// relationship edge projection (issue #805) joins against; see
	// docs/internal/aws-relationship-edge-materialization-design.md.
	DomainAWSResourceMaterialization Domain = "aws_resource_materialization"
	// DomainGCPResourceMaterialization materializes gcp_cloud_resource facts into
	// canonical CloudResource graph nodes, mirroring DomainAWSResourceMaterialization
	// for GCP. It is the node substrate the GCP relationship edge projection
	// (issue #2348) joins against and publishes the
	// GraphProjectionPhaseCanonicalNodesCommitted readiness phase under its own
	// distinct entity key (gcp_resource_materialization:<scope>) so the GCP edge
	// stage gates on GCP node readiness independently of the AWS node phase. See
	// docs/internal/gcp-cloud-resource-materialization-design.md.
	DomainGCPResourceMaterialization Domain = "gcp_resource_materialization"
	// DomainAzureResourceMaterialization materializes azure_cloud_resource facts
	// into canonical CloudResource graph nodes. It is the node substrate the
	// Azure relationship edge projection joins against and publishes the
	// GraphProjectionPhaseCanonicalNodesCommitted readiness phase under
	// azure_resource_materialization:<scope> so Azure edges never race AWS or GCP
	// node readiness.
	DomainAzureResourceMaterialization Domain = "azure_resource_materialization"
	// DomainGCPRelationshipMaterialization projects gcp_cloud_relationship facts
	// into canonical GCP relationship edges between the CloudResource nodes that
	// DomainGCPResourceMaterialization committed. It gates on the
	// GraphProjectionPhaseCanonicalNodesCommitted readiness phase (the shared
	// gcp_resource_materialization:<scope> acceptance unit) so edges never resolve
	// against nodes that have not committed, mirroring
	// DomainAWSRelationshipMaterialization for GCP (issue #2348). Endpoints resolve
	// by the globally-unique CAI full_resource_name; only supported relationships
	// materialize (partial/unsupported are provenance only). See
	// docs/internal/gcp-cloud-relationship-edge-materialization-design.md.
	DomainGCPRelationshipMaterialization Domain = "gcp_relationship_materialization"
	// DomainAzureRelationshipMaterialization projects azure_cloud_relationship
	// facts into canonical Azure relationship edges between CloudResource nodes
	// committed by DomainAzureResourceMaterialization. Endpoints resolve by exact
	// normalized ARM resource id; partial, unsupported, unresolved, invalid-type,
	// and self-loop evidence stays provenance-only.
	DomainAzureRelationshipMaterialization Domain = "azure_relationship_materialization"
	// DomainWorkloadCloudRelationshipMaterialization projects exact
	// reducer-owned service/workload anchors on CloudResource facts into
	// canonical WorkloadInstance USES CloudResource graph edges. Queue claiming
	// gates on CloudResource node readiness; the graph writer still uses
	// MATCH-only endpoint anchoring so missing workload instances are a no-op
	// instead of fabricated graph truth.
	DomainWorkloadCloudRelationshipMaterialization Domain = "workload_cloud_relationship_materialization"
	// DomainEC2InstanceNodeMaterialization materializes ec2_instance_posture facts
	// into canonical :CloudResource graph nodes on the existing cloud_resource_uid
	// keyspace (issue #1146 PR-A). The EC2 scanner deliberately does not emit an
	// aws_resource inventory fact for instances, so this domain is the only path
	// that materializes an EC2 instance as a node. After the node write succeeds it
	// publishes the GraphProjectionKeyspaceCloudResourceUID /
	// GraphProjectionPhaseCanonicalNodesCommitted readiness phase under its own
	// distinct entity key (ec2_instance_node_materialization:<scope>), so the later
	// USES_PROFILE edge slice (#1146 PR-B) gates on instance-node readiness
	// independently of the aws_resource node phase, exactly like the security-group
	// reachability edge gates on multiple node phases (#1135). See issue #1146 and
	// docs/internal/design/1146-ec2-instance-node.md.
	DomainEC2InstanceNodeMaterialization Domain = "ec2_instance_node_materialization"
	// DomainAWSRelationshipMaterialization projects aws_relationship facts into
	// canonical AWS relationship edges between the CloudResource nodes that
	// DomainAWSResourceMaterialization committed. It gates on the
	// GraphProjectionPhaseCanonicalNodesCommitted readiness phase so edges never
	// resolve against nodes that have not committed (issue #805 PR 2); see
	// docs/internal/aws-relationship-edge-materialization-design.md §5–§8.
	DomainAWSRelationshipMaterialization Domain = "aws_relationship_materialization"
	// DomainObservabilityCoverageCorrelation correlates which monitored
	// CloudResource nodes have observability coverage (CloudWatch alarms,
	// dashboards, log groups, X-Ray) versus which are uncovered, emitting durable
	// provenance-only reducer facts with the six-outcome contract. It is
	// cross-source (observability object vs. the resource it covers) and
	// cross-scope (a resource in one scan scope may be covered by an alarm
	// discovered in another). PR1 writes facts only; the optional COVERS graph
	// edge is a later gated PR. See issue #391 for the design.
	DomainObservabilityCoverageCorrelation Domain = "observability_coverage_correlation"
	// DomainObservabilityCoverageMaterialization projects the exact-outcome
	// observability coverage decisions into canonical COVERS edges between the
	// CloudResource nodes that DomainAWSResourceMaterialization committed: an
	// observability object (alarm/dashboard/log group/X-Ray) covering a monitored
	// resource. It gates on the GraphProjectionPhaseCanonicalNodesCommitted
	// readiness phase so edges never resolve against nodes that have not committed
	// (issue #391 PR3), exactly like DomainAWSRelationshipMaterialization. Only
	// exact coverage with a resolved target uid materializes an edge; derived,
	// ambiguous, unresolved, stale, and rejected coverage stays provenance-only in
	// the PR1 read model and fabricates no edge. See issue #391 for the design.
	DomainObservabilityCoverageMaterialization Domain = "observability_coverage_materialization"
	// DomainKubernetesCorrelation correlates live Kubernetes workload evidence
	// (kubernetes_live.* facts) against deployment-source image and identity
	// evidence, emitting durable provenance-only reducer facts with the
	// six-outcome contract plus a drift classification. Live image refs join
	// digest-first then repository+tag; a label-selector edge that cannot prove
	// exact ownership stays ambiguous and is never promoted to exact. It is
	// cross-source (live cluster vs. registry/Git/IaC source) and cross-scope
	// (live facts live in a cluster scope, source facts in repo/cloud scopes).
	// PR1 writes facts only; the gated canonical graph edge is a later PR. See
	// issue #388 for the design.
	DomainKubernetesCorrelation Domain = "kubernetes_correlation"
	// DomainKubernetesWorkloadMaterialization materializes
	// kubernetes_live.pod_template facts into canonical KubernetesWorkload graph
	// nodes keyed by the collector-emitted object_id. It is the live-workload node
	// substrate that the #388 live-workload edge projection (PR3) joins against;
	// the edge resolves a workload's deployment-source identity to these nodes in a
	// separate, gated stage. After the node write succeeds it publishes the
	// GraphProjectionKeyspaceKubernetesWorkloadUID /
	// GraphProjectionPhaseCanonicalNodesCommitted readiness phase so the later edge
	// slice gates exactly like DomainAWSRelationshipMaterialization (#805). See
	// issue #388 and docs/internal/design/388-kubernetes-correlation-readmodel.md.
	DomainKubernetesWorkloadMaterialization Domain = "kubernetes_workload_materialization"
	// DomainKubernetesCorrelationMaterialization projects the exact-outcome live
	// Kubernetes correlation decisions into canonical RUNS_IMAGE edges between a
	// KubernetesWorkload node (committed by DomainKubernetesWorkloadMaterialization)
	// and the digest-addressed OCI source node a live workload was observed running.
	// It gates on the GraphProjectionKeyspaceKubernetesWorkloadUID /
	// GraphProjectionPhaseCanonicalNodesCommitted readiness phase so edges never
	// resolve against workload nodes that have not committed (issue #388 PR3),
	// exactly like DomainAWSRelationshipMaterialization (#805) and
	// DomainObservabilityCoverageMaterialization (#391 PR3). Only an exact image
	// digest match whose source digest resolves a canonical OCI node uid
	// materializes an edge; derived, ambiguous, unresolved, stale, and rejected
	// outcomes — and the structural owner_reference identity decision, which is a
	// workload->workload edge rather than a workload->image edge — stay
	// provenance-only and fabricate no edge. See issue #388 and
	// docs/internal/design/388-kubernetes-correlation-readmodel.md.
	DomainKubernetesCorrelationMaterialization Domain = "kubernetes_correlation_materialization"
	// DomainCrossplaneSatisfiedByMaterialization projects Crossplane Claim ->
	// XRD classification decisions into canonical SATISFIED_BY edges between a
	// K8sResource node (the Claim — never parser-labeled, see issue #5347) and
	// the CrossplaneXRD node it resolved against. A K8sResource content-entity
	// row is classified as a Claim by resolving (group, kind) — derived from
	// api_version/kind, not a parse-time label — against exactly one
	// CrossplaneXRD's (spec.group, spec.claimNames.kind); zero matches is an
	// ordinary Kubernetes object and two or more is ambiguous, and both
	// produce no edge. It is cross-scope: a platform repo's XRDs are joined
	// against Claims in app repos via ListActiveCrossplaneXRDFacts, mirroring
	// DomainKubernetesCorrelationMaterialization's cross-scope OCI source
	// join. See issue #5347.
	DomainCrossplaneSatisfiedByMaterialization Domain = "crossplane_satisfied_by_materialization"
)

// IntentStatus captures the durable reducer intent lifecycle state.
type IntentStatus string

const (
	// IntentStatusPending means the intent is ready to be claimed.
	IntentStatusPending IntentStatus = "pending"
	// IntentStatusClaimed means the intent has been leased for execution.
	IntentStatusClaimed IntentStatus = "claimed"
	// IntentStatusRunning means the reducer is actively processing the intent.
	IntentStatusRunning IntentStatus = "running"
	// IntentStatusSucceeded means the intent finished successfully.
	IntentStatusSucceeded IntentStatus = "succeeded"
	// IntentStatusFailed means the intent is terminally failed.
	IntentStatusFailed IntentStatus = "failed"
)

// ResultStatus captures the terminal outcome of one reducer execution.
type ResultStatus string

const (
	// ResultStatusSucceeded means the execution completed successfully.
	ResultStatusSucceeded ResultStatus = "succeeded"
	// ResultStatusFailed means the execution failed.
	ResultStatusFailed ResultStatus = "failed"
	// ResultStatusSuperseded means the intent was skipped because a newer
	// generation is already active for the scope.
	ResultStatusSuperseded ResultStatus = "superseded"
)

// FailureRecord captures the durable reducer failure classification.
type FailureRecord struct {
	FailureClass string
	Message      string
	Details      string
}

// RetryableError marks reducer failures that should re-enter the durable
// queue instead of becoming terminal on the first failure.
type RetryableError interface {
	error
	Retryable() bool
}

// IsRetryable reports whether the supplied error explicitly opts into bounded
// retry behavior.
func IsRetryable(err error) bool {
	var retryable RetryableError
	if !errors.As(err, &retryable) {
		return false
	}

	return retryable.Retryable()
}

// The durable Intent value type and its lifecycle methods live in the sibling
// intent_value.go to keep this file focused on the Domain enum and the durable
// status/result/failure contracts.
