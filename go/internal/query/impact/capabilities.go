// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// This file owns the ImpactHandler family's capability rows. The rows live
// here — declared by the family that implements the routes, following the
// semanticsearch.Support precedent — so the family's own test binary
// observes the same profile gates production does without importing package
// query (which would cycle through compare.go and family_impact_shim.go).
// The query root's matrix no longer repeats these rows: two copies drift
// silently. Registration is last-write-wins and idempotent for identical
// rows; the contract suite rejects duplicate initialization, so keep each
// capability in exactly one home. See #6060.
package impact

import "github.com/eshu-hq/eshu/go/internal/query/querycontract"

var (
	impactTruthExact   = querycontract.TruthLevelExact
	impactTruthDerived = querycontract.TruthLevelDerived
)

func init() {
	querycontract.RegisterCapabilities(
		querycontract.CapabilityRegistration{
			Capability: "platform_impact.deployment_chain",
			Support: querycontract.CapabilitySupport{
				LocalLightweightMax:   nil,
				LocalAuthoritativeMax: &impactTruthExact,
				LocalFullStackMax:     &impactTruthExact,
				ProductionMax:         &impactTruthExact,
				RequiredProfile:       querycontract.ProfileLocalAuthoritative,
			},
		},
		querycontract.CapabilityRegistration{
			Capability: "platform_impact.deployment_config_influence",
			Support: querycontract.CapabilitySupport{
				LocalLightweightMax:   nil,
				LocalAuthoritativeMax: &impactTruthExact,
				LocalFullStackMax:     &impactTruthExact,
				ProductionMax:         &impactTruthExact,
				RequiredProfile:       querycontract.ProfileLocalAuthoritative,
			},
		},
		querycontract.CapabilityRegistration{
			Capability: "platform_impact.blast_radius",
			Support: querycontract.CapabilitySupport{
				LocalLightweightMax:   nil,
				LocalAuthoritativeMax: &impactTruthDerived,
				LocalFullStackMax:     &impactTruthExact,
				ProductionMax:         &impactTruthExact,
				RequiredProfile:       querycontract.ProfileLocalAuthoritative,
			},
		},
		querycontract.CapabilityRegistration{
			Capability: "platform_impact.change_surface",
			Support: querycontract.CapabilitySupport{
				LocalLightweightMax:   nil,
				LocalAuthoritativeMax: &impactTruthExact,
				LocalFullStackMax:     &impactTruthExact,
				ProductionMax:         &impactTruthExact,
				RequiredProfile:       querycontract.ProfileLocalAuthoritative,
			},
		},
		querycontract.CapabilityRegistration{
			Capability: "platform_impact.pre_change",
			Support: querycontract.CapabilitySupport{
				LocalLightweightMax:   nil,
				LocalAuthoritativeMax: &impactTruthExact,
				LocalFullStackMax:     &impactTruthExact,
				ProductionMax:         &impactTruthExact,
				RequiredProfile:       querycontract.ProfileLocalAuthoritative,
			},
		},
		querycontract.CapabilityRegistration{
			Capability: "platform_impact.developer_change_plan",
			Support: querycontract.CapabilitySupport{
				LocalLightweightMax:   nil,
				LocalAuthoritativeMax: &impactTruthExact,
				LocalFullStackMax:     &impactTruthExact,
				ProductionMax:         &impactTruthExact,
				RequiredProfile:       querycontract.ProfileLocalAuthoritative,
			},
		},
		querycontract.CapabilityRegistration{
			Capability: "platform_impact.entity_map",
			Support: querycontract.CapabilitySupport{
				LocalLightweightMax:   nil,
				LocalAuthoritativeMax: &impactTruthExact,
				LocalFullStackMax:     &impactTruthExact,
				ProductionMax:         &impactTruthExact,
				RequiredProfile:       querycontract.ProfileLocalAuthoritative,
			},
		},
		querycontract.CapabilityRegistration{
			Capability: "platform_impact.resource_to_code",
			Support: querycontract.CapabilitySupport{
				LocalLightweightMax:   nil,
				LocalAuthoritativeMax: &impactTruthExact,
				LocalFullStackMax:     &impactTruthExact,
				ProductionMax:         &impactTruthExact,
				RequiredProfile:       querycontract.ProfileLocalAuthoritative,
			},
		},
		querycontract.CapabilityRegistration{
			Capability: "platform_impact.resource_investigation",
			Support: querycontract.CapabilitySupport{
				LocalLightweightMax:   nil,
				LocalAuthoritativeMax: &impactTruthExact,
				LocalFullStackMax:     &impactTruthExact,
				ProductionMax:         &impactTruthExact,
				RequiredProfile:       querycontract.ProfileLocalAuthoritative,
			},
		},
		querycontract.CapabilityRegistration{
			Capability: "platform_impact.dependency_path",
			Support: querycontract.CapabilitySupport{
				LocalLightweightMax:   nil,
				LocalAuthoritativeMax: &impactTruthExact,
				LocalFullStackMax:     &impactTruthExact,
				ProductionMax:         &impactTruthExact,
				RequiredProfile:       querycontract.ProfileLocalAuthoritative,
			},
		},
		querycontract.CapabilityRegistration{
			Capability: "code_to_cloud.trace_exposure_path",
			Support: querycontract.CapabilitySupport{
				// Level 1 reachability is symbol-level, never value-flow, so the ceiling is
				// derived, never exact (#2704 non-goals). It needs the authoritative call
				// graph for the bounded CALLS traversal.
				LocalLightweightMax:   nil,
				LocalAuthoritativeMax: &impactTruthDerived,
				LocalFullStackMax:     &impactTruthDerived,
				ProductionMax:         &impactTruthDerived,
				RequiredProfile:       querycontract.ProfileLocalAuthoritative,
			},
		},
	)
}
