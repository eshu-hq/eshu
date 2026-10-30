// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer //nolint:filelength // DefaultHandlers is one wiring struct that cannot split across files; the #5385 handle-ladder dep tipped the at-cap file, and splitting the struct is out of scope.

import (
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/cicdrun"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/materialization"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/shell"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossrepo"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/reducer/admissiondecision"
	"github.com/eshu-hq/eshu/go/internal/reducer/awscloud"
	"github.com/eshu-hq/eshu/go/internal/reducer/cloudasset"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/semantic"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/value/affected"
	"github.com/eshu-hq/eshu/go/internal/reducer/containerimage"
	"github.com/eshu-hq/eshu/go/internal/reducer/ec2blockkms"
	"github.com/eshu-hq/eshu/go/internal/reducer/ec2instance"
	"github.com/eshu-hq/eshu/go/internal/reducer/ec2usesprofile"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/reducer/iamcan"
	"github.com/eshu-hq/eshu/go/internal/reducer/iamescalation"
	"github.com/eshu-hq/eshu/go/internal/reducer/iaminstprofile"
	"github.com/eshu-hq/eshu/go/internal/reducer/inheritance"
	"github.com/eshu-hq/eshu/go/internal/reducer/internetexposure"
	"github.com/eshu-hq/eshu/go/internal/reducer/obscoverage"
	"github.com/eshu-hq/eshu/go/internal/reducer/packages/correlation"
	"github.com/eshu-hq/eshu/go/internal/reducer/platformfam"
	"github.com/eshu-hq/eshu/go/internal/reducer/rdsposture"
	"github.com/eshu-hq/eshu/go/internal/reducer/s3grant"
	"github.com/eshu-hq/eshu/go/internal/reducer/s3logsto"
	"github.com/eshu-hq/eshu/go/internal/reducer/secgroup"
	"github.com/eshu-hq/eshu/go/internal/reducer/servicecatalog"
	"github.com/eshu-hq/eshu/go/internal/reducer/sqlrelationship"
	"github.com/eshu-hq/eshu/go/internal/reducer/ssmhost"
	"github.com/eshu-hq/eshu/go/internal/reducer/workload/retract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// DefaultHandlers captures the reducer-owned backend adapters available for the
// default domain catalog.
type DefaultHandlers struct {
	DeployableUnitCorrelationHandler Handler
	WorkloadIdentityWriter           WorkloadIdentityWriter
	CloudAssetResolutionWriter       cloudasset.CloudAssetResolutionWriter
	PlatformMaterializationWriter    platformfam.PlatformMaterializationWriter
	PlatformGraphLocker              platformfam.PlatformGraphLocker
	WorkloadMaterializationReplayer  platformfam.WorkloadMaterializationReplayer

	// Cypher-backed adapters for canonical graph writes.
	WorkloadMaterializer               *WorkloadMaterializer
	InfrastructurePlatformMaterializer *InfrastructurePlatformMaterializer
	InfrastructurePlatformLookup       InfrastructurePlatformLookup
	SemanticEntityWriter               semantic.EntityWriter
	WorkloadProjectionInputLoader      WorkloadProjectionInputLoader
	WorkloadDependencyLookup           WorkloadDependencyGraphLookup
	// InstanceRetractionLookup resolves superseded WorkloadInstance ids (#5473)
	// for workload materialization; nil keeps existing behavior (no retraction).
	InstanceRetractionLookup WorkloadInstanceRetractionLookup
	// RepositoryEdgeReader guards the #7285 stale DEFINES/EXPOSES_ENDPOINT retract.
	RepositoryEdgeReader retract.Reader

	// factload.FactLoader loads fact envelopes for workload and platform materialization.
	FactLoader factload.FactLoader

	// AdmissionDecisionWriter persists shared explainability decisions for
	// reducer domains that map local admission outcomes to the cross-domain
	// admission_decisions read model. Nil keeps existing reducer behavior.
	AdmissionDecisionWriter admissiondecision.AdmissionDecisionWriter
	// AdmissionDecisionNow supplies timestamps for shared admission decisions.
	AdmissionDecisionNow func() time.Time

	// CodeCallIntentWriter persists durable shared-intent rows for code-call
	// and Python metaclass materialization.
	CodeCallIntentWriter materialization.IntentWriter

	// InheritanceIntentWriter persists durable shared-intent rows for inheritance
	// edge materialization (#2867). The promoted inheritance.MaterializationHandler
	// emits file-scoped per-edge intents plus a per-repo refresh intent instead of
	// writing canonical edges directly, so the partitioned runner and the #2898
	// refresh fence project them.
	InheritanceIntentWriter inheritance.IntentWriter

	// SQLRelationshipIntentWriter persists durable shared-intent rows for SQL
	// relationship edge materialization (#2868). The promoted
	// SQLRelationshipMaterializationHandler emits file-scoped per-edge intents plus
	// a per-repo refresh intent instead of writing canonical edges directly, so the
	// partitioned runner and the #2898 refresh fence project them.
	SQLRelationshipIntentWriter sqlrelationship.SQLRelationshipIntentWriter

	// ShellExecIntentWriter persists durable shared-intent rows for shell-exec
	// edge materialization. The handler emits file-scoped per-edge intents plus a
	// per-repo refresh intent instead of writing canonical edges directly, so the
	// partitioned runner and the refresh fence project them.
	ShellExecIntentWriter shell.IntentWriter

	// RationaleEdgeIntentWriter persists durable shared-intent rows for rationale
	// EXPLAINS edge materialization (#2869). The promoted
	// RationaleEdgeMaterializationHandler emits file-scoped per-edge intents plus a
	// per-repo refresh intent instead of writing canonical edges directly, so the
	// partitioned runner and the #2898 refresh fence project them.
	RationaleEdgeIntentWriter RationaleEdgeIntentWriter

	// GraphProjectionPhasePublisher persists durable graph-readiness publications
	// for canonical and semantic node writers.
	GraphProjectionPhasePublisher gpphase.PhasePublisher
	GraphProjectionRepairQueue    gpphase.PhaseRepairQueue
	ReadinessLookup               gpphase.ReadinessLookup
	ReadinessPrefetch             gpphase.ReadinessPrefetch

	// CodeCallEdgeWriter is retained for compatibility with older reducer tests
	// and wiring. Code-call materialization no longer uses it directly.
	CodeCallEdgeWriter sharedintent.EdgeWriter

	// SQLRelationshipEdgeWriter is retained for compatibility with older reducer
	// tests and wiring. SQL relationship materialization no longer uses it directly:
	// it rides the shared-projection intent path via SQLRelationshipIntentWriter
	// (#2868).
	SQLRelationshipEdgeWriter sharedintent.EdgeWriter

	// DocumentationEdgeWriter writes canonical DOCUMENTS edges from reducer-owned
	// exact documentation entity mentions.
	DocumentationEdgeWriter sharedintent.EdgeWriter

	// CodeownersOwnershipEdgeWriter writes canonical DECLARES_CODEOWNER edges from
	// directly-emitted codeowners.ownership facts (issue #5419 Phase 3).
	CodeownersOwnershipEdgeWriter sharedintent.EdgeWriter

	// SubmodulePinEdgeWriter writes canonical PINS_SUBMODULE edges from
	// directly-emitted submodule.pin facts (issue #5420 Phase 3).
	SubmodulePinEdgeWriter sharedintent.EdgeWriter

	// RationaleEdgeWriter is retained for compatibility with older reducer tests
	// and wiring. Rationale materialization no longer uses it directly: it rides the
	// shared-projection intent path via RationaleEdgeIntentWriter (#2869).
	RationaleEdgeWriter sharedintent.EdgeWriter

	// Cross-repo relationship resolution adapters. All optional; nil disables
	// cross-repo resolution during deployment_mapping reduction.
	EvidenceFactLoader  crossrepo.EvidenceFactLoader
	AssertionLoader     crossrepo.AssertionLoader
	ResolutionPersister crossrepo.ResolutionPersister
	// crossrepo.DeployableUnitCorrelationWaker is the #8294 completion-driven wake:
	// every resolution pass wakes the deployable_unit_correlation items
	// whose recorded input no longer holds over the pass's resolved repos
	// (succeeded items reopen, including missing-fingerprint rows; in-flight
	// ones replay after their ACK only when they carry a recorded
	// fingerprint — a first-execution item with none is left for the next
	// pass, see #8608), fingerprint-gated and replay-floored so retries
	// converge. Nil disables the wake; items then wait for the next
	// maintenance pass as before.
	DeployableUnitCorrelationWaker crossrepo.DeployableUnitCorrelationWaker
	ScopeRepos                     crossrepo.ScopeRepositoryReader
	ResolvedRelationshipLoader     ResolvedRelationshipLoader
	RepoDependencyIntentWriter     crossrepo.RepoDependencyIntentWriter

	// WorkloadSiblingLookup reads same-named Workload nodes for the #5385
	// handle ladder (one workload_name index seek per handle, batched per
	// intent). Nil keeps every consumer on legacy pass-through rows; set it
	// alongside ScopeRepos to resolve provider-asserted workload handles.
	WorkloadSiblingLookup WorkloadSiblingLookup

	// ECSBindingDeclarationCensus lists active Terraform/Terragrunt
	// declarations corpus-wide for workload↔ECS-service binding (#9661).
	// Nil keeps the workload-cloud handler on anchor-only rows; set it to
	// enable the declaration-binding section.
	ECSBindingDeclarationCensus ECSBindingDeclarationCensus
	// ECSBindingInstanceEnvironments lists materialized instance
	// environments by workload name for the binding's single-instance
	// environment leg. Nil disables that leg.
	ECSBindingInstanceEnvironments ECSBindingInstanceLookup

	// RepoDependencyEdgeWriter writes cross-repo dependency edges resolved
	// from durable repo-dependency intents. Optional; nil disables the
	// repo-dependency projection runner.
	RepoDependencyEdgeWriter     sharedintent.EdgeWriter
	WorkloadDependencyEdgeWriter sharedintent.EdgeWriter

	// GenerationCheck reports whether an intent's generation is still current.
	// Nil disables the guard and lets all intents execute unconditionally.
	GenerationCheck GenerationFreshnessCheck
	// PriorGenerationCheck reports whether a scope has a generation strictly older than the given generation.
	// Nil keeps retract behavior conservative for handlers that need cleanup.
	PriorGenerationCheck PriorGenerationCheck
	// PriorGeneration resolves the predecessor generation id for the #6887
	// generation-diff cloud node retract. Nil skips that retract.
	PriorGeneration PriorGenerationID

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
	CrossScopeHandlers

	// CloudResourceNodeWriter materializes aws_resource facts into canonical
	// CloudResource graph nodes (issue #805). It must be non-nil alongside
	// factload.FactLoader for the registry to register DomainAWSResourceMaterialization;
	// missing either one would drop every aws_resource fact before it reaches
	// the graph.
	CloudResourceNodeWriter CloudResourceNodeWriter

	// CloudResourceNodeRetracter deletes predecessor-only CloudResource uids
	// under the globally-gated #6887 retract (graphowner.CloudResourceRetracter
	// in production). Nil skips the retract; the materialization domains
	// register and write exactly as before.
	CloudResourceNodeRetracter CloudResourceNodeRetracter

	// EC2InstanceNodeWriter materializes ec2_instance_posture facts into canonical
	// :CloudResource graph nodes on the existing cloud_resource_uid keyspace (issue
	// #1146 PR-A). It must be non-nil alongside factload.FactLoader for the registry to
	// register DomainEC2InstanceNodeMaterialization; missing either one would drop
	// every ec2_instance_posture fact before it reached the graph. The handler also
	// publishes the canonical-nodes-committed phase through
	// GraphProjectionPhasePublisher so the later USES_PROFILE edge slice (#1146
	// PR-B) can gate on it exactly like the AWS relationship edge gates on the
	// CloudResource node phase (#805).
	EC2InstanceNodeWriter ec2instance.EC2InstanceNodeWriter

	// EC2InstanceNodeRetracter deletes predecessor-only EC2 instance uids
	// under the globally-gated #6887 retract (graphowner.EC2InstanceRetracter
	// in production). Nil skips the retract.
	EC2InstanceNodeRetracter ec2instance.EC2InstanceNodeRetracter

	// CloudResourceEdgeWriter projects aws_relationship facts into canonical
	// AWS relationship edges between CloudResource nodes (issue #805 PR 2). It
	// must be non-nil alongside factload.FactLoader for the registry to register
	// DomainAWSRelationshipMaterialization; missing either one would drop every
	// aws_relationship fact before it reaches the graph. The handler also gates
	// on ReadinessLookup so edges never resolve against uncommitted nodes.
	CloudResourceEdgeWriter CloudResourceEdgeWriter

	// CloudResourceContainerImageEdgeWriter projects lambda_function_uses_image and
	// ecs_task_definition_uses_image aws_relationship facts into canonical
	// CloudResource -> ContainerImage edges (issue #5450), the additive sibling
	// of CloudResourceEdgeWriter for cross-label targets (an image is not a
	// CloudResource). Non-nil alongside factload.FactLoader registers
	// DomainAWSCloudImageMaterialization; the handler gates on ReadinessLookup
	// so edges never resolve against an uncommitted source.
	CloudResourceContainerImageEdgeWriter awscloud.CloudResourceContainerImageEdgeWriter

	// RefreshAffectedGraph runs the value-flow refresh emit gate (issue #6785)
	// over each producer's committed keys. The four producer handlers consult
	// it after their graph writes commit, so the gate reads the run's own
	// edges. Optional: a nil value keeps every gate failed open (a spurious
	// refresh is a bounded extra solve; a missed one is silent accuracy loss);
	// production wires the durable graph-backed runner.
	RefreshAffectedGraph affected.Runner

	// ContainerImageExistence reports which candidate target ContainerImage
	// uids already exist in the canonical graph, so
	// AWSCloudImageMaterializationHandler.Handle can reclassify a
	// resolved-but-unmaterialized row as a skip instead of over-reporting an
	// edge the graph does not have (issue #5450 P1 follow-up). Optional: a nil
	// value skips the filter, matching ReadinessLookup/PriorGenerationCheck's
	// nil-safe test-wiring convention; production wires the durable graph-backed
	// lookup.
	ContainerImageExistence containerimage.ContainerImageExistenceLookup

	// GCPCloudResourceEdgeWriter projects gcp_cloud_relationship facts into
	// canonical GCP relationship edges between CloudResource nodes (issue #2348).
	// It must be non-nil alongside factload.FactLoader for the registry to register
	// DomainGCPRelationshipMaterialization; missing either one would drop every
	// gcp_cloud_relationship fact before it reaches the graph. The handler gates
	// on ReadinessLookup so edges never resolve against uncommitted GCP nodes.
	GCPCloudResourceEdgeWriter CloudResourceEdgeWriter

	// AzureCloudResourceEdgeWriter projects azure_cloud_relationship facts into
	// canonical Azure relationship edges between CloudResource nodes. It must be
	// non-nil alongside factload.FactLoader for the registry to register
	// DomainAzureRelationshipMaterialization; missing either one would drop every
	// azure_cloud_relationship fact before it reaches the graph. The handler gates
	// on ReadinessLookup so edges never resolve against uncommitted Azure nodes.
	AzureCloudResourceEdgeWriter CloudResourceEdgeWriter

	// WorkloadCloudRelationshipEdgeWriter projects exact workload anchors on
	// CloudResource facts into canonical WorkloadInstance USES CloudResource
	// edges. It must be non-nil alongside factload.FactLoader for the registry to register
	// DomainWorkloadCloudRelationshipMaterialization; missing either one keeps the
	// domain unregistered rather than dropping graph truth.
	WorkloadCloudRelationshipEdgeWriter WorkloadCloudRelationshipEdgeWriter

	// ObservabilityCoverageEdgeWriter projects exact observability coverage
	// decisions into canonical COVERS edges between CloudResource nodes (issue
	// #391 PR3). It must be non-nil alongside factload.FactLoader for the registry to
	// register DomainObservabilityCoverageMaterialization; missing either one
	// would drop every coverage materialization intent before it reaches the
	// graph. The handler also gates on ReadinessLookup so edges never resolve
	// against uncommitted nodes.
	ObservabilityCoverageEdgeWriter obscoverage.ObservabilityCoverageEdgeWriter

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
	// PR2). It must be non-nil alongside factload.FactLoader for the registry to register
	// DomainIAMCanAssumeMaterialization; missing either one would drop every
	// CAN_ASSUME materialization intent before it reaches the graph. The handler
	// also gates on ReadinessLookup so edges never resolve against uncommitted
	// nodes.
	IAMCanAssumeEdgeWriter iamcan.IAMCanAssumeEdgeWriter

	// S3LogsToEdgeWriter projects s3_bucket_posture logging_target_bucket fields
	// into canonical LOGS_TO edges between S3 bucket CloudResource nodes (issue
	// #1144 PR2). It must be non-nil alongside factload.FactLoader for the registry to
	// register DomainS3LogsToMaterialization; missing either one would drop every
	// LOGS_TO materialization intent before it reaches the graph. The handler
	// also gates on ReadinessLookup so edges never resolve against uncommitted
	// nodes.
	S3LogsToEdgeWriter s3logsto.S3LogsToEdgeWriter

	// S3ExternalPrincipalGrantWriter projects metadata-only
	// s3_external_principal_grant facts into canonical ExternalPrincipal nodes
	// and GRANTS_ACCESS_TO edges (issue #1231). It must be non-nil alongside
	// factload.FactLoader for the registry to register
	// DomainS3ExternalPrincipalGrantMaterialization; missing either one would
	// drop every external-principal grant intent before it reaches graph truth.
	// The handler also gates on ReadinessLookup so source S3 buckets resolve only
	// after CloudResource nodes commit.
	S3ExternalPrincipalGrantWriter s3grant.S3ExternalPrincipalGrantWriter

	// RDSPostureNodeWriter projects rds_instance_posture facts onto existing RDS
	// CloudResource nodes (issue #1233). It must be non-nil alongside factload.FactLoader
	// for the registry to register DomainRDSPostureMaterialization; missing either
	// one would drop every RDS posture intent before it reaches graph truth. The
	// handler also gates on ReadinessLookup so posture fields never write against
	// uncommitted CloudResource nodes.
	RDSPostureNodeWriter rdsposture.RDSPostureNodeWriter
	// EC2InstanceIdentityNodeWriter projects the #5448 aws_ec2_instance
	// aws_resource fact's ami_id onto the already-materialized EC2 instance
	// CloudResource node (owned by DomainEC2InstanceNodeMaterialization). It
	// must be non-nil alongside factload.FactLoader for the registry to register
	// DomainEC2InstanceIdentityMaterialization; missing either one would drop
	// every EC2 instance identity intent before it reaches graph truth. The
	// handler gates on ReadinessLookup against the EC2 instance node phase
	// (not the generic aws_resource phase) so ami_id never writes against an
	// uncommitted EC2 instance CloudResource node, and it never creates a
	// node — a missing uid is always a no-op.
	EC2InstanceIdentityNodeWriter ec2instance.EC2InstanceIdentityNodeWriter
	// EC2UsesProfileEdgeWriter projects ec2_instance_posture instance_profile_arn
	// into canonical USES_PROFILE edges between an EC2 instance CloudResource node
	// and the IAM instance-profile CloudResource node it uses (issue #1146 PR-B). It
	// must be non-nil alongside factload.FactLoader for the registry to register
	// DomainEC2UsesProfileMaterialization; missing either one would drop every
	// USES_PROFILE materialization intent before it reaches the graph. The handler
	// gates on a DUAL readiness lookup — both the EC2 instance node phase and the
	// IAM instance-profile node phase — so edges never resolve against an endpoint
	// that has not committed.
	EC2UsesProfileEdgeWriter ec2usesprofile.EC2UsesProfileEdgeWriter

	// IAMInstanceProfileRoleEdgeWriter projects IAM instance-profile role_arns
	// into canonical HAS_ROLE edges between IAM instance-profile and role
	// CloudResource nodes (issue #1299). It must be non-nil alongside factload.FactLoader
	// for the registry to register DomainIAMInstanceProfileRoleMaterialization;
	// missing either one would drop every HAS_ROLE materialization intent before it
	// reaches graph truth. The handler also gates on ReadinessLookup so edges never
	// resolve against uncommitted IAM nodes.
	IAMInstanceProfileRoleEdgeWriter iaminstprofile.IAMInstanceProfileRoleEdgeWriter

	// EC2BlockDeviceKMSPostureNodeWriter derives EC2 block-device KMS posture
	// from ec2_instance_posture block devices joined to EBS volume and KMS facts,
	// then writes reducer-owned properties onto existing EC2 CloudResource nodes
	// (issue #1304). It must be non-nil alongside factload.FactLoader for the registry to
	// register DomainEC2BlockDeviceKMSPostureMaterialization; missing either one
	// would drop every posture intent before it reaches graph truth. The handler
	// gates on a DUAL readiness lookup — the EC2 instance node phase plus the
	// EBS/KMS CloudResource node phase — so properties never write against
	// uncommitted node truth.
	EC2BlockDeviceKMSPostureNodeWriter ec2blockkms.EC2BlockDeviceKMSPostureNodeWriter

	// S3InternetExposureNodeWriter derives s3_bucket_posture internet exposure
	// state and writes reducer-owned properties onto existing S3 CloudResource
	// nodes (issue #1232). It must be non-nil alongside factload.FactLoader for the
	// registry to register DomainS3InternetExposureMaterialization; missing either
	// one would drop every exposure materialization intent before it reaches the
	// graph. The handler also gates on ReadinessLookup so node properties never
	// resolve against uncommitted S3 nodes.
	S3InternetExposureNodeWriter internetexposure.S3InternetExposureNodeWriter

	// EC2InternetExposureNodeWriter derives ec2_instance_posture internet
	// exposure state and writes reducer-owned properties onto existing EC2
	// CloudResource nodes (issue #1301). It must be non-nil alongside factload.FactLoader
	// for the registry to register DomainEC2InternetExposureMaterialization;
	// missing either one would drop every exposure materialization intent before
	// it reaches the graph. The handler gates on ReadinessLookup so node
	// properties never resolve against uncommitted EC2 nodes.
	EC2InternetExposureNodeWriter internetexposure.EC2InternetExposureNodeWriter

	// SSMHostGraphWriter projects admitted SSM host inventory observations
	// into canonical observation nodes and platform-pairing edges
	// (eshu-hq/eshu#9558). It must be non-nil alongside factload.FactLoader
	// and valid SSMHostBounds for the registry to register
	// DomainSSMHostInventoryMaterialization; missing any of the three would
	// drop every SSM inventory intent before it reaches graph truth.
	SSMHostGraphWriter ssmhost.HostGraphWriter
	// SSMHostBounds carries the fail-closed freshness configuration for SSM
	// host supersession (max capture age, cross-type skew, platform
	// observation age). Zero bounds leave the domain unwired.
	SSMHostBounds ssmhost.Bounds

	// ContainerImageIdentityWriter persists image-reference-keyed identity
	// decisions for Git, OCI registry, and runtime image evidence.
	ContainerImageIdentityWriter containerimage.ContainerImageIdentityWriter

	// ContainerImageIdentityGenerationCheck disambiguates an activation-epoch miss (issue #6502):
	// pending defers, superseded acks superseded, missing surfaces loudly. Nil keeps the legacy loud error.
	ContainerImageIdentityGenerationCheck GenerationFreshnessCheck

	// PackageProvenanceEdgeWriter projects exact/derived package-ownership and
	// package-publication decisions into canonical PUBLISHES graph edges
	// (issue #5457). Nil skips the projection; the package-source-correlation
	// domain still registers (Postgres decisions are unaffected).
	PackageProvenanceEdgeWriter correlation.PackageProvenanceEdgeWriter

	// ContainerImageProvenanceEdgeWriter projects exact_digest container image
	// identity decisions into canonical BUILT_FROM graph edges (issue #5457).
	// Nil skips the projection; the container_image_identity domain still
	// registers (Postgres decisions are unaffected).
	ContainerImageProvenanceEdgeWriter containerimage.ContainerImageProvenanceEdgeWriter

	// ContainerImageDerivedFromEdgeWriter projects Dockerfile base-image
	// lineage into canonical DERIVED_FROM graph edges (issue #5460). Nil skips
	// the projection; the container_image_identity domain still registers
	// (Postgres decisions are unaffected).
	ContainerImageDerivedFromEdgeWriter containerimage.ContainerImageDerivedFromEdgeWriter

	// cicdrun.CICDRunCorrelationWriter persists provider run, artifact, and
	// environment correlation decisions for CI/CD evidence.
	CICDRunCorrelationWriter cicdrun.CICDRunCorrelationWriter

	// ServiceCatalogCorrelationWriter persists service-catalog ownership and
	// repository correlation decisions.
	ServiceCatalogCorrelationWriter servicecatalog.ServiceCatalogCorrelationWriter

	// ServiceMaterializationWriter, when set, commits the additive per-service
	// ownership generation lineage (#1943) alongside the correlation facts so the
	// service-scope changed-since delta has a durable prior snapshot to diff. It
	// is optional; when nil the existing service-catalog contract is unchanged.
	ServiceMaterializationWriter servicecatalog.ServiceMaterializationWriter

	// ServiceRuntimeInstanceLoader (with ServiceMaterializationWriter) sources the
	// runtime evidence family (#1986); nil leaves generations without runtime rows.
	// It is wired only with ServiceRuntimeProducerReadiness, the #7816 fence on
	// in-flight workload_materialization work (serviceCatalogRuntimeInstanceLoader).
	ServiceRuntimeInstanceLoader    servicecatalog.RepositoryScopedRuntimeInstanceLoader
	ServiceRuntimeProducerReadiness servicecatalog.RuntimeProducerReadiness

	// ServiceDocumentationEvidenceLoader, when set alongside
	// ServiceMaterializationWriter, supplies the documentation facts that reference
	// each correlated service so the docs evidence family (#1988) is snapshotted
	// into each service generation. It is keyed by service id, not repository id.
	// It is optional; when nil the generation simply carries no docs rows.
	ServiceDocumentationEvidenceLoader servicecatalog.ServiceScopedDocumentationEvidenceLoader

	// ServiceIncidentEvidenceLoader, when set alongside
	// ServiceMaterializationWriter, supplies exact incident-routing evidence that
	// resolves to each correlated service so the incidents evidence family (#1989)
	// is snapshotted into each service generation. It is keyed by Eshu catalog
	// service id after the provider service id is resolved through durable reducer
	// correlations. It is optional; when nil the generation simply carries no
	// incident rows.
	ServiceIncidentEvidenceLoader servicecatalog.ServiceScopedIncidentEvidenceLoader

	// ServiceVulnerabilityAdvisoryLoader, when set alongside
	// ServiceMaterializationWriter, supplies the supply-chain advisory findings on
	// each correlated service's repository so the vulnerabilities evidence family
	// (#1990) is snapshotted into each service generation. It is keyed by repository
	// id: a service is attributed an advisory only through a real
	// reducer_supply_chain_impact_finding on its repository (#2127). It is optional;
	// when nil the generation simply carries no vulnerabilities rows.
	ServiceVulnerabilityAdvisoryLoader servicecatalog.ServiceVulnerabilityAdvisoryLoader

	// ObservabilityCoverageCorrelationWriter persists observability coverage
	// correlation decisions (covered, gap, ambiguous, stale, rejected) for
	// CloudWatch, CloudWatch Logs, and X-Ray facts.
	ObservabilityCoverageCorrelationWriter obscoverage.ObservabilityCoverageCorrelationWriter

	// SecurityGroupEndpointNodeWriter materializes the CIDR and managed
	// prefix-list source endpoints of aws_security_group_rule facts into canonical
	// CidrBlock and PrefixList graph nodes (issue #1135 PR2a). It must be non-nil
	// alongside factload.FactLoader for the registry to register
	// DomainSecurityGroupCidrMaterialization; missing either one would drop every
	// security-group rule endpoint before it reaches the graph. The handler also
	// publishes the canonical-nodes-committed phase through
	// GraphProjectionPhasePublisher so the later network-reachability edge slice
	// (#1135 PR2b) can gate on it exactly like the AWS relationship edge gates on
	// the CloudResource node phase (#805).
	SecurityGroupEndpointNodeWriter secgroup.SecurityGroupEndpointNodeWriter

	// SecurityGroupRuleNodeWriter materializes aws_security_group_rule facts into
	// canonical port-precise :SecurityGroupRule graph nodes and (via the handler)
	// publishes the rule-uid canonical-nodes phase the edge slice gates on (issue
	// #1135 PR2b, Option D). Required alongside factload.FactLoader to register
	// DomainSecurityGroupRuleMaterialization.
	SecurityGroupRuleNodeWriter secgroup.SecurityGroupRuleNodeWriter

	// SecurityGroupReachabilityWriter projects aws_security_group_rule facts into
	// the Option D reachability edges (SecurityGroup -> rule ALLOWS_INGRESS/EGRESS,
	// rule -[:TO]-> endpoint). Required alongside factload.FactLoader to register
	// DomainSecurityGroupReachabilityMaterialization; the handler gates on the rule,
	// endpoint, and SG node keyspaces via ReadinessLookup (#1135 PR2b).
	SecurityGroupReachabilityWriter secgroup.SecurityGroupReachabilityWriter

	// IAMEscalationEdgeWriter projects merged aws_iam_permission facts into
	// conservative IAM CAN_ESCALATE_TO privilege-escalation edges between IAM
	// principal and target CloudResource nodes (issue #1134 PR3). It must be non-nil
	// alongside factload.FactLoader for the registry to register
	// DomainIAMEscalationMaterialization; missing either one would drop every
	// escalation intent before it reached the graph. The handler also gates on
	// ReadinessLookup so edges never resolve against uncommitted IAM nodes.
	IAMEscalationEdgeWriter iamescalation.IAMEscalationEdgeWriter

	// IAMCanPerformEdgeWriter projects merged aws_iam_permission facts into
	// conservative identity-policy-only IAM CAN_PERFORM effective-permission edges
	// between an IAM principal and the resource CloudResource node an identity
	// policy grants a catalogued sensitive action on (issue #1134 PR4a). It must be
	// non-nil alongside factload.FactLoader for the registry to register
	// DomainIAMCanPerformMaterialization; missing either one would drop every
	// CAN_PERFORM intent before it reached the graph. The handler also gates on
	// ReadinessLookup so edges never resolve against uncommitted nodes.
	IAMCanPerformEdgeWriter iamcan.IAMCanPerformEdgeWriter

	// PackageWriter persists package ownership candidates and
	// manifest-backed consumption decisions for package-registry evidence.
	PackageWriter correlation.PackageWriter
}
