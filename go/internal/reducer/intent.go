// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"

// Domain identifies a canonical shared-truth reducer domain.
type Domain = reducercontract.Domain

const (
	// DomainWorkloadIdentity resolves canonical workload identity.
	DomainWorkloadIdentity = reducercontract.DomainWorkloadIdentity
	// DomainDeployableUnitCorrelation correlates cross-source deployable-unit
	// evidence before workload admission and materialization.
	DomainDeployableUnitCorrelation = reducercontract.DomainDeployableUnitCorrelation
	// DomainCloudAssetResolution resolves canonical cloud asset identity.
	DomainCloudAssetResolution = reducercontract.DomainCloudAssetResolution
	// DomainDeploymentMapping resolves deployment relationships.
	DomainDeploymentMapping = reducercontract.DomainDeploymentMapping
	// DomainDataLineage resolves lineage across sources and scopes.
	DomainDataLineage = reducercontract.DomainDataLineage
	// DomainCodeTaintEvidence projects value-flow taint findings into graph
	// evidence nodes attached to their Function.
	DomainCodeTaintEvidence = reducercontract.DomainCodeTaintEvidence
	// DomainCodeInterprocEvidence projects cross-function value-flow findings into
	// TAINT_FLOWS_TO edges between the source and sink Function nodes.
	DomainCodeInterprocEvidence = reducercontract.DomainCodeInterprocEvidence
	// DomainCodeFunctionSummary persists each function's durable value-flow
	// summary (its structural Effects) to the function-summary store so the
	// interprocedural fixpoint can recompose summaries across runs and repos.
	DomainCodeFunctionSummary = reducercontract.DomainCodeFunctionSummary
	// DomainOwnership resolves ownership and responsibility records.
	DomainOwnership = reducercontract.DomainOwnership
	// DomainGovernance resolves governance and policy attribution.
	DomainGovernance = reducercontract.DomainGovernance
	// DomainWorkloadMaterialization materializes canonical workload graph nodes.
	DomainWorkloadMaterialization = reducercontract.DomainWorkloadMaterialization
	// DomainCodeCallMaterialization materializes canonical code call edges.
	DomainCodeCallMaterialization = reducercontract.DomainCodeCallMaterialization
	// DomainPlatformInfraMaterialization extracts Terraform/terragrunt IaC
	// platform-provisioning signals from a repository's facts and emits
	// platform_infra shared-projection intents, which the shared worker writes as
	// Repository-[:PROVISIONS_PLATFORM]->Platform edges. It owns the
	// infrastructure-provisioning verb on its own dedicated trigger rather than
	// riding the deployment_mapping handler as a side-effect.
	DomainPlatformInfraMaterialization = reducercontract.DomainPlatformInfraMaterialization
	// DomainSemanticEntityMaterialization materializes Annotation, Typedef,
	// TypeAlias, and Component semantic nodes.
	DomainSemanticEntityMaterialization = reducercontract.DomainSemanticEntityMaterialization
	// DomainSQLRelationshipMaterialization materializes canonical SQL
	// relationship edges (READS_FROM, HAS_COLUMN, TRIGGERS).
	DomainSQLRelationshipMaterialization = reducercontract.DomainSQLRelationshipMaterialization
	// DomainShellExecMaterialization materializes parser command-execution call
	// evidence into canonical shell-exec graph edges.
	DomainShellExecMaterialization = reducercontract.DomainShellExecMaterialization
	// DomainInheritanceMaterialization materializes canonical inheritance,
	// override, and alias edges from parser entity bases and trait adaptation
	// metadata.
	DomainInheritanceMaterialization = reducercontract.DomainInheritanceMaterialization
	// DomainDocumentationMaterialization materializes canonical DOCUMENTS edges
	// from exact documentation entity mentions to the code entities or workloads
	// they resolve to.
	DomainDocumentationMaterialization = reducercontract.DomainDocumentationMaterialization
	// DomainRationaleMaterialization materializes canonical EXPLAINS edges from
	// intent-comment rationale to the code entities they precede.
	DomainRationaleMaterialization = reducercontract.DomainRationaleMaterialization
	// DomainCodeownersOwnership materializes canonical DECLARES_CODEOWNER edges
	// from directly-emitted codeowners.ownership facts to the CodeownerTeam a
	// CODEOWNERS rule pattern names (issue #5419 Phase 3). One rule with N
	// owners projects N edges (owners are per-rule), riding the shared-projection
	// intent-queue path the same way DomainDocumentationMaterialization does.
	DomainCodeownersOwnership = reducercontract.DomainCodeownersOwnership
	// DomainSubmodulePin materializes canonical Repository-[:PINS_SUBMODULE]->
	// Repository edges from directly-emitted submodule.pin facts (issue #5420
	// Phase 3). Each fact is one parent-repository ".gitmodules"/gitlink
	// declaration; a fact whose submodule URL never resolved to a known
	// repository (ResolvedRepoID nil) projects no edge, mirroring
	// DomainCodeownersOwnership's shared-projection intent-queue path.
	DomainSubmodulePin = reducercontract.DomainSubmodulePin
	// DomainConfigStateDrift correlates Terraform config (parsed HCL) against
	// Terraform state to detect five drift kinds. Cross-source, cross-scope,
	// non-canonical-write — counters and structured logs are the v1 surface.
	// Current proof gates are documented in docs/public/reference/local-testing.md
	// under "Terraform Config-vs-State Drift Compose Proofs".
	DomainConfigStateDrift = reducercontract.DomainConfigStateDrift
	// DomainPackageSourceCorrelation classifies package-registry source hints
	// against active repository remotes without promoting package ownership.
	DomainPackageSourceCorrelation = reducercontract.DomainPackageSourceCorrelation
	// DomainCodeImportRepoEdge projects repo-to-repo DEPENDS_ON edges from
	// per-file external import sources correlated to package-registry ownership.
	// It runs in the git-repository scope so the per-file import facts are
	// scope-local, and resolves owners from cross-scope package-registry facts
	// through the same (ecosystem, name) join the package-consumption path uses
	// (issue #3642).
	DomainCodeImportRepoEdge = reducercontract.DomainCodeImportRepoEdge
	// DomainContainerImageIdentity joins Git, registry, and runtime image
	// evidence into digest-correlated, image-reference-keyed identity decisions.
	DomainContainerImageIdentity = reducercontract.DomainContainerImageIdentity
	// DomainCICDRunCorrelation correlates provider CI/CD runs, artifacts, and
	// environment observations with reducer-owned artifact identity evidence.
	DomainCICDRunCorrelation = reducercontract.DomainCICDRunCorrelation
	// DomainServiceCatalogCorrelation correlates service-catalog entity
	// declarations with repository and ownership evidence without letting
	// catalog names create workloads.
	DomainServiceCatalogCorrelation = reducercontract.DomainServiceCatalogCorrelation
	// DomainSBOMAttestationAttachment attaches SBOM and attestation evidence to
	// image digests only when subject evidence is explicit.
	DomainSBOMAttestationAttachment = reducercontract.DomainSBOMAttestationAttachment
	// DomainSupplyChainImpact publishes reducer-owned vulnerability impact
	// findings only when vulnerability, package, SBOM, image, or repository
	// evidence forms an explicit path.
	DomainSupplyChainImpact = reducercontract.DomainSupplyChainImpact
	// DomainSecurityAlertReconciliation compares provider-reported repository
	// security alerts against Eshu-owned dependency and impact evidence without
	// promoting provider alerts into impact truth.
	DomainSecurityAlertReconciliation = reducercontract.DomainSecurityAlertReconciliation
	// DomainSecretsIAMTrustChain builds reducer-owned secrets/IAM read models
	// from AWS IAM, Kubernetes ServiceAccount/RBAC, and Vault metadata source
	// facts. It writes durable reducer facts only: no graph labels, edges, or DDL
	// are part of this domain.
	DomainSecretsIAMTrustChain = reducercontract.DomainSecretsIAMTrustChain // #nosec G101 -- domain name identifier, not a credential
	// DomainAWSCloudRuntimeDrift publishes admitted AWS runtime-vs-IaC drift
	// findings as canonical reducer facts. The domain stays graph-neutral until
	// the drift node and query shape are frozen.
	DomainAWSCloudRuntimeDrift = reducercontract.DomainAWSCloudRuntimeDrift
	// DomainMultiCloudRuntimeDrift publishes admitted provider-neutral
	// runtime-vs-IaC drift findings keyed on canonical cloud_resource_uid for
	// GCP and Azure (issues #1997, #1998, #5759). It mirrors
	// DomainAWSCloudRuntimeDrift but joins on the shared identity keyspace so the
	// orphaned/unmanaged/ambiguous/unknown vocabulary is shared across providers.
	// Provider partitioning (#5759): AWS is exclusively owned by
	// DomainAWSCloudRuntimeDrift, which already publishes
	// reducer_aws_cloud_runtime_drift_finding end-to-end. This domain's shared
	// evidence loader still joins AWS inventory facts into the same
	// cloud_resource_uid keyspace for implementation reuse, but
	// MultiCloudRuntimeDriftHandler.Handle drops every AWS-provider row before
	// publication (see excludeAWSOwnedRows), so the two domains never disagree
	// about the same AWS resource. The domain stays graph-neutral until the
	// drift node and query shape freeze.
	DomainMultiCloudRuntimeDrift = reducercontract.DomainMultiCloudRuntimeDrift
	// DomainAWSResourceMaterialization materializes aws_resource facts into
	// canonical CloudResource graph nodes. It is the node substrate the AWS
	// relationship edge projection (issue #805) joins against; see
	// docs/internal/aws-relationship-edge-materialization-design.md.
	DomainAWSResourceMaterialization = reducercontract.DomainAWSResourceMaterialization
	// DomainGCPResourceMaterialization materializes gcp_cloud_resource facts into
	// canonical CloudResource graph nodes, mirroring DomainAWSResourceMaterialization
	// for GCP. It is the node substrate the GCP relationship edge projection
	// (issue #2348) joins against and publishes the
	// GraphProjectionPhaseCanonicalNodesCommitted readiness phase under its own
	// distinct entity key (gcp_resource_materialization:<scope>) so the GCP edge
	// stage gates on GCP node readiness independently of the AWS node phase. See
	// docs/internal/gcp-cloud-resource-materialization-design.md.
	DomainGCPResourceMaterialization = reducercontract.DomainGCPResourceMaterialization
	// DomainAzureResourceMaterialization materializes azure_cloud_resource facts
	// into canonical CloudResource graph nodes. It is the node substrate the
	// Azure relationship edge projection joins against and publishes the
	// GraphProjectionPhaseCanonicalNodesCommitted readiness phase under
	// azure_resource_materialization:<scope> so Azure edges never race AWS or GCP
	// node readiness.
	DomainAzureResourceMaterialization = reducercontract.DomainAzureResourceMaterialization
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
	DomainGCPRelationshipMaterialization = reducercontract.DomainGCPRelationshipMaterialization
	// DomainAzureRelationshipMaterialization projects azure_cloud_relationship
	// facts into canonical Azure relationship edges between CloudResource nodes
	// committed by DomainAzureResourceMaterialization. Endpoints resolve by exact
	// normalized ARM resource id; partial, unsupported, unresolved, invalid-type,
	// and self-loop evidence stays provenance-only.
	DomainAzureRelationshipMaterialization = reducercontract.DomainAzureRelationshipMaterialization
	// DomainWorkloadCloudRelationshipMaterialization projects exact
	// reducer-owned service/workload anchors on CloudResource facts into
	// canonical WorkloadInstance USES CloudResource graph edges. Queue claiming
	// gates on CloudResource node readiness; the graph writer still uses
	// MATCH-only endpoint anchoring so missing workload instances are a no-op
	// instead of fabricated graph truth.
	DomainWorkloadCloudRelationshipMaterialization = reducercontract.DomainWorkloadCloudRelationshipMaterialization
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
	DomainEC2InstanceNodeMaterialization = reducercontract.DomainEC2InstanceNodeMaterialization
	// DomainAWSRelationshipMaterialization projects aws_relationship facts into
	// canonical AWS relationship edges between the CloudResource nodes that
	// DomainAWSResourceMaterialization committed. It gates on the
	// GraphProjectionPhaseCanonicalNodesCommitted readiness phase so edges never
	// resolve against nodes that have not committed (issue #805 PR 2); see
	// docs/internal/aws-relationship-edge-materialization-design.md §5–§8.
	DomainAWSRelationshipMaterialization = reducercontract.DomainAWSRelationshipMaterialization
	// DomainAWSCloudImageMaterialization projects the lambda_function_uses_image
	// aws_relationship into a canonical CloudResource -> ContainerImage edge
	// (issue #5450), an ADDITIVE SIBLING of DomainAWSRelationshipMaterialization
	// rather than a change to it: DomainAWSRelationshipMaterialization only ever
	// resolves CloudResource -> CloudResource, so a domain whose target label is
	// :ContainerImage needs its own truth contract, mirroring the split between
	// DomainKubernetesWorkloadMaterialization and
	// DomainKubernetesCorrelationMaterialization (which projects the analogous
	// KubernetesWorkload -[:RUNS_IMAGE]-> OCI-node cross-label edge). It gates on
	// the SAME GraphProjectionPhaseCanonicalNodesCommitted readiness phase
	// DomainAWSResourceMaterialization publishes on the CloudResource keyspace
	// (the source endpoint), and resolves the target directly from the
	// relationship's own resolved_image_uri attribute (an exact
	// registry+repository@digest), never against the aws_resource join index — a
	// container image is not an aws_resource. Two MATCHes precede the MERGE, so
	// an unscanned image degrades gracefully (counted, not fabricated).
	// ecs_task_definition_uses_image is a DIFFERENT relationship_type this
	// domain recognizes and always skips: the task DEFINITION's image is
	// tag-only (no digest), so the #5472 EXACT-ONLY graph-projection policy
	// keeps it Postgres-only. See
	// docs/internal/aws-relationship-edge-materialization-design.md and
	// docs/internal/design/5472-graph-projection-policy.md.
	DomainAWSCloudImageMaterialization = reducercontract.DomainAWSCloudImageMaterialization
	// DomainObservabilityCoverageCorrelation correlates which monitored
	// CloudResource nodes have observability coverage (CloudWatch alarms,
	// dashboards, log groups, X-Ray) versus which are uncovered, emitting durable
	// provenance-only reducer facts with the six-outcome contract. It is
	// cross-source (observability object vs. the resource it covers) and
	// cross-scope (a resource in one scan scope may be covered by an alarm
	// discovered in another). PR1 writes facts only; the optional COVERS graph
	// edge is a later gated PR. See issue #391 for the design.
	DomainObservabilityCoverageCorrelation = reducercontract.DomainObservabilityCoverageCorrelation
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
	DomainObservabilityCoverageMaterialization = reducercontract.DomainObservabilityCoverageMaterialization
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
	DomainKubernetesCorrelation = reducercontract.DomainKubernetesCorrelation
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
	DomainKubernetesWorkloadMaterialization = reducercontract.DomainKubernetesWorkloadMaterialization
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
	DomainKubernetesCorrelationMaterialization = reducercontract.DomainKubernetesCorrelationMaterialization
	// DomainKubernetesNamespaceMaterialization materializes
	// kubernetes_live.namespace facts into canonical KubernetesNamespace graph
	// nodes keyed by the collector-emitted object_id ((cluster_id, namespace)
	// identity). For each namespace it checks a small documented set of label
	// keys against go/internal/environment's known-token vocabulary
	// (environment.IsKnownToken); a recognized value binds the namespace to a
	// canonical Environment node with EvidenceClassNamespaceLabel evidence, a
	// TARGETS_ENVIRONMENT edge (the same edge type
	// batchCanonicalRepoEvidenceArtifactWithEnvironmentUpsertCypher uses), and
	// StateBound. No recognized label leaves the namespace
	// StateEnvironmentUnbound and creates NO Environment node -- namespace,
	// folder, and repo-name heuristics never invent environment truth (see
	// docs/public/reference/environment-alias-contract.md). This is the FIRST
	// live-cluster namespace->environment binding; ClusterTarget.Environment
	// stays inert. See issue #5434.
	DomainKubernetesNamespaceMaterialization = reducercontract.DomainKubernetesNamespaceMaterialization
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
	DomainCrossplaneSatisfiedByMaterialization = reducercontract.DomainCrossplaneSatisfiedByMaterialization
)

// IntentStatus captures the durable reducer intent lifecycle state.
type IntentStatus = reducercontract.IntentStatus

const (
	// IntentStatusPending means the intent is ready to be claimed.
	IntentStatusPending = reducercontract.IntentStatusPending
	// IntentStatusClaimed means the intent has been leased for execution.
	IntentStatusClaimed = reducercontract.IntentStatusClaimed
	// IntentStatusRunning means the reducer is actively processing the intent.
	IntentStatusRunning = reducercontract.IntentStatusRunning
	// IntentStatusSucceeded means the intent finished successfully.
	IntentStatusSucceeded = reducercontract.IntentStatusSucceeded
	// IntentStatusFailed means the intent is terminally failed.
	IntentStatusFailed = reducercontract.IntentStatusFailed
)

// ResultStatus captures the terminal outcome of one reducer execution.
type ResultStatus = reducercontract.ResultStatus

const (
	// ResultStatusSucceeded means the execution completed successfully.
	ResultStatusSucceeded = reducercontract.ResultStatusSucceeded
	// ResultStatusFailed means the execution failed.
	ResultStatusFailed = reducercontract.ResultStatusFailed
	// ResultStatusSuperseded means the intent was skipped because a newer
	// generation is already active for the scope.
	ResultStatusSuperseded = reducercontract.ResultStatusSuperseded
)

// FailureRecord captures the durable reducer failure classification.
type FailureRecord = reducercontract.FailureRecord

// RetryableError marks reducer failures that should re-enter the durable
// queue instead of becoming terminal on the first failure.
type RetryableError = reducercontract.RetryableError

// ContainerImageIdentityOutcome names the reducer decision for one image
// reference seen in Git or runtime evidence. The type and its constants are
// aliased from contract so every existing unqualified use in this package
// (container_image_identity_contract.go and its callers) keeps compiling
// unchanged; see that file's header for why the vocabulary moved but the
// decision/write/result records that embed a reducer-root-only type did not.
type ContainerImageIdentityOutcome = reducercontract.ContainerImageIdentityOutcome

const (
	// ContainerImageIdentityExactDigest means the source reference already
	// named a digest also observed in registry facts.
	ContainerImageIdentityExactDigest = reducercontract.ContainerImageIdentityExactDigest
	// ContainerImageIdentityTagResolved means one registry tag observation
	// resolved the source tag to exactly one digest.
	ContainerImageIdentityTagResolved = reducercontract.ContainerImageIdentityTagResolved
	// ContainerImageIdentityAmbiguousTag means tag observations for the same
	// image reference point at multiple digests.
	ContainerImageIdentityAmbiguousTag = reducercontract.ContainerImageIdentityAmbiguousTag
	// ContainerImageIdentityUnresolved means no registry digest observation
	// matched the source image reference.
	ContainerImageIdentityUnresolved = reducercontract.ContainerImageIdentityUnresolved
	// ContainerImageIdentityStaleTag means runtime evidence resolved a tag to
	// a digest that registry facts report as the previous digest.
	ContainerImageIdentityStaleTag = reducercontract.ContainerImageIdentityStaleTag
	// containerImageIdentityFactKind aliases the exported contract constant so
	// every existing unqualified use in this package keeps compiling unchanged --
	// container_image_identity_writer.go and its callers.
	containerImageIdentityFactKind = reducercontract.ContainerImageIdentityFactKind
	// sbomAttestationAttachmentFactKind aliases the exported contract constant
	// so every existing unqualified use in this package keeps compiling
	// unchanged -- the supply_chain_impact family's EvidencePath construction
	// and active-fact-kind switches. See
	// [reducercontract.SBOMAttestationAttachmentFactKind]: the
	// sbom_attestation_attachment family itself moved to
	// internal/reducer/sbomattest (#6061) and imports contract directly.
	sbomAttestationAttachmentFactKind = reducercontract.SBOMAttestationAttachmentFactKind
)

// IsRetryable reports whether the supplied error explicitly opts into bounded
// retry behavior.
func IsRetryable(err error) bool {
	return reducercontract.IsRetryable(err)
}

// The durable Intent value type and its lifecycle methods live in the sibling
// intent_value.go to keep this file focused on the Domain enum and the durable
// status/result/failure contracts.
