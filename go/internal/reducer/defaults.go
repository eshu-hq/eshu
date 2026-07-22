// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// DefaultHandlers captures the reducer-owned backend adapters available for the
// default domain catalog.
type DefaultHandlers struct {
	DeployableUnitCorrelationHandler Handler
	WorkloadIdentityWriter           WorkloadIdentityWriter
	CloudAssetResolutionWriter       CloudAssetResolutionWriter
	PlatformMaterializationWriter    PlatformMaterializationWriter
	PlatformGraphLocker              PlatformGraphLocker
	WorkloadMaterializationReplayer  WorkloadMaterializationReplayer

	// Cypher-backed adapters for canonical graph writes.
	WorkloadMaterializer               *WorkloadMaterializer
	InfrastructurePlatformMaterializer *InfrastructurePlatformMaterializer
	InfrastructurePlatformLookup       InfrastructurePlatformLookup
	SemanticEntityWriter               SemanticEntityWriter
	WorkloadProjectionInputLoader      WorkloadProjectionInputLoader
	WorkloadDependencyLookup           WorkloadDependencyGraphLookup
	// InstanceRetractionLookup resolves superseded WorkloadInstance ids (e.g. a
	// pre-canonical environment alias key retired by the #5473 environment-alias
	// contract) so workload materialization can retract the orphaned node and its
	// INSTANCE_OF/DEPLOYMENT_SOURCE/RUNS_ON edges after the replacement MERGE
	// write commits. Nil keeps existing reducer behavior (no retraction).
	InstanceRetractionLookup WorkloadInstanceRetractionLookup

	// FactLoader loads fact envelopes for workload and infrastructure
	// platform materialization.
	FactLoader FactLoader

	// AdmissionDecisionWriter persists shared explainability decisions for
	// reducer domains that map local admission outcomes to the cross-domain
	// admission_decisions read model. Nil keeps existing reducer behavior.
	AdmissionDecisionWriter AdmissionDecisionWriter
	// AdmissionDecisionNow supplies timestamps for shared admission decisions.
	AdmissionDecisionNow func() time.Time

	// CodeCallIntentWriter persists durable shared-intent rows for code-call
	// and Python metaclass materialization.
	CodeCallIntentWriter CodeCallIntentWriter

	// InheritanceIntentWriter persists durable shared-intent rows for inheritance
	// edge materialization (#2867). The promoted InheritanceMaterializationHandler
	// emits file-scoped per-edge intents plus a per-repo refresh intent instead of
	// writing canonical edges directly, so the partitioned runner and the #2898
	// refresh fence project them.
	InheritanceIntentWriter InheritanceIntentWriter

	// SQLRelationshipIntentWriter persists durable shared-intent rows for SQL
	// relationship edge materialization (#2868). The promoted
	// SQLRelationshipMaterializationHandler emits file-scoped per-edge intents plus
	// a per-repo refresh intent instead of writing canonical edges directly, so the
	// partitioned runner and the #2898 refresh fence project them.
	SQLRelationshipIntentWriter SQLRelationshipIntentWriter

	// ShellExecIntentWriter persists durable shared-intent rows for shell-exec
	// edge materialization. The handler emits file-scoped per-edge intents plus a
	// per-repo refresh intent instead of writing canonical edges directly, so the
	// partitioned runner and the refresh fence project them.
	ShellExecIntentWriter ShellExecIntentWriter

	// RationaleEdgeIntentWriter persists durable shared-intent rows for rationale
	// EXPLAINS edge materialization (#2869). The promoted
	// RationaleEdgeMaterializationHandler emits file-scoped per-edge intents plus a
	// per-repo refresh intent instead of writing canonical edges directly, so the
	// partitioned runner and the #2898 refresh fence project them.
	RationaleEdgeIntentWriter RationaleEdgeIntentWriter

	// GraphProjectionPhasePublisher persists durable graph-readiness publications
	// for canonical and semantic node writers.
	GraphProjectionPhasePublisher GraphProjectionPhasePublisher
	GraphProjectionRepairQueue    GraphProjectionPhaseRepairQueue
	ReadinessLookup               GraphProjectionReadinessLookup
	ReadinessPrefetch             GraphProjectionReadinessPrefetch

	// CodeCallEdgeWriter is retained for compatibility with older reducer tests
	// and wiring. Code-call materialization no longer uses it directly.
	CodeCallEdgeWriter SharedProjectionEdgeWriter

	// SQLRelationshipEdgeWriter is retained for compatibility with older reducer
	// tests and wiring. SQL relationship materialization no longer uses it directly:
	// it rides the shared-projection intent path via SQLRelationshipIntentWriter
	// (#2868).
	SQLRelationshipEdgeWriter SharedProjectionEdgeWriter

	// DocumentationEdgeWriter writes canonical DOCUMENTS edges from reducer-owned
	// exact documentation entity mentions.
	DocumentationEdgeWriter SharedProjectionEdgeWriter

	// CodeownersOwnershipEdgeWriter writes canonical DECLARES_CODEOWNER edges from
	// directly-emitted codeowners.ownership facts (issue #5419 Phase 3).
	CodeownersOwnershipEdgeWriter SharedProjectionEdgeWriter

	// SubmodulePinEdgeWriter writes canonical PINS_SUBMODULE edges from
	// directly-emitted submodule.pin facts (issue #5420 Phase 3).
	SubmodulePinEdgeWriter SharedProjectionEdgeWriter

	// RationaleEdgeWriter is retained for compatibility with older reducer tests
	// and wiring. Rationale materialization no longer uses it directly: it rides the
	// shared-projection intent path via RationaleEdgeIntentWriter (#2869).
	RationaleEdgeWriter SharedProjectionEdgeWriter

	// Cross-repo relationship resolution adapters. All optional; nil disables
	// cross-repo resolution during deployment_mapping reduction.
	EvidenceFactLoader         EvidenceFactLoader
	AssertionLoader            AssertionLoader
	ResolutionPersister        ResolutionPersister
	ResolvedRelationshipLoader ResolvedRelationshipLoader
	RepoDependencyIntentWriter RepoDependencyIntentWriter

	// RepoDependencyEdgeWriter writes cross-repo dependency edges resolved
	// from durable repo-dependency intents. Optional; nil disables the
	// repo-dependency projection runner.
	RepoDependencyEdgeWriter     SharedProjectionEdgeWriter
	WorkloadDependencyEdgeWriter SharedProjectionEdgeWriter

	// GenerationCheck reports whether an intent's generation is still current.
	// Nil disables the guard and lets all intents execute unconditionally.
	GenerationCheck GenerationFreshnessCheck
	// PriorGenerationCheck reports whether a scope has any prior generation.
	// Nil keeps retract behavior conservative for handlers that need cleanup.
	PriorGenerationCheck PriorGenerationCheck

	// Tracer and Instruments for cross-repo resolution telemetry.
	Tracer      trace.Tracer
	Instruments *telemetry.Instruments

	// Cohesive adapter groups split into defaults_handlers.go to keep this file
	// within the repository file-size limit. They embed into DefaultHandlers, so
	// promoted field access (handlers.FieldName) and the reducer registry wiring
	// stay unchanged; only struct-literal construction nests fields under their
	// group.
	DriftHandlers
	CloudInventoryHandlers
	SearchDocumentHandlers
	KubernetesHandlers
	CrossplaneHandlers
	SupplyChainSecurityHandlers
	IncidentRoutingHandlers
	CodeEvidenceHandlers

	// CloudResourceNodeWriter materializes aws_resource facts into canonical
	// CloudResource graph nodes (issue #805). It must be non-nil alongside
	// FactLoader for the registry to register DomainAWSResourceMaterialization;
	// missing either one would drop every aws_resource fact before it reaches
	// the graph.
	CloudResourceNodeWriter CloudResourceNodeWriter

	// EC2InstanceNodeWriter materializes ec2_instance_posture facts into canonical
	// :CloudResource graph nodes on the existing cloud_resource_uid keyspace (issue
	// #1146 PR-A). It must be non-nil alongside FactLoader for the registry to
	// register DomainEC2InstanceNodeMaterialization; missing either one would drop
	// every ec2_instance_posture fact before it reached the graph. The handler also
	// publishes the canonical-nodes-committed phase through
	// GraphProjectionPhasePublisher so the later USES_PROFILE edge slice (#1146
	// PR-B) can gate on it exactly like the AWS relationship edge gates on the
	// CloudResource node phase (#805).
	EC2InstanceNodeWriter EC2InstanceNodeWriter

	// CloudResourceEdgeWriter projects aws_relationship facts into canonical
	// AWS relationship edges between CloudResource nodes (issue #805 PR 2). It
	// must be non-nil alongside FactLoader for the registry to register
	// DomainAWSRelationshipMaterialization; missing either one would drop every
	// aws_relationship fact before it reaches the graph. The handler also gates
	// on ReadinessLookup so edges never resolve against uncommitted nodes.
	CloudResourceEdgeWriter CloudResourceEdgeWriter

	// GCPCloudResourceEdgeWriter projects gcp_cloud_relationship facts into
	// canonical GCP relationship edges between CloudResource nodes (issue #2348).
	// It must be non-nil alongside FactLoader for the registry to register
	// DomainGCPRelationshipMaterialization; missing either one would drop every
	// gcp_cloud_relationship fact before it reaches the graph. The handler gates
	// on ReadinessLookup so edges never resolve against uncommitted GCP nodes.
	GCPCloudResourceEdgeWriter CloudResourceEdgeWriter

	// AzureCloudResourceEdgeWriter projects azure_cloud_relationship facts into
	// canonical Azure relationship edges between CloudResource nodes. It must be
	// non-nil alongside FactLoader for the registry to register
	// DomainAzureRelationshipMaterialization; missing either one would drop every
	// azure_cloud_relationship fact before it reaches the graph. The handler gates
	// on ReadinessLookup so edges never resolve against uncommitted Azure nodes.
	AzureCloudResourceEdgeWriter CloudResourceEdgeWriter

	// WorkloadCloudRelationshipEdgeWriter projects exact workload anchors on
	// CloudResource facts into canonical WorkloadInstance USES CloudResource
	// edges. It must be non-nil alongside FactLoader for the registry to register
	// DomainWorkloadCloudRelationshipMaterialization; missing either one keeps the
	// domain unregistered rather than dropping graph truth.
	WorkloadCloudRelationshipEdgeWriter WorkloadCloudRelationshipEdgeWriter

	// ObservabilityCoverageEdgeWriter projects exact observability coverage
	// decisions into canonical COVERS edges between CloudResource nodes (issue
	// #391 PR3). It must be non-nil alongside FactLoader for the registry to
	// register DomainObservabilityCoverageMaterialization; missing either one
	// would drop every coverage materialization intent before it reaches the
	// graph. The handler also gates on ReadinessLookup so edges never resolve
	// against uncommitted nodes.
	ObservabilityCoverageEdgeWriter ObservabilityCoverageEdgeWriter

	// ProjectedSourceLedger records and enumerates the source uids of edges
	// projected by the AWS, Azure, GCP relationship, observability-coverage,
	// and security-group reachability handlers (issue #4858, #4881), so their
	// prior-generation retract can enumerate uids from the ledger and delete by
	// an anchored source-uid MATCH instead of scanning the whole
	// :CloudResource (or, for security-group reachability's rule->endpoint
	// family, :SecurityGroupRule) label. It is optional and shared across all
	// five handlers, distinguished by each handler's own evidence_source
	// string; nil preserves each handler's pre-ledger whole-scope retract.
	ProjectedSourceLedger ProjectedSourceLedger

	// IAMCanAssumeEdgeWriter projects aws_iam_permission trust statements into
	// canonical CAN_ASSUME edges between IAM CloudResource nodes (issue #1134
	// PR2). It must be non-nil alongside FactLoader for the registry to register
	// DomainIAMCanAssumeMaterialization; missing either one would drop every
	// CAN_ASSUME materialization intent before it reaches the graph. The handler
	// also gates on ReadinessLookup so edges never resolve against uncommitted
	// nodes.
	IAMCanAssumeEdgeWriter IAMCanAssumeEdgeWriter

	// S3LogsToEdgeWriter projects s3_bucket_posture logging_target_bucket fields
	// into canonical LOGS_TO edges between S3 bucket CloudResource nodes (issue
	// #1144 PR2). It must be non-nil alongside FactLoader for the registry to
	// register DomainS3LogsToMaterialization; missing either one would drop every
	// LOGS_TO materialization intent before it reaches the graph. The handler
	// also gates on ReadinessLookup so edges never resolve against uncommitted
	// nodes.
	S3LogsToEdgeWriter S3LogsToEdgeWriter

	// S3ExternalPrincipalGrantWriter projects metadata-only
	// s3_external_principal_grant facts into canonical ExternalPrincipal nodes
	// and GRANTS_ACCESS_TO edges (issue #1231). It must be non-nil alongside
	// FactLoader for the registry to register
	// DomainS3ExternalPrincipalGrantMaterialization; missing either one would
	// drop every external-principal grant intent before it reaches graph truth.
	// The handler also gates on ReadinessLookup so source S3 buckets resolve only
	// after CloudResource nodes commit.
	S3ExternalPrincipalGrantWriter S3ExternalPrincipalGrantWriter

	// RDSPostureNodeWriter projects rds_instance_posture facts onto existing RDS
	// CloudResource nodes (issue #1233). It must be non-nil alongside FactLoader
	// for the registry to register DomainRDSPostureMaterialization; missing either
	// one would drop every RDS posture intent before it reaches graph truth. The
	// handler also gates on ReadinessLookup so posture fields never write against
	// uncommitted CloudResource nodes.
	RDSPostureNodeWriter RDSPostureNodeWriter
	// EC2UsesProfileEdgeWriter projects ec2_instance_posture instance_profile_arn
	// into canonical USES_PROFILE edges between an EC2 instance CloudResource node
	// and the IAM instance-profile CloudResource node it uses (issue #1146 PR-B). It
	// must be non-nil alongside FactLoader for the registry to register
	// DomainEC2UsesProfileMaterialization; missing either one would drop every
	// USES_PROFILE materialization intent before it reaches the graph. The handler
	// gates on a DUAL readiness lookup — both the EC2 instance node phase and the
	// IAM instance-profile node phase — so edges never resolve against an endpoint
	// that has not committed.
	EC2UsesProfileEdgeWriter EC2UsesProfileEdgeWriter

	// IAMInstanceProfileRoleEdgeWriter projects IAM instance-profile role_arns
	// into canonical HAS_ROLE edges between IAM instance-profile and role
	// CloudResource nodes (issue #1299). It must be non-nil alongside FactLoader
	// for the registry to register DomainIAMInstanceProfileRoleMaterialization;
	// missing either one would drop every HAS_ROLE materialization intent before it
	// reaches graph truth. The handler also gates on ReadinessLookup so edges never
	// resolve against uncommitted IAM nodes.
	IAMInstanceProfileRoleEdgeWriter IAMInstanceProfileRoleEdgeWriter

	// EC2BlockDeviceKMSPostureNodeWriter derives EC2 block-device KMS posture
	// from ec2_instance_posture block devices joined to EBS volume and KMS facts,
	// then writes reducer-owned properties onto existing EC2 CloudResource nodes
	// (issue #1304). It must be non-nil alongside FactLoader for the registry to
	// register DomainEC2BlockDeviceKMSPostureMaterialization; missing either one
	// would drop every posture intent before it reaches graph truth. The handler
	// gates on a DUAL readiness lookup — the EC2 instance node phase plus the
	// EBS/KMS CloudResource node phase — so properties never write against
	// uncommitted node truth.
	EC2BlockDeviceKMSPostureNodeWriter EC2BlockDeviceKMSPostureNodeWriter

	// S3InternetExposureNodeWriter derives s3_bucket_posture internet exposure
	// state and writes reducer-owned properties onto existing S3 CloudResource
	// nodes (issue #1232). It must be non-nil alongside FactLoader for the
	// registry to register DomainS3InternetExposureMaterialization; missing either
	// one would drop every exposure materialization intent before it reaches the
	// graph. The handler also gates on ReadinessLookup so node properties never
	// resolve against uncommitted S3 nodes.
	S3InternetExposureNodeWriter S3InternetExposureNodeWriter

	// EC2InternetExposureNodeWriter derives ec2_instance_posture internet
	// exposure state and writes reducer-owned properties onto existing EC2
	// CloudResource nodes (issue #1301). It must be non-nil alongside FactLoader
	// for the registry to register DomainEC2InternetExposureMaterialization;
	// missing either one would drop every exposure materialization intent before
	// it reaches the graph. The handler gates on ReadinessLookup so node
	// properties never resolve against uncommitted EC2 nodes.
	EC2InternetExposureNodeWriter EC2InternetExposureNodeWriter

	// ContainerImageIdentityWriter persists digest-keyed image identity
	// decisions for Git, OCI registry, and runtime image evidence.
	ContainerImageIdentityWriter ContainerImageIdentityWriter

	// CICDRunCorrelationWriter persists provider run, artifact, and
	// environment correlation decisions for CI/CD evidence.
	CICDRunCorrelationWriter CICDRunCorrelationWriter

	// ServiceCatalogCorrelationWriter persists service-catalog ownership and
	// repository correlation decisions.
	ServiceCatalogCorrelationWriter ServiceCatalogCorrelationWriter

	// ServiceMaterializationWriter, when set, commits the additive per-service
	// ownership generation lineage (#1943) alongside the correlation facts so the
	// service-scope changed-since delta has a durable prior snapshot to diff. It
	// is optional; when nil the existing service-catalog contract is unchanged.
	ServiceMaterializationWriter ServiceMaterializationWriter

	// ServiceRuntimeInstanceLoader, when set alongside ServiceMaterializationWriter,
	// supplies the materialized runtime instances for a service's repository so the
	// runtime evidence family (#1986) is snapshotted into each service generation.
	// It is optional; when nil the generation simply carries no runtime rows.
	ServiceRuntimeInstanceLoader RepositoryScopedRuntimeInstanceLoader

	// ServiceDocumentationEvidenceLoader, when set alongside
	// ServiceMaterializationWriter, supplies the documentation facts that reference
	// each correlated service so the docs evidence family (#1988) is snapshotted
	// into each service generation. It is keyed by service id, not repository id.
	// It is optional; when nil the generation simply carries no docs rows.
	ServiceDocumentationEvidenceLoader ServiceScopedDocumentationEvidenceLoader

	// ServiceIncidentEvidenceLoader, when set alongside
	// ServiceMaterializationWriter, supplies exact incident-routing evidence that
	// resolves to each correlated service so the incidents evidence family (#1989)
	// is snapshotted into each service generation. It is keyed by Eshu catalog
	// service id after the provider service id is resolved through durable reducer
	// correlations. It is optional; when nil the generation simply carries no
	// incident rows.
	ServiceIncidentEvidenceLoader ServiceScopedIncidentEvidenceLoader

	// ServiceVulnerabilityAdvisoryLoader, when set alongside
	// ServiceMaterializationWriter, supplies the supply-chain advisory findings on
	// each correlated service's repository so the vulnerabilities evidence family
	// (#1990) is snapshotted into each service generation. It is keyed by repository
	// id: a service is attributed an advisory only through a real
	// reducer_supply_chain_impact_finding on its repository (#2127). It is optional;
	// when nil the generation simply carries no vulnerabilities rows.
	ServiceVulnerabilityAdvisoryLoader ServiceVulnerabilityAdvisoryLoader

	// ObservabilityCoverageCorrelationWriter persists observability coverage
	// correlation decisions (covered, gap, ambiguous, stale, rejected) for
	// CloudWatch, CloudWatch Logs, and X-Ray facts.
	ObservabilityCoverageCorrelationWriter ObservabilityCoverageCorrelationWriter

	// SecurityGroupEndpointNodeWriter materializes the CIDR and managed
	// prefix-list source endpoints of aws_security_group_rule facts into canonical
	// CidrBlock and PrefixList graph nodes (issue #1135 PR2a). It must be non-nil
	// alongside FactLoader for the registry to register
	// DomainSecurityGroupCidrMaterialization; missing either one would drop every
	// security-group rule endpoint before it reaches the graph. The handler also
	// publishes the canonical-nodes-committed phase through
	// GraphProjectionPhasePublisher so the later network-reachability edge slice
	// (#1135 PR2b) can gate on it exactly like the AWS relationship edge gates on
	// the CloudResource node phase (#805).
	SecurityGroupEndpointNodeWriter SecurityGroupEndpointNodeWriter

	// SecurityGroupRuleNodeWriter materializes aws_security_group_rule facts into
	// canonical port-precise :SecurityGroupRule graph nodes and (via the handler)
	// publishes the rule-uid canonical-nodes phase the edge slice gates on (issue
	// #1135 PR2b, Option D). Required alongside FactLoader to register
	// DomainSecurityGroupRuleMaterialization.
	SecurityGroupRuleNodeWriter SecurityGroupRuleNodeWriter

	// SecurityGroupReachabilityWriter projects aws_security_group_rule facts into
	// the Option D reachability edges (SecurityGroup -> rule ALLOWS_INGRESS/EGRESS,
	// rule -[:TO]-> endpoint). Required alongside FactLoader to register
	// DomainSecurityGroupReachabilityMaterialization; the handler gates on the rule,
	// endpoint, and SG node keyspaces via ReadinessLookup (#1135 PR2b).
	SecurityGroupReachabilityWriter SecurityGroupReachabilityWriter

	// IAMEscalationEdgeWriter projects merged aws_iam_permission facts into
	// conservative IAM CAN_ESCALATE_TO privilege-escalation edges between IAM
	// principal and target CloudResource nodes (issue #1134 PR3). It must be non-nil
	// alongside FactLoader for the registry to register
	// DomainIAMEscalationMaterialization; missing either one would drop every
	// escalation intent before it reached the graph. The handler also gates on
	// ReadinessLookup so edges never resolve against uncommitted IAM nodes.
	IAMEscalationEdgeWriter IAMEscalationEdgeWriter

	// IAMCanPerformEdgeWriter projects merged aws_iam_permission facts into
	// conservative identity-policy-only IAM CAN_PERFORM effective-permission edges
	// between an IAM principal and the resource CloudResource node an identity
	// policy grants a catalogued sensitive action on (issue #1134 PR4a). It must be
	// non-nil alongside FactLoader for the registry to register
	// DomainIAMCanPerformMaterialization; missing either one would drop every
	// CAN_PERFORM intent before it reached the graph. The handler also gates on
	// ReadinessLookup so edges never resolve against uncommitted nodes.
	IAMCanPerformEdgeWriter IAMCanPerformEdgeWriter

	// PackageCorrelationWriter persists package ownership candidates and
	// manifest-backed consumption decisions for package-registry evidence.
	PackageCorrelationWriter PackageCorrelationWriter
}
