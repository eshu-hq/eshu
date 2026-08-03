// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semanticcode

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// normalizeConfig trims every operator-supplied handle and fills the defaults.
// The defaults are the conservative reading of each state: extraction is
// assistant-mediated rather than hosted, the payload is treated as having had
// nothing sensitive to redact rather than as verified-clean, and the hint is
// fresh only because the caller has just extracted it.
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

// validateConfig fails a config that could never produce an admissible payload,
// by running the real payload validator over a probe carrying the config's own
// state fields. Checking the config against the validator rather than against a
// hand-copied list of rules is what keeps this from drifting when the admission
// contract changes.
func validateConfig(config Config) error {
	if config.Provider.ProviderProfileID == "" {
		return fmt.Errorf("provider_profile_id must not be blank")
	}
	if config.Provider.ProviderKind == "" {
		return fmt.Errorf("provider_kind must not be blank")
	}
	// unsafe_payload means the redaction gate REJECTED this content. Emitting a
	// hint under that state would persist the very text the gate withheld, and
	// the read model would serve it. There is no safe way to carry hint text,
	// confidence rationale, missing-evidence notes or entity references through
	// a state that says the payload should have been quarantined.
	if config.RedactionState == facts.SemanticRedactionUnsafePayload {
		return fmt.Errorf(
			"redaction_state %q cannot emit: it marks content the redaction gate rejected, "+
				"so the hint must be dropped rather than serialized",
			facts.SemanticRedactionUnsafePayload)
	}

	probe := facts.SemanticCodeHintPayload{
		HintID:   "config-probe",
		HintType: "config_probe",
		HintHash: "sha256:config-probe",
		Source: facts.SemanticSourceRef{
			SourceID:    "config-probe",
			SourceClass: facts.SemanticSourceClassCode,
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
		Subject:            facts.SemanticCodeEntityRef{RepositoryID: "config-probe", EntityKind: "function", EntityID: "config-probe"},
		CorroborationState: facts.SemanticCorroborationUncorroborated,
		PromotionPolicy:    facts.SemanticPromotionRequiresDeterministicEvidence,
		PolicyState:        config.PolicyState,
		RedactionState:     config.RedactionState,
		FreshnessState:     config.FreshnessState,
	}
	if err := facts.ValidateSemanticCodeHintPayload(probe); err != nil {
		return fmt.Errorf("semantic code hint emitter config: %w", err)
	}
	return nil
}

// validateSpan rejects a span that cannot be traced back to its source. Every
// field here is what a reader needs to follow a hint to the code it describes,
// or what replay needs to rebuild the same fact identity.
func validateSpan(span CodeSpanInput) error {
	required := map[string]string{
		"scope_id":      span.ScopeID,
		"generation_id": span.GenerationID,
		"source_system": span.SourceSystem,
		"repository_id": span.RepositoryID,
		"relative_path": span.RelativePath,
		"span_id":       span.SpanID,
		"canonical_uri": span.CanonicalURI,
		"content_hash":  span.ContentHash,
	}
	for field, value := range required {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s must not be blank", field)
		}
	}

	// A span a reader cannot open is not provenance. Zero means "unattributed"
	// and is allowed; a negative or reversed range is a producer bug that would
	// otherwise ship as a hint pointing at an impossible place in the file.
	if span.LineStart < 0 || span.LineEnd < 0 {
		return fmt.Errorf("line range %d-%d is negative", span.LineStart, span.LineEnd)
	}
	if span.LineStart > 0 && span.LineEnd > 0 && span.LineEnd < span.LineStart {
		return fmt.Errorf("line range %d-%d ends before it starts", span.LineStart, span.LineEnd)
	}
	return nil
}
