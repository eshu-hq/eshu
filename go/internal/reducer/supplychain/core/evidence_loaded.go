// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// fact counts SupplyChainImpactHandler.Handle's multi-stage evidence-loading
// pipeline produces, so the pipeline lives in its own function
// (loadSupplyChainImpactEvidence) while Handle stays focused on
// classification, suppression, and the write/emit tail.
type supplyChainImpactLoadedEvidence struct {
	envelopes    []facts.Envelope
	asAuthorized *AuthorizedSupplyChainImpactLoad
	scopeFacts   int
	// scopeFactKinds are the fact kinds of the intent's own generation (the
	// first load, before any cross-scope expansion). The holder fan-out
	// classifies its cause from them (#9878): they name what this pass's
	// generation carried, never what the expansion resolved elsewhere.
	scopeFactKinds          []string
	repositoryFacts         int
	manifestDependencyFacts int
	activeEvidenceFacts     int
	// truncation names why a bounded load stopped short, by cause (#7154).
	// Only truncation.partial() makes the finding set incomplete.
	truncation supplyChainImpactTruncation
	// suppressionEvidenceTruncated is true when any bounded load stopped
	// short: a suppression candidate set that is not provably complete is
	// discarded so findings fail open.
	suppressionEvidenceTruncated bool
	// expansionEnvelopes counts the expansion envelopes in the evidence set:
	// the static load's charged envelopes plus, on the protected-scan path, the
	// derived facts added after it.
	expansionEnvelopes int
	// budgetEnvelopes is what the pass charged against the per-intent evidence
	// budget (budget.used) and nothing else. The #9778 headroom gauge and the
	// evidence_budget_ratio sub-signal read it, because the protected-scan path
	// raises expansionEnvelopes with derived facts the budget never charged.
	budgetEnvelopes int
	// budgetPayloadBytes is the raw JSON bytes the pass kept decoded, charged
	// against the retained-payload-bytes budget (#9781).
	budgetPayloadBytes int64
	// activeEvidence is the pass's per-statement active-evidence accounting
	// (#8880), merged into the success log's sub-signals and sub-durations.
	activeEvidence                  supplyChainImpactActiveEvidenceTally
	osPackageAdvisoryFacts          int
	osPackageAdvisoryTargetsSkipped int
	scannerAnalysisScopeFacts       int
	resolvedDigestEvidenceFacts     int
	pythonReachabilityFacts         int
	// pythonReachabilityPayloadBytes is the raw JSON bytes the Python stage
	// read and folded, dropped instead of retained (#9860). Folded bytes are
	// observed, not charged: like the #9781 active-evidence fold, folding
	// bounds memory by dropping the payloads, so only retained bytes trip
	// the byte budget.
	pythonReachabilityPayloadBytes int64
	jvmReachabilityFactCount       int
	goReachabilityFacts            int
	postSecurityAlertScopeFacts    int
	securityAlertScopingApplied    bool
	securityAlertScopedOutFacts    int
	// floorArmed reports whether the #5709 floor could have deferred this
	// pass: a producer-reachable initial filter on a wired seam. It is the
	// probe's H1b discriminator (unreachable initial filter means the
	// resolved-digest stage ran unprotected) and is carried here so Handle
	// and the deferral branch log the same value the load decided on.
	floorArmed bool
	// scoped carries the #9779 re-derivation scope when this pass qualified:
	// the stamped CVEs plus the package, advisory, and upstream seeds those
	// CVEs reach. Nil runs the full pass. Handle reads it for the writer's
	// scope, the pass-mode counter, and the scoped sub-signals.
	scoped *scopedSupplyChainImpactScope
	// crossScopeProducerDelta is the batch-wide cross-scope producer delta
	// across the until-stable, resolved-digest, and peer-identity stages.
	// It is the probe's H1a discriminator: a committing pass with zero
	// pinned-digest rows but a positive batch delta had its floor disarmed
	// by another finding's evidence.
	crossScopeProducerDelta int
}
