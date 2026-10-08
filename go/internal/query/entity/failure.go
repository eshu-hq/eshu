// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

// Fixed response messages for a service context, investigation, or story read
// that failed for a reason that is not a fence or graph verdict (#7626). A
// backend error quotes SQL, Cypher, and host detail, so the client sees only
// one of these; the error itself goes to the request span through the tracing
// server-failure helpers.
const (
	serviceContextQueryFailedMessage            = "service context query failed"
	serviceContextEnrichmentFailedMessage       = "service context enrichment failed"
	serviceInvestigationQueryFailedMessage      = "service investigation query failed"
	serviceInvestigationEnrichmentFailedMessage = "service investigation enrichment failed"
	serviceStoryQueryFailedMessage              = "service story query failed"
	serviceStoryEnrichmentFailedMessage         = "service story enrichment failed"
	serviceStoryCICDEvidenceFailedMessage       = "service story ci/cd evidence load failed"
	serviceStorySupplyChainFailedMessage        = "service story supply chain evidence enrichment failed"
)
