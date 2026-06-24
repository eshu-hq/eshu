// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import (
	"fmt"
	"slices"
)

const (
	// SemanticDocumentationObservationFactKind identifies one LLM-assisted
	// documentation observation that remains evidence until reducer admission.
	SemanticDocumentationObservationFactKind = "semantic.documentation_observation"
	// SemanticCodeHintFactKind identifies one LLM-assisted code relationship
	// hint that remains non-canonical until deterministic evidence corroborates it.
	SemanticCodeHintFactKind = "semantic.code_hint"

	// SemanticFactSchemaVersion is the first semantic evidence fact schema.
	SemanticFactSchemaVersion = "1.0.0"
)

const (
	// SemanticSourceClassDocumentation marks documentation text, diagrams, or runbooks.
	SemanticSourceClassDocumentation = "documentation"
	// SemanticSourceClassCode marks repository code snippets used for hinting.
	SemanticSourceClassCode = "code"

	// SemanticExtractionModeHosted marks extraction performed by hosted Eshu jobs.
	SemanticExtractionModeHosted = "hosted"
	// SemanticExtractionModeLocal marks extraction performed by a local provider.
	SemanticExtractionModeLocal = "local"
	// SemanticExtractionModeAssistant marks extraction mediated by a trusted assistant.
	SemanticExtractionModeAssistant = "assistant_mediated"

	// SemanticConfidenceHigh marks high-confidence semantic evidence.
	SemanticConfidenceHigh = "high"
	// SemanticConfidenceMedium marks medium-confidence semantic evidence.
	SemanticConfidenceMedium = "medium"
	// SemanticConfidenceLow marks low-confidence semantic evidence.
	SemanticConfidenceLow = "low"

	// SemanticPolicyAllowed marks a source scope allowed for semantic extraction.
	SemanticPolicyAllowed = "allowed"
	// SemanticPolicyDenied marks a source scope denied by semantic extraction policy.
	SemanticPolicyDenied = "denied"
	// SemanticPolicyDisabledForScope marks a provider that is unavailable for this scope.
	SemanticPolicyDisabledForScope = "disabled_for_scope"
	// SemanticPolicyDisabledByPolicy marks an explicit policy disablement.
	SemanticPolicyDisabledByPolicy = "disabled_by_policy"

	// SemanticRedactionApplied marks payloads redacted before extraction.
	SemanticRedactionApplied = "applied"
	// SemanticRedactionSkippedNoSensitiveContent marks payloads with nothing to redact.
	SemanticRedactionSkippedNoSensitiveContent = "skipped_no_sensitive_content"
	// SemanticRedactionUnsafePayload marks payloads rejected before provider egress.
	SemanticRedactionUnsafePayload = "unsafe_payload"

	// SemanticFreshnessFresh marks evidence matching the current source hash.
	SemanticFreshnessFresh = "fresh"
	// SemanticFreshnessStale marks evidence whose source hash is no longer current.
	SemanticFreshnessStale = "stale"
	// SemanticFreshnessUnavailable marks evidence whose source freshness cannot be proven.
	SemanticFreshnessUnavailable = "unavailable"

	// SemanticAdmissionProvenanceOnly keeps semantic output as evidence only.
	SemanticAdmissionProvenanceOnly = "provenance_only"
	// SemanticAdmissionDocumentationFindingCandidate allows reducer-owned
	// documentation finding admission to inspect the observation.
	SemanticAdmissionDocumentationFindingCandidate = "documentation_finding_candidate"
	// SemanticAdmissionExact marks an observation admitted only after
	// deterministic source, graph, or read-model corroboration.
	SemanticAdmissionExact = "exact"
	// SemanticAdmissionPartial marks an observation with some corroborated
	// claim parts but incomplete deterministic support.
	SemanticAdmissionPartial = "partial"
	// SemanticAdmissionAmbiguous marks an observation whose candidate targets or
	// meanings cannot be reduced to one canonical truth.
	SemanticAdmissionAmbiguous = "ambiguous"
	// SemanticAdmissionStale marks an observation whose source, ACL, prompt, or
	// corroborating truth is no longer current.
	SemanticAdmissionStale = "stale"
	// SemanticAdmissionUnsafe marks an observation rejected by prompt, source,
	// response, or payload safety policy.
	SemanticAdmissionUnsafe = "unsafe"
	// SemanticAdmissionUnsupported marks an observation kind, source family,
	// provider mode, or claim family without an admitted reducer rule.
	SemanticAdmissionUnsupported = "unsupported"

	// SemanticCorroborationUncorroborated marks hints with no deterministic support.
	SemanticCorroborationUncorroborated = "uncorroborated"
	// SemanticCorroborationCorroborated marks hints supported by deterministic evidence.
	SemanticCorroborationCorroborated = "corroborated"
	// SemanticCorroborationAmbiguous marks hints with multiple possible deterministic matches.
	SemanticCorroborationAmbiguous = "ambiguous"
	// SemanticCorroborationContradicted marks hints contradicted by deterministic evidence.
	SemanticCorroborationContradicted = "contradicted"
	// SemanticCorroborationUnsupported marks hints that cannot be checked by current parsers.
	SemanticCorroborationUnsupported = "unsupported"

	// SemanticPromotionRequiresDeterministicEvidence prevents model output from
	// directly promoting service, deployment, runtime, vulnerability, or infrastructure truth.
	SemanticPromotionRequiresDeterministicEvidence = "requires_deterministic_evidence"
)

var semanticFactKinds = []string{
	SemanticDocumentationObservationFactKind,
	SemanticCodeHintFactKind,
}

var semanticSchemaVersions = map[string]string{
	SemanticDocumentationObservationFactKind: SemanticFactSchemaVersion,
	SemanticCodeHintFactKind:                 SemanticFactSchemaVersion,
}

// SemanticFactKinds returns the accepted semantic evidence fact kinds.
func SemanticFactKinds() []string {
	return slices.Clone(semanticFactKinds)
}

// SemanticSchemaVersion returns the schema version for a semantic evidence fact kind.
func SemanticSchemaVersion(factKind string) (string, bool) {
	version, ok := semanticSchemaVersions[factKind]
	return version, ok
}

// ValidateSemanticDocumentationObservationPayload validates the required
// provenance and admission boundary for a documentation observation payload.
func ValidateSemanticDocumentationObservationPayload(payload SemanticDocumentationObservationPayload) error {
	if payload.ObservationID == "" {
		return fmt.Errorf("semantic documentation observation_id must not be blank")
	}
	if payload.ObservationType == "" {
		return fmt.Errorf("semantic documentation observation_type must not be blank")
	}
	if payload.ObservationHash == "" {
		return fmt.Errorf("semantic documentation observation_hash must not be blank")
	}
	if err := validateSemanticSource(payload.Source); err != nil {
		return fmt.Errorf("semantic documentation source: %w", err)
	}
	if err := validateSemanticChunk(payload.Chunk); err != nil {
		return fmt.Errorf("semantic documentation chunk: %w", err)
	}
	if err := validateSemanticProvider(payload.Provider); err != nil {
		return fmt.Errorf("semantic documentation provider: %w", err)
	}
	if err := validateSemanticCommonState(payload.PolicyState, payload.RedactionState, payload.FreshnessState); err != nil {
		return err
	}
	switch payload.AdmissionState {
	case SemanticAdmissionProvenanceOnly,
		SemanticAdmissionDocumentationFindingCandidate,
		SemanticAdmissionExact,
		SemanticAdmissionPartial,
		SemanticAdmissionAmbiguous,
		SemanticAdmissionStale,
		SemanticAdmissionUnsafe,
		SemanticAdmissionUnsupported:
		return nil
	case "":
		return fmt.Errorf("semantic documentation admission_state must not be blank")
	default:
		return fmt.Errorf("semantic documentation admission_state %q is unsupported", payload.AdmissionState)
	}
}

// ValidateSemanticCodeHintPayload validates the required provenance and
// non-canonical promotion boundary for a semantic code hint payload.
func ValidateSemanticCodeHintPayload(payload SemanticCodeHintPayload) error {
	if payload.HintID == "" {
		return fmt.Errorf("semantic code hint_id must not be blank")
	}
	if payload.HintType == "" {
		return fmt.Errorf("semantic code hint_type must not be blank")
	}
	if payload.HintHash == "" {
		return fmt.Errorf("semantic code hint_hash must not be blank")
	}
	if err := validateSemanticSource(payload.Source); err != nil {
		return fmt.Errorf("semantic code source: %w", err)
	}
	if err := validateSemanticChunk(payload.Chunk); err != nil {
		return fmt.Errorf("semantic code chunk: %w", err)
	}
	if err := validateSemanticProvider(payload.Provider); err != nil {
		return fmt.Errorf("semantic code provider: %w", err)
	}
	if payload.Subject.EntityID == "" {
		return fmt.Errorf("semantic code subject entity_id must not be blank")
	}
	switch payload.CorroborationState {
	case SemanticCorroborationUncorroborated,
		SemanticCorroborationCorroborated,
		SemanticCorroborationAmbiguous,
		SemanticCorroborationContradicted,
		SemanticCorroborationUnsupported:
	case "":
		return fmt.Errorf("semantic code corroboration_state must not be blank")
	default:
		return fmt.Errorf("semantic code corroboration_state %q is unsupported", payload.CorroborationState)
	}
	if payload.PromotionPolicy != SemanticPromotionRequiresDeterministicEvidence {
		return fmt.Errorf("semantic code promotion_policy must be %q", SemanticPromotionRequiresDeterministicEvidence)
	}
	return validateSemanticCommonState(payload.PolicyState, payload.RedactionState, payload.FreshnessState)
}

func validateSemanticSource(source SemanticSourceRef) error {
	if source.SourceID == "" {
		return fmt.Errorf("source_id must not be blank")
	}
	if source.SourceClass == "" {
		return fmt.Errorf("source_class must not be blank")
	}
	return nil
}

func validateSemanticChunk(chunk SemanticChunkRef) error {
	if chunk.ChunkID == "" {
		return fmt.Errorf("chunk_id must not be blank")
	}
	if chunk.ChunkHash == "" {
		return fmt.Errorf("chunk_hash must not be blank")
	}
	if chunk.SourceHash == "" {
		return fmt.Errorf("source_hash must not be blank")
	}
	if chunk.PromptVersion == "" {
		return fmt.Errorf("prompt_version must not be blank")
	}
	if chunk.RedactionVersion == "" {
		return fmt.Errorf("redaction_version must not be blank")
	}
	if chunk.ExtractorVersion == "" {
		return fmt.Errorf("extractor_version must not be blank")
	}
	if chunk.ExtractionMode == "" {
		return fmt.Errorf("extraction_mode must not be blank")
	}
	switch chunk.ExtractionMode {
	case SemanticExtractionModeHosted, SemanticExtractionModeLocal, SemanticExtractionModeAssistant:
		return nil
	default:
		return fmt.Errorf("extraction_mode %q is unsupported", chunk.ExtractionMode)
	}
}

func validateSemanticProvider(provider SemanticProviderRef) error {
	if provider.ProviderProfileID == "" {
		return fmt.Errorf("provider_profile_id must not be blank")
	}
	if provider.ProviderKind == "" {
		return fmt.Errorf("provider_kind must not be blank")
	}
	return nil
}

func validateSemanticCommonState(policyState, redactionState, freshnessState string) error {
	switch policyState {
	case SemanticPolicyAllowed,
		SemanticPolicyDenied,
		SemanticPolicyDisabledForScope,
		SemanticPolicyDisabledByPolicy:
	case "":
		return fmt.Errorf("semantic policy_state must not be blank")
	default:
		return fmt.Errorf("semantic policy_state %q is unsupported", policyState)
	}

	switch redactionState {
	case SemanticRedactionApplied,
		SemanticRedactionSkippedNoSensitiveContent,
		SemanticRedactionUnsafePayload:
	case "":
		return fmt.Errorf("semantic redaction_state must not be blank")
	default:
		return fmt.Errorf("semantic redaction_state %q is unsupported", redactionState)
	}

	switch freshnessState {
	case SemanticFreshnessFresh,
		SemanticFreshnessStale,
		SemanticFreshnessUnavailable:
	case "":
		return fmt.Errorf("semantic freshness_state must not be blank")
	default:
		return fmt.Errorf("semantic freshness_state %q is unsupported", freshnessState)
	}
	return nil
}

// SemanticSourceRef identifies the source span used to build semantic evidence.
type SemanticSourceRef struct {
	SourceID       string `json:"source_id"`
	SourceClass    string `json:"source_class"`
	SourceHandle   string `json:"source_handle,omitempty"`
	RepositoryID   string `json:"repository_id,omitempty"`
	DocumentID     string `json:"document_id,omitempty"`
	RelativePath   string `json:"relative_path,omitempty"`
	ExternalAnchor string `json:"external_anchor,omitempty"`
	SectionID      string `json:"section_id,omitempty"`
	LineStart      int    `json:"line_start,omitempty"`
	LineEnd        int    `json:"line_end,omitempty"`
	PageStart      int    `json:"page_start,omitempty"`
	PageEnd        int    `json:"page_end,omitempty"`
}

// SemanticChunkRef identifies the normalized, redacted chunk sent for extraction.
type SemanticChunkRef struct {
	ChunkID          string `json:"chunk_id"`
	ChunkHash        string `json:"chunk_hash"`
	SourceHash       string `json:"source_hash"`
	PromptVersion    string `json:"prompt_version"`
	RedactionVersion string `json:"redaction_version"`
	ExtractorVersion string `json:"extractor_version"`
	ExtractionMode   string `json:"extraction_mode"`
}

// SemanticProviderRef identifies the configured provider profile without credentials.
type SemanticProviderRef struct {
	ProviderProfileID string `json:"provider_profile_id"`
	ProviderKind      string `json:"provider_kind"`
	ModelID           string `json:"model_id,omitempty"`
	EndpointProfileID string `json:"endpoint_profile_id,omitempty"`
}

// SemanticCodeEntityRef identifies a code entity referenced by a semantic hint.
type SemanticCodeEntityRef struct {
	RepositoryID string `json:"repository_id"`
	RelativePath string `json:"relative_path,omitempty"`
	EntityKind   string `json:"entity_kind"`
	EntityID     string `json:"entity_id"`
	LineStart    int    `json:"line_start,omitempty"`
	LineEnd      int    `json:"line_end,omitempty"`
}

// SemanticDocumentationObservationPayload describes one redacted documentation observation.
type SemanticDocumentationObservationPayload struct {
	ObservationID       string                     `json:"observation_id"`
	ObservationType     string                     `json:"observation_type"`
	ObservationText     string                     `json:"observation_text,omitempty"`
	ObservationHash     string                     `json:"observation_hash"`
	Source              SemanticSourceRef          `json:"source"`
	Chunk               SemanticChunkRef           `json:"chunk"`
	Provider            SemanticProviderRef        `json:"provider"`
	Confidence          string                     `json:"confidence,omitempty"`
	ConfidenceRationale string                     `json:"confidence_rationale,omitempty"`
	MissingEvidence     []string                   `json:"missing_evidence,omitempty"`
	UnsupportedReason   string                     `json:"unsupported_reason,omitempty"`
	FreshnessState      string                     `json:"freshness_state"`
	PolicyState         string                     `json:"policy_state"`
	RedactionState      string                     `json:"redaction_state"`
	RedactionSummary    string                     `json:"redaction_summary,omitempty"`
	AdmissionState      string                     `json:"admission_state"`
	EvidenceRefs        []DocumentationEvidenceRef `json:"evidence_refs,omitempty"`
	// ACLSummary carries the bounded source access posture observed for the
	// document this observation was extracted from, propagated verbatim from
	// the owning documentation source/document fact (see
	// DocumentationACLSummary.SourceACLState). It is additive evidence
	// metadata: an observation inherits its document's observed
	// source_acl_state so the docs-evidence projection and readbacks carry the
	// posture end-to-end. It is omitted when the document asserted no bounded
	// ACL claim (absence means "no ACL claim"); a denied, partial, missing, or
	// stale observation is never upgraded to allowed. It is factual
	// propagation only and never decides disclosure or enforcement.
	ACLSummary *DocumentationACLSummary `json:"acl_summary,omitempty"`
	ObservedAt string                   `json:"observed_at,omitempty"`
}

// SemanticCodeHintPayload describes one non-canonical code relationship hint.
type SemanticCodeHintPayload struct {
	HintID              string                  `json:"hint_id"`
	HintType            string                  `json:"hint_type"`
	RelationshipKind    string                  `json:"relationship_kind,omitempty"`
	HintText            string                  `json:"hint_text,omitempty"`
	HintHash            string                  `json:"hint_hash"`
	Source              SemanticSourceRef       `json:"source"`
	Chunk               SemanticChunkRef        `json:"chunk"`
	Provider            SemanticProviderRef     `json:"provider"`
	Subject             SemanticCodeEntityRef   `json:"subject"`
	ObjectRefs          []SemanticCodeEntityRef `json:"object_refs,omitempty"`
	Confidence          string                  `json:"confidence,omitempty"`
	ConfidenceRationale string                  `json:"confidence_rationale,omitempty"`
	MissingEvidence     []string                `json:"missing_evidence,omitempty"`
	UnsupportedReason   string                  `json:"unsupported_reason,omitempty"`
	CorroborationState  string                  `json:"corroboration_state"`
	PromotionPolicy     string                  `json:"promotion_policy"`
	PolicyState         string                  `json:"policy_state"`
	RedactionState      string                  `json:"redaction_state"`
	FreshnessState      string                  `json:"freshness_state"`
	ObservedAt          string                  `json:"observed_at,omitempty"`
}

// SemanticDocumentationObservationStableID returns a stable ID for one observation.
func SemanticDocumentationObservationStableID(payload SemanticDocumentationObservationPayload) string {
	return StableID(SemanticDocumentationObservationFactKind, map[string]any{
		"observation_id":      payload.ObservationID,
		"observation_hash":    payload.ObservationHash,
		"source_id":           payload.Source.SourceID,
		"document_id":         payload.Source.DocumentID,
		"section_id":          payload.Source.SectionID,
		"chunk_id":            payload.Chunk.ChunkID,
		"chunk_hash":          payload.Chunk.ChunkHash,
		"source_hash":         payload.Chunk.SourceHash,
		"prompt_version":      payload.Chunk.PromptVersion,
		"redaction_version":   payload.Chunk.RedactionVersion,
		"extractor_version":   payload.Chunk.ExtractorVersion,
		"extraction_mode":     payload.Chunk.ExtractionMode,
		"provider_profile_id": payload.Provider.ProviderProfileID,
	})
}

// SemanticCodeHintStableID returns a stable ID for one non-canonical code hint.
func SemanticCodeHintStableID(payload SemanticCodeHintPayload) string {
	return StableID(SemanticCodeHintFactKind, map[string]any{
		"hint_id":             payload.HintID,
		"hint_hash":           payload.HintHash,
		"repository_id":       payload.Source.RepositoryID,
		"relative_path":       payload.Source.RelativePath,
		"chunk_id":            payload.Chunk.ChunkID,
		"chunk_hash":          payload.Chunk.ChunkHash,
		"source_hash":         payload.Chunk.SourceHash,
		"prompt_version":      payload.Chunk.PromptVersion,
		"redaction_version":   payload.Chunk.RedactionVersion,
		"extractor_version":   payload.Chunk.ExtractorVersion,
		"extraction_mode":     payload.Chunk.ExtractionMode,
		"provider_profile_id": payload.Provider.ProviderProfileID,
		"subject_entity_id":   payload.Subject.EntityID,
		"relationship_kind":   payload.RelationshipKind,
	})
}
