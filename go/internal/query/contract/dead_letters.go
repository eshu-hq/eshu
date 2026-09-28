// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

// operatorDeadLettersListCapability is the capability key for the bounded
// operator dead-letter list. It reads durable fact_work_items state from
// Postgres and does not require the graph backend.
const operatorDeadLettersListCapability = "operator.dead_letters.list"

// operatorChangedSincePoisonedLinksListCapability is the capability key for
// the bounded changed-since poisoned/retrying link read (#7290). It reads
// durable changed_since_scope_cursor state from Postgres and does not
// require the graph backend. Registered here, alongside its sibling
// operator-read capability, rather than in its own file: this package sits
// at the dirgate 40-file cap (issue #6054), and both rows are the same
// shape.
const operatorChangedSincePoisonedLinksListCapability = "operator.changed_since_poisoned_links.list"

func init() {
	register(operatorDeadLettersListCapability, capabilitySupport{
		LocalLightweightMax:   &truthExact,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalLightweight,
	})
	register(operatorChangedSincePoisonedLinksListCapability, capabilitySupport{
		LocalLightweightMax:   &truthExact,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalLightweight,
	})
}
