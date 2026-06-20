package query

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// investigationPacketID derives a deterministic identity from the packet's
// identity plus a content digest over its evidence layers. The same inputs
// always produce the same id, and different evidence under the same identity
// produces a different id. It returns an error only if the content digest cannot
// be computed, so a failing digest never collapses to a fixed, ambiguous id.
func investigationPacketID(packet InvestigationEvidencePacket) (string, error) {
	digest, err := packetContentDigest(packet)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(packet.Schema)
	b.WriteString("|")
	b.WriteString(string(packet.Identity.Family))
	b.WriteString("|")
	b.WriteString(subjectFingerprint(packet.Identity.Subject))
	b.WriteString("|")
	b.WriteString(packet.Identity.Question)
	b.WriteString("|")
	b.WriteString(packet.Identity.Generation)
	b.WriteString("|")
	b.WriteString(string(packet.Identity.Basis))
	b.WriteString("|")
	b.WriteString(string(packet.Identity.Profile))
	b.WriteString("|")
	b.WriteString(string(packet.Identity.Backend))
	b.WriteString("|")
	b.WriteString(string(packet.Refusal))
	b.WriteString("|")
	b.WriteString(digest)
	sum := sha256.Sum256([]byte(b.String()))
	return "investigation-evidence-packet:" + hex.EncodeToString(sum[:]), nil
}

// packetContentDigest hashes the evidence layers so two packets with the same
// identity but different evidence get different ids. It deliberately excludes
// the derived Answer block: Answer is computed from the truth envelope,
// truncation, and missing-evidence state, so including it would change the id
// whenever answer-classification logic changes even though the raw evidence is
// unchanged. The layers marshal deterministically (maps sort by key), so the
// digest is reproducible for identical evidence.
func packetContentDigest(packet InvestigationEvidencePacket) (string, error) {
	content := struct {
		Truth     *TruthEnvelope              `json:"truth"`
		Source    []PacketSourceFact          `json:"source_facts"`
		Decisions []PacketReducerDecision     `json:"reducer_decisions"`
		Graph     []PacketGraphAnswer         `json:"graph_answers"`
		Citations []evidenceCitationHandle    `json:"citations"`
		Missing   []PacketMissingHop          `json:"missing_evidence"`
		Semantic  []PacketSemanticObservation `json:"semantic_observations"`
	}{
		Truth:     packet.Truth,
		Source:    packet.SourceFacts,
		Decisions: packet.ReducerDecisions,
		Graph:     packet.GraphAnswers,
		Citations: packet.Citations,
		Missing:   packet.MissingEvidence,
		Semantic:  packet.SemanticObservations,
	}
	raw, err := json.Marshal(content)
	if err != nil {
		return "", fmt.Errorf("marshal packet content digest: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// knownFactKeys collects the fact identities (FactID and StableKey) present in a
// source-fact layer, for referential-integrity checks of reducer decisions and
// semantic observations.
func knownFactKeys(facts []PacketSourceFact) map[string]struct{} {
	known := make(map[string]struct{}, len(facts)*2)
	for _, fact := range facts {
		if id := strings.TrimSpace(fact.FactID); id != "" {
			known[id] = struct{}{}
		}
		if key := strings.TrimSpace(fact.StableKey); key != "" {
			known[key] = struct{}{}
		}
	}
	return known
}

// referencesResolve reports whether every source-fact reference from a reducer
// decision or semantic observation resolves to a fact in known, and that every
// decision carries a state. It is used pre-truncation against the full input so
// a dangling reference is rejected before bounds can mask it.
func referencesResolve(known map[string]struct{}, decisions []PacketReducerDecision, semantic []PacketSemanticObservation) (bool, string) {
	for i, decision := range decisions {
		if strings.TrimSpace(decision.State) == "" {
			return false, fmt.Sprintf("reducer decision %d has no state", i)
		}
		for _, ref := range decision.SourceFactIDs {
			if _, ok := known[strings.TrimSpace(ref)]; !ok {
				return false, fmt.Sprintf("reducer decision %d references unknown source fact %q", i, ref)
			}
		}
	}
	for i, obs := range semantic {
		for _, ref := range obs.SourceFactIDs {
			if _, ok := known[strings.TrimSpace(ref)]; !ok {
				return false, fmt.Sprintf("semantic observation %d references unknown source fact %q", i, ref)
			}
		}
	}
	return true, ""
}
