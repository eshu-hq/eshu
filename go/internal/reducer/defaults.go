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

	// ContainerImageIdentityWriter persists digest-keyed image identity
	// decisions for Git, OCI registry, and runtime image evidence.
	ContainerImageIdentityWriter ContainerImageIdentityWriter
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
	if handlers.FactLoader != nil {
		packageSource := packageSourceCorrelationDomainDefinition()
		packageSource.Handler = PackageSourceCorrelationHandler{
			FactLoader:  handlers.FactLoader,
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
