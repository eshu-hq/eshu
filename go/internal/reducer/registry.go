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
// cross-scope reconciliation.
type OwnershipShape struct {
	CrossSource    bool
	CrossScope     bool
	CanonicalWrite bool
}

// Validate checks that the ownership shape matches the reducer boundary.
func (o OwnershipShape) Validate() error {
	if !o.CrossSource {
		return errors.New("reducers must be cross-source")
	}
	if !o.CrossScope {
		return errors.New("reducers must be cross-scope")
	}
	if !o.CanonicalWrite {
		return errors.New("reducers must target canonical shared truth")
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
