// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

import documentationv1 "github.com/eshu-hq/eshu/sdk/go/factschema/documentation/v1"

// SourceRef identifies the source span used to build semantic evidence.
type SourceRef struct {
	// SourceID is the stable source identifier. Required.
	SourceID string `json:"source_id"`
	// SourceClass classifies the source, such as documentation or code. Required.
	SourceClass string `json:"source_class"`
	// SourceHandle is a source-native handle. Optional.
	SourceHandle *string `json:"source_handle,omitempty"`
	// RepositoryID is the repository identifier for code evidence. Optional.
	RepositoryID *string `json:"repository_id,omitempty"`
	// DocumentID is the document identifier for documentation evidence. Optional.
	DocumentID *string `json:"document_id,omitempty"`
	// RelativePath is the source-relative path for code evidence. Optional.
	RelativePath *string `json:"relative_path,omitempty"`
	// ExternalAnchor is a source-native anchor. Optional.
	ExternalAnchor *string `json:"external_anchor,omitempty"`
	// SectionID is the documentation section identifier. Optional.
	SectionID *string `json:"section_id,omitempty"`
	// LineStart is the starting source line. Optional.
	LineStart *int `json:"line_start,omitempty"`
	// LineEnd is the ending source line. Optional.
	LineEnd *int `json:"line_end,omitempty"`
	// PageStart is the starting source page. Optional.
	PageStart *int `json:"page_start,omitempty"`
	// PageEnd is the ending source page. Optional.
	PageEnd *int `json:"page_end,omitempty"`
}

// ChunkRef identifies the normalized, redacted chunk sent for extraction.
type ChunkRef struct {
	// ChunkID is the stable chunk identifier. Required.
	ChunkID string `json:"chunk_id"`
	// ChunkHash is the redacted chunk hash. Required.
	ChunkHash string `json:"chunk_hash"`
	// SourceHash is the source revision hash. Required.
	SourceHash string `json:"source_hash"`
	// PromptVersion identifies the prompt pack. Required.
	PromptVersion string `json:"prompt_version"`
	// RedactionVersion identifies the redaction policy. Required.
	RedactionVersion string `json:"redaction_version"`
	// ExtractorVersion identifies the extractor. Required.
	ExtractorVersion string `json:"extractor_version"`
	// ExtractionMode identifies where extraction ran. Required.
	ExtractionMode string `json:"extraction_mode"`
}

// ProviderRef identifies the configured provider profile without credentials.
type ProviderRef struct {
	// ProviderProfileID is the configured provider profile. Required.
	ProviderProfileID string `json:"provider_profile_id"`
	// ProviderKind classifies the provider. Required.
	ProviderKind string `json:"provider_kind"`
	// ModelID is the provider model identifier. Optional.
	ModelID *string `json:"model_id,omitempty"`
	// EndpointProfileID is the endpoint profile identifier. Optional.
	EndpointProfileID *string `json:"endpoint_profile_id,omitempty"`
}

// CodeEntityRef identifies a code entity referenced by a semantic hint.
type CodeEntityRef struct {
	// EntityID is the referenced entity identifier. Required.
	EntityID string `json:"entity_id"`
	// RepositoryID is the repository identifier. Optional.
	RepositoryID *string `json:"repository_id,omitempty"`
	// RelativePath is the source-relative path. Optional.
	RelativePath *string `json:"relative_path,omitempty"`
	// EntityKind classifies the entity. Optional.
	EntityKind *string `json:"entity_kind,omitempty"`
	// LineStart is the starting source line. Optional.
	LineStart *int `json:"line_start,omitempty"`
	// LineEnd is the ending source line. Optional.
	LineEnd *int `json:"line_end,omitempty"`
}

// DocumentationObservation is one redacted semantic documentation observation.
type DocumentationObservation struct {
	// ObservationID is the observation identifier. Required.
	ObservationID string `json:"observation_id"`
	// ObservationType classifies the observation. Required.
	ObservationType string `json:"observation_type"`
	// ObservationText is the redacted observation text. Optional.
	ObservationText *string `json:"observation_text,omitempty"`
	// ObservationHash is the observation hash. Required.
	ObservationHash string `json:"observation_hash"`
	// Source identifies the source span. Required.
	Source SourceRef `json:"source"`
	// Chunk identifies the extracted source chunk. Required.
	Chunk ChunkRef `json:"chunk"`
	// Provider identifies the provider profile. Required.
	Provider ProviderRef `json:"provider"`
	// Confidence is the provider confidence label. Optional.
	Confidence *string `json:"confidence,omitempty"`
	// ConfidenceRationale explains Confidence. Optional.
	ConfidenceRationale *string `json:"confidence_rationale,omitempty"`
	// MissingEvidence lists missing evidence notes. Optional.
	MissingEvidence []string `json:"missing_evidence,omitempty"`
	// UnsupportedReason explains unsupported observations. Optional.
	UnsupportedReason *string `json:"unsupported_reason,omitempty"`
	// FreshnessState records source freshness. Required.
	FreshnessState string `json:"freshness_state"`
	// PolicyState records semantic policy state. Required.
	PolicyState string `json:"policy_state"`
	// RedactionState records redaction state. Required.
	RedactionState string `json:"redaction_state"`
	// RedactionSummary summarizes redaction. Optional.
	RedactionSummary *string `json:"redaction_summary,omitempty"`
	// AdmissionState records reducer admission state. Required.
	AdmissionState string `json:"admission_state"`
	// EvidenceRefs references deterministic evidence. Optional.
	EvidenceRefs []documentationv1.EvidenceRef `json:"evidence_refs,omitempty"`
	// ACLSummary carries bounded source access posture. Optional.
	ACLSummary *documentationv1.ACLSummary `json:"acl_summary,omitempty"`
	// ObservedAt is the observed timestamp. Optional.
	ObservedAt *string `json:"observed_at,omitempty"`
}

// CodeHint is one non-canonical semantic code relationship hint.
type CodeHint struct {
	// HintID is the hint identifier. Required.
	HintID string `json:"hint_id"`
	// HintType classifies the hint. Required.
	HintType string `json:"hint_type"`
	// RelationshipKind classifies the relationship. Optional.
	RelationshipKind *string `json:"relationship_kind,omitempty"`
	// HintText is the redacted hint text. Optional.
	HintText *string `json:"hint_text,omitempty"`
	// HintHash is the hint hash. Required.
	HintHash string `json:"hint_hash"`
	// Source identifies the source span. Required.
	Source SourceRef `json:"source"`
	// Chunk identifies the extracted source chunk. Required.
	Chunk ChunkRef `json:"chunk"`
	// Provider identifies the provider profile. Required.
	Provider ProviderRef `json:"provider"`
	// Subject is the code entity the hint describes. Required.
	Subject CodeEntityRef `json:"subject"`
	// ObjectRefs are referenced code entities. Optional.
	ObjectRefs []CodeEntityRef `json:"object_refs,omitempty"`
	// Confidence is the provider confidence label. Optional.
	Confidence *string `json:"confidence,omitempty"`
	// ConfidenceRationale explains Confidence. Optional.
	ConfidenceRationale *string `json:"confidence_rationale,omitempty"`
	// MissingEvidence lists missing evidence notes. Optional.
	MissingEvidence []string `json:"missing_evidence,omitempty"`
	// UnsupportedReason explains unsupported hints. Optional.
	UnsupportedReason *string `json:"unsupported_reason,omitempty"`
	// CorroborationState records deterministic corroboration state. Required.
	CorroborationState string `json:"corroboration_state"`
	// PromotionPolicy records the promotion policy. Required.
	PromotionPolicy string `json:"promotion_policy"`
	// PolicyState records semantic policy state. Required.
	PolicyState string `json:"policy_state"`
	// RedactionState records redaction state. Required.
	RedactionState string `json:"redaction_state"`
	// FreshnessState records source freshness. Required.
	FreshnessState string `json:"freshness_state"`
	// ObservedAt is the observed timestamp. Optional.
	ObservedAt *string `json:"observed_at,omitempty"`
}
