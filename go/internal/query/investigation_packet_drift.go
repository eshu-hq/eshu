// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"sort"
	"strings"
)

// driftReproduceRoute is the bounded read that backs the drift packet.
const driftReproduceRoute = "POST /api/v0/cloud/runtime-drift/findings"

// BuildDriftPacket maps reducer-owned cloud runtime drift findings into the
// investigation_evidence_packet.v2 shape. It is the shared composer for the drift
// family. Each finding becomes a source fact plus a reducer decision whose state
// reflects the drift kind (orphaned/unmanaged → rejected reconciliation,
// ambiguous → ambiguous, unknown → missing_evidence), and a matched Terraform
// state address becomes a present management edge. It reads nothing and consults
// no provider, so the packet is deterministic.
//
// A nil bounds override uses the contract defaults.
func BuildDriftPacket(findings []CloudRuntimeDriftFindingView, scope map[string]string, truth *TruthEnvelope, bounds *PacketBounds) (InvestigationEvidencePacket, error) {
	in := InvestigationPacketInput{
		Family:           InvestigationFamilyDrift,
		Subject:          normalizeSubject(scope),
		Question:         driftQuestion(scope),
		Generation:       driftGeneration(findings),
		Truth:            truth,
		Bounds:           bounds,
		SourceFacts:      driftSourceFacts(findings),
		ReducerDecisions: driftReducerDecisions(findings),
		GraphAnswers:     driftGraphAnswers(findings),
		MissingEvidence:  driftMissingHops(findings),
		Summary:          driftSummary(findings),
		Limitations:      driftLimitations(findings),
		Reproduce: []PacketReproduceStep{{
			Description: "list cloud runtime drift findings for this scope",
			Route:       driftReproduceRoute,
			Tool:        "list_cloud_runtime_drift_findings",
		}},
	}
	return NewInvestigationEvidencePacket(in)
}

func driftQuestion(scope map[string]string) string {
	if v := strings.TrimSpace(scope["cloud_resource_uid"]); v != "" {
		return fmt.Sprintf("What runtime drift exists for %s?", v)
	}
	if v := strings.TrimSpace(scope["scope_id"]); v != "" {
		return fmt.Sprintf("What runtime drift exists in scope %s?", v)
	}
	return "What runtime drift exists for this scope?"
}

// driftSourceFacts maps each finding with a durable fact id into the raw-evidence
// layer. A finding with no fact id is skipped here (its reducer decision still
// represents the drift) so the layer never carries an unreferenceable, untraceable
// entry.
func driftSourceFacts(findings []CloudRuntimeDriftFindingView) []PacketSourceFact {
	facts := make([]PacketSourceFact, 0, len(findings))
	for _, finding := range findings {
		factID := strings.TrimSpace(finding.FactID)
		if factID == "" {
			continue
		}
		facts = append(facts, PacketSourceFact{
			FactID:         factID,
			EvidenceFamily: strings.TrimSpace(finding.FindingKind),
			CollectorKind:  strings.TrimSpace(finding.SourceSystem),
			Generation:     strings.TrimSpace(finding.GenerationID),
			Subject:        strings.TrimSpace(finding.CloudResourceUID),
			Summary:        driftFactSummary(finding),
		})
	}
	return facts
}

func driftFactSummary(finding CloudRuntimeDriftFindingView) string {
	parts := []string{}
	if p := strings.TrimSpace(finding.Provider); p != "" {
		parts = append(parts, p)
	}
	if m := strings.TrimSpace(finding.ManagementStatus); m != "" {
		parts = append(parts, m)
	}
	return strings.Join(parts, " ")
}

func driftReducerDecisions(findings []CloudRuntimeDriftFindingView) []PacketReducerDecision {
	out := make([]PacketReducerDecision, 0, len(findings))
	for _, finding := range findings {
		factID := strings.TrimSpace(finding.FactID)
		var refs []string
		if factID != "" {
			refs = []string{factID}
		}
		out = append(out, PacketReducerDecision{
			Domain:        "cloud_runtime_drift",
			Subject:       strings.TrimSpace(finding.CloudResourceUID),
			State:         driftFindingState(finding),
			Target:        strings.TrimSpace(finding.MatchedTerraformStateAddress),
			Reason:        driftDecisionReason(finding),
			Generation:    strings.TrimSpace(finding.GenerationID),
			SourceFactIDs: refs,
		})
	}
	return out
}

// driftFindingState maps a drift finding kind onto the admission-audit vocabulary
// so the drift state is explicit. Orphaned and unmanaged resources are rejected
// reconciliations, ambiguous resources are ambiguous, and unknown resources lack
// the evidence to decide. An unrecognized future kind defaults to missing_evidence
// rather than ambiguous, so a new kind is never silently presented as a resolved
// classification before this emitter is taught about it.
func driftFindingState(finding CloudRuntimeDriftFindingView) string {
	switch strings.ToLower(strings.TrimSpace(finding.FindingKind)) {
	case "orphaned_cloud_resource", "unmanaged_cloud_resource":
		return "rejected"
	case "ambiguous_cloud_resource":
		return "ambiguous"
	case "unknown_cloud_resource":
		return "missing_evidence"
	default:
		return "missing_evidence"
	}
}

func driftDecisionReason(finding CloudRuntimeDriftFindingView) string {
	parts := []string{}
	if k := strings.TrimSpace(finding.FindingKind); k != "" {
		parts = append(parts, k)
	}
	if s := strings.TrimSpace(finding.SourceState); s != "" {
		parts = append(parts, "source_state="+s)
	}
	if a := strings.TrimSpace(finding.RecommendedAction); a != "" {
		parts = append(parts, a)
	}
	if len(parts) == 0 {
		return "drift finding"
	}
	return strings.Join(parts, "; ")
}

// driftGraphAnswers projects a matched Terraform state address into a present
// management edge so a reconciled resource is visible as graph truth.
func driftGraphAnswers(findings []CloudRuntimeDriftFindingView) []PacketGraphAnswer {
	answers := []PacketGraphAnswer{}
	for _, finding := range findings {
		matched := strings.TrimSpace(finding.MatchedTerraformStateAddress)
		if matched == "" {
			continue
		}
		var refs []string
		if id := strings.TrimSpace(finding.FactID); id != "" {
			refs = []string{id}
		}
		answers = append(answers, PacketGraphAnswer{
			Relationship:  "MANAGED_BY_TERRAFORM",
			From:          strings.TrimSpace(finding.CloudResourceUID),
			To:            matched,
			Hop:           "iac_management",
			Present:       true,
			TruthClass:    AnswerTruthDeterministic,
			SourceFactIDs: refs,
		})
	}
	return answers
}

func driftMissingHops(findings []CloudRuntimeDriftFindingView) []PacketMissingHop {
	hops := []PacketMissingHop{}
	for _, finding := range findings {
		for _, reason := range finding.MissingEvidence {
			if r := strings.TrimSpace(reason); r != "" {
				hops = append(hops, PacketMissingHop{Hop: "iac_management", Reason: r})
			}
		}
	}
	return hops
}

func driftSummary(findings []CloudRuntimeDriftFindingView) string {
	if len(findings) == 0 {
		return ""
	}
	counts := map[string]int{}
	for _, finding := range findings {
		kind := strings.TrimSpace(finding.FindingKind)
		if kind == "" {
			kind = "unknown"
		}
		counts[kind]++
	}
	kinds := make([]string, 0, len(counts))
	for kind := range counts {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	parts := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		parts = append(parts, fmt.Sprintf("%d %s", counts[kind], kind))
	}
	return fmt.Sprintf("%d drift findings: %s.", len(findings), strings.Join(parts, ", "))
}

func driftLimitations(findings []CloudRuntimeDriftFindingView) []string {
	limitations := []string{}
	for _, finding := range findings {
		for _, warning := range finding.SafetyGate.Warnings {
			if w := strings.TrimSpace(warning); w != "" {
				limitations = append(limitations, "safety: "+w)
			}
		}
	}
	sort.Strings(limitations)
	return dedupePacketLimitations(limitations)
}

func driftGeneration(findings []CloudRuntimeDriftFindingView) string {
	for _, finding := range findings {
		if g := strings.TrimSpace(finding.GenerationID); g != "" {
			return g
		}
	}
	return ""
}
