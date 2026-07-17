// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// replatformingPlanReadinessCapability is the provider-neutral capability that
// describes replatforming plan and readiness behavior over the source-state
// taxonomy. It is declared before its serving route/tool lands so the truth
// contract, capability matrix, and profile gating are fixed first. Lightweight
// local runtime cannot materialize the reducer-owned evidence the plan needs,
// so that profile returns unsupported_capability rather than a downgraded plan.
const replatformingPlanReadinessCapability = "replatforming.plan.readiness"

// replatformingSelectorInventoryCapability identifies the bounded active AWS
// collector-scope inventory used to choose an honest replatforming review
// anchor. It is distinct from plan composition so operators can attribute
// selector discovery latency and missing collector evidence independently.
const replatformingSelectorInventoryCapability = "replatforming.selector_inventory"

func init() {
	capabilityMatrix[replatformingPlanReadinessCapability] = capabilitySupport{
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthDerived,
		LocalFullStackMax:     &truthDerived,
		ProductionMax:         &truthDerived,
		RequiredProfile:       ProfileLocalAuthoritative,
	}
	capabilityMatrix[replatformingSelectorInventoryCapability] = capabilitySupport{
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthDerived,
		LocalFullStackMax:     &truthDerived,
		ProductionMax:         &truthDerived,
		RequiredProfile:       ProfileLocalAuthoritative,
	}
}
