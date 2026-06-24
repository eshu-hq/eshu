// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"sort"
	"strings"
)

// BuildSupplyChainImpactPacket maps a reducer-owned supply-chain impact
// explanation into the portable investigation_evidence_packet.v2 shape. It is
// the single composer for the supply-chain family, so the CLI, API, and MCP
// surfaces emit an identical packet for the same investigation (the parity
// requirement from #3140). It reads nothing: it transforms the explanation
// result and the canonical truth envelope the explain route already produced.
//
// The mapping keeps the layers separated: result.Evidence becomes the
// raw-source-fact layer, the finding becomes a reducer decision, result.ImpactPath
// becomes the graph-answer and missing-hop layers, and result.Freshness overlays
// the freshness state. No provider is consulted, so the packet is deterministic.
//
// A nil bounds override uses the contract defaults; a non-nil override (for
// example the CLI --max-source-facts flag) lowers a per-layer cap.
func BuildSupplyChainImpactPacket(result SupplyChainImpactExplanationResult, truth *TruthEnvelope, bounds *PacketBounds) (InvestigationEvidencePacket, error) {
	in := InvestigationPacketInput{
		Family:     InvestigationFamilySupplyChainImpact,
		Subject:    supplyChainPacketSubject(result.Input),
		Question:   supplyChainPacketQuestion(result.Input, result.Finding),
		Generation: strings.TrimSpace(result.Freshness.LatestObservedAt),
		Truth:      supplyChainPacketTruth(truth, result.Freshness),
		Bounds:     bounds,
	}

	sourceFacts, knownFactIDs := supplyChainPacketSourceFacts(result.Evidence)
	in.SourceFacts = sourceFacts
	in.ReducerDecisions = supplyChainPacketDecisions(result.Finding, knownFactIDs)
	in.GraphAnswers = supplyChainPacketGraphAnswers(result.ImpactPath, knownFactIDs)
	in.MissingEvidence = supplyChainPacketMissingHops(result)
	in.Summary = supplyChainPacketSummary(result)
	in.Limitations = supplyChainPacketLimitations(result.Readiness)

	return NewInvestigationEvidencePacket(in)
}

// supplyChainPacketSubject collects the canonical, non-empty scope keys that name
// the investigation.
func supplyChainPacketSubject(filter SupplyChainImpactExplanationFilter) map[string]string {
	subject := map[string]string{}
	addSubjectKey(subject, "finding_id", filter.FindingID)
	addSubjectKey(subject, "advisory_id", filter.AdvisoryID)
	addSubjectKey(subject, "cve_id", filter.CVEID)
	addSubjectKey(subject, "package_id", filter.PackageID)
	addSubjectKey(subject, "repository_id", filter.RepositoryID)
	addSubjectKey(subject, "subject_digest", filter.SubjectDigest)
	return subject
}

func addSubjectKey(subject map[string]string, key, value string) {
	if v := strings.TrimSpace(value); v != "" {
		subject[key] = v
	}
}

// supplyChainPacketQuestion derives the canonical question. When the filter named
// only a finding id, it prefers the resolved finding's advisory so a
// finding-scoped lookup still produces the more useful advisory-reach question.
func supplyChainPacketQuestion(filter SupplyChainImpactExplanationFilter, finding *SupplyChainImpactFindingResult) string {
	advisory := strings.TrimSpace(filter.AdvisoryID)
	cve := strings.TrimSpace(filter.CVEID)
	if advisory == "" && cve == "" && finding != nil {
		advisory = strings.TrimSpace(finding.AdvisoryID)
		cve = strings.TrimSpace(finding.CVEID)
	}
	switch {
	case advisory != "":
		return fmt.Sprintf("What does advisory %s reach?", advisory)
	case cve != "":
		return fmt.Sprintf("What does %s reach?", cve)
	case strings.TrimSpace(filter.PackageID) != "":
		return fmt.Sprintf("What does package %s reach?", strings.TrimSpace(filter.PackageID))
	case strings.TrimSpace(filter.FindingID) != "":
		return fmt.Sprintf("What is the impact of finding %s?", strings.TrimSpace(filter.FindingID))
	default:
		return "What does this vulnerability reach?"
	}
}

// supplyChainPacketTruth overlays the explanation freshness onto a clone of the
// canonical truth so the packet's freshness reflects the evidence snapshot
// without mutating the caller's envelope. A nil truth yields an unsupported
// packet downstream.
func supplyChainPacketTruth(truth *TruthEnvelope, freshness SupplyChainImpactExplanationFreshness) *TruthEnvelope {
	if truth == nil {
		return nil
	}
	clone := *truth
	if state := freshnessStateFromString(freshness.State); state != "" {
		clone.Freshness.State = state
	}
	return &clone
}

func freshnessStateFromString(raw string) FreshnessState {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(FreshnessFresh):
		return FreshnessFresh
	case string(FreshnessStale):
		return FreshnessStale
	case string(FreshnessBuilding):
		return FreshnessBuilding
	case string(FreshnessUnavailable):
		return FreshnessUnavailable
	default:
		return ""
	}
}

// supplyChainPacketSourceFacts maps evidence-fact summaries into the raw-evidence
// layer and returns the set of known fact ids for referential-integrity checks.
func supplyChainPacketSourceFacts(evidence []SupplyChainImpactEvidenceFactSummary) ([]PacketSourceFact, map[string]struct{}) {
	facts := make([]PacketSourceFact, 0, len(evidence))
	known := make(map[string]struct{}, len(evidence))
	for _, fact := range evidence {
		id := strings.TrimSpace(fact.FactID)
		facts = append(facts, PacketSourceFact{
			FactID:         id,
			EvidenceFamily: fact.FactKind,
			CollectorKind:  fact.SourceSystem,
			Generation:     fact.ObservedAt,
			Summary:        supplyChainFactSummary(fact),
		})
		if id != "" {
			known[id] = struct{}{}
		}
	}
	return facts, known
}

func supplyChainFactSummary(fact SupplyChainImpactEvidenceFactSummary) string {
	parts := []string{}
	if kind := strings.TrimSpace(fact.FactKind); kind != "" {
		parts = append(parts, kind)
	}
	if src := strings.TrimSpace(fact.SourceSystem); src != "" {
		parts = append(parts, "from "+src)
	}
	if conf := strings.TrimSpace(fact.SourceConfidence); conf != "" {
		parts = append(parts, "confidence "+conf)
	}
	return strings.Join(parts, " ")
}

// supplyChainPacketDecisions maps the reducer-owned finding into a single
// reducer-decision entry. Source-fact references are filtered to those present
// in the source layer so the decision is always traceable.
func supplyChainPacketDecisions(finding *SupplyChainImpactFindingResult, knownFactIDs map[string]struct{}) []PacketReducerDecision {
	if finding == nil {
		return nil
	}
	return []PacketReducerDecision{{
		Domain:        "supply_chain_impact",
		Subject:       strings.TrimSpace(finding.FindingID),
		State:         impactStatusToDecisionState(finding.ImpactStatus),
		Target:        "supply_chain_impact_finding",
		Reason:        supplyChainDecisionReason(finding),
		SourceFactIDs: filterKnownFactIDs(finding.EvidenceFactIDs, knownFactIDs),
	}}
}

// impactStatusToDecisionState maps a finding's persisted impact_status verdict
// onto the admission-audit decision vocabulary. The reducer emits qualified
// statuses (affected_exact, affected_derived, possibly_affected,
// not_affected_known_fixed, unknown_impact), so this matches on the family rather
// than bare strings: any affected_* verdict is admitted, a not_affected_* verdict
// is rejected, possibly_affected is ambiguous, and unknown_impact lacks the
// evidence to decide. A not-affected check precedes the affected check because
// "not_affected" contains "affected".
func impactStatusToDecisionState(impactStatus string) string {
	s := strings.ToLower(strings.TrimSpace(impactStatus))
	switch {
	case s == "":
		return "ambiguous"
	case strings.HasPrefix(s, "not_affected"), s == "unaffected", s == "not affected":
		return "rejected"
	case strings.HasPrefix(s, "possibly_affected"):
		return "ambiguous"
	case strings.HasPrefix(s, "affected"), s == "impacted", s == "vulnerable":
		return "admitted"
	case strings.HasPrefix(s, "unknown"):
		return "missing_evidence"
	default:
		return "ambiguous"
	}
}

func supplyChainDecisionReason(finding *SupplyChainImpactFindingResult) string {
	status := strings.TrimSpace(finding.ImpactStatus)
	if status == "" {
		status = "unknown"
	}
	reason := fmt.Sprintf("impact_status=%s", status)
	if match := strings.TrimSpace(finding.MatchReason); match != "" {
		reason += "; " + match
	}
	return reason
}

func filterKnownFactIDs(ids []string, known map[string]struct{}) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := known[id]; ok {
			out = append(out, id)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// supplyChainPacketGraphAnswers maps the impact path hops into the graph-truth
// layer, preserving each present hop's backing source-fact ids (filtered to the
// facts actually present in the source layer) so the graph answer is traceable.
// Missing hops are carried by supplyChainPacketMissingHops instead.
func supplyChainPacketGraphAnswers(path []SupplyChainImpactPathHop, knownFactIDs map[string]struct{}) []PacketGraphAnswer {
	answers := make([]PacketGraphAnswer, 0, len(path))
	for _, hop := range path {
		present := strings.EqualFold(strings.TrimSpace(hop.Status), "present")
		answer := PacketGraphAnswer{
			Hop:           strings.TrimSpace(hop.Hop),
			Present:       present,
			SourceFactIDs: filterKnownFactIDs(hop.EvidenceFactIDs, knownFactIDs),
		}
		if present {
			answer.TruthClass = AnswerTruthDeterministic
		}
		answers = append(answers, answer)
	}
	return answers
}

// supplyChainPacketMissingHops names every unresolved hop with a reason so a gap
// is explicit rather than hidden. It folds per-hop missing evidence and the
// top-level missing-evidence reasons, deduplicated.
func supplyChainPacketMissingHops(result SupplyChainImpactExplanationResult) []PacketMissingHop {
	seen := map[string]struct{}{}
	hops := []PacketMissingHop{}
	add := func(hop, reason string) {
		hop = strings.TrimSpace(hop)
		reason = strings.TrimSpace(reason)
		if hop == "" || reason == "" {
			return
		}
		key := hop + "|" + reason
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		hops = append(hops, PacketMissingHop{Hop: hop, Reason: reason})
	}
	for _, hop := range result.ImpactPath {
		if strings.EqualFold(strings.TrimSpace(hop.Status), "present") {
			continue
		}
		reason := strings.Join(hop.MissingEvidence, "; ")
		if reason == "" {
			reason = "hop evidence missing"
		}
		add(hop.Hop, reason)
	}
	for _, reason := range result.MissingEvidence {
		add("correlation", reason)
	}
	return hops
}

func supplyChainPacketSummary(result SupplyChainImpactExplanationResult) string {
	if result.Finding == nil {
		return ""
	}
	finding := result.Finding
	pkg := packetFirstNonEmpty(finding.PackageName, finding.PackageID, finding.PURL, "the package")
	advisory := packetFirstNonEmpty(finding.AdvisoryID, finding.CVEID, "the advisory")
	status := packetFirstNonEmpty(finding.ImpactStatus, "unknown")
	reach := fmt.Sprintf("%d workload(s), %d service(s)", len(finding.WorkloadIDs), len(finding.ServiceIDs))
	return fmt.Sprintf("Advisory %s affects %s (impact_status=%s); reaches %s.", advisory, pkg, status, reach)
}

// supplyChainPacketLimitations surfaces readiness constraints as packet
// limitations. The upstream readiness envelope only populates IncompleteReasons
// for the target-incomplete state, so a non-ready state is also recorded by name
// to ensure the packet always explains why the investigation is constrained.
func supplyChainPacketLimitations(readiness SupplyChainImpactReadinessEnvelope) []string {
	limitations := []string{}
	for _, reason := range readiness.IncompleteReasons {
		if r := strings.TrimSpace(reason); r != "" {
			limitations = append(limitations, "readiness: "+r)
		}
	}
	if state := strings.TrimSpace(string(readiness.State)); state != "" && !supplyChainReadinessIsReady(readiness.State) {
		limitations = append(limitations, "readiness state: "+state)
	}
	sort.Strings(limitations)
	return dedupePacketLimitations(limitations)
}

// supplyChainReadinessIsReady reports whether a readiness state means evidence is
// fully collected (no constraint to surface).
func supplyChainReadinessIsReady(state SupplyChainImpactReadinessState) bool {
	switch state {
	case ReadinessStateReadyWithFindings, ReadinessStateReadyZeroFindings:
		return true
	default:
		return false
	}
}

func dedupePacketLimitations(in []string) []string {
	if len(in) == 0 {
		return in
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func packetFirstNonEmpty(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
