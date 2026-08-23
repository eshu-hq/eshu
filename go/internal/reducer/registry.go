// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"sort"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/truth"
)

// OwnershipShape records whether a reducer domain owns cross-source and
// cross-scope reconciliation and how it produces canonical truth. A valid
// reducer domain MUST be cross-source, cross-scope, and produce canonical
// truth via at least one of two surfaces: a durable canonical write
// (CanonicalWrite) or a metric counter + structured log emission (CounterEmit).
// CanonicalWrite covers reducer-owned truth written to durable facts or graph
// nodes; graph-neutral domains must document that boundary explicitly.
// CounterEmit was added in chunk #43 to admit the terraform_config_state_drift
// domain, whose v1 truth surface is bounded counter emission rather than
// canonical graph nodes (graph projection of drift nodes lands in a follow-up
// chunk per the design doc §10).
type OwnershipShape = reducercontract.OwnershipShape

// DomainDefinition describes one reducer domain and its ownership shape.
type DomainDefinition = reducercontract.DomainDefinition

// Registry owns the explicit reducer domain catalog and handlers.
type Registry struct {
	ordered []Domain
	defs    map[Domain]DomainDefinition
}

// Handler executes one reducer intent for a registered domain.
type Handler = reducercontract.Handler

// HandlerFunc adapts a function into a Handler.
type HandlerFunc = reducercontract.HandlerFunc

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
			Domain:  DomainPlatformInfraMaterialization,
			Summary: "emit platform_infra intents for Repository PROVISIONS_PLATFORM edges from Terraform/terragrunt facts",
			Ownership: OwnershipShape{
				CrossSource:    true,
				CrossScope:     true,
				CanonicalWrite: true,
			},
			TruthContract: truth.Contract{
				CanonicalKind: "platform_infra_materialization",
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
			Domain:  DomainShellExecMaterialization,
			Summary: "materialize canonical shell execution edges from parser command-call evidence",
			Ownership: OwnershipShape{
				CrossSource:    true,
				CrossScope:     true,
				CanonicalWrite: true,
			},
			TruthContract: truth.Contract{
				CanonicalKind: "shell_exec_materialization",
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
		{
			Domain:  DomainDocumentationMaterialization,
			Summary: "materialize canonical DOCUMENTS edges from exact documentation entity mentions to resolved code entities and workloads",
			Ownership: OwnershipShape{
				CrossSource:    true,
				CrossScope:     true,
				CanonicalWrite: true,
			},
			TruthContract: truth.Contract{
				CanonicalKind: "documentation_materialization",
				SourceLayers: []truth.Layer{
					truth.LayerSourceDeclaration,
				},
			},
		},
		{
			Domain:  DomainRationaleMaterialization,
			Summary: "materialize canonical EXPLAINS edges from intent-comment rationale to the code entities they precede",
			Ownership: OwnershipShape{
				CrossSource:    true,
				CrossScope:     true,
				CanonicalWrite: true,
			},
			TruthContract: truth.Contract{
				CanonicalKind: "rationale_materialization",
				SourceLayers: []truth.Layer{
					truth.LayerSourceDeclaration,
				},
			},
		},
		{
			Domain:  DomainCodeownersOwnership,
			Summary: "materialize canonical DECLARES_CODEOWNER edges from directly-emitted codeowners.ownership facts to the CodeownerTeam a CODEOWNERS rule pattern names",
			Ownership: OwnershipShape{
				CrossSource:    true,
				CrossScope:     true,
				CanonicalWrite: true,
			},
			TruthContract: truth.Contract{
				CanonicalKind: "codeowners_ownership",
				SourceLayers: []truth.Layer{
					truth.LayerSourceDeclaration,
				},
			},
		},
		{
			Domain:  DomainSubmodulePin,
			Summary: "materialize canonical PINS_SUBMODULE edges from directly-emitted submodule.pin facts between a parent Repository and the Repository its submodule URL resolved to",
			Ownership: OwnershipShape{
				CrossSource:    true,
				CrossScope:     true,
				CanonicalWrite: true,
			},
			TruthContract: truth.Contract{
				CanonicalKind: "submodule_pin",
				SourceLayers: []truth.Layer{
					truth.LayerSourceDeclaration,
				},
			},
		},
	}
}
