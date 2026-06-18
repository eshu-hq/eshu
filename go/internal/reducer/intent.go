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
	// DomainSemanticEntityMaterialization materializes Annotation, Typedef,
	// TypeAlias, and Component semantic nodes.
	DomainSemanticEntityMaterialization Domain = "semantic_entity_materialization"
	// DomainSQLRelationshipMaterialization materializes canonical SQL
	// relationship edges (REFERENCES_TABLE, HAS_COLUMN, TRIGGERS).
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
	// DomainConfigStateDrift correlates Terraform config (parsed HCL) against
	// Terraform state to detect five drift kinds. Cross-source, cross-scope,
	// non-canonical-write — counters and structured logs are the v1 surface.
	// Current proof gates are documented in docs/public/reference/local-testing.md
	// under "Terraform Config-vs-State Drift Compose Proofs".
	DomainConfigStateDrift Domain = "config_state_drift"
	// DomainPackageSourceCorrelation classifies package-registry source hints
	// against active repository remotes without promoting package ownership.
	DomainPackageSourceCorrelation Domain = "package_source_correlation"
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
	DomainSecretsIAMTrustChain Domain = "secrets_iam_trust_chain"
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
	// DomainSecurityGroupCidrMaterialization materializes the CIDR and managed
	// prefix-list source endpoints of aws_security_group_rule facts into canonical
	// CidrBlock and PrefixList graph nodes. CidrBlock nodes are keyed by a
	// deterministic hash of the canonicalized (masked, lowercased) CIDR plus its
	// address family, with an is_internet property for 0.0.0.0/0 and ::/0;
	// PrefixList nodes are keyed by the prefix-list id scoped to its account and
	// region. It is the endpoint-node substrate the #1135 network-reachability edge
	// projection (PR2b) joins against; referenced-security-group endpoints already
	// have CloudResource nodes and are not re-materialized here. After the node
	// write succeeds it publishes the GraphProjectionKeyspaceSecurityGroupEndpointUID
	// / GraphProjectionPhaseCanonicalNodesCommitted readiness phase so the later
	// ALLOWS_INGRESS/EGRESS edge slice gates exactly like
	// DomainAWSRelationshipMaterialization (#805). See issue #1135.
	DomainSecurityGroupCidrMaterialization Domain = "security_group_cidr_materialization"
	// DomainSecurityGroupRuleMaterialization materializes aws_security_group_rule
	// facts into canonical port-precise :SecurityGroupRule graph nodes (issue #1135
	// PR2b, Option D). Each live rule whose SecurityGroup anchor resolved to a
	// committed CloudResource node becomes one node keyed by a deterministic hash of
	// the SG anchor uid, direction, protocol, normalized port range, and source —
	// so port and protocol live in the NODE key (two ports key two nodes) rather
	// than in a relationship-property MERGE that times out at 20s on NornicDB. It is
	// the rule-node substrate the reachability edge projection joins against; after
	// the node write succeeds it publishes the
	// GraphProjectionKeyspaceSecurityGroupRuleUID /
	// GraphProjectionPhaseCanonicalNodesCommitted readiness phase so the edge slice
	// gates exactly like DomainAWSRelationshipMaterialization (#805). See issue #1135.
	DomainSecurityGroupRuleMaterialization Domain = "security_group_rule_materialization"
	// DomainSecurityGroupReachabilityMaterialization projects aws_security_group_rule
	// facts into the Option D network-reachability graph: each live rule becomes a
	// port-precise :SecurityGroupRule node, with a SecurityGroup -> rule
	// ALLOWS_INGRESS/EGRESS edge and a rule -[:TO]-> endpoint edge whose endpoint is
	// a CidrBlock, PrefixList, or referenced SecurityGroup CloudResource node. Port
	// and protocol live in the rule NODE key (keyed in its uid), never in a
	// relationship-property MERGE that times out at 20s on NornicDB. It gates on
	// THREE GraphProjectionPhaseCanonicalNodesCommitted phases —
	// GraphProjectionKeyspaceSecurityGroupRuleUID (the rule nodes this domain itself
	// commits before edges), GraphProjectionKeyspaceSecurityGroupEndpointUID (the
	// CidrBlock/PrefixList endpoints, #1135 PR2a), and
	// GraphProjectionKeyspaceCloudResourceUID (the SG nodes, #805) — so an edge
	// never resolves against any endpoint that has not committed. Unresolved SG
	// anchors or endpoints, unknown sources, and tombstoned rules materialize no
	// node and no edge and are counted, never dropped silently. See issue #1135.
	DomainSecurityGroupReachabilityMaterialization Domain = "security_group_reachability_materialization"
	// DomainIAMCanAssumeMaterialization projects aws_iam_permission trust
	// statements into canonical CAN_ASSUME edges between the IAM CloudResource
	// nodes that DomainAWSResourceMaterialization committed: an assuming
	// principal (role/user) and the role whose trust policy grants the assume.
	// It gates on the GraphProjectionPhaseCanonicalNodesCommitted readiness phase
	// on the cloud_resource_uid keyspace so edges never resolve against nodes that
	// have not committed (issue #1134 PR2), exactly like
	// DomainAWSRelationshipMaterialization (#805) and
	// DomainObservabilityCoverageMaterialization (#391 PR3). Only an
	// effect=Allow trust statement whose assume-principal resolves to a scanned
	// role/user node materializes an edge; external, AWS-service, wildcard,
	// account-root, and unscanned principals fabricate no edge and are counted.
	// The escalation edges (CAN_PERFORM, CAN_ESCALATE_TO) are a follow-up design
	// fork; see docs/internal/design/1134-iam-can-assume-trust-graph.md §8.
	DomainIAMCanAssumeMaterialization Domain = "iam_can_assume_materialization"
	// DomainIAMEscalationMaterialization projects merged aws_iam_permission facts
	// into the IAM privilege-escalation graph: each principal that holds a complete,
	// well-known escalation primitive (all required actions Allow, unconditioned,
	// not Deny-blocked) and whose target resolves to EXACTLY ONE scanned IAM
	// CloudResource node becomes one CAN_ESCALATE_TO edge carrying the merged
	// primitive set as an edge property. It is EDGE-ONLY on the existing
	// cloud_resource_uid keyspace (both endpoints are IAM CloudResource nodes #805
	// materializes); no new node type. It gates on the
	// GraphProjectionKeyspaceCloudResourceUID /
	// GraphProjectionPhaseCanonicalNodesCommitted phase so an edge never resolves
	// against an IAM node that has not committed. Wildcard/many-resource targets,
	// Deny, condition-gated, NotAction, unresolved, and cross-account-unscanned
	// targets materialize no edge and are counted, never dropped silently;
	// sts:AssumeRole is deferred to the separate CAN_ASSUME trust edge. It is
	// security-sensitive and conservative by design (issue #1134 PR3).
	DomainIAMEscalationMaterialization Domain = "iam_escalation_materialization"
	// DomainIAMCanPerformMaterialization projects merged aws_iam_permission and
	// aws_resource_policy_permission facts into the IAM CAN_PERFORM
	// effective-permission graph: each scanned principal whose trusted-Allow
	// identity statement or exact resource-policy grantee grants a closed-catalog
	// sensitive action (Allow, unconditioned, no NotAction/NotResource, not
	// Deny-blocked) on a resource ARN that resolves to EXACTLY ONE scanned
	// CloudResource node of the catalog-expected type becomes one CAN_PERFORM edge
	// carrying the granted action set, grant_sources, and evaluation_scope as edge
	// properties. It is
	// EDGE-ONLY on the existing cloud_resource_uid keyspace (both endpoints are
	// CloudResource nodes #805 materializes); no new node type. It gates on the
	// GraphProjectionKeyspaceCloudResourceUID /
	// GraphProjectionPhaseCanonicalNodesCommitted phase so an edge never resolves
	// against a node that has not committed. Uncatalogued actions, wildcard/many
	// resources, public or unscanned principals, Deny, condition-gated, NotAction,
	// unresolved/cross-account, and self-loops materialize no edge and are counted,
	// never dropped silently. The honesty boundary is explicit: a PRESENT edge
	// means an identity policy, resource policy, or both grants the action while
	// permission boundaries, SCPs, condition values, and session policies remain
	// outside this slice; a MISSING edge does NOT mean "cannot perform." It is
	// security-sensitive and conservative by design (issue #1134 PR4a/PR4b).
	DomainIAMCanPerformMaterialization Domain = "iam_can_perform_materialization"
	// DomainS3LogsToMaterialization projects s3_bucket_posture
	// logging_target_bucket fields into canonical LOGS_TO edges between the S3
	// bucket CloudResource nodes that DomainAWSResourceMaterialization committed:
	// a source bucket that emits server-access logs and the target log bucket
	// those logs are delivered to. It is EDGE-ONLY on the existing
	// cloud_resource_uid keyspace (both endpoints are S3 CloudResource nodes #805
	// materializes); no new node type. It gates on the
	// GraphProjectionKeyspaceCloudResourceUID /
	// GraphProjectionPhaseCanonicalNodesCommitted phase so an edge never resolves
	// against an S3 node that has not committed (issue #1144 PR2), exactly like
	// DomainIAMCanAssumeMaterialization (#1134 PR2) and
	// DomainAWSRelationshipMaterialization (#805). A blank logging_target_bucket
	// (logging disabled) produces no edge and is not a skip; a self-target (a
	// bucket logging to itself) is a legal S3 config and DOES produce an edge.
	// Cross-account, out-of-scope, and unscanned log targets fabricate no edge and
	// are counted. The GRANTS_ACCESS_TO :ExternalPrincipal node-then-edge slice is
	// a deferred follow-up; see docs/internal/design/1144-s3-logs-to-edge.md §8.
	DomainS3LogsToMaterialization Domain = "s3_logs_to_materialization"
	// DomainRDSPostureMaterialization projects rds_instance_posture facts onto
	// existing RDS DB instance and Aurora cluster CloudResource nodes. It is a
	// NODE-PROPERTY-ONLY slice on the cloud_resource_uid keyspace: storage
	// encryption, public-endpoint candidacy, backup/deletion-protection, IAM DB
	// auth, Performance Insights, CA certificate, parameter/option group names,
	// and curated security parameters become reducer-owned properties. It gates on
	// GraphProjectionKeyspaceCloudResourceUID /
	// GraphProjectionPhaseCanonicalNodesCommitted so it never fabricates an RDS
	// node or writes against uncommitted CloudResource truth. RDS dependency edges
	// for KMS, security groups, subnet groups, IAM roles, parameter groups, and
	// option groups stay owned by the generic aws_relationship_materialization
	// path.
	DomainRDSPostureMaterialization Domain = "rds_posture_materialization"
	// DomainEC2BlockDeviceKMSPostureMaterialization derives EC2 instance
	// block-device KMS posture from ec2_instance_posture block_devices joined to
	// scanned aws_ec2_volume, aws_kms_key, and ec2_volume_uses_kms_key facts. It
	// is NODE-PROPERTY-ONLY on the existing EC2 CloudResource node materialized by
	// DomainEC2InstanceNodeMaterialization; no new node type and no raw block-device
	// maps are written. It gates on BOTH the EC2 instance node phase
	// (ec2_instance_node_materialization:<scope>) and the EBS/KMS CloudResource
	// phase (aws_resource_materialization:<scope>) so posture never writes against
	// uncommitted EC2, volume, or key node truth. Missing volume facts, missing KMS
	// key facts, AWS-managed/default keys, detached volumes, tombstones, and
	// ambiguous evidence stay conservative state=unknown rather than fabricating
	// encryption ownership. See issue #1304.
	DomainEC2BlockDeviceKMSPostureMaterialization Domain = "ec2_block_device_kms_posture_materialization"
	// DomainS3InternetExposureMaterialization derives conservative internet
	// exposure state from s3_bucket_posture facts and writes reducer-owned
	// properties onto existing S3 CloudResource nodes. It is NODE-PROPERTY-ONLY on
	// the existing cloud_resource_uid keyspace; no new node type and no raw policy,
	// ACL, object-key, or object-data persistence. It gates on the
	// GraphProjectionKeyspaceCloudResourceUID /
	// GraphProjectionPhaseCanonicalNodesCommitted phase so posture never resolves
	// against an S3 node that has not committed. Unknown or partial posture stays
	// state=unknown with no boolean exposure property, never fabricated false.
	// See issue #1232.
	DomainS3InternetExposureMaterialization Domain = "s3_internet_exposure_materialization"
	// DomainIncidentRoutingMaterialization projects exact PagerDuty
	// incident-routing evidence into reducer-owned IncidentRoutingEvidence graph
	// nodes and evidence relationships. It preserves declared/applied/observed
	// source class and never promotes routing evidence into deployable, image,
	// commit, pull-request, work-item, service-health, blast-radius, or root-cause
	// truth. Drifted, stale, permission-hidden, ambiguous, unresolved, rejected,
	// derived, and missing routing stays provenance-only in the incident-context
	// read model. See issue #1168.
	DomainIncidentRoutingMaterialization Domain = "incident_routing_materialization"
	// DomainIncidentRepositoryCorrelation correlates applied PagerDuty
	// incident-routing evidence to its owning config repository through the
	// durable Terraform backend-locator join, emitting one reducer-owned
	// reducer_incident_repository_correlation fact per PagerDuty provider service
	// id. The join is structural, not name-based: an applied
	// incident_routing.applied_pagerduty_resource fact carries the real provider
	// service id (incident.Service.ID) plus the backend (kind, locator_hash) that
	// the tfstatebackend resolver maps to a single owning repository. Only exact
	// and derived single-owner resolutions carry a durable repository edge; blank
	// provider ids, name-fingerprint-only matches, multi-repo ambiguity, and
	// unresolved backends stay provenance-only so a downstream scoped-token
	// predicate is fail-closed. It is the prerequisite durable edge for scoped
	// incident-context reads (#2161, blocking #2144) and never lets a PagerDuty
	// service name create repository truth.
	DomainIncidentRepositoryCorrelation Domain = "incident_repository_correlation"
	// DomainSecretsIAMGraphProjection projects exact reducer secrets/IAM
	// trust-chain read-model rows into the SecretsIAM* graph nodes and the five
	// resolvable SECRETS_IAM_* edges. Only exact rows promote; non-exact states,
	// privilege-posture observations, and posture gaps stay provenance-only, and a
	// missing endpoint is skipped and counted, never fabricated (ADR #1314, #1347).
	DomainSecretsIAMGraphProjection Domain = "secrets_iam_graph_projection"
	// DomainCloudInventoryAdmission admits provider cloud-inventory source facts
	// (aws_resource, gcp_cloud_resource, azure_cloud_resource) for the current
	// generation into the shared canonical cloud_resource_uid keyspace. It
	// resolves provider raw identity — AWS ARN, GCP Cloud Asset Inventory full
	// resource name, Azure ARM resource id — into one stable uid and publishes
	// reducer-owned canonical CloudResource read-model facts, one per admitted
	// resource. It preserves the evidence layer: declared/applied/observed are
	// distinct inputs and a provider observation never overwrites declared truth.
	// Blank, malformed, ambiguous, and unsupported identities are counted and
	// surfaced, never fabricated into a uid. It is graph-neutral: canonical graph
	// node and edge projection, the multi-cloud drift join, and API/MCP readback
	// are deferred follow-ups (issues #1997, #1998).
	DomainCloudInventoryAdmission Domain = "cloud_inventory_admission"
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
