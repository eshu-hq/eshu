// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// supplyChainImpactLoadedEvidence carries the fact envelopes and per-stage
// fact counts SupplyChainImpactHandler.Handle's multi-stage evidence-loading
// pipeline produces, so the pipeline lives in its own function
// (loadSupplyChainImpactEvidence) while Handle stays focused on
// classification, suppression, and the write/emit tail.
type supplyChainImpactLoadedEvidence struct {
	envelopes                       []facts.Envelope
	scopeFacts                      int
	repositoryFacts                 int
	manifestDependencyFacts         int
	activeEvidenceFacts             int
	activeEvidenceTruncated         bool
	suppressionEvidenceTruncated    bool
	osPackageAdvisoryFacts          int
	osPackageAdvisoryTargetsSkipped int
	scannerAnalysisScopeFacts       int
	resolvedDigestEvidenceFacts     int
	pythonReachabilityFacts         int
	jvmReachabilityFactCount        int
	postSecurityAlertScopeFacts     int
	securityAlertScopingApplied     bool
	securityAlertScopedOutFacts     int
}

// loadSupplyChainImpactEvidence runs the scope-fact, repository, manifest-
// dependency, active-evidence, os-package-advisory, scanner-analysis-scope,
// resolved-digest-evidence, Python/JVM reachability, and security-alert
// scoping load stages for one supply-chain-impact intent, in the same order
// and with the same per-stage timing SupplyChainImpactHandler.Handle recorded
// before this extraction. The os-package-advisory stage runs right after
// active-evidence, deriving candidate vendor advisory sources from the
// affected_package facts already loaded and fetching cross-scope
// vulnerability.os_package evidence through the advisory-target reader
// (loadSupplyChainImpactOSPackageAdvisoryFacts) — the only path that kind
// reaches this pipeline through, since supplyChainImpactFactKinds
// intentionally omits it. The scanner-analysis-scope stage runs right after
// that because it depends on the os_package facts the new stage (and any
// active-evidence os_package already present) loads: each os_package's own
// ScopeID+GenerationID (not the intent's) is where its sibling
// scanner_worker.analysis fact lives in production, so it is queried directly
// rather than through the shared active-evidence filter. The
// resolved-digest-evidence stage runs immediately after that: it re-runs the
// active-evidence reader seeded with the digest(s) the scanner-analysis-scope
// stage just resolved, so reducer_container_image_identity (unreachable at
// the original active-evidence stage, which ran before that digest existed)
// gets loaded and can anchor an os_package finding's RepositoryID (issue
// #5464). It returns the accumulated evidence plus a timing value with every
// load-stage duration filled in; Handle continues filling the remaining
// (classification/write/emit) stages on the same value.
func (h SupplyChainImpactHandler) loadSupplyChainImpactEvidence(
	ctx context.Context,
	intent Intent,
) (supplyChainImpactLoadedEvidence, supplyChainImpactTiming, error) {
	var timing supplyChainImpactTiming

	phaseStarted := time.Now()
	envelopes, err := loadFactsForKinds(ctx, h.FactLoader, intent.ScopeID, intent.GenerationID, supplyChainImpactFactKinds())
	timing.loadScopeFactsDuration = time.Since(phaseStarted)
	if err != nil {
		return supplyChainImpactLoadedEvidence{}, timing, fmt.Errorf("load supply chain impact facts: %w", err)
	}
	scopeFacts := len(envelopes)

	phaseStarted = time.Now()
	repositories, err := h.loadActiveSupplyChainImpactRepositoryFacts(ctx, envelopes)
	timing.loadRepositoryFactsDuration = time.Since(phaseStarted)
	if err != nil {
		return supplyChainImpactLoadedEvidence{}, timing, fmt.Errorf("load active supply chain impact repository facts: %w", err)
	}
	repositoryFacts := len(repositories)
	envelopes = append(envelopes, repositories...)

	phaseStarted = time.Now()
	manifestDependencies, err := h.loadActivePackageManifestDependencyFacts(ctx, envelopes)
	timing.loadManifestDependenciesDuration = time.Since(phaseStarted)
	if err != nil {
		return supplyChainImpactLoadedEvidence{}, timing, fmt.Errorf("load active package manifest dependency facts: %w", err)
	}
	manifestDependencyFacts := len(manifestDependencies)
	envelopes = append(envelopes, manifestDependencies...)

	// The #5709 cross-scope floor sits on the loads that read the producers'
	// output, not at admission: producer readiness is not knowable when this
	// intent is enqueued. armCrossScopeProducerFloor samples readiness BEFORE
	// the first load and crossScopeProducerDeferralAfterLoad decides once every
	// producer-bearing stage has run; both halves and the reason the order
	// matters are below in this file.
	//
	// activeEvidenceFilter is computed once here and handed to both, so the
	// floor and the load cannot disagree about what this pass asked for.
	activeEvidenceFilter := supplyChainImpactFilter(envelopes)
	floor, err := h.armCrossScopeProducerFloor(ctx, intent, envelopes, activeEvidenceFilter)
	if err != nil {
		return supplyChainImpactLoadedEvidence{}, timing, err
	}

	activeEvidenceStartCount := len(envelopes)
	phaseStarted = time.Now()
	envelopes, activeEvidenceTruncated, err := h.loadActiveSupplyChainImpactFactsUntilStable(ctx, envelopes, activeEvidenceFilter)
	timing.loadActiveEvidenceDuration = time.Since(phaseStarted)
	if err != nil {
		return supplyChainImpactLoadedEvidence{}, timing, fmt.Errorf("load active supply chain impact facts: %w", err)
	}
	activeEvidenceFacts := len(envelopes) - activeEvidenceStartCount
	suppressionEvidenceTruncated := activeEvidenceTruncated

	osPackageAdvisoryStartCount := len(envelopes)
	phaseStarted = time.Now()
	osPackageAdvisoryEnvelopes, osPackageAdvisorySkipped, err := h.loadSupplyChainImpactOSPackageAdvisoryFacts(ctx, envelopes)
	timing.loadOSPackageAdvisoryDuration = time.Since(phaseStarted)
	if err != nil {
		return supplyChainImpactLoadedEvidence{}, timing, fmt.Errorf("load supply chain impact os package advisory facts: %w", err)
	}
	envelopes = appendUniqueSupplyChainImpactFacts(envelopes, osPackageAdvisoryEnvelopes...)
	osPackageAdvisoryFacts := len(envelopes) - osPackageAdvisoryStartCount

	scannerAnalysisScopeStartCount := len(envelopes)
	phaseStarted = time.Now()
	scannerAnalysisScopeEnvelopes, scannerAnalysisScopeTruncated, err := h.loadSupplyChainImpactScannerAnalysisScopeFacts(ctx, envelopes)
	timing.loadScannerAnalysisScopeDuration = time.Since(phaseStarted)
	if err != nil {
		return supplyChainImpactLoadedEvidence{}, timing, fmt.Errorf("load supply chain impact scanner analysis scope facts: %w", err)
	}
	envelopes = appendUniqueSupplyChainImpactFacts(envelopes, scannerAnalysisScopeEnvelopes...)
	scannerAnalysisScopeFacts := len(envelopes) - scannerAnalysisScopeStartCount

	resolvedDigestEvidenceStartCount := len(envelopes)
	phaseStarted = time.Now()
	resolvedDigestEvidenceEnvelopes, resolvedDigestTruncated, err := h.loadSupplyChainImpactResolvedDigestEvidenceFacts(ctx, scannerAnalysisScopeEnvelopes)
	timing.loadResolvedDigestEvidenceDuration = time.Since(phaseStarted)
	if err != nil {
		return supplyChainImpactLoadedEvidence{}, timing, fmt.Errorf("load supply chain impact resolved digest evidence facts: %w", err)
	}
	envelopes = appendUniqueSupplyChainImpactFacts(envelopes, resolvedDigestEvidenceEnvelopes...)
	resolvedDigestEvidenceFacts := len(envelopes) - resolvedDigestEvidenceStartCount
	activeEvidenceTruncated = activeEvidenceTruncated || scannerAnalysisScopeTruncated || resolvedDigestTruncated
	suppressionEvidenceTruncated = suppressionEvidenceTruncated || resolvedDigestTruncated

	peerIdentityEnvelopes, peerIdentityTruncated, err := h.loadSupplyChainImpactPeerIdentityFacts(ctx, resolvedDigestEvidenceEnvelopes)
	if err != nil {
		return supplyChainImpactLoadedEvidence{}, timing, fmt.Errorf("load supply chain impact peer identity facts: %w", err)
	}
	envelopes = appendUniqueSupplyChainImpactFacts(envelopes, peerIdentityEnvelopes...)
	activeEvidenceTruncated = activeEvidenceTruncated || peerIdentityTruncated
	suppressionEvidenceTruncated = suppressionEvidenceTruncated || peerIdentityTruncated

	// Every stage that can return producer output has now run: the until-stable
	// loop, the resolved-digest re-run (#5464), and the peer-identity pass
	// (#5468).
	if deferral := h.crossScopeProducerDeferralAfterLoad(ctx, floor, intent, envelopes); deferral != nil {
		return supplyChainImpactLoadedEvidence{}, timing, deferral
	}

	pythonReachabilityStartCount := len(envelopes)
	phaseStarted = time.Now()
	pythonReachabilityEvidence, err := h.loadPythonReachabilityEvidenceFacts(ctx, envelopes)
	timing.loadPythonReachabilityDuration = time.Since(phaseStarted)
	if err != nil {
		return supplyChainImpactLoadedEvidence{}, timing, fmt.Errorf("load Python reachability evidence facts: %w", err)
	}
	envelopes = appendUniqueSupplyChainImpactFacts(envelopes, pythonReachabilityEvidence...)
	pythonReachabilityFacts := len(envelopes) - pythonReachabilityStartCount

	jvmReachabilityStartCount := len(envelopes)
	phaseStarted = time.Now()
	jvmReachabilityFacts, err := h.loadActiveJVMReachabilityFacts(ctx, envelopes)
	timing.loadJVMReachabilityDuration = time.Since(phaseStarted)
	if err != nil {
		return supplyChainImpactLoadedEvidence{}, timing, fmt.Errorf("load active JVM reachability facts: %w", err)
	}
	envelopes = appendUniqueSupplyChainImpactFacts(envelopes, jvmReachabilityFacts...)
	jvmReachabilityFactCount := len(envelopes) - jvmReachabilityStartCount

	preSecurityAlertScopeFacts := len(envelopes)
	phaseStarted = time.Now()
	securityAlertScopingApplied := supplyChainImpactUsesSecurityAlertScope(intent, envelopes)
	if securityAlertScopingApplied {
		envelopes = scopeSupplyChainImpactEvidenceToSecurityAlerts(envelopes)
	}
	timing.securityAlertScopingDuration = time.Since(phaseStarted)
	postSecurityAlertScopeFacts := len(envelopes)
	securityAlertScopedOutFacts := 0
	if securityAlertScopingApplied && preSecurityAlertScopeFacts > postSecurityAlertScopeFacts {
		securityAlertScopedOutFacts = preSecurityAlertScopeFacts - postSecurityAlertScopeFacts
	}

	return supplyChainImpactLoadedEvidence{
		envelopes:                       envelopes,
		scopeFacts:                      scopeFacts,
		repositoryFacts:                 repositoryFacts,
		manifestDependencyFacts:         manifestDependencyFacts,
		activeEvidenceFacts:             activeEvidenceFacts,
		activeEvidenceTruncated:         activeEvidenceTruncated,
		suppressionEvidenceTruncated:    suppressionEvidenceTruncated,
		osPackageAdvisoryFacts:          osPackageAdvisoryFacts,
		osPackageAdvisoryTargetsSkipped: osPackageAdvisorySkipped,
		scannerAnalysisScopeFacts:       scannerAnalysisScopeFacts,
		resolvedDigestEvidenceFacts:     resolvedDigestEvidenceFacts,
		pythonReachabilityFacts:         pythonReachabilityFacts,
		jvmReachabilityFactCount:        jvmReachabilityFactCount,
		postSecurityAlertScopeFacts:     postSecurityAlertScopeFacts,
		securityAlertScopingApplied:     securityAlertScopingApplied,
		securityAlertScopedOutFacts:     securityAlertScopedOutFacts,
	}, timing, nil
}

// supplyChainImpactCrossScopeFloor carries the #5709 floor's state across the
// active-evidence stages: the readiness signal sampled BEFORE the first load,
// the single clock reading both halves share, and the producer-fact count taken
// at the same moment.
//
// One clock reading rather than two, because the elapsed bound must measure how
// long the consumer has waited on its producer, not how long its own loads took.
// Re-reading the clock after the load would let a slow pass push its own intent
// past the 30-minute bound and commit an answer it should have deferred.
type supplyChainImpactCrossScopeFloor struct {
	signal    crossScopeProducerReadinessSignal
	sampledAt time.Time
	// producerFactsBefore is keyed by PRODUCER DOMAIN, because the floor's
	// decision is per producer. One combined number here would let a producer
	// fact already sitting in the consumer's own scope offset a different
	// producer's delta.
	producerFactsBefore map[Domain]int
}

// armCrossScopeProducerFloor samples producer readiness before the first
// active-evidence load.
//
// The order is the whole point. Sampling after the load would let a producer
// generation activate in between, so the readiness store reports "ready" about a
// snapshot the load had already taken without it, and the handler durably writes
// findings computed from that snapshot. Nothing later repairs them: the
// completion fanout's reopen selects 'succeeded' rows, and a maintenance pass
// racing this intent while it is still claimed skips it.
//
// The reverse race is benign. A producer activating BETWEEN this sample and the
// load only means the load reads fresher data than the signal assumed, which can
// add evidence and never remove it, and the post-load producer count is what
// decides.
func (h SupplyChainImpactHandler) armCrossScopeProducerFloor(
	ctx context.Context,
	intent Intent,
	envelopes []facts.Envelope,
	filter SupplyChainImpactFactFilter,
) (supplyChainImpactCrossScopeFloor, error) {
	sampledAt := time.Now()
	signal, err := checkCrossScopeProducerReadinessBeforeLoad(
		ctx, h.ProducerReadiness, intent, sampledAt, h.crossScopeProducerLookupPlanned(filter),
	)
	if err != nil {
		return supplyChainImpactCrossScopeFloor{}, err
	}
	return supplyChainImpactCrossScopeFloor{
		signal:              signal,
		sampledAt:           sampledAt,
		producerFactsBefore: countSupplyChainImpactCrossScopeProducerFacts(envelopes),
	}, nil
}

// crossScopeProducerDeferralAfterLoad combines the pre-load readiness signal
// with the producer facts the cross-scope stages actually resolved, and returns
// the non-counting readiness error when this pass must defer. It returns nil in
// every other case.
//
// It must be called after EVERY stage that can return producer output, which for
// this consumer is three: the until-stable loop, the resolved-digest re-run
// (#5464), and the peer-identity pass (#5468). The resolved-digest stage is
// where a pure OS-package finding's container image identity arrives, long after
// the loop settles, so a count taken earlier defers exactly the findings that
// read exists to serve.
//
// Each count is a DELTA, because supplyChainImpactFactKinds also asks the
// intent's OWN scope for both producer fact kinds. An absolute count would let a
// producer fact sitting in the consumer's own vulnerability scope stand in for
// one the cross-scope read resolved, and that says nothing about whether the
// producer's other scopes have activated.
//
// The counts are kept PER PRODUCER DOMAIN and compared against that producer's
// own readiness answer. This domain declares two producers, and a combined
// count could not tell "each producer answered once" from "one producer
// answered twice" -- so a pass that resolved container image identity and no
// deployment context at all used to commit findings with no deployment context
// (found reviewing #6093, guarded by
// TestSupplyChainImpactDefersWhenOnlyOneProducerResolved).
//
// The returned error is deliberately unwrapped so the durable queue reads the
// non-counting failure class off it; SupplyChainImpactHandler.Handle passes it
// through without wrapping.
func (h SupplyChainImpactHandler) crossScopeProducerDeferralAfterLoad(
	ctx context.Context,
	floor supplyChainImpactCrossScopeFloor,
	intent Intent,
	envelopes []facts.Envelope,
) error {
	resolved := countSupplyChainImpactCrossScopeProducerFacts(envelopes)
	for producer, before := range floor.producerFactsBefore {
		resolved[producer] -= before
	}
	unready := crossScopeUnreadyProducers(floor.signal, resolved)
	if len(unready) == 0 {
		return nil
	}
	logCrossScopeProducerNotReadyDefer(ctx, h.Logger, intent, floor.sampledAt, unready)
	return newCrossScopeProducerNotReadyError(intent.Domain, intent.ScopeID, intent.GenerationID, unready)
}

// supplyChainImpactProducerFactKindByDomain maps each producer domain
// crossscope.dependencyCatalog declares for supply_chain_impact to the fact kind
// that producer writes.
//
// The floor needs this because supply_chain_impact's cross-scope read is the
// SHARED active-evidence reader (listActiveSupplyChainImpactFactsQuery), which
// returns twenty-odd fact kinds — sbom.component, vulnerability.*,
// package_registry.*, file. The CI/CD consumer does not need an equivalent: its
// cross-scope load asks a dedicated reader that returns container-image-identity
// support facts and nothing else, so for it any returned envelope is producer
// output and a plain envelope count is the right signal.
//
// Counting every envelope here instead would let a pass that resolved a pile of
// SBOM components and zero producer facts disarm the floor, which is the exact
// case the floor exists for.
//
// The two entries are the two writer constants, not restatements of them:
// container_image_identity_writer.go and ci_cd_run_correlation_writer.go define
// the kinds these producers publish under.
// TestSupplyChainImpactProducerFactKindsCoverEveryDeclaredProducer fails if a
// third producer joins the catalog without an entry here, because that
// producer's output would silently never disarm the floor.
var supplyChainImpactProducerFactKindByDomain = map[Domain]string{
	DomainContainerImageIdentity: containerImageIdentityFactKind,
	DomainCICDRunCorrelation:     cicdRunCorrelationFactKind,
}

// countSupplyChainImpactCrossScopeProducerFacts counts the envelopes written by
// each declared cross-scope producer, keyed by that producer's domain. The floor
// reads the DELTA across the active-evidence stages rather than these absolute
// numbers, so a producer fact that happened to live in the consumer's own
// vulnerability scope cannot stand in for one the cross-scope read actually
// resolved.
//
// Keyed by domain rather than summed, because an envelope carries the identity
// of the producer that wrote it and a total does not. The floor compares each
// producer's count against that producer's own readiness answer.
func countSupplyChainImpactCrossScopeProducerFacts(envelopes []facts.Envelope) map[Domain]int {
	producerByKind := make(map[string]Domain, len(supplyChainImpactProducerFactKindByDomain))
	for _, dependency := range crossScopeDependenciesForRegistration(DomainSupplyChainImpact) {
		for _, producer := range dependency.ProducerDomains {
			if kind, ok := supplyChainImpactProducerFactKindByDomain[producer]; ok {
				producerByKind[kind] = producer
			}
		}
	}
	resolved := make(map[Domain]int, len(producerByKind))
	for _, envelope := range envelopes {
		if producer, ok := producerByKind[envelope.FactKind]; ok {
			resolved[producer]++
		}
	}
	return resolved
}

// crossScopeProducerLookupPlanned reports whether this pass can reach a producer
// fact at all, which is what arms the #5709 readiness floor.
//
// Both false cases produce an empty producer count that says nothing about the
// producer, so neither may be answered with a deferral:
//
//   - No loader implementing the cross-scope seam.
//     loadActiveSupplyChainImpactFacts returns no envelopes without querying,
//     exactly like an unwired readiness seam.
//   - A filter with no dimension listActiveSupplyChainImpactFactsQuery matches a
//     producer row on. That query reaches
//     reducer_container_image_identity and reducer_ci_cd_run_correlation
//     through exactly three predicates: the digest branch (subject_digest,
//     digest, artifact_digest, referrer_digest, resolved_digest against
//     SubjectDigests), image_ref against ImageRefs, and the repository branch,
//     whose fact-kind list names both producer kinds, against RepositoryIDs.
//     Package IDs, purls, CVE IDs, advisory IDs, product criteria, and document
//     IDs cannot match either producer's payload, so a pass carrying only those
//     can never resolve one however long it waits.
//
// Gating on this keeps such a pass from deferring for the full
// crossScopeProducerReadinessMaxWait on a retry that does not back off: this
// deferral's failure class freezes attempt_count, so the exponential term never
// grows and the row re-claims roughly every 30 seconds for 30 minutes, per
// repair cycle, to look up nothing.
//
// The filter is the INITIAL one, computed before the first active-evidence
// load. Later stages can seed dimensions this one lacks — the resolved-digest
// stage (#5464) learns an os_package's scanned digest only after the
// scanner-analysis stage — so a pass whose only producer-reachable dimension
// appears in a later stage reads as "no floor" here. That is the safe
// direction: a missed gate leaves the pre-floor behaviour, where a wrong gate
// would defer a pass that could never resolve anything. It is recorded as a
// residual window in docs/internal/evidence/5709-supply-chain-consumer.md.
func (h SupplyChainImpactHandler) crossScopeProducerLookupPlanned(filter SupplyChainImpactFactFilter) bool {
	if _, ok := h.FactLoader.(activeSupplyChainImpactFactLoader); !ok {
		return false
	}
	return len(filter.SubjectDigests) > 0 || len(filter.ImageRefs) > 0 || len(filter.RepositoryIDs) > 0
}
