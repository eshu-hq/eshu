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

// Fixed response messages for the entity resolve, entity context, and
// workload context and story routes (#7674), one per route step, with the
// same contract as the service messages above.
const (
	resolveEntityQueryFailedMessage            = "entity resolve query failed"
	resolveEntityEnrichmentFailedMessage       = "entity resolve enrichment failed"
	resolveEntityRepoIdentityFailedMessage     = "entity resolve repository identity hydration failed"
	resolveEntityContentFallbackFailedMessage  = "entity resolve content fallback failed"
	resolveGlobalContentEntitiesFailedMessage  = "global entity resolve failed"
	resolveCanonicalContentEntityFailedMessage = "canonical content entity resolve failed"
	resolveCanonicalRepoIdentityFailedMessage  = "canonical content entity repository identity hydration failed"
	resolveWorkloadEntitiesFailedMessage       = "workload entity resolve failed"
	entityContextQueryFailedMessage            = "entity context query failed"
	entityContextContentFallbackFailedMessage  = "entity context content fallback failed"
	entityContextRepoIdentityFailedMessage     = "entity context repository identity hydration failed"
	entityContextEnrichmentFailedMessage       = "entity context enrichment failed"
	workloadContextQueryFailedMessage          = "workload context query failed"
	workloadContextEnrichmentFailedMessage     = "workload context enrichment failed"
	workloadStoryQueryFailedMessage            = "workload story query failed"
	workloadStoryEnrichmentFailedMessage       = "workload story enrichment failed"
)
