package query

import (
	"fmt"
	"sort"
	"strings"
)

// deployableUnitReproduceRoute is the bounded read that backs the deployable-unit
// packet, surfaced so a reader can reproduce the evidence.
const deployableUnitReproduceRoute = "GET /api/v0/evidence/admission-decisions?domain=deployable_unit"

// BuildDeployableUnitPacket maps reducer-owned deployable-unit correlation
// admission decisions into the investigation_evidence_packet.v2 shape. It is the
// shared composer for the deployable_unit family, so the CLI, API, and MCP
// surfaces emit an identical packet. Accepted, ambiguous, rejected, and stale
// candidates are represented explicitly in the reducer-decision layer rather than
// hidden. It reads nothing and consults no provider, so the packet is
// deterministic.
//
// A nil bounds override uses the contract defaults.
func BuildDeployableUnitPacket(decisions []AdmissionDecisionResult, scope map[string]string, truth *TruthEnvelope, bounds *PacketBounds) (InvestigationEvidencePacket, error) {
	sourceFacts, known := admissionDecisionSourceFacts(decisions)
	in := InvestigationPacketInput{
		Family:           InvestigationFamilyDeployableUnit,
		Subject:          normalizeSubject(scope),
		Question:         deployableUnitQuestion(scope),
		Generation:       admissionGeneration(decisions),
		Truth:            truth,
		Bounds:           bounds,
		SourceFacts:      sourceFacts,
		ReducerDecisions: admissionDecisionReducerDecisions(decisions, known),
		GraphAnswers:     admissionDecisionGraphAnswers(decisions, known),
		MissingEvidence:  admissionDecisionMissingHops(decisions),
		Summary:          deployableUnitSummary(decisions),
		Reproduce: []PacketReproduceStep{{
			Description: "list deployable-unit admission decisions for this scope",
			Route:       deployableUnitReproduceRoute,
			Tool:        "list_admission_decisions",
		}},
	}
	return NewInvestigationEvidencePacket(in)
}

func deployableUnitQuestion(scope map[string]string) string {
	for _, key := range []string{"workload_id", "service_id", "repository_id", "scope_id"} {
		if v := strings.TrimSpace(scope[key]); v != "" {
			return fmt.Sprintf("What deployable-unit truth is materialized for %s?", v)
		}
	}
	return "What deployable-unit truth is materialized for this scope?"
}

// admissionDecisionSourceFacts collects the redaction-safe source handles across
// all decisions into the raw-evidence layer, deduplicated by handle id.
func admissionDecisionSourceFacts(decisions []AdmissionDecisionResult) ([]PacketSourceFact, map[string]struct{}) {
	facts := []PacketSourceFact{}
	known := map[string]struct{}{}
	for _, decision := range decisions {
		for _, handle := range decision.SourceHandles {
			id := strings.TrimSpace(handle.ID)
			if id == "" {
				continue
			}
			if _, seen := known[id]; seen {
				continue
			}
			known[id] = struct{}{}
			facts = append(facts, PacketSourceFact{
				FactID:         id,
				EvidenceFamily: strings.TrimSpace(handle.Kind),
				Generation:     strings.TrimSpace(decision.GenerationID),
				Subject:        strings.TrimSpace(handle.ScopeID),
				Summary:        fmt.Sprintf("%s handle for %s decision", handle.Kind, decision.Domain),
			})
		}
	}
	return facts, known
}

func admissionDecisionReducerDecisions(decisions []AdmissionDecisionResult, known map[string]struct{}) []PacketReducerDecision {
	out := make([]PacketReducerDecision, 0, len(decisions))
	for _, decision := range decisions {
		out = append(out, PacketReducerDecision{
			Domain:        strings.TrimSpace(decision.Domain),
			Subject:       admissionDecisionSubject(decision),
			State:         admissionDecisionState(decision.State),
			Target:        admissionDecisionTarget(decision),
			Reason:        admissionDecisionReason(decision),
			Generation:    strings.TrimSpace(decision.GenerationID),
			SourceFactIDs: admissionHandleIDs(decision.SourceHandles, known),
		})
	}
	return out
}

// admissionDecisionGraphAnswers projects each decision whose canonical write was
// performed into a present graph edge, with the materialized target. The
// canonical-write record is itself the materialization evidence, so the edge is
// emitted even when the decision carries no resolvable source handle; dropping it
// would hide real graph truth. SourceFactIDs is populated when handles resolve.
func admissionDecisionGraphAnswers(decisions []AdmissionDecisionResult, known map[string]struct{}) []PacketGraphAnswer {
	answers := []PacketGraphAnswer{}
	for _, decision := range decisions {
		if !decision.CanonicalWrite.Written {
			continue
		}
		answers = append(answers, PacketGraphAnswer{
			Relationship:  strings.TrimSpace(decision.CanonicalWrite.TargetKind),
			From:          strings.TrimSpace(decision.AnchorID),
			To:            strings.TrimSpace(decision.CanonicalWrite.TargetID),
			Hop:           strings.TrimSpace(decision.CandidateKind),
			Present:       true,
			TruthClass:    AnswerTruthDeterministic,
			SourceFactIDs: admissionHandleIDs(decision.SourceHandles, known),
		})
	}
	return answers
}

// admissionDecisionMissingHops surfaces ambiguous, rejected, stale, and
// missing-evidence candidates as explicit gaps so they are never hidden.
func admissionDecisionMissingHops(decisions []AdmissionDecisionResult) []PacketMissingHop {
	hops := []PacketMissingHop{}
	for _, decision := range decisions {
		state := admissionDecisionState(decision.State)
		if state == "admitted" {
			continue
		}
		hop := strings.TrimSpace(decision.CandidateKind)
		if hop == "" {
			hop = "deployable_unit"
		}
		hops = append(hops, PacketMissingHop{
			Hop:    hop,
			Reason: fmt.Sprintf("%s candidate: %s", state, admissionDecisionReason(decision)),
		})
	}
	return hops
}

func deployableUnitSummary(decisions []AdmissionDecisionResult) string {
	if len(decisions) == 0 {
		return ""
	}
	counts := map[string]int{}
	for _, decision := range decisions {
		counts[admissionDecisionState(decision.State)]++
	}
	return fmt.Sprintf("%d deployable-unit decisions: %s.", len(decisions), formatStateCounts(counts))
}

func admissionDecisionSubject(decision AdmissionDecisionResult) string {
	if id := strings.TrimSpace(decision.CandidateID); id != "" {
		return id
	}
	if id := strings.TrimSpace(decision.AnchorID); id != "" {
		return id
	}
	return strings.TrimSpace(decision.DecisionID)
}

func admissionDecisionTarget(decision AdmissionDecisionResult) string {
	kind := strings.TrimSpace(decision.CanonicalWrite.TargetKind)
	id := strings.TrimSpace(decision.CanonicalWrite.TargetID)
	switch {
	case kind != "" && id != "":
		return kind + ":" + id
	case kind != "":
		return kind
	default:
		return id
	}
}

func admissionDecisionReason(decision AdmissionDecisionResult) string {
	parts := []string{}
	if ds := strings.TrimSpace(decision.DomainState); ds != "" {
		parts = append(parts, ds)
	}
	if action := strings.TrimSpace(decision.RecommendedAction.Action); action != "" {
		reason := action
		if r := strings.TrimSpace(decision.RecommendedAction.Reason); r != "" {
			reason += " (" + r + ")"
		}
		parts = append(parts, reason)
	}
	if skip := strings.TrimSpace(decision.CanonicalWrite.SkippedReason); skip != "" {
		parts = append(parts, "write skipped: "+skip)
	}
	if len(parts) == 0 {
		return "state " + admissionDecisionState(decision.State)
	}
	return strings.Join(parts, "; ")
}

// admissionDecisionState normalizes the persisted decision state to the
// admission-audit vocabulary, defaulting an empty value to ambiguous rather than
// presenting it as clean.
func admissionDecisionState(state string) string {
	s := strings.ToLower(strings.TrimSpace(state))
	switch s {
	case "admitted", "rejected", "ambiguous", "stale", "missing_evidence",
		"permission_hidden", "unsupported", "unsafe":
		return s
	case "":
		return "ambiguous"
	default:
		return s
	}
}

func admissionGeneration(decisions []AdmissionDecisionResult) string {
	for _, decision := range decisions {
		if g := strings.TrimSpace(decision.GenerationID); g != "" {
			return g
		}
	}
	return ""
}

func admissionHandleIDs(handles []AdmissionDecisionSourceHandle, known map[string]struct{}) []string {
	ids := make([]string, 0, len(handles))
	for _, handle := range handles {
		id := strings.TrimSpace(handle.ID)
		if id == "" {
			continue
		}
		if _, ok := known[id]; ok {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	return ids
}

// formatStateCounts renders state counts in a stable, sorted order.
func formatStateCounts(counts map[string]int) string {
	states := make([]string, 0, len(counts))
	for state := range counts {
		states = append(states, state)
	}
	sort.Strings(states)
	parts := make([]string, 0, len(states))
	for _, state := range states {
		parts = append(parts, fmt.Sprintf("%d %s", counts[state], state))
	}
	return strings.Join(parts, ", ")
}
