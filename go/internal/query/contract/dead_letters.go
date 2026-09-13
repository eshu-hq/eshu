// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

// operatorDeadLettersListCapability is the capability key for the bounded
// operator dead-letter list. It reads durable fact_work_items state from
// Postgres and does not require the graph backend.
const operatorDeadLettersListCapability = "operator.dead_letters.list"

func init() {
	register(operatorDeadLettersListCapability, capabilitySupport{
		LocalLightweightMax:   &truthExact,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalLightweight,
	})
}
