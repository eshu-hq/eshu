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
	// SecretsIAM* node families and the four resolvable SECRETS_IAM_* edge
	// families (ADR #1314 §4). It must be non-nil alongside FactLoader for the
	// registry to register DomainSecretsIAMGraphProjection; missing either one
	// keeps the domain unregistered so no projection intent is silently dropped.
	// It defaults to nil: live graph writes stay OFF until the §11/§12 backend
	// proofs land and the §14 principal+security sign-off explicitly enables the
	// writer through cmd/reducer's opt-in flag.
	SecretsIAMGraphWriter SecretsIAMGraphWriter

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
}

// NewDefaultRegistry constructs the canonical reducer catalog for the default
// domain definitions, wiring handlers for the domains implemented today and
// allowing additive registration of source-neutral domains when handlers are
// provided explicitly.
func NewDefaultRegistry(handlers DefaultHandlers) (Registry, error) {
	registry := NewRegistry()
	for _, def := range implementedDefaultDomainDefinitions(handlers) {
		if err := registry.Register(def); err != nil {
			return Registry{}, err
		}
	}

	return registry, nil
}

// NewDefaultRuntime builds a reducer runtime from the default domain catalog.
//
// This is the additive seam for reducer main wiring: callers can replace the
// manual DefaultDomainDefinitions registration loop with one constructor call
// while keeping the surrounding service, queue, and polling setup unchanged.
func NewDefaultRuntime(handlers DefaultHandlers) (*Runtime, error) {
	registry, err := NewDefaultRegistry(handlers)
	if err != nil {
		return nil, err
	}

	rt, err := NewRuntime(registry)
	if err != nil {
		return nil, err
	}
	rt.GenerationCheck = handlers.GenerationCheck
	return rt, nil
}

func implementedDefaultDomainDefinitions(handlers DefaultHandlers) []DomainDefinition {
	definitions := make([]DomainDefinition, 0, len(DefaultDomainDefinitions())+1)
	for _, def := range DefaultDomainDefinitions() {
		switch def.Domain {
		case DomainWorkloadIdentity:
			def.Handler = WorkloadIdentityHandler{
				Writer:         handlers.WorkloadIdentityWriter,
				PhasePublisher: handlers.GraphProjectionPhasePublisher,
			}
		case DomainCloudAssetResolution:
			def.Handler = CloudAssetResolutionHandler{
				Writer:         handlers.CloudAssetResolutionWriter,
				PhasePublisher: handlers.GraphProjectionPhasePublisher,
			}
		case DomainDeploymentMapping:
			var crossRepoResolver *CrossRepoRelationshipHandler
			if handlers.EvidenceFactLoader != nil && handlers.RepoDependencyIntentWriter != nil {
				crossRepoResolver = &CrossRepoRelationshipHandler{
					EvidenceLoader:    handlers.EvidenceFactLoader,
					Assertions:        handlers.AssertionLoader,
					Persister:         handlers.ResolutionPersister,
					IntentWriter:      handlers.RepoDependencyIntentWriter,
					ReadinessLookup:   handlers.ReadinessLookup,
					ReadinessPrefetch: handlers.ReadinessPrefetch,
					Tracer:            handlers.Tracer,
					Instruments:       handlers.Instruments,
				}
			}
			def.Handler = PlatformMaterializationHandler{
				Writer:                          handlers.PlatformMaterializationWriter,
				FactLoader:                      handlers.FactLoader,
				InfrastructureMaterializer:      handlers.InfrastructurePlatformMaterializer,
				PlatformGraphLocker:             handlers.PlatformGraphLocker,
				CrossRepoResolver:               crossRepoResolver,
				WorkloadMaterializationReplayer: handlers.WorkloadMaterializationReplayer,
				PhasePublisher:                  handlers.GraphProjectionPhasePublisher,
			}
		case DomainWorkloadMaterialization:
			def.Handler = WorkloadMaterializationHandler{
				FactLoader:                   handlers.FactLoader,
				ResolvedLoader:               handlers.ResolvedRelationshipLoader,
				InputLoader:                  handlers.WorkloadProjectionInputLoader,
				InfrastructurePlatformLookup: handlers.InfrastructurePlatformLookup,
				Materializer:                 handlers.WorkloadMaterializer,
				DependencyLookup:             handlers.WorkloadDependencyLookup,
				WorkloadDependencyEdgeWriter: handlers.WorkloadDependencyEdgeWriter,
				PhasePublisher:               handlers.GraphProjectionPhasePublisher,
			}
		case DomainCodeCallMaterialization:
			def.Handler = CodeCallMaterializationHandler{
				FactLoader:   handlers.FactLoader,
				IntentWriter: handlers.CodeCallIntentWriter,
			}
		case DomainSemanticEntityMaterialization:
			def.Handler = SemanticEntityMaterializationHandler{
				FactLoader:           handlers.FactLoader,
				Writer:               handlers.SemanticEntityWriter,
				PriorGenerationCheck: handlers.PriorGenerationCheck,
				PhasePublisher:       handlers.GraphProjectionPhasePublisher,
				RepairQueue:          handlers.GraphProjectionRepairQueue,
			}
		case DomainSQLRelationshipMaterialization:
			def.Handler = SQLRelationshipMaterializationHandler{
				FactLoader:           handlers.FactLoader,
				EdgeWriter:           handlers.SQLRelationshipEdgeWriter,
				PriorGenerationCheck: handlers.PriorGenerationCheck,
			}
		case DomainInheritanceMaterialization:
			def.Handler = InheritanceMaterializationHandler{
				FactLoader:           handlers.FactLoader,
				EdgeWriter:           handlers.InheritanceEdgeWriter,
				PriorGenerationCheck: handlers.PriorGenerationCheck,
			}
		}
		definitions = append(definitions, def)
	}
	return appendAdditiveDomainDefinitions(definitions, handlers)
}
