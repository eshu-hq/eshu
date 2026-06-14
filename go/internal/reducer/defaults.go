package reducer

import (
	"log/slog"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
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

	// FactLoader loads fact envelopes for workload and infrastructure
	// platform materialization.
	FactLoader FactLoader

	// CodeCallIntentWriter persists durable shared-intent rows for code-call
	// and Python metaclass materialization.
	CodeCallIntentWriter CodeCallIntentWriter

	// GraphProjectionPhasePublisher persists durable graph-readiness publications
	// for canonical and semantic node writers.
	GraphProjectionPhasePublisher GraphProjectionPhasePublisher
	GraphProjectionRepairQueue    GraphProjectionPhaseRepairQueue
	ReadinessLookup               GraphProjectionReadinessLookup
	ReadinessPrefetch             GraphProjectionReadinessPrefetch

	// CodeCallEdgeWriter is retained for compatibility with older reducer tests
	// and wiring. Code-call materialization no longer uses it directly.
	CodeCallEdgeWriter SharedProjectionEdgeWriter

	// SQLRelationshipEdgeWriter writes canonical SQL relationship edges
	// (REFERENCES_TABLE, HAS_COLUMN, TRIGGERS) from reducer-owned SQL entity
	// metadata.
	SQLRelationshipEdgeWriter SharedProjectionEdgeWriter

	// InheritanceEdgeWriter writes canonical INHERITS, OVERRIDES, and ALIASES
	// edges from reducer-owned parser entity bases and trait adaptation
	// metadata.
	InheritanceEdgeWriter SharedProjectionEdgeWriter

	// DocumentationEdgeWriter writes canonical DOCUMENTS edges from reducer-owned
	// exact documentation entity mentions.
	DocumentationEdgeWriter SharedProjectionEdgeWriter

	// RationaleEdgeWriter writes canonical EXPLAINS edges from reducer-owned
	// intent-comment rationale metadata.
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

	// Terraform config-vs-state drift adapters (chunk #43). Optional; nil
	// values cause the DomainConfigStateDrift handler to short-circuit with
	// success and emit a structured log only — drift detection requires the
	// resolver and the evidence loader to be wired.
	TerraformBackendResolver *tfstatebackend.Resolver
	DriftEvidenceLoader      DriftEvidenceLoader
	DriftLogger              *slog.Logger

	// AWS cloud-runtime drift adapters (issue #39). Both must be non-nil for
	// the registry to register DomainAWSCloudRuntimeDrift; missing either one
	// would either drop evidence before publication or admit findings with no
	// durable truth surface.
	AWSCloudRuntimeDriftEvidenceLoader AWSCloudRuntimeDriftEvidenceLoader
	AWSCloudRuntimeDriftWriter         AWSCloudRuntimeDriftFindingWriter
	AWSCloudRuntimeDriftLogger         *slog.Logger

	// Curated search-document projection (design 430). Both must be non-nil for
	// the registry to register DomainEshuSearchDocument; it loads the scope's
	// indexed content and writes derived EshuSearchDocument facts.
	EshuSearchDocumentSourceLoader SearchDocumentSourceLoader
	EshuSearchDocumentWriter       SearchDocumentWriter
	EshuSearchDocumentLogger       *slog.Logger

	// Multi-cloud runtime drift adapters (issues #1997, #1998). Both must be
	// non-nil for the registry to register DomainMultiCloudRuntimeDrift; missing
	// either one would either drop provider-neutral drift evidence before
	// publication or admit findings with no durable truth surface. The path
	// mirrors the AWS drift adapters but joins on canonical cloud_resource_uid so
	// AWS, GCP, and Azure share one drift domain.
	MultiCloudRuntimeDriftEvidenceLoader MultiCloudRuntimeDriftEvidenceLoader
	MultiCloudRuntimeDriftWriter         MultiCloudRuntimeDriftFindingWriter
	MultiCloudRuntimeDriftLogger         *slog.Logger

	// Cloud inventory admission adapters (issues #1997, #1998). Both must be
	// non-nil for the registry to register DomainCloudInventoryAdmission; missing
	// either one would either drop provider cloud-inventory facts before
	// admission or admit canonical identities with no durable truth surface.
	// CloudInventoryGenerationCheck is optional and supersedes stale generations
	// before any load or write.
	CloudInventoryEvidenceLoader  CloudInventoryEvidenceLoader
	CloudInventoryAdmissionWriter CloudInventoryAdmissionWriter
	CloudInventoryGenerationCheck GenerationFreshnessCheck
	// CloudInventoryTagEvidenceLoader is optional; when set, tag-evidence
	// fingerprints (e.g. azure_tag_observation) attach to the canonical resource
	// sharing their cloud_resource_uid. A nil loader leaves the AWS/GCP resource
	// admission path unchanged.
	CloudInventoryTagEvidenceLoader CloudTagEvidenceLoader

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

	// KubernetesCorrelationWriter persists live Kubernetes correlation decisions
	// (exact, derived, ambiguous, unresolved, stale, rejected) plus a drift kind
	// for kubernetes_live.* facts joined to deployment-source image evidence.
	KubernetesCorrelationWriter KubernetesCorrelationWriter

	// KubernetesWorkloadNodeWriter materializes kubernetes_live.pod_template facts
	// into canonical KubernetesWorkload graph nodes (issue #388). It must be
	// non-nil alongside FactLoader for the registry to register
	// DomainKubernetesWorkloadMaterialization; missing either one would drop every
	// pod-template fact before it reaches the graph. The handler also publishes the
	// canonical-nodes-committed phase through GraphProjectionPhasePublisher so the
	// later live-workload edge slice can gate on it exactly like the AWS
	// relationship edge gates on the CloudResource node phase (#805).
	KubernetesWorkloadNodeWriter KubernetesWorkloadNodeWriter

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

	// KubernetesCorrelationEdgeWriter projects exact live-workload correlation
	// decisions into canonical RUNS_IMAGE edges between a KubernetesWorkload node
	// and the digest-addressed OCI source node it runs (issue #388 PR3). It must be
	// non-nil alongside FactLoader for the registry to register
	// DomainKubernetesCorrelationMaterialization; missing either one would drop
	// every correlation materialization intent before it reaches the graph. The
	// handler also gates on ReadinessLookup so edges never resolve against
	// uncommitted KubernetesWorkload nodes.
	KubernetesCorrelationEdgeWriter KubernetesCorrelationEdgeWriter

	// SBOMAttestationAttachmentWriter persists SBOM and attestation document
	// attachment decisions for digest-keyed image evidence.
	SBOMAttestationAttachmentWriter SBOMAttestationAttachmentWriter

	// SupplyChainImpactWriter persists vulnerability impact findings with
	// explicit package, SBOM, image, and repository evidence paths.
	SupplyChainImpactWriter SupplyChainImpactWriter

	// SecurityAlertReconciliationWriter persists provider alert comparison
	// state without promoting provider alerts into impact truth.
	SecurityAlertReconciliationWriter SecurityAlertReconciliationWriter

	// SecretsIAMTrustChainEvidenceLoader loads the bounded AWS IAM,
	// Kubernetes, and Vault source-fact packet used by the secrets/IAM reducer
	// read-model domain.
	SecretsIAMTrustChainEvidenceLoader SecretsIAMTrustChainEvidenceLoader

	// SecretsIAMTrustChainWriter persists identity-trust-chain,
	// privilege-posture, secret-access-path, and posture-gap reducer facts.
	SecretsIAMTrustChainWriter SecretsIAMTrustChainWriter

	// SecretsIAMGraphWriter projects exact reducer-owned secrets/IAM
	// read-model rows (identity_trust_chain, secret_access_path) into the four
	// SecretsIAM* node families and the five resolvable SECRETS_IAM_* edge
	// families (ADR #1314 §4). It must be non-nil alongside FactLoader for the
	// registry to register DomainSecretsIAMGraphProjection; missing either one
	// keeps the domain unregistered so no projection intent is silently dropped.
	// It defaults to nil: live graph writes stay OFF until the target-bound
	// activation record binds approval to one deployment and captures flag-on
	// proof before cmd/reducer's opt-in flag is set.
	SecretsIAMGraphWriter SecretsIAMGraphWriter

	// EndpointPresenceWriter records uid-exact presence for committed
	// CloudResource and KubernetesWorkload nodes so the cross-scope secrets/IAM
	// projection gate can prove an endpoint committed (issue #1380). It is nil
	// unless the secrets/IAM graph projection feature is enabled, so the default
	// hot materializer paths carry no extra write.
	EndpointPresenceWriter EndpointPresenceWriter

	// EndpointPresenceLookup answers uid-exact cross-scope endpoint readiness for
	// the secrets/IAM projection gate (issue #1380). Nil disables gating; it is
	// wired only when the projection feature is enabled.
	EndpointPresenceLookup EndpointPresenceLookup

	// PackageCorrelationWriter persists package ownership candidates and
	// manifest-backed consumption decisions for package-registry evidence.
	PackageCorrelationWriter PackageCorrelationWriter

	// IncidentRoutingEvidenceLoader loads PagerDuty incident-routing packets from
	// incident.record facts, Terraform-source PagerDutyDeclaration content rows,
	// Terraform-state routing facts, and optional live PagerDuty routing facts.
	// It must be non-nil alongside IncidentRoutingEvidenceWriter to register
	// DomainIncidentRoutingMaterialization; missing either one would drop every
	// incident-routing graph materialization intent before it reaches graph truth.
	IncidentRoutingEvidenceLoader IncidentRoutingEvidenceLoader

	// IncidentRoutingEvidenceWriter projects exact PagerDuty routing evidence into
	// canonical IncidentRoutingEvidence graph nodes and evidence relationships.
	IncidentRoutingEvidenceWriter IncidentRoutingEvidenceWriter

	// AppliedPagerDutyServiceRoutingLoader loads applied PagerDuty service
	// routing facts (provider service id + Terraform backend locator) for the
	// incident-repository correlation domain. It must be non-nil alongside
	// IncidentRepositoryCorrelationWriter to register
	// DomainIncidentRepositoryCorrelation; the BackendRepositoryResolver is also
	// required so the durable backend-locator-to-repository join can run.
	AppliedPagerDutyServiceRoutingLoader AppliedPagerDutyServiceRoutingLoader

	// BackendRepositoryResolver resolves a Terraform backend locator to its
	// single owning config repository for the incident-repository correlation
	// domain. A nil resolver leaves every correlation unresolved (no durable
	// edge), so it must be wired for the domain to emit edges.
	BackendRepositoryResolver BackendRepositoryResolver

	// IncidentRepositoryCorrelationWriter persists durable
	// incident-routing-to-repository correlation decisions. It must be non-nil
	// alongside AppliedPagerDutyServiceRoutingLoader to register
	// DomainIncidentRepositoryCorrelation.
	IncidentRepositoryCorrelationWriter IncidentRepositoryCorrelationWriter
}
