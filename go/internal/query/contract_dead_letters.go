// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// operatorDeadLettersListCapability is the capability key for the bounded
// operator dead-letter list. It reads durable fact_work_items state from
// Postgres and does not require the graph backend.
const operatorDeadLettersListCapability = "operator.dead_letters.list"

func init() {
	capabilityMatrix[operatorDeadLettersListCapability] = capabilitySupport{
		LocalLightweightMax:   &truthExact,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalLightweight,
	}
}
