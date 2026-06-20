package query

// InvestigationEvidencePacketSchema is the stable schema identifier for the
// portable, source-backed investigation evidence packet (packet v2). The "v2"
// distinguishes this layered, portable artifact from the v1 answer-facing
// companions (AnswerPacket and answer_metadata.v1), which are views over a
// single ResponseEnvelope rather than a self-contained, multi-layer artifact.
//
// The packet separates the evidence layers the epic requires — raw source
// facts, reducer decisions, graph/query truth, missing-evidence reasons,
// freshness, and optional semantic observations — so a reader can audit each
// layer independently without re-running a query. The contract is documented in
// docs/public/reference/investigation-evidence-packet.md.
const InvestigationEvidencePacketSchema = "investigation_evidence_packet.v2"

// InvestigationEvidencePacketRedactionProfile names the share-safe redaction
// profile every packet declares. The profile records that the packet carries
// only addressable evidence handles and bounded, reducer-approved summaries —
// never raw fact payloads, secrets, or transport metadata.
const InvestigationEvidencePacketRedactionProfile = "share_safe_v2"

// InvestigationFamily is the closed set of investigation families a v2 packet
// can describe. The CLI contract refuses any family outside this set with the
// PacketRefusalUnknownFamily state, so an unrecognized family never produces a
// silently empty artifact.
type InvestigationFamily string

const (
	// InvestigationFamilySupplyChainImpact traces a vulnerable package through
	// advisory, SBOM/image subject, and workload/service anchors. Implemented by
	// the supply-chain packet emitter (#3141).
	InvestigationFamilySupplyChainImpact InvestigationFamily = "supply_chain_impact"
	// InvestigationFamilyDeployableUnit describes deployable-unit truth: source
	// repo, deployment config, image/workload, and reducer admission decisions.
	// Implemented by the deployable-unit packet emitter (#3142).
	InvestigationFamilyDeployableUnit InvestigationFamily = "deployable_unit"
	// InvestigationFamilyDrift describes IaC-versus-runtime drift evidence for a
	// deployable unit or cloud resource. Implemented alongside the deployable-unit
	// emitter (#3142).
	InvestigationFamilyDrift InvestigationFamily = "drift"
	// InvestigationFamilyServiceContext describes a service dossier: the code,
	// deployment, and incident evidence anchored to one service. Reserved for the
	// dogfood benchmark service-context scenario (#3143).
	InvestigationFamilyServiceContext InvestigationFamily = "service_context"
)

// SupportedInvestigationFamilies returns the closed set of families the v2
// packet contract recognizes, in stable order. Callers use it to render the CLI
// contract help and to validate a requested family before building a packet.
func SupportedInvestigationFamilies() []InvestigationFamily {
	return []InvestigationFamily{
		InvestigationFamilySupplyChainImpact,
		InvestigationFamilyDeployableUnit,
		InvestigationFamilyDrift,
		InvestigationFamilyServiceContext,
	}
}

// ValidInvestigationFamily reports whether raw names a recognized family.
func ValidInvestigationFamily(raw InvestigationFamily) bool {
	for _, family := range SupportedInvestigationFamilies() {
		if family == raw {
			return true
		}
	}
	return false
}

// PacketRefusalState is the CLI-facing terminal state for a packet that could
// not be built into a supported answer. A refusal is an honest, structured
// outcome — never a partial or fabricated artifact. The empty value means the
// packet was not refused.
type PacketRefusalState string

const (
	// PacketRefusalNone marks a packet that was not refused.
	PacketRefusalNone PacketRefusalState = ""
	// PacketRefusalUnknownFamily is returned when the requested investigation
	// family is outside SupportedInvestigationFamilies. Maps to
	// ErrorCodeInvalidArgument on the API/MCP surfaces.
	PacketRefusalUnknownFamily PacketRefusalState = "unknown_family"
	// PacketRefusalScopeNotFound is returned when the requested subject scope
	// resolves to no canonical entity. Maps to ErrorCodeScopeNotFound.
	PacketRefusalScopeNotFound PacketRefusalState = "scope_not_found"
	// PacketRefusalProfileUnsupported is returned when the active query profile
	// cannot answer the family at all (for example a graph-authoritative family
	// under local_lightweight). Maps to ErrorCodeUnsupportedCapability.
	PacketRefusalProfileUnsupported PacketRefusalState = "profile_unsupported"
	// PacketRefusalBackendUnavailable is returned when the graph or content
	// backend is unavailable. Maps to ErrorCodeBackendUnavailable.
	PacketRefusalBackendUnavailable PacketRefusalState = "backend_unavailable"
)

// SupportedPacketRefusalStates returns the closed set of non-empty refusal
// states, in stable order, for the CLI contract documentation and tests.
func SupportedPacketRefusalStates() []PacketRefusalState {
	return []PacketRefusalState{
		PacketRefusalUnknownFamily,
		PacketRefusalScopeNotFound,
		PacketRefusalProfileUnsupported,
		PacketRefusalBackendUnavailable,
	}
}

// PacketBasis records whether a packet is purely deterministic or includes
// optional, policy-gated semantic observations. The default is deterministic so
// a no-provider build is reproducible byte-for-byte from the same inputs.
type PacketBasis string

const (
	// PacketBasisDeterministic marks a packet built only from reducer-owned,
	// deterministic evidence. No provider keys are required and no semantic
	// observations are present. This is the default and the no-provider contract.
	PacketBasisDeterministic PacketBasis = "deterministic"
	// PacketBasisSemanticAugmented marks a packet that additionally carries
	// optional semantic observations. Semantic observations are always labeled
	// and only permitted under this basis (see InvestigationPacketInput.AllowSemantic).
	PacketBasisSemanticAugmented PacketBasis = "semantic_augmented"
)

// PacketIdentity names the investigation a packet describes: its family, the
// canonical scope keys, the question, the observed generation, and the active
// profile/backend. The packet_id is derived deterministically from this
// identity plus a content digest, so the same inputs always produce the same id.
type PacketIdentity struct {
	// Family is the investigation family. Required.
	Family InvestigationFamily `json:"family"`
	// Subject carries canonical scope keys (for example repo_id, workload_id,
	// service_id, advisory_id, package_purl, subject_digest). encoding/json
	// marshals maps with sorted keys, keeping the wire form deterministic.
	Subject map[string]string `json:"subject"`
	// Question is the canonical, normalized question the packet answers. Optional.
	Question string `json:"question,omitempty"`
	// Generation is the source/projection generation the evidence was observed
	// at. It pins the snapshot so a packet is reproducible against a fixed graph
	// state. Optional but recommended.
	Generation string `json:"generation,omitempty"`
	// Profile is the active query profile that produced the evidence.
	Profile QueryProfile `json:"profile,omitempty"`
	// Backend is the active graph backend.
	Backend GraphBackend `json:"backend,omitempty"`
	// Basis records the deterministic-versus-semantic posture of the packet.
	Basis PacketBasis `json:"basis"`
}

// PacketSourceFact is one entry in the raw-evidence layer: an observed fact
// before any reducer decision. It carries identity and a bounded, redaction-safe
// summary plus an optional addressable citation handle — never the raw payload.
type PacketSourceFact struct {
	// FactID is the generation-scoped fact identity. Optional when StableKey is set.
	FactID string `json:"fact_id,omitempty"`
	// StableKey is the generation-independent durable key for the fact, preferred
	// for cross-generation references.
	StableKey string `json:"stable_key,omitempty"`
	// EvidenceFamily classifies the fact (for example vulnerability_advisory,
	// sbom_component, deployment_config).
	EvidenceFamily string `json:"evidence_family,omitempty"`
	// CollectorKind names the collector that observed the fact.
	CollectorKind string `json:"collector_kind,omitempty"`
	// Generation is the generation the fact was observed at.
	Generation string `json:"generation,omitempty"`
	// Subject is the canonical entity reference the fact is about.
	Subject string `json:"subject,omitempty"`
	// Summary is a bounded, reducer-approved one-line description. No raw payloads.
	Summary string `json:"summary,omitempty"`
	// Citation is an optional addressable handle into the underlying evidence,
	// in the evidence-citation handle shape.
	Citation *evidenceCitationHandle `json:"citation,omitempty"`
}

// PacketReducerDecision is one entry in the reducer-decision layer: a
// reducer-owned admission, correlation, or drift decision. State uses the
// admission-audit vocabulary so accepted, rejected, ambiguous, and stale
// outcomes are represented explicitly rather than hidden.
type PacketReducerDecision struct {
	// Domain names the reducer domain that owns the decision.
	Domain string `json:"domain,omitempty"`
	// Subject is the canonical entity the decision is about.
	Subject string `json:"subject,omitempty"`
	// State is the admission-audit outcome: admitted, rejected, ambiguous, stale,
	// or missing_evidence. Required.
	State string `json:"state"`
	// Target names the edge or node the decision materialized (when admitted).
	Target string `json:"target,omitempty"`
	// Reason is a bounded human-readable explanation, required for non-admitted
	// states so a rejection or ambiguity is never silent.
	Reason string `json:"reason,omitempty"`
	// Generation is the generation the decision was made at.
	Generation string `json:"generation,omitempty"`
	// SourceFactIDs references entries in the source-facts layer (by FactID or
	// StableKey) that the decision was based on. Validated for referential
	// integrity.
	SourceFactIDs []string `json:"source_fact_ids,omitempty"`
}

// PacketGraphAnswer is one entry in the graph/query-truth layer: a materialized
// edge or path hop the query surface returns, with its prompt-facing truth class.
type PacketGraphAnswer struct {
	// Relationship is the edge type (for example RUNS_IN, HANDLES_ROUTE).
	Relationship string `json:"relationship,omitempty"`
	// From is the canonical source node reference.
	From string `json:"from,omitempty"`
	// To is the canonical target node reference.
	To string `json:"to,omitempty"`
	// Hop is the path-hop label (for example image, workload, service).
	Hop string `json:"hop,omitempty"`
	// Present reports whether the hop/edge is materialized in the graph.
	Present bool `json:"present"`
	// TruthClass is the prompt-facing classification of the answer truth.
	TruthClass AnswerTruthClass `json:"truth_class,omitempty"`
	// SourceFactIDs references the source-fact ids (by FactID or StableKey) that
	// back a present hop, so a reader can trace the graph answer to raw evidence.
	SourceFactIDs []string `json:"source_fact_ids,omitempty"`
}

// PacketMissingHop names an unresolved hop, why it is missing, and a bounded
// drilldown call. It keeps the missing-evidence layer explicit: a gap is named
// and explained, never silently dropped or fabricated.
type PacketMissingHop struct {
	// Hop is the path-hop label that could not be resolved. Required.
	Hop string `json:"hop"`
	// Reason explains why the hop is unresolved. Required.
	Reason string `json:"reason"`
	// NextCheck is an optional bounded follow-up call (tool/route/reason shape)
	// that would resolve or explain the gap.
	NextCheck map[string]any `json:"next_check,omitempty"`
}

// PacketSemanticObservation is one entry in the optional semantic layer. It is
// always labeled as a semantic observation, names its provider, and is only
// permitted under PacketBasisSemanticAugmented. It never carries deterministic
// truth and is excluded from a no-provider build.
type PacketSemanticObservation struct {
	// Label is always "semantic_observation" so a reader can never mistake the
	// entry for deterministic truth.
	Label string `json:"label"`
	// Provider names the semantic provider that produced the observation.
	Provider string `json:"provider,omitempty"`
	// Observation is the bounded semantic note.
	Observation string `json:"observation"`
	// Confidence is an optional provider-reported confidence label.
	Confidence string `json:"confidence,omitempty"`
	// SourceFactIDs references the source-facts the observation was derived from.
	SourceFactIDs []string `json:"source_fact_ids,omitempty"`
}

// PacketAnswer is the short, user-facing answer plan, mirroring the AnswerPacket
// invariant: no confident summary survives when the answer is unsupported.
type PacketAnswer struct {
	// TruthClass is the prompt-facing truth classification of the answer.
	TruthClass AnswerTruthClass `json:"truth_class"`
	// Summary is the human-readable answer, empty when Supported is false.
	Summary string `json:"summary,omitempty"`
	// Supported is false when required evidence is unavailable or the packet was
	// refused.
	Supported bool `json:"supported"`
	// Partial is true when the answer is usable but incomplete (truncated, stale,
	// or missing hops).
	Partial bool `json:"partial"`
	// Limitations carries bounded human-readable caveats.
	Limitations []string `json:"limitations,omitempty"`
	// UnsupportedReasons explains why the answer is unsupported or partial.
	UnsupportedReasons []string `json:"unsupported_reasons,omitempty"`
}

// PacketBounds records the per-layer caps applied to a packet and whether any
// layer was truncated, so a large investigation produces a bounded artifact that
// signals truncation rather than an unbounded dump.
type PacketBounds struct {
	// MaxSourceFacts caps the source-facts layer.
	MaxSourceFacts int `json:"max_source_facts"`
	// MaxReducerDecisions caps the reducer-decision layer.
	MaxReducerDecisions int `json:"max_reducer_decisions"`
	// MaxGraphAnswers caps the graph/query-truth layer.
	MaxGraphAnswers int `json:"max_graph_answers"`
	// MaxCitations caps the citations layer; defaults to the evidence-citation
	// route cap.
	MaxCitations int `json:"max_citations"`
	// MaxSemanticObservations caps the optional semantic layer.
	MaxSemanticObservations int `json:"max_semantic_observations"`
	// MaxMissingEvidence caps the missing-evidence layer.
	MaxMissingEvidence int `json:"max_missing_evidence"`
	// Truncated is true when any layer was capped.
	Truncated bool `json:"truncated"`
	// TruncatedLayers names the layers that were capped, in apply order.
	TruncatedLayers []string `json:"truncated_layers,omitempty"`
}

// PacketRedaction records the share-safe redaction posture, mirroring the
// operator-digest artifact's redaction block.
type PacketRedaction struct {
	// Profile is the redaction profile identifier (share_safe_v2).
	Profile string `json:"profile"`
	// Version is the redaction profile version.
	Version string `json:"version"`
	// AppliedRules names the redaction rules the profile applies.
	AppliedRules []string `json:"applied_rules"`
	// ReplacedFields names any fields whose values were replaced by redaction.
	ReplacedFields []string `json:"replaced_fields"`
}

// PacketValidationCheck is one named validation gate result.
type PacketValidationCheck struct {
	// ID is the stable gate identifier (for example schema_consistent).
	ID string `json:"id"`
	// OK reports whether the gate passed.
	OK bool `json:"ok"`
}

// PacketValidation records the artifact validation outcome.
type PacketValidation struct {
	// Status is "passed" or "failed".
	Status string `json:"status"`
	// Checks lists every gate result so a reader sees which rule a packet broke.
	Checks []PacketValidationCheck `json:"checks"`
}

// InvestigationEvidencePacket is the portable, source-backed v2 evidence packet.
// It is a self-contained artifact (JSON, with markdown/HTML renderers added by
// the emitters) that separates raw evidence, reducer decisions, graph/query
// truth, missing-evidence reasons, freshness, and optional semantic
// observations. The packet never replaces reducer-owned truth; it carries copies
// of the truth labels and addressable handles so a reader can re-derive the
// answer from the canonical surfaces.
type InvestigationEvidencePacket struct {
	// Schema is always InvestigationEvidencePacketSchema.
	Schema string `json:"schema"`
	// PacketID is the deterministic identity derived from Identity plus a content
	// digest. The same inputs always produce the same id.
	PacketID string `json:"packet_id"`
	// Identity names the investigation and its scope.
	Identity PacketIdentity `json:"identity"`
	// Refusal is the terminal refusal state, empty for a built packet.
	Refusal PacketRefusalState `json:"refusal,omitempty"`
	// Truth is a copy of the canonical truth envelope, nil for refused packets.
	Truth *TruthEnvelope `json:"truth,omitempty"`
	// Freshness surfaces the freshness label at the top level for quick scanning.
	Freshness TruthFreshness `json:"freshness"`
	// Answer is the short user-facing answer plan.
	Answer PacketAnswer `json:"answer"`
	// SourceFacts is the raw-evidence layer.
	SourceFacts []PacketSourceFact `json:"source_facts"`
	// ReducerDecisions is the reducer-decision layer.
	ReducerDecisions []PacketReducerDecision `json:"reducer_decisions"`
	// GraphAnswers is the graph/query-truth layer.
	GraphAnswers []PacketGraphAnswer `json:"graph_answers"`
	// Citations is the addressable-evidence layer, in the citation handle shape.
	Citations []evidenceCitationHandle `json:"citations"`
	// MissingEvidence is the explicit missing-hop layer.
	MissingEvidence []PacketMissingHop `json:"missing_evidence"`
	// SemanticObservations is the optional, policy-gated semantic layer.
	SemanticObservations []PacketSemanticObservation `json:"semantic_observations,omitempty"`
	// Bounds records the per-layer caps and truncation state.
	Bounds PacketBounds `json:"bounds"`
	// Redaction records the share-safe redaction posture.
	Redaction PacketRedaction `json:"redaction"`
	// Validation records the artifact validation outcome.
	Validation PacketValidation `json:"validation"`
	// Limitations carries packet-level bounded caveats.
	Limitations []string `json:"limitations,omitempty"`
}
