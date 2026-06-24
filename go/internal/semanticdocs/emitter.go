// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semanticdocs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/doctruth"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	// CollectorKind marks semantic documentation facts produced by this package.
	CollectorKind = "semantic_extraction"
	// ProviderKindMock identifies synthetic provider output used by tests and
	// future fixtures. It is not a hosted provider integration.
	ProviderKindMock = "mock"

	// DefaultPromptVersion is the first semantic-docs prompt-pack handle.
	DefaultPromptVersion = "semantic-docs.v1"
	// DefaultRedactionVersion is the first semantic-docs redaction policy handle.
	DefaultRedactionVersion = "semantic-redaction.v1"
	// DefaultExtractorVersion is the first mock-only semantic-docs emitter handle.
	DefaultExtractorVersion = "semantic-docs-mock-emitter.v1"
)

// ProviderProfile identifies a configured semantic provider profile without
// credentials or request details.
type ProviderProfile struct {
	ProviderProfileID string
	ProviderKind      string
	ModelID           string
	EndpointProfileID string
}

// MockObservation is one parsed, already-redacted provider observation.
//
// This type is intentionally a fixture-facing boundary. It does not carry raw
// prompts, provider request bodies, provider responses, or credentials.
type MockObservation struct {
	ObservationType     string
	ObservationText     string
	ObservationHash     string
	Confidence          string
	ConfidenceRationale string
	MissingEvidence     []string
	UnsupportedReason   string
	AdmissionState      string
}

// Config controls semantic documentation envelope construction.
type Config struct {
	Provider         ProviderProfile
	PromptVersion    string
	RedactionVersion string
	ExtractorVersion string
	ExtractionMode   string
	PolicyState      string
	RedactionState   string
	RedactionSummary string
	FreshnessState   string
	ObservedAt       func() time.Time
}

// Emitter converts mocked semantic documentation observations into fact
// envelopes that keep model output provenance-only.
type Emitter struct {
	config Config
}

// NewEmitter validates configuration and returns a mock-only semantic
// documentation emitter.
func NewEmitter(config Config) (*Emitter, error) {
	normalized := normalizeConfig(config)
	if err := validateConfig(normalized); err != nil {
		return nil, err
	}
	return &Emitter{config: normalized}, nil
}

// Emit builds semantic.documentation_observation envelopes for one bounded
// documentation section.
func (e *Emitter) Emit(ctx context.Context, section doctruth.SectionInput, observations []MockObservation) ([]facts.Envelope, error) {
	if e == nil {
		return nil, fmt.Errorf("semantic documentation emitter is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateSection(section); err != nil {
		return nil, err
	}
	envelopes := make([]facts.Envelope, 0, len(observations))
	for index, observation := range observations {
		payload, err := e.payload(section, observation, index)
		if err != nil {
			return nil, err
		}
		if err := facts.ValidateSemanticDocumentationObservationPayload(payload); err != nil {
			return nil, fmt.Errorf("semantic documentation observation payload: %w", err)
		}
		stableKey := facts.SemanticDocumentationObservationStableID(payload)
		payloadMap, err := semanticPayloadMap(payload)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, facts.Envelope{
			FactID:           stableKey,
			ScopeID:          section.ScopeID,
			GenerationID:     section.GenerationID,
			FactKind:         facts.SemanticDocumentationObservationFactKind,
			StableFactKey:    stableKey,
			SchemaVersion:    facts.SemanticFactSchemaVersion,
			CollectorKind:    CollectorKind,
			SourceConfidence: facts.SourceConfidenceDerived,
			ObservedAt:       e.observedAt(section),
			Payload:          payloadMap,
			SourceRef: facts.Ref{
				SourceSystem:   section.SourceSystem,
				ScopeID:        section.ScopeID,
				GenerationID:   section.GenerationID,
				FactKey:        stableKey,
				SourceURI:      section.CanonicalURI,
				SourceRecordID: section.SectionID,
			},
		})
	}
	return envelopes, nil
}

func normalizeConfig(config Config) Config {
	config.Provider.ProviderProfileID = strings.TrimSpace(config.Provider.ProviderProfileID)
	config.Provider.ProviderKind = strings.TrimSpace(config.Provider.ProviderKind)
	config.Provider.ModelID = strings.TrimSpace(config.Provider.ModelID)
	config.Provider.EndpointProfileID = strings.TrimSpace(config.Provider.EndpointProfileID)
	config.PromptVersion = strings.TrimSpace(config.PromptVersion)
	config.RedactionVersion = strings.TrimSpace(config.RedactionVersion)
	config.ExtractorVersion = strings.TrimSpace(config.ExtractorVersion)
	config.ExtractionMode = strings.TrimSpace(config.ExtractionMode)
	config.PolicyState = strings.TrimSpace(config.PolicyState)
	config.RedactionState = strings.TrimSpace(config.RedactionState)
	config.RedactionSummary = strings.TrimSpace(config.RedactionSummary)
	config.FreshnessState = strings.TrimSpace(config.FreshnessState)
	if config.PromptVersion == "" {
		config.PromptVersion = DefaultPromptVersion
	}
	if config.RedactionVersion == "" {
		config.RedactionVersion = DefaultRedactionVersion
	}
	if config.ExtractorVersion == "" {
		config.ExtractorVersion = DefaultExtractorVersion
	}
	if config.ExtractionMode == "" {
		config.ExtractionMode = facts.SemanticExtractionModeAssistant
	}
	if config.PolicyState == "" {
		config.PolicyState = facts.SemanticPolicyAllowed
	}
	if config.RedactionState == "" {
		config.RedactionState = facts.SemanticRedactionSkippedNoSensitiveContent
	}
	if config.FreshnessState == "" {
		config.FreshnessState = facts.SemanticFreshnessFresh
	}
	return config
}

func validateConfig(config Config) error {
	if strings.TrimSpace(config.Provider.ProviderProfileID) == "" {
		return fmt.Errorf("provider_profile_id must not be blank")
	}
	if strings.TrimSpace(config.Provider.ProviderKind) == "" {
		return fmt.Errorf("provider_kind must not be blank")
	}
	probe := facts.SemanticDocumentationObservationPayload{
		ObservationID:   "config-probe",
		ObservationType: "config_probe",
		ObservationHash: "sha256:config-probe",
		Source: facts.SemanticSourceRef{
			SourceID:    "config-probe",
			SourceClass: facts.SemanticSourceClassDocumentation,
		},
		Chunk: facts.SemanticChunkRef{
			ChunkID:          "config-probe",
			ChunkHash:        "sha256:config-probe",
			SourceHash:       "sha256:config-probe",
			PromptVersion:    config.PromptVersion,
			RedactionVersion: config.RedactionVersion,
			ExtractorVersion: config.ExtractorVersion,
			ExtractionMode:   config.ExtractionMode,
		},
		Provider: facts.SemanticProviderRef{
			ProviderProfileID: config.Provider.ProviderProfileID,
			ProviderKind:      config.Provider.ProviderKind,
		},
		PolicyState:    config.PolicyState,
		RedactionState: config.RedactionState,
		FreshnessState: config.FreshnessState,
		AdmissionState: facts.SemanticAdmissionProvenanceOnly,
	}
	if err := facts.ValidateSemanticDocumentationObservationPayload(probe); err != nil {
		return fmt.Errorf("semantic documentation emitter config: %w", err)
	}
	return nil
}

func validateSection(section doctruth.SectionInput) error {
	required := map[string]string{
		"scope_id":      section.ScopeID,
		"generation_id": section.GenerationID,
		"source_system": section.SourceSystem,
		"document_id":   section.DocumentID,
		"revision_id":   section.RevisionID,
		"section_id":    section.SectionID,
		"canonical_uri": section.CanonicalURI,
		"excerpt_hash":  section.ExcerptHash,
	}
	for field, value := range required {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s must not be blank", field)
		}
	}
	return nil
}

func (e *Emitter) payload(section doctruth.SectionInput, observation MockObservation, index int) (facts.SemanticDocumentationObservationPayload, error) {
	observationType := strings.TrimSpace(observation.ObservationType)
	if observationType == "" {
		return facts.SemanticDocumentationObservationPayload{}, fmt.Errorf("observation_type must not be blank")
	}

	admissionState := strings.TrimSpace(observation.AdmissionState)
	if admissionState == "" {
		admissionState = facts.SemanticAdmissionProvenanceOnly
	}
	redactionState := e.config.RedactionState
	observationText := strings.TrimSpace(observation.ObservationText)
	observationHash := strings.TrimSpace(observation.ObservationHash)
	confidence := strings.TrimSpace(observation.Confidence)
	confidenceRationale := strings.TrimSpace(observation.ConfidenceRationale)
	missingEvidence := compactStrings(observation.MissingEvidence)
	unsupportedReason := strings.TrimSpace(observation.UnsupportedReason)
	redactionSummary := e.config.RedactionSummary
	if redactionState == facts.SemanticRedactionUnsafePayload {
		admissionState = facts.SemanticAdmissionProvenanceOnly
		observationType = facts.SemanticRedactionUnsafePayload
		observationText = ""
		observationHash = ""
		confidence = ""
		confidenceRationale = ""
		missingEvidence = nil
		unsupportedReason = facts.SemanticRedactionUnsafePayload
		if redactionSummary == "" {
			redactionSummary = "unsafe payload rejected before provider egress"
		}
	}
	if !semanticDocsAdmissionStateAllowed(admissionState) {
		return facts.SemanticDocumentationObservationPayload{}, fmt.Errorf(
			"semantic documentation emitter admission_state %q must stay provenance-only or finding-candidate before reducer admission",
			admissionState,
		)
	}

	if observationHash == "" {
		observationHash = prefixedHash("semantic-docs-observation", map[string]any{
			"document_id":         section.DocumentID,
			"section_id":          section.SectionID,
			"revision_id":         section.RevisionID,
			"excerpt_hash":        section.ExcerptHash,
			"observation_type":    observationType,
			"observation_index":   index,
			"prompt_version":      e.config.PromptVersion,
			"redaction_version":   e.config.RedactionVersion,
			"extractor_version":   e.config.ExtractorVersion,
			"provider_profile_id": e.config.Provider.ProviderProfileID,
		})
	}
	observationID := "semantic-doc-obs:" + facts.StableID("semantic-docs-observation-id", map[string]any{
		"document_id":      section.DocumentID,
		"section_id":       section.SectionID,
		"observation_hash": observationHash,
	})

	return facts.SemanticDocumentationObservationPayload{
		ObservationID:       observationID,
		ObservationType:     observationType,
		ObservationText:     observationText,
		ObservationHash:     observationHash,
		Confidence:          confidence,
		ConfidenceRationale: confidenceRationale,
		MissingEvidence:     missingEvidence,
		UnsupportedReason:   unsupportedReason,
		Source: facts.SemanticSourceRef{
			SourceID:       semanticSourceID(section),
			SourceClass:    facts.SemanticSourceClassDocumentation,
			SourceHandle:   section.CanonicalURI,
			DocumentID:     section.DocumentID,
			ExternalAnchor: uriFragment(section.CanonicalURI),
			SectionID:      section.SectionID,
		},
		Chunk: facts.SemanticChunkRef{
			ChunkID:          semanticChunkID(e.config, section),
			ChunkHash:        section.ExcerptHash,
			SourceHash:       section.RevisionID,
			PromptVersion:    e.config.PromptVersion,
			RedactionVersion: e.config.RedactionVersion,
			ExtractorVersion: e.config.ExtractorVersion,
			ExtractionMode:   e.config.ExtractionMode,
		},
		Provider: facts.SemanticProviderRef{
			ProviderProfileID: e.config.Provider.ProviderProfileID,
			ProviderKind:      e.config.Provider.ProviderKind,
			ModelID:           e.config.Provider.ModelID,
			EndpointProfileID: e.config.Provider.EndpointProfileID,
		},
		PolicyState:      e.config.PolicyState,
		RedactionState:   redactionState,
		RedactionSummary: redactionSummary,
		FreshnessState:   e.config.FreshnessState,
		AdmissionState:   admissionState,
		EvidenceRefs: []facts.DocumentationEvidenceRef{{
			Kind:       facts.DocumentationSectionFactKind,
			ID:         section.SectionID,
			URI:        section.CanonicalURI,
			Confidence: facts.SourceConfidenceObserved,
		}},
		ACLSummary: observationACLSummary(section.SourceACLState),
		ObservedAt: e.observedAt(section).UTC().Format(time.RFC3339),
	}, nil
}

func semanticDocsAdmissionStateAllowed(state string) bool {
	switch state {
	case facts.SemanticAdmissionProvenanceOnly,
		facts.SemanticAdmissionDocumentationFindingCandidate:
		return true
	default:
		return false
	}
}

func (e *Emitter) observedAt(section doctruth.SectionInput) time.Time {
	if e.config.ObservedAt != nil {
		return e.config.ObservedAt().UTC()
	}
	if !section.ObservedAt.IsZero() {
		return section.ObservedAt.UTC()
	}
	return time.Now().UTC()
}

func semanticPayloadMap(payload facts.SemanticDocumentationObservationPayload) (map[string]any, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal semantic documentation observation payload: %w", err)
	}
	var out map[string]any
	if err := json.Unmarshal(encoded, &out); err != nil {
		return nil, fmt.Errorf("unmarshal semantic documentation observation payload: %w", err)
	}
	return out, nil
}

func semanticSourceID(section doctruth.SectionInput) string {
	return "semantic-doc-source:" + facts.StableID("semantic-docs-source", map[string]any{
		"source_system": section.SourceSystem,
		"document_id":   section.DocumentID,
	})
}

func semanticChunkID(config Config, section doctruth.SectionInput) string {
	return "semantic-doc-chunk:" + facts.StableID("semantic-docs-chunk", map[string]any{
		"document_id":         section.DocumentID,
		"revision_id":         section.RevisionID,
		"section_id":          section.SectionID,
		"excerpt_hash":        section.ExcerptHash,
		"prompt_version":      config.PromptVersion,
		"redaction_version":   config.RedactionVersion,
		"extractor_version":   config.ExtractorVersion,
		"provider_profile_id": config.Provider.ProviderProfileID,
	})
}

func prefixedHash(kind string, identity map[string]any) string {
	return "sha256:" + facts.StableID(kind, identity)
}

func uriFragment(rawURI string) string {
	parsed, err := url.Parse(rawURI)
	if err != nil {
		return ""
	}
	return parsed.Fragment
}

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
