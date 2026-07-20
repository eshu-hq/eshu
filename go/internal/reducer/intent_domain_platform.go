// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// This file continues the Domain constant catalog started in intent.go
// (security-group, IAM edge, S3/RDS posture, incident, secrets/IAM graph,
// and cloud-inventory admission domains), split out to keep intent.go under
// the repository 500-line file cap.

const (
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
