package reducer

import (
	"log/slog"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/truth"
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

	// PackageCorrelationWriter persists package ownership candidates and
	// manifest-backed consumption decisions for package-registry evidence.
	PackageCorrelationWriter PackageCorrelationWriter
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
	if handlers.TerraformBackendResolver != nil &&
		handlers.DriftEvidenceLoader != nil &&
		handlers.DriftLogger != nil {
		drift := configStateDriftDomainDefinition()
		drift.Handler = TerraformConfigStateDriftHandler{
			Resolver:       handlers.TerraformBackendResolver,
			EvidenceLoader: handlers.DriftEvidenceLoader,
			Instruments:    handlers.Instruments,
			Logger:         handlers.DriftLogger,
		}
		definitions = append(definitions, drift)
	}
	if handlers.FactLoader != nil && handlers.PackageCorrelationWriter != nil {
		packageSource := packageSourceCorrelationDomainDefinition()
		packageSource.Handler = PackageSourceCorrelationHandler{
			FactLoader:  handlers.FactLoader,
			Writer:      handlers.PackageCorrelationWriter,
			Instruments: handlers.Instruments,
		}
		definitions = append(definitions, packageSource)
	}
	if handlers.FactLoader != nil && handlers.ContainerImageIdentityWriter != nil {
		imageIdentity := containerImageIdentityDomainDefinition()
		imageIdentity.Handler = ContainerImageIdentityHandler{
			FactLoader:  handlers.FactLoader,
			Writer:      handlers.ContainerImageIdentityWriter,
			Instruments: handlers.Instruments,
		}
		definitions = append(definitions, imageIdentity)
	}
	if handlers.FactLoader != nil && handlers.CICDRunCorrelationWriter != nil {
		cicdRun := cicdRunCorrelationDomainDefinition()
		cicdRun.Handler = CICDRunCorrelationHandler{
			FactLoader:  handlers.FactLoader,
			Writer:      handlers.CICDRunCorrelationWriter,
			Instruments: handlers.Instruments,
		}
		definitions = append(definitions, cicdRun)
	}
	if handlers.FactLoader != nil && handlers.ServiceCatalogCorrelationWriter != nil {
		serviceCatalog := serviceCatalogCorrelationDomainDefinition()
		serviceCatalog.Handler = ServiceCatalogCorrelationHandler{
			FactLoader:  handlers.FactLoader,
			Writer:      handlers.ServiceCatalogCorrelationWriter,
			Instruments: handlers.Instruments,
		}
		definitions = append(definitions, serviceCatalog)
	}
	if handlers.FactLoader != nil && handlers.ObservabilityCoverageCorrelationWriter != nil {
		observability := observabilityCoverageCorrelationDomainDefinition()
		observability.Handler = ObservabilityCoverageCorrelationHandler{
			FactLoader:  handlers.FactLoader,
			Writer:      handlers.ObservabilityCoverageCorrelationWriter,
			Instruments: handlers.Instruments,
		}
		definitions = append(definitions, observability)
	}
	if handlers.FactLoader != nil && handlers.KubernetesCorrelationWriter != nil {
		kubernetes := kubernetesCorrelationDomainDefinition()
		kubernetes.Handler = KubernetesCorrelationHandler{
			FactLoader:  handlers.FactLoader,
			Writer:      handlers.KubernetesCorrelationWriter,
			Instruments: handlers.Instruments,
		}
		definitions = append(definitions, kubernetes)
	}
	if handlers.FactLoader != nil && handlers.SBOMAttestationAttachmentWriter != nil {
		attachments := sbomAttestationAttachmentDomainDefinition()
		attachments.Handler = SBOMAttestationAttachmentHandler{
			FactLoader:  handlers.FactLoader,
			Writer:      handlers.SBOMAttestationAttachmentWriter,
			Instruments: handlers.Instruments,
		}
		definitions = append(definitions, attachments)
	}
	if handlers.FactLoader != nil && handlers.SupplyChainImpactWriter != nil {
		impact := supplyChainImpactDomainDefinition()
		impact.Handler = SupplyChainImpactHandler{
			FactLoader:  handlers.FactLoader,
			Writer:      handlers.SupplyChainImpactWriter,
			Instruments: handlers.Instruments,
		}
		definitions = append(definitions, impact)
	}
	if handlers.FactLoader != nil && handlers.SecurityAlertReconciliationWriter != nil {
		securityAlerts := securityAlertReconciliationDomainDefinition()
		securityAlerts.Handler = SecurityAlertReconciliationHandler{
			FactLoader:  handlers.FactLoader,
			Writer:      handlers.SecurityAlertReconciliationWriter,
			Instruments: handlers.Instruments,
		}
		definitions = append(definitions, securityAlerts)
	}
	if handlers.AWSCloudRuntimeDriftEvidenceLoader != nil &&
		handlers.AWSCloudRuntimeDriftWriter != nil {
		awsRuntimeDrift := awsCloudRuntimeDriftDomainDefinition()
		awsRuntimeDrift.Handler = AWSCloudRuntimeDriftHandler{
			EvidenceLoader: handlers.AWSCloudRuntimeDriftEvidenceLoader,
			Writer:         handlers.AWSCloudRuntimeDriftWriter,
			Instruments:    handlers.Instruments,
			Logger:         handlers.AWSCloudRuntimeDriftLogger,
		}
		definitions = append(definitions, awsRuntimeDrift)
	}
	if handlers.FactLoader != nil && handlers.CloudResourceNodeWriter != nil {
		awsResources := awsResourceMaterializationDomainDefinition()
		awsResources.Handler = AWSResourceMaterializationHandler{
			FactLoader:     handlers.FactLoader,
			NodeWriter:     handlers.CloudResourceNodeWriter,
			PhasePublisher: handlers.GraphProjectionPhasePublisher,
		}
		definitions = append(definitions, awsResources)
	}
	if handlers.FactLoader != nil && handlers.KubernetesWorkloadNodeWriter != nil {
		kubernetesWorkloads := kubernetesWorkloadMaterializationDomainDefinition()
		kubernetesWorkloads.Handler = KubernetesWorkloadMaterializationHandler{
			FactLoader:     handlers.FactLoader,
			NodeWriter:     handlers.KubernetesWorkloadNodeWriter,
			PhasePublisher: handlers.GraphProjectionPhasePublisher,
			Instruments:    handlers.Instruments,
		}
		definitions = append(definitions, kubernetesWorkloads)
	}
	if handlers.FactLoader != nil && handlers.CloudResourceEdgeWriter != nil {
		awsRelationships := awsRelationshipMaterializationDomainDefinition()
		awsRelationships.Handler = AWSRelationshipMaterializationHandler{
			FactLoader:           handlers.FactLoader,
			EdgeWriter:           handlers.CloudResourceEdgeWriter,
			ReadinessLookup:      handlers.ReadinessLookup,
			PriorGenerationCheck: handlers.PriorGenerationCheck,
			Tracer:               handlers.Tracer,
			Instruments:          handlers.Instruments,
		}
		definitions = append(definitions, awsRelationships)
	}
	if handlers.FactLoader != nil && handlers.ObservabilityCoverageEdgeWriter != nil {
		coverageEdges := observabilityCoverageMaterializationDomainDefinition()
		coverageEdges.Handler = ObservabilityCoverageMaterializationHandler{
			FactLoader:           handlers.FactLoader,
			EdgeWriter:           handlers.ObservabilityCoverageEdgeWriter,
			ReadinessLookup:      handlers.ReadinessLookup,
			PriorGenerationCheck: handlers.PriorGenerationCheck,
			Tracer:               handlers.Tracer,
			Instruments:          handlers.Instruments,
		}
		definitions = append(definitions, coverageEdges)
	}
	if handlers.FactLoader != nil && handlers.KubernetesCorrelationEdgeWriter != nil {
		kubernetesEdges := kubernetesCorrelationMaterializationDomainDefinition()
		kubernetesEdges.Handler = KubernetesCorrelationMaterializationHandler{
			FactLoader:           handlers.FactLoader,
			EdgeWriter:           handlers.KubernetesCorrelationEdgeWriter,
			ReadinessLookup:      handlers.ReadinessLookup,
			PriorGenerationCheck: handlers.PriorGenerationCheck,
			Tracer:               handlers.Tracer,
			Instruments:          handlers.Instruments,
		}
		definitions = append(definitions, kubernetesEdges)
	}
	if handlers.DeployableUnitCorrelationHandler != nil {
		definitions = append(definitions, DomainDefinition{
			Domain:  DomainDeployableUnitCorrelation,
			Summary: "correlate deployable-unit candidates across sources before workload admission",
			Ownership: OwnershipShape{
				CrossSource:    true,
				CrossScope:     true,
				CanonicalWrite: true,
			},
			TruthContract: truth.Contract{
				CanonicalKind: "deployable_unit_correlation",
				SourceLayers: []truth.Layer{
					truth.LayerSourceDeclaration,
				},
			},
			Handler: handlers.DeployableUnitCorrelationHandler,
		})
	}

	return definitions
}
