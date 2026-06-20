package query

import (
	"fmt"
	"strings"
)

// validateInvestigationPacket runs the contract gates for a supported packet and
// returns the recorded PacketValidation plus an error when any required gate
// fails. The builder calls it before stamping the packet id, so a failing packet
// is never returned to a caller or written to disk.
//
// The gates cover schema consistency, a known family and present scope, bounded
// layers, share-safe redaction metadata, the semantic-observation policy, and
// referential integrity from reducer decisions and semantic observations back
// into the source-fact layer. Every gate result is recorded so an operator can
// see exactly which contract rule a malformed packet broke.
func validateInvestigationPacket(packet *InvestigationEvidencePacket) (PacketValidation, error) {
	checks := []PacketValidationCheck{}
	var failures []string

	record := func(id string, ok bool, failure string) {
		checks = append(checks, PacketValidationCheck{ID: id, OK: ok})
		if !ok {
			failures = append(failures, failure)
		}
	}

	record("schema_consistent", packet.Schema == InvestigationEvidencePacketSchema,
		fmt.Sprintf("schema = %q, want %q", packet.Schema, InvestigationEvidencePacketSchema))
	record("family_known", ValidInvestigationFamily(packet.Identity.Family),
		fmt.Sprintf("unknown investigation family %q", packet.Identity.Family))
	record("scope_present", len(packet.Identity.Subject) > 0,
		"identity.subject is empty; an investigation must name its scope")
	record("redaction_present",
		packet.Redaction.Profile == InvestigationEvidencePacketRedactionProfile && len(packet.Redaction.AppliedRules) > 0,
		"redaction metadata missing or wrong profile")

	boundsOK, boundsFail := boundsConsistent(packet)
	record("bounds_consistent", boundsOK, boundsFail)

	semanticOK, semanticFail := semanticPolicyOK(packet)
	record("semantic_policy", semanticOK, semanticFail)

	integrityOK, integrityFail := sourceRefIntegrity(packet)
	record("source_ref_integrity", integrityOK, integrityFail)

	missingOK, missingFail := missingHopsExplained(packet)
	record("missing_hops_explained", missingOK, missingFail)

	validation := PacketValidation{Checks: checks}
	if len(failures) > 0 {
		validation.Status = "failed"
		return validation, fmt.Errorf("investigation packet validation failed: %s", strings.Join(failures, "; "))
	}
	validation.Status = "passed"
	return validation, nil
}

func boundsConsistent(packet *InvestigationEvidencePacket) (bool, string) {
	if len(packet.SourceFacts) > packet.Bounds.MaxSourceFacts {
		return false, "source_facts exceed bound"
	}
	if len(packet.ReducerDecisions) > packet.Bounds.MaxReducerDecisions {
		return false, "reducer_decisions exceed bound"
	}
	if len(packet.GraphAnswers) > packet.Bounds.MaxGraphAnswers {
		return false, "graph_answers exceed bound"
	}
	if len(packet.Citations) > packet.Bounds.MaxCitations {
		return false, "citations exceed bound"
	}
	return true, ""
}

// semanticPolicyOK enforces the deterministic no-provider contract: semantic
// observations may appear only under the semantic_augmented basis and each must
// carry the semantic_observation label so a reader can never mistake one for
// deterministic truth.
func semanticPolicyOK(packet *InvestigationEvidencePacket) (bool, string) {
	if len(packet.SemanticObservations) == 0 {
		return true, ""
	}
	if packet.Identity.Basis != PacketBasisSemanticAugmented {
		return false, "semantic observations present under non-semantic basis"
	}
	for i, obs := range packet.SemanticObservations {
		if obs.Label != "semantic_observation" {
			return false, fmt.Sprintf("semantic observation %d missing semantic_observation label", i)
		}
		if strings.TrimSpace(obs.Observation) == "" {
			return false, fmt.Sprintf("semantic observation %d has no observation text", i)
		}
	}
	return true, ""
}

// sourceRefIntegrity verifies that every source-fact reference from a reducer
// decision or semantic observation resolves to a fact in the (possibly
// truncated) source layer, and that every decision carries a state. The builder
// already validated integrity against the full pre-truncation fact set, so when
// the source-facts layer was truncated a reference that no longer resolves is
// tolerated (the referenced fact was dropped by bounds, not missing). When the
// layer was not truncated, a dangling reference still fails the build so the
// artifact never ships a broken citation chain.
func sourceRefIntegrity(packet *InvestigationEvidencePacket) (bool, string) {
	if sourceFactsTruncated(packet.Bounds) {
		for i, decision := range packet.ReducerDecisions {
			if strings.TrimSpace(decision.State) == "" {
				return false, fmt.Sprintf("reducer decision %d has no state", i)
			}
		}
		return true, ""
	}
	known := knownFactKeys(packet.SourceFacts)
	return referencesResolve(known, packet.ReducerDecisions, packet.SemanticObservations)
}

func sourceFactsTruncated(bounds PacketBounds) bool {
	for _, layer := range bounds.TruncatedLayers {
		if layer == "source_facts" {
			return true
		}
	}
	return false
}

func missingHopsExplained(packet *InvestigationEvidencePacket) (bool, string) {
	for i, hop := range packet.MissingEvidence {
		if strings.TrimSpace(hop.Hop) == "" {
			return false, fmt.Sprintf("missing-evidence entry %d has no hop", i)
		}
		if strings.TrimSpace(hop.Reason) == "" {
			return false, fmt.Sprintf("missing-evidence entry %d has no reason", i)
		}
	}
	return true, ""
}
