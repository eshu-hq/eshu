package reducer

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/truth"
)

// OwnershipShape records whether a reducer domain owns cross-source and
// cross-scope reconciliation and how it produces canonical truth. A valid
// reducer domain MUST be cross-source, cross-scope, and produce canonical
// truth via at least one of two surfaces: a graph write (CanonicalWrite) or a
// metric counter + structured log emission (CounterEmit). CounterEmit was
// added in chunk #43 to admit the terraform_config_state_drift domain, whose
// v1 truth surface is bounded counter emission rather than canonical graph
// nodes (graph projection of drift nodes lands in a follow-up chunk per the
// design doc §10).
type OwnershipShape struct {
	CrossSource    bool
	CrossScope     bool
	CanonicalWrite bool
	// CounterEmit marks a domain whose canonical truth contract is satisfied
	// by emitting bounded metric counters and structured logs rather than
	// writing canonical graph nodes. At least one of CanonicalWrite or
	// CounterEmit MUST be true; both may be true for domains that emit
	// counters alongside graph writes.
	CounterEmit bool
}

// Validate checks that the ownership shape matches the reducer boundary.
//
// The invariant: a valid reducer domain is cross-source, cross-scope, and
// produces canonical truth via at least one of CanonicalWrite or CounterEmit.
// Prior to chunk #43 the rule required CanonicalWrite specifically; it was
// relaxed to accept CounterEmit so domains whose v1 truth surface is bounded
// metric emission can register without setting CanonicalWrite to a value the
// handler does not honor.
func (o OwnershipShape) Validate() error {
	if !o.CrossSource {
		return errors.New("reducers must be cross-source")
	}
	if !o.CrossScope {
		return errors.New("reducers must be cross-scope")
	}
	if !o.CanonicalWrite && !o.CounterEmit {
		return errors.New("reducers must declare CanonicalWrite or CounterEmit")
	}

	return nil
}

// DomainDefinition describes one reducer domain and its ownership shape.
type DomainDefinition struct {
	Domain        Domain
	Summary       string
	Ownership     OwnershipShape
	TruthContract truth.Contract
	Handler       Handler
}

// Validate checks the domain definition for registration.
func (d DomainDefinition) Validate() error {
	if err := d.Domain.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(d.Summary) == "" {
		return errors.New("summary must not be blank")
	}
	if err := d.Ownership.Validate(); err != nil {
		return err
	}
	if err := d.TruthContract.Validate(); err != nil {
		return err
	}

	return nil
}

// Registry owns the explicit reducer domain catalog and handlers.
type Registry struct {
	ordered []Domain
	defs    map[Domain]DomainDefinition
}

// Handler executes one reducer intent for a registered domain.
type Handler interface {
	Handle(context.Context, Intent) (Result, error)
}

// HandlerFunc adapts a function into a Handler.
type HandlerFunc func(context.Context, Intent) (Result, error)

// Handle executes the wrapped function.
func (f HandlerFunc) Handle(ctx context.Context, intent Intent) (Result, error) {
	return f(ctx, intent)
}

// NewRegistry constructs an empty reducer registry.
func NewRegistry() Registry {
	return Registry{
		defs: make(map[Domain]DomainDefinition),
	}
}

// Register adds a reducer domain definition to the registry.
func (r *Registry) Register(def DomainDefinition) error {
	if err := def.Validate(); err != nil {
		return err
	}
	if _, exists := r.defs[def.Domain]; exists {
		return fmt.Errorf("domain %q already registered", def.Domain)
	}

	if r.defs == nil {
		r.defs = make(map[Domain]DomainDefinition)
	}
	r.defs[def.Domain] = def
	r.ordered = append(r.ordered, def.Domain)

	return nil
}

// Definition returns the registered domain definition.
func (r Registry) Definition(domain Domain) (DomainDefinition, bool) {
	def, ok := r.defs[domain]
	return def, ok
}

// Definitions returns the registered domain definitions in registration order.
func (r Registry) Definitions() []DomainDefinition {
	definitions := make([]DomainDefinition, 0, len(r.ordered))
	for _, domain := range r.ordered {
		definitions = append(definitions, r.defs[domain])
	}

	return definitions
}

// SortedDomains returns the registered domains in deterministic order.
func (r Registry) SortedDomains() []Domain {
	domains := make([]Domain, 0, len(r.ordered))
	domains = append(domains, r.ordered...)
	sort.SliceStable(domains, func(i, j int) bool {
		return domains[i] < domains[j]
	})

	return domains
}

// DefaultDomainDefinitions returns the truthful default reducer domain catalog
// for the domains implemented by the current rewrite slice.
func DefaultDomainDefinitions() []DomainDefinition {
	return []DomainDefinition{
		{
			Domain:  DomainWorkloadIdentity,
			Summary: "resolve canonical workload identity across sources",
			Ownership: OwnershipShape{
				CrossSource:    true,
				CrossScope:     true,
				CanonicalWrite: true,
			},
			TruthContract: truth.Contract{
				CanonicalKind: "workload_identity",
				SourceLayers: []truth.Layer{
					truth.LayerSourceDeclaration,
				},
			},
		},
		{
			Domain:  DomainCloudAssetResolution,
			Summary: "resolve canonical cloud asset identity across sources",
			Ownership: OwnershipShape{
				CrossSource:    true,
				CrossScope:     true,
				CanonicalWrite: true,
			},
			TruthContract: truth.Contract{
				CanonicalKind: "cloud_asset",
				SourceLayers: []truth.Layer{
					truth.LayerSourceDeclaration,
					truth.LayerAppliedDeclaration,
					truth.LayerObservedResource,
				},
			},
		},
		{
			Domain:  DomainDeploymentMapping,
			Summary: "materialize platform bindings across sources",
			Ownership: OwnershipShape{
				CrossSource:    true,
				CrossScope:     true,
				CanonicalWrite: true,
			},
			TruthContract: truth.Contract{
				CanonicalKind: "deployment_mapping",
				SourceLayers: []truth.Layer{
					truth.LayerSourceDeclaration,
					truth.LayerAppliedDeclaration,
				},
			},
		},
		{
			Domain:  DomainCodeCallMaterialization,
			Summary: "materialize canonical code call edges from parser facts",
			Ownership: OwnershipShape{
				CrossSource:    true,
				CrossScope:     true,
				CanonicalWrite: true,
			},
			TruthContract: truth.Contract{
				CanonicalKind: "code_call_materialization",
				SourceLayers: []truth.Layer{
					truth.LayerSourceDeclaration,
				},
			},
		},
		{
			Domain:  DomainWorkloadMaterialization,
			Summary: "materialize canonical workload graph from content store facts",
			Ownership: OwnershipShape{
				CrossSource:    true,
				CrossScope:     true,
				CanonicalWrite: true,
			},
			TruthContract: truth.Contract{
				CanonicalKind: "workload_materialization",
				SourceLayers: []truth.Layer{
					truth.LayerSourceDeclaration,
				},
			},
		},
		{
			Domain:  DomainSemanticEntityMaterialization,
			Summary: "materialize annotation, typedef, type alias, and component semantic nodes from parser facts",
			Ownership: OwnershipShape{
				CrossSource:    true,
				CrossScope:     true,
				CanonicalWrite: true,
			},
			TruthContract: truth.Contract{
				CanonicalKind: "semantic_entity_materialization",
				SourceLayers: []truth.Layer{
					truth.LayerSourceDeclaration,
				},
			},
		},
		{
			Domain:  DomainSQLRelationshipMaterialization,
			Summary: "materialize canonical SQL relationship edges from parser SQL entity metadata",
			Ownership: OwnershipShape{
				CrossSource:    true,
				CrossScope:     true,
				CanonicalWrite: true,
			},
			TruthContract: truth.Contract{
				CanonicalKind: "sql_relationship_materialization",
				SourceLayers: []truth.Layer{
					truth.LayerSourceDeclaration,
				},
			},
		},
		{
			Domain:  DomainInheritanceMaterialization,
			Summary: "materialize canonical inheritance, override, and alias edges from parser entity bases and trait adaptation metadata",
			Ownership: OwnershipShape{
				CrossSource:    true,
				CrossScope:     true,
				CanonicalWrite: true,
			},
			TruthContract: truth.Contract{
				CanonicalKind: "inheritance_materialization",
				SourceLayers: []truth.Layer{
					truth.LayerSourceDeclaration,
				},
			},
		},
	}
}

// configStateDriftDomainDefinition returns the additive DomainDefinition for
// terraform_config_state_drift. The drift domain is intentionally NOT part of
// DefaultDomainDefinitions because its handler requires three adapters
// (TerraformBackendResolver, DriftEvidenceLoader, DriftLogger) that the
// production reducer binary wires explicitly. Registering the domain without
// those adapters silently drops every intent — the additive pattern keeps the
// catalog honest about what the runtime can actually serve. See defaults.go
// (implementedDefaultDomainDefinitions) for the wiring gate.
func configStateDriftDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainConfigStateDrift,
		Summary: "correlate Terraform config (parsed HCL) against state snapshots to detect five drift kinds",
		// CounterEmit declares the v1 truth surface: bounded metric
		// counters + structured logs. Graph projection follows per
		// design doc §10.
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: false,
			CounterEmit:    true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "config_state_drift",
			SourceLayers: []truth.Layer{
				truth.LayerSourceDeclaration,
				truth.LayerObservedResource,
			},
		},
	}
}

// packageSourceCorrelationDomainDefinition returns the additive definition for
// the package source-correlation classifier. Its v1 truth surface is bounded
// outcome counters; source hints remain provenance-only and emit no canonical
// package ownership graph writes.
func packageSourceCorrelationDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainPackageSourceCorrelation,
		Summary: "classify package-registry source hints against active repository remotes",
		Ownership: OwnershipShape{
			CrossSource: true,
			CrossScope:  true,
			CounterEmit: true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "package_source_correlation",
			SourceLayers: []truth.Layer{
				truth.LayerSourceDeclaration,
			},
		},
	}
}

// awsCloudRuntimeDriftDomainDefinition returns the additive definition for
// AWS runtime drift publication. The domain consumes admitted
// aws_cloud_runtime_drift candidates and writes durable reducer facts, but it
// deliberately does not declare graph writes until the drift node and query
// surface shape are frozen in the active ADR.
func awsCloudRuntimeDriftDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainAWSCloudRuntimeDrift,
		Summary: "publish admitted AWS runtime orphan and unmanaged drift findings as canonical reducer facts",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
			CounterEmit:    true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "aws_cloud_runtime_drift",
			SourceLayers: []truth.Layer{
				truth.LayerSourceDeclaration,
				truth.LayerAppliedDeclaration,
				truth.LayerObservedResource,
			},
		},
	}
}
