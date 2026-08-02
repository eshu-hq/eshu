// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semanticcode

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	// CollectorKind marks semantic code-hint facts produced by this package.
	CollectorKind = "semantic_extraction"
	// ProviderKindMock identifies synthetic provider output used by tests and
	// fixtures. It is not a hosted provider integration.
	ProviderKindMock = "mock"

	// DefaultPromptVersion is the first semantic code-hint prompt-pack handle.
	DefaultPromptVersion = "semantic-code.v1"
	// DefaultRedactionVersion is the first semantic code-hint redaction policy handle.
	DefaultRedactionVersion = "semantic-redaction.v1"
	// DefaultExtractorVersion is the first semantic code-hint emitter handle.
	DefaultExtractorVersion = "semantic-code-emitter.v1"
)

// ProviderProfile identifies a configured semantic provider profile without
// credentials or request details.
type ProviderProfile struct {
	ProviderProfileID string
	ProviderKind      string
	ModelID           string
	EndpointProfileID string
}

// CodeSpanInput identifies the code the hints describe: the chunk that was
// extracted, and the repository span it came from.
//
// It is the code-side counterpart of doctruth.SectionInput. Every field is
// provenance the read surface and the replay path need; none of it is provider
// request detail.
type CodeSpanInput struct {
	ScopeID      string
	GenerationID string
	SourceSystem string
	RepositoryID string
	RelativePath string
	// SpanID identifies the extracted span within the file, and becomes the
	// fact's source record id.
	SpanID string
	// CanonicalURI addresses the span for a reader following the evidence back.
	CanonicalURI string
	// ContentHash is the hash of the source the provider actually saw. It is
	// what makes a hint go stale when the file changes.
	ContentHash string
	LineStart   int
	LineEnd     int
	ObservedAt  time.Time
}

// HintInput is one parsed, already-redacted provider hint.
//
// This type is intentionally a fixture-facing boundary. It carries no raw
// prompts, provider request bodies, provider responses, or credentials.
type HintInput struct {
	HintType            string
	RelationshipKind    string
	HintText            string
	HintHash            string
	Subject             facts.SemanticCodeEntityRef
	ObjectRefs          []facts.SemanticCodeEntityRef
	Confidence          string
	ConfidenceRationale string
	MissingEvidence     []string
	UnsupportedReason   string
	// CorroborationState records what deterministic evidence says about this
	// hint. It defaults to uncorroborated, which is the honest state for a hint
	// nothing has checked yet.
	CorroborationState string
}

// Config controls semantic code-hint envelope construction.
type Config struct {
	Provider         ProviderProfile
	PromptVersion    string
	RedactionVersion string
	ExtractorVersion string
	ExtractionMode   string
	PolicyState      string
	RedactionState   string
	FreshnessState   string
	Now              func() time.Time
}

// Emitter builds semantic.code_hint envelopes for one provider profile.
type Emitter struct {
	config Config
}

// NewEmitter validates config and returns an emitter bound to it. It fails
// closed on a config that could not produce an admissible payload, so a
// misconfigured provider profile is a startup error rather than a run of facts
// that dead-letter one at a time.
func NewEmitter(config Config) (*Emitter, error) {
	config = normalizeConfig(config)
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	return &Emitter{config: config}, nil
}

// Emit builds one envelope per hint. It returns an error rather than a partial
// batch: a hint that fails validation means the caller's provider output does
// not meet the admission contract, and silently dropping it would reproduce the
// empty-answer failure this package exists to fix.
func (e *Emitter) Emit(ctx context.Context, span CodeSpanInput, hints []HintInput) ([]facts.Envelope, error) {
	if e == nil {
		return nil, fmt.Errorf("semantic code hint emitter is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("semantic code hint emit cancelled: %w", err)
	}
	if err := validateSpan(span); err != nil {
		return nil, err
	}

	envelopes := make([]facts.Envelope, 0, len(hints))
	for index, hint := range hints {
		payload, err := e.payload(span, hint, index)
		if err != nil {
			return nil, err
		}
		if err := facts.ValidateSemanticCodeHintPayload(payload); err != nil {
			return nil, fmt.Errorf("semantic code hint payload: %w", err)
		}
		stableKey := facts.SemanticCodeHintStableID(payload)
		payloadMap, err := facts.EncodeSemanticCodeHint(payload)
		if err != nil {
			return nil, fmt.Errorf("encode semantic code hint: %w", err)
		}
		envelopes = append(envelopes, facts.Envelope{
			FactID:           stableKey,
			ScopeID:          span.ScopeID,
			GenerationID:     span.GenerationID,
			FactKind:         facts.SemanticCodeHintFactKind,
			StableFactKey:    stableKey,
			SchemaVersion:    facts.SemanticFactSchemaVersion,
			CollectorKind:    CollectorKind,
			SourceConfidence: facts.SourceConfidenceDerived,
			ObservedAt:       e.observedAt(span),
			Payload:          payloadMap,
			SourceRef: facts.Ref{
				SourceSystem:   span.SourceSystem,
				ScopeID:        span.ScopeID,
				GenerationID:   span.GenerationID,
				FactKey:        stableKey,
				SourceURI:      span.CanonicalURI,
				SourceRecordID: span.SpanID,
			},
		})
	}
	return envelopes, nil
}

func (e *Emitter) observedAt(span CodeSpanInput) time.Time {
	if !span.ObservedAt.IsZero() {
		return span.ObservedAt.UTC()
	}
	if e.config.Now != nil {
		return e.config.Now().UTC()
	}
	return time.Now().UTC()
}
