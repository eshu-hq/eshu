// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"sort"
	"strings"
)

// Default per-layer caps for a v2 packet. They bound the artifact so a large
// investigation produces a truncated, signalled packet rather than an unbounded
// dump. Emitters may lower them via InvestigationPacketInput.Bounds but must not
// raise them without performance evidence.
const (
	defaultPacketMaxSourceFacts          = 200
	defaultPacketMaxReducerDecisions     = 200
	defaultPacketMaxGraphAnswers         = 200
	defaultPacketMaxCitations            = evidenceCitationMaxLimit
	defaultPacketMaxSemanticObservations = 50
	defaultPacketMaxMissingEvidence      = 200
)

// InvestigationPacketInput carries the composition inputs for an
// InvestigationEvidencePacket. The builder never reads a store, calls a
// provider, or invents identifiers; it composes a packet from evidence the
// caller already resolved from canonical surfaces.
type InvestigationPacketInput struct {
	// Family is the investigation family. Required; an unrecognized family yields
	// a PacketRefusalUnknownFamily packet rather than an error.
	Family InvestigationFamily
	// Subject carries canonical scope keys for the investigation.
	Subject map[string]string
	// Question is the canonical question the packet answers.
	Question string
	// Generation pins the observed source/projection generation.
	Generation string
	// Truth is the canonical truth envelope for the answer. A nil envelope yields
	// an unsupported (but valid) packet.
	Truth *TruthEnvelope
	// Summary is the proposed human-readable answer. It is dropped when the packet
	// is unsupported or partial-with-no-evidence.
	Summary string
	// SourceFacts is the raw-evidence layer.
	SourceFacts []PacketSourceFact
	// ReducerDecisions is the reducer-decision layer.
	ReducerDecisions []PacketReducerDecision
	// GraphAnswers is the graph/query-truth layer.
	GraphAnswers []PacketGraphAnswer
	// Citations is the addressable-evidence layer.
	Citations []evidenceCitationHandle
	// MissingEvidence is the explicit missing-hop layer.
	MissingEvidence []PacketMissingHop
	// SemanticObservations is the optional semantic layer. It is only permitted
	// when AllowSemantic is true; otherwise the builder returns an error so a
	// no-provider build stays deterministic.
	SemanticObservations []PacketSemanticObservation
	// AllowSemantic is the policy gate for semantic observations. When false, the
	// packet basis is deterministic and any supplied semantic observations are a
	// contract violation.
	AllowSemantic bool
	// Limitations carries packet-level caveats.
	Limitations []string
	// Reproduce lists bounded commands/routes/tools that reproduce the evidence.
	Reproduce []PacketReproduceStep
	// Refusal, when non-empty, builds a structured refusal packet for the given
	// terminal state instead of a supported answer.
	Refusal PacketRefusalState
	// Bounds optionally overrides the default per-layer caps.
	Bounds *PacketBounds
}

// NewInvestigationEvidencePacket composes and validates an
// InvestigationEvidencePacket from already-resolved evidence. It returns an
// error only when the inputs violate the contract (an unsupported semantic
// posture, a dangling source reference, or a failed validation gate). An
// unrecognized family or an explicit refusal yields a valid refusal packet, not
// an error, because a refusal is a legitimate, share-safe artifact.
func NewInvestigationEvidencePacket(in InvestigationPacketInput) (InvestigationEvidencePacket, error) {
	if in.Refusal == PacketRefusalNone && !ValidInvestigationFamily(in.Family) {
		in.Refusal = PacketRefusalUnknownFamily
	}
	if in.Refusal != PacketRefusalNone {
		return buildRefusalPacket(in)
	}
	if len(in.SemanticObservations) > 0 && !in.AllowSemantic {
		return InvestigationEvidencePacket{}, fmt.Errorf(
			"investigation packet: %d semantic observations supplied without AllowSemantic; "+
				"no-provider builds must stay deterministic", len(in.SemanticObservations),
		)
	}

	// Validate referential integrity against the full, pre-truncation fact set so
	// a typo'd or dangling source reference is rejected regardless of whether
	// bounds later drop the referenced fact. Post-truncation references that no
	// longer resolve are tolerated by validateInvestigationPacket because the
	// source-facts layer is explicitly marked truncated.
	knownInput := knownFactKeys(in.SourceFacts)
	if ok, msg := referencesResolve(knownInput, in.ReducerDecisions, in.SemanticObservations); !ok {
		return InvestigationEvidencePacket{}, fmt.Errorf("investigation packet: %s", msg)
	}

	basis := PacketBasisDeterministic
	if in.AllowSemantic && len(in.SemanticObservations) > 0 {
		basis = PacketBasisSemanticAugmented
	}

	packet := InvestigationEvidencePacket{
		Schema:               InvestigationEvidencePacketSchema,
		Identity:             buildPacketIdentity(in, basis),
		Truth:                cloneTruthEnvelope(in.Truth),
		SourceFacts:          nonNilSlice(in.SourceFacts),
		ReducerDecisions:     nonNilSlice(in.ReducerDecisions),
		GraphAnswers:         nonNilSlice(in.GraphAnswers),
		Citations:            nonNilSlice(in.Citations),
		MissingEvidence:      nonNilSlice(in.MissingEvidence),
		SemanticObservations: in.SemanticObservations,
		Reproduce:            in.Reproduce,
		Redaction:            defaultPacketRedaction(),
		Limitations:          dedupeStrings(in.Limitations),
	}
	packet.Bounds = applyPacketBounds(&packet, in.Bounds)
	packet.Freshness = packetFreshness(in.Truth)
	packet.Answer = buildPacketAnswer(in, &packet)

	checks, err := validateInvestigationPacket(&packet)
	packet.Validation = checks
	if err != nil {
		return InvestigationEvidencePacket{}, err
	}
	id, err := investigationPacketID(packet)
	if err != nil {
		return InvestigationEvidencePacket{}, fmt.Errorf("investigation packet: derive id: %w", err)
	}
	packet.PacketID = id
	return packet, nil
}

// nonNilSlice returns an empty slice for a nil input so a required-present
// evidence layer always marshals to a JSON array rather than null. This keeps
// the wire schema stable and the content digest reproducible whether a caller
// passes a nil or an empty slice for an empty layer.
func nonNilSlice[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// buildRefusalPacket builds a minimal but valid refusal artifact. A refusal
// packet is unsupported, carries no truth, and records the refusal reason as an
// unsupported reason so a reader sees why the investigation could not be built.
func buildRefusalPacket(in InvestigationPacketInput) (InvestigationEvidencePacket, error) {
	if !validRefusalState(in.Refusal) {
		return InvestigationEvidencePacket{}, fmt.Errorf("investigation packet: invalid refusal state %q", in.Refusal)
	}
	packet := InvestigationEvidencePacket{
		Schema:    InvestigationEvidencePacketSchema,
		Identity:  buildPacketIdentity(in, PacketBasisDeterministic),
		Refusal:   in.Refusal,
		Freshness: TruthFreshness{State: FreshnessUnavailable},
		Answer: PacketAnswer{
			TruthClass:         AnswerTruthUnsupported,
			Supported:          false,
			UnsupportedReasons: []string{refusalReason(in.Refusal)},
		},
		SourceFacts:      []PacketSourceFact{},
		ReducerDecisions: []PacketReducerDecision{},
		GraphAnswers:     []PacketGraphAnswer{},
		Citations:        []evidenceCitationHandle{},
		MissingEvidence:  []PacketMissingHop{},
		Redaction:        defaultPacketRedaction(),
		Limitations:      dedupeStrings(in.Limitations),
	}
	packet.Bounds = applyPacketBounds(&packet, in.Bounds)
	packet.Validation = PacketValidation{
		Status: "passed",
		Checks: []PacketValidationCheck{
			{ID: "schema_consistent", OK: true},
			{ID: "refusal_state_known", OK: true},
			{ID: "redaction_present", OK: true},
		},
	}
	id, err := investigationPacketID(packet)
	if err != nil {
		return InvestigationEvidencePacket{}, fmt.Errorf("investigation packet: derive id: %w", err)
	}
	packet.PacketID = id
	return packet, nil
}

func buildPacketIdentity(in InvestigationPacketInput, basis PacketBasis) PacketIdentity {
	identity := PacketIdentity{
		Family:     in.Family,
		Subject:    normalizeSubject(in.Subject),
		Question:   strings.TrimSpace(in.Question),
		Generation: strings.TrimSpace(in.Generation),
		Basis:      basis,
	}
	if in.Truth != nil {
		identity.Profile = in.Truth.Profile
		identity.Backend = in.Truth.Backend
	}
	return identity
}

// buildPacketAnswer derives the user-facing answer plan, mirroring the
// AnswerPacket invariant: a confident summary survives only for a supported
// answer that is either complete or still backed by resolved evidence.
func buildPacketAnswer(in InvestigationPacketInput, packet *InvestigationEvidencePacket) PacketAnswer {
	answer := PacketAnswer{
		TruthClass:  ClassifyAnswerTruth(in.Truth),
		Supported:   in.Truth != nil,
		Limitations: dedupeStrings(in.Limitations),
	}
	if !answer.Supported {
		answer.UnsupportedReasons = appendReason(answer.UnsupportedReasons,
			"no truth envelope; the investigation resolved no answerable evidence")
		return answer
	}
	if packet.Bounds.Truncated {
		answer.Partial = true
		answer.UnsupportedReasons = appendReason(answer.UnsupportedReasons,
			"packet truncated; not all evidence is included")
	}
	if len(packet.MissingEvidence) > 0 {
		answer.Partial = true
		answer.UnsupportedReasons = appendReason(answer.UnsupportedReasons,
			"some hops could not be resolved; see missing_evidence")
	}
	switch in.Truth.Freshness.State {
	case FreshnessStale:
		answer.Partial = true
		answer.UnsupportedReasons = appendReason(answer.UnsupportedReasons,
			freshnessReason("underlying data is stale", in.Truth.Freshness.Cause))
	case FreshnessBuilding:
		answer.Partial = true
		answer.UnsupportedReasons = appendReason(answer.UnsupportedReasons,
			freshnessReason("underlying index is still building", in.Truth.Freshness.Cause))
	}
	hasEvidence := len(packet.SourceFacts) > 0 || len(packet.GraphAnswers) > 0 || len(packet.Citations) > 0
	if !hasEvidence {
		answer.Partial = true
		answer.UnsupportedReasons = appendReason(answer.UnsupportedReasons,
			"no supporting evidence resolved for this investigation")
	}
	if !answer.Partial || hasEvidence {
		answer.Summary = strings.TrimSpace(in.Summary)
	}
	return answer
}

// applyPacketBounds caps each evidence layer and records truncation. It mutates
// the packet layers in place and returns the resolved bounds block.
func applyPacketBounds(packet *InvestigationEvidencePacket, override *PacketBounds) PacketBounds {
	bounds := PacketBounds{
		MaxSourceFacts:          defaultPacketMaxSourceFacts,
		MaxReducerDecisions:     defaultPacketMaxReducerDecisions,
		MaxGraphAnswers:         defaultPacketMaxGraphAnswers,
		MaxCitations:            defaultPacketMaxCitations,
		MaxSemanticObservations: defaultPacketMaxSemanticObservations,
		MaxMissingEvidence:      defaultPacketMaxMissingEvidence,
	}
	if override != nil {
		// Overrides may only lower a cap. A caller cannot raise a cap above the
		// contract default and thereby avoid truncation; raising requires a code
		// change with performance evidence, not a per-call override.
		bounds.MaxSourceFacts = clampBound(override.MaxSourceFacts, bounds.MaxSourceFacts)
		bounds.MaxReducerDecisions = clampBound(override.MaxReducerDecisions, bounds.MaxReducerDecisions)
		bounds.MaxGraphAnswers = clampBound(override.MaxGraphAnswers, bounds.MaxGraphAnswers)
		bounds.MaxCitations = clampBound(override.MaxCitations, bounds.MaxCitations)
		bounds.MaxSemanticObservations = clampBound(override.MaxSemanticObservations, bounds.MaxSemanticObservations)
		bounds.MaxMissingEvidence = clampBound(override.MaxMissingEvidence, bounds.MaxMissingEvidence)
	}
	if len(packet.SourceFacts) > bounds.MaxSourceFacts {
		packet.SourceFacts = packet.SourceFacts[:bounds.MaxSourceFacts]
		bounds.Truncated = true
		bounds.TruncatedLayers = append(bounds.TruncatedLayers, "source_facts")
	}
	if len(packet.ReducerDecisions) > bounds.MaxReducerDecisions {
		packet.ReducerDecisions = packet.ReducerDecisions[:bounds.MaxReducerDecisions]
		bounds.Truncated = true
		bounds.TruncatedLayers = append(bounds.TruncatedLayers, "reducer_decisions")
	}
	if len(packet.GraphAnswers) > bounds.MaxGraphAnswers {
		packet.GraphAnswers = packet.GraphAnswers[:bounds.MaxGraphAnswers]
		bounds.Truncated = true
		bounds.TruncatedLayers = append(bounds.TruncatedLayers, "graph_answers")
	}
	if len(packet.Citations) > bounds.MaxCitations {
		packet.Citations = packet.Citations[:bounds.MaxCitations]
		bounds.Truncated = true
		bounds.TruncatedLayers = append(bounds.TruncatedLayers, "citations")
	}
	if len(packet.SemanticObservations) > bounds.MaxSemanticObservations {
		packet.SemanticObservations = packet.SemanticObservations[:bounds.MaxSemanticObservations]
		bounds.Truncated = true
		bounds.TruncatedLayers = append(bounds.TruncatedLayers, "semantic_observations")
	}
	if len(packet.MissingEvidence) > bounds.MaxMissingEvidence {
		packet.MissingEvidence = packet.MissingEvidence[:bounds.MaxMissingEvidence]
		bounds.Truncated = true
		bounds.TruncatedLayers = append(bounds.TruncatedLayers, "missing_evidence")
	}
	return bounds
}

func packetFreshness(truth *TruthEnvelope) TruthFreshness {
	if truth == nil {
		return TruthFreshness{State: FreshnessUnavailable}
	}
	return truth.Freshness
}

func defaultPacketRedaction() PacketRedaction {
	return PacketRedaction{
		Profile: InvestigationEvidencePacketRedactionProfile,
		Version: "2",
		AppliedRules: []string{
			"addressable_handles_only",
			"reducer_approved_summaries",
			"no_raw_fact_payloads",
			"no_transport_metadata",
		},
		ReplacedFields: []string{},
	}
}

func subjectFingerprint(subject map[string]string) string {
	if len(subject) == 0 {
		return ""
	}
	keys := make([]string, 0, len(subject))
	for k := range subject {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(subject[k])
	}
	return b.String()
}

func normalizeSubject(subject map[string]string) map[string]string {
	if len(subject) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(subject))
	for k, v := range subject {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		out[key] = strings.TrimSpace(v)
	}
	return out
}

// clampBound resolves a per-layer cap override. A non-positive override keeps the
// default; a positive override is honored only when it lowers the cap, so an
// override can never raise a cap above the contract default and avoid truncation.
func clampBound(override, def int) int {
	if override <= 0 || override > def {
		return def
	}
	return override
}

func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func validRefusalState(state PacketRefusalState) bool {
	for _, s := range SupportedPacketRefusalStates() {
		if s == state {
			return true
		}
	}
	return false
}

func refusalReason(state PacketRefusalState) string {
	switch state {
	case PacketRefusalUnknownFamily:
		return "unknown investigation family; not in the supported set"
	case PacketRefusalScopeNotFound:
		return "requested subject scope resolved to no canonical entity"
	case PacketRefusalProfileUnsupported:
		return "active query profile cannot answer this investigation family"
	case PacketRefusalBackendUnavailable:
		return "graph or content backend is unavailable"
	default:
		return string(state)
	}
}
