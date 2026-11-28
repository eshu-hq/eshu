// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/reducer/cicdrun"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// normalizeCriteriaValue folds a criteria or CPE string the way the
// classifier compares them (componentMatchesAffectedProduct, purl_match.go:38):
// surrounding space is insignificant and case is insignificant. The split
// keys provenance sets by this form but keeps the stored spelling on the
// filter lists, so the storage probes still bind the exact stored rows.
func normalizeCriteriaValue(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// SupplyChainImpactCriteriaExpansionBound is the #11087 provenance-bound
// switch. A non-nil pointer on the handler enables the bound on
// criteria-to-CVE transitive expansion (production wiring sets it); nil
// keeps the prior unbounded semantics, which is also the one-line rollback.
// The struct carries no options: its presence is the switch, so the bound is
// never reinterpreted through a knob. The ruled design is
// docs/internal/design/11087-criteria-expansion-bound.md.
type SupplyChainImpactCriteriaExpansionBound struct{}

// supplyChainImpactCriteriaBoundOutcome is the per-pass telemetry of the
// #11087 provenance bound: how many distinct transitive criteria were
// suppressed (summed over rounds; each round counts its own distinct set),
// how many loads ran, and the size of each criteria class in the final
// filter. Zero when the bound is off.
type supplyChainImpactCriteriaBoundOutcome struct {
	SuppressedTransitiveCriteria int
	Loads                        int
	ComponentCriteria            int
	SeedCriteria                 int
	SeedCVECount                 int
}

// criteriaExpansionBoundState is the loop-local provenance state of one
// bounded pass: the seed CVE set plus the suppression counters. It lives in
// the until-stable loop, never on the handler, so concurrent passes cannot
// share seeds.
type criteriaExpansionBoundState struct {
	seeds map[string]struct{}
	// ownScopeID is the intent scope of the pass. A component bears
	// component-origin criteria only when the pass owns it: its scope is
	// ownScopeID, or its document is attached to a digest the pass's own
	// scope holds (ownedDocuments). A component reached through a document
	// rooted in another source's digest belongs to that source, whose own run
	// follows its product names to vulnerability records.
	ownScopeID      string
	suppressedTotal int
	loads           int
	componentCount  int
	seedCount       int
}

// newCriteriaExpansionBoundState seeds the bound from the pass's initial
// filter: the initial CVEIDs plus, for a scoped pass, the stamp set. Alias
// and upstream identity closure of seeded records grows the set as their
// records load (observe); nothing else ever joins it — in particular, CVEs
// that enter through another CVE's product criteria never do.
func newCriteriaExpansionBoundState(initial SupplyChainImpactFactFilter, scoped *scopedSupplyChainImpactScope, ownScopeID string) *criteriaExpansionBoundState {
	seeds := make(map[string]struct{}, len(initial.CVEIDs))
	for _, id := range initial.CVEIDs {
		if id != "" {
			seeds[id] = struct{}{}
		}
	}
	if scoped != nil {
		for _, id := range scoped.cves {
			if id != "" {
				seeds[id] = struct{}{}
			}
		}
	}
	return &criteriaExpansionBoundState{seeds: seeds, ownScopeID: ownScopeID}
}

// observe grows the seed set by identity closure of seeded records only: a
// loaded vulnerability.cve record whose CVE id is seeded contributes its
// aliases list and its upstream group keys. A transitive record's aliases
// never seed, which is what stops hop >= 2 from re-entering through the
// alias arm.
func (s *criteriaExpansionBoundState) observe(envelopes []facts.Envelope) {
	for _, envelope := range envelopes {
		if envelope.FactKind != chain.VulnerabilityCVEFactKind {
			continue
		}
		if _, ok := s.seeds[supplyChainCVEID(envelope.Payload)]; !ok {
			continue
		}
		for _, alias := range appendAliasIDs(nil, envelope.Payload) {
			s.seeds[alias] = struct{}{}
		}
		for _, key := range appendUpstreamCVEKeys(nil, envelope.Payload) {
			s.seeds[key] = struct{}{}
		}
	}
}

// split partitions a derived filter's ProductCriteria by provenance: values an
// owned component bears stay in ProductCriteria (they may drive both probes,
// decision 2a); vulnerable product-row values whose CVE is seeded move to
// SeedProductCriteria (component probe only, decision 2b); every other value
// is suppressed and counted. A component is owned when its scope is the
// pass's own, or its document is attached to a subject digest the pass's own
// scope holds (an image pass reaches its SBOM only through the attachment).
// The CVE arm is untouched: every loaded row still widens CVEIDs (decision
// 4). Both lists are diffed against the already-requested filter, so a
// re-derived value never re-requests a load: without the diff the loop would
// spin on its own seed list. It returns the count of distinct criteria
// suppressed in this derivation.
func (s *criteriaExpansionBoundState) split(filter SupplyChainImpactFactFilter, envelopes []facts.Envelope, requested SupplyChainImpactFactFilter) (SupplyChainImpactFactFilter, int) {
	component := make(map[string]struct{})
	seedProduct := make(map[string]struct{})
	// Re-derive provenance from the pass's envelopes rather than trusting
	// the mixed list: the same envelope set derived the filter, so every
	// value in it classifies. Component membership wins over seed-product
	// membership; a criteria that is both still drives both probes.
	ownDocs := ownedDocuments(envelopes, s.ownScopeID)
	for _, envelope := range envelopes {
		switch envelope.FactKind {
		case chain.SBOMComponentFactKind:
			if _, ownDoc := ownDocs[payloadcore.PayloadStr(envelope.Payload, "document_id")]; envelope.ScopeID != s.ownScopeID && !ownDoc {
				continue
			}
			if cpe := payloadcore.PayloadStr(envelope.Payload, "cpe"); cpe != "" {
				component[normalizeCriteriaValue(cpe)] = struct{}{}
			}
		case chain.VulnerabilityAffectedProductFactKind:
			if !payloadBool(envelope.Payload, "vulnerable") {
				continue
			}
			criteria := payloadcore.PayloadStr(envelope.Payload, "criteria")
			if criteria == "" {
				continue
			}
			if _, ok := s.seeds[supplyChainCVEID(envelope.Payload)]; ok {
				seedProduct[normalizeCriteriaValue(criteria)] = struct{}{}
			}
		}
	}
	var product, seed []string
	suppressed := make(map[string]struct{})
	for _, criteria := range filter.ProductCriteria {
		switch folded := normalizeCriteriaValue(criteria); {
		case criteria == "":
		case containsCriteria(component, folded):
			product = append(product, criteria)
		case containsCriteria(seedProduct, folded):
			seed = append(seed, criteria)
		default:
			suppressed[criteria] = struct{}{}
		}
	}
	filter.ProductCriteria = missingStringValues(payloadcore.UniqueSortedStrings(product), requested.ProductCriteria)
	filter.SeedProductCriteria = missingStringValues(payloadcore.UniqueSortedStrings(seed), requested.SeedProductCriteria)
	s.suppressedTotal += len(suppressed)
	s.componentCount = len(filter.ProductCriteria)
	s.seedCount = len(filter.SeedProductCriteria)
	return filter, len(suppressed)
}

func containsCriteria(set map[string]struct{}, value string) bool {
	_, ok := set[value]
	return ok
}

// outcome snapshots the loop-local #11087 bound state for telemetry. A nil
// state (bound off) reports the zero outcome.
func (s *criteriaExpansionBoundState) outcome() supplyChainImpactCriteriaBoundOutcome {
	if s == nil {
		return supplyChainImpactCriteriaBoundOutcome{}
	}
	return supplyChainImpactCriteriaBoundOutcome{
		SuppressedTransitiveCriteria: s.suppressedTotal,
		Loads:                        s.loads,
		ComponentCriteria:            s.componentCount,
		SeedCriteria:                 s.seedCount,
		SeedCVECount:                 len(s.seeds),
	}
}

// apply runs one derivation step of the provenance bound: identity closure
// over the newly loaded records, then the criteria split. A nil state (bound
// off) returns the filter untouched, keeping the until-stable loop
// byte-for-byte on the prior semantics.
func (s *criteriaExpansionBoundState) apply(filter SupplyChainImpactFactFilter, envelopes, fresh []facts.Envelope, requested SupplyChainImpactFactFilter) (SupplyChainImpactFactFilter, int) {
	if s == nil {
		return filter, 0
	}
	s.observe(fresh)
	return s.split(filter, envelopes, requested)
}

// mergeCriteriaBoundSubSignals adds the #11087 provenance-bound signals to a
// result's sub-signals on every pass, zeros included when the bound is off,
// so bound-on and bound-off passes stay comparable. Success is judged on
// loads and pass-state conversion, never on the suppressed count: a
// pure-seed scope correctly suppresses nothing.
func mergeCriteriaBoundSubSignals(subSignals map[string]float64, loaded supplyChainImpactLoadedEvidence) {
	bound := loaded.expansionBounds.criteria
	subSignals["criteria_bound_suppressed_transitive"] = float64(bound.SuppressedTransitiveCriteria)
	subSignals["criteria_bound_loads"] = float64(bound.Loads)
	subSignals["criteria_bound_component_criteria"] = float64(bound.ComponentCriteria)
	subSignals["criteria_bound_seed_criteria"] = float64(bound.SeedCriteria)
	subSignals["criteria_bound_seed_cves"] = float64(bound.SeedCVECount)
}

// ownedDocuments returns the SBOM document ids the pass owns through its own
// digests: the document_id of every loaded attestation attachment, in any
// scope, whose subject digest an envelope of the pass's own scope carries.
// Vulnerability suppression rows never root a document: their scope map can
// name any digest, so letting them count would let another source's
// suppression make a foreign document own (#11087).
func ownedDocuments(envelopes []facts.Envelope, ownScopeID string) map[string]struct{} {
	own := map[string]struct{}{}
	for _, envelope := range envelopes {
		if envelope.ScopeID != ownScopeID || envelope.FactKind == chain.VulnerabilitySuppressionFactKind {
			continue
		}
		for _, digest := range ownershipDigests(envelope) {
			if digest != "" {
				own[digest] = struct{}{}
			}
		}
	}
	docs := map[string]struct{}{}
	if len(own) == 0 {
		return docs
	}
	for _, envelope := range envelopes {
		if envelope.FactKind != reducercontract.SBOMAttestationAttachmentFactKind {
			continue
		}
		if _, ok := own[payloadcore.PayloadStr(envelope.Payload, "subject_digest")]; !ok {
			continue
		}
		if doc := payloadcore.PayloadStr(envelope.Payload, "document_id"); doc != "" {
			docs[doc] = struct{}{}
		}
	}
	return docs
}

// ownershipDigests lists the subject digests one envelope carries, using the
// digest keys of supplyChainImpactFilter (active_filter.go) without the
// suppression case. It is the lean per-envelope form: the split runs it every
// round, so it must not build the full filter. The drift test
// TestCriteriaBoundOwnershipDigestsMatchFilter pins the two together.
func ownershipDigests(envelope facts.Envelope) []string {
	payload := envelope.Payload
	switch envelope.FactKind {
	case reducercontract.SBOMAttestationAttachmentFactKind:
		return []string{payloadcore.PayloadStr(payload, "subject_digest")}
	case chain.OCIImageReferrerFactKind:
		return []string{payloadcore.PayloadStr(payload, "subject_digest"), payloadcore.PayloadStr(payload, "referrer_digest")}
	case reducercontract.ContainerImageIdentityFactKind, chain.OCIImageManifestFactKind, chain.OCIImageIndexFactKind:
		return []string{payloadcore.PayloadStr(payload, "digest")}
	case facts.ScannerWorkerAnalysisFactKind:
		return []string{payloadcore.PayloadStr(payload, "image_digest")}
	case chain.OCIImageTagObservationFactKind:
		return []string{payloadcore.PayloadStr(payload, "resolved_digest"), payloadcore.PayloadStr(payload, "digest")}
	case cicdrun.CICDRunCorrelationFactKind:
		return []string{payloadcore.PayloadStr(payload, "artifact_digest")}
	}
	return nil
}
