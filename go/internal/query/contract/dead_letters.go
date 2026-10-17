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

// operatorWorkItemsListCapability is the capability key for the bounded
// operator fact-work-item query (POST /api/v0/admin/work-items/query). It
// reads durable fact_work_items state from Postgres and does not require
// the graph backend (#8176).
const operatorWorkItemsListCapability = "operator.work_items.list"

// operatorDecisionsListCapability is the capability key for the bounded
// operator projection-decision query (POST /api/v0/admin/decisions/query).
// It reads durable projection decision state from Postgres and does not
// require the graph backend (#8176).
const operatorDecisionsListCapability = "operator.decisions.list"

// operatorDecisionEvidenceListCapability is the capability key for the
// bounded operator decision-evidence read backing include_evidence on the
// decisions query. It reads durable decision evidence state from Postgres
// and does not require the graph backend (#8176).
const operatorDecisionEvidenceListCapability = "operator.decision_evidence.list"

// operatorReplayEventsListCapability is the capability key for the bounded
// operator replay-event audit-log query
// (POST /api/v0/admin/replay-events/query). It reads durable
// fact_replay_events state from Postgres and does not require the graph
// backend (#8176).
const operatorReplayEventsListCapability = "operator.replay_events.list"

// operatorRepositoryCatalogLookupCapability is the capability key for the
// repository-catalog selector lookup backing repository-scoped reindex
// (POST /api/v0/admin/reindex with scope repository). It reads the
// repository catalog from the content reader and does not require the graph
// backend (#8176).
const operatorRepositoryCatalogLookupCapability = "operator.repository_catalog.lookup"

// operatorDeltaUnionCompletenessCheckCapability is the capability key for the
// read-only delta-union completeness check
// (GET /api/v0/admin/delta-union/completeness). It compares the ACTIVE
// delta-union file set from Postgres against one anchored graph query per
// delta-active repository, so it requires the graph backend and is unsupported
// in the lightweight profile. It answers a graph outage or deadline with the
// shared 503 or 504 verdict (#8292).
const operatorDeltaUnionCompletenessCheckCapability = "operator.delta_union_completeness.check"

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
	register(operatorWorkItemsListCapability, capabilitySupport{
		LocalLightweightMax:   &truthExact,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalLightweight,
	})
	register(operatorDecisionsListCapability, capabilitySupport{
		LocalLightweightMax:   &truthExact,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalLightweight,
	})
	register(operatorDecisionEvidenceListCapability, capabilitySupport{
		LocalLightweightMax:   &truthExact,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalLightweight,
	})
	register(operatorReplayEventsListCapability, capabilitySupport{
		LocalLightweightMax:   &truthExact,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalLightweight,
	})
	register(operatorRepositoryCatalogLookupCapability, capabilitySupport{
		LocalLightweightMax:   &truthExact,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalLightweight,
	})
	register(operatorDeltaUnionCompletenessCheckCapability, capabilitySupport{
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalAuthoritative,
	})
}
