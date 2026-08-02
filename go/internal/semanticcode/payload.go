// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semanticcode

import (
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// payload builds one code-hint payload from a span and one provider hint.
//
// index disambiguates two hints of the same type on the same span, so their
// fact identities differ. It is deliberately part of the hint id and not of the
// hint hash: the hash identifies the hint's content, the id identifies its
// position in this span's output, and replaying the same provider output must
// reproduce both.
func (e *Emitter) payload(span CodeSpanInput, hint HintInput, index int) (facts.SemanticCodeHintPayload, error) {
	hintType := strings.TrimSpace(hint.HintType)
	if hintType == "" {
		return facts.SemanticCodeHintPayload{}, fmt.Errorf("hint_type must not be blank")
	}
	if strings.TrimSpace(hint.Subject.EntityID) == "" {
		return facts.SemanticCodeHintPayload{}, fmt.Errorf("hint subject entity_id must not be blank")
	}

	hintHash := strings.TrimSpace(hint.HintHash)
	if hintHash == "" {
		hintHash = prefixedHash("semantic-code-hint", map[string]any{
			"hint_type":         hintType,
			"relationship_kind": strings.TrimSpace(hint.RelationshipKind),
			"hint_text":         strings.TrimSpace(hint.HintText),
			"subject_entity_id": strings.TrimSpace(hint.Subject.EntityID),
			"source_hash":       strings.TrimSpace(span.ContentHash),
		})
	}

	corroboration := strings.TrimSpace(hint.CorroborationState)
	if corroboration == "" {
		// Nothing has checked this hint against deterministic evidence yet, and
		// saying so is the whole point of the corroboration field.
		corroboration = facts.SemanticCorroborationUncorroborated
	}

	subject := hint.Subject
	if strings.TrimSpace(subject.RepositoryID) == "" {
		subject.RepositoryID = span.RepositoryID
	}
	if strings.TrimSpace(subject.RelativePath) == "" {
		subject.RelativePath = span.RelativePath
	}

	return facts.SemanticCodeHintPayload{
		HintID:           fmt.Sprintf("%s:%s:%d", span.SpanID, hintType, index),
		HintType:         hintType,
		RelationshipKind: strings.TrimSpace(hint.RelationshipKind),
		HintText:         strings.TrimSpace(hint.HintText),
		HintHash:         hintHash,
		Source: facts.SemanticSourceRef{
			SourceID:     semanticSourceID(span),
			SourceClass:  facts.SemanticSourceClassCode,
			SourceHandle: span.CanonicalURI,
			RepositoryID: span.RepositoryID,
			RelativePath: span.RelativePath,
			SectionID:    span.SpanID,
			LineStart:    span.LineStart,
			LineEnd:      span.LineEnd,
		},
		Chunk: facts.SemanticChunkRef{
			ChunkID:          semanticChunkID(e.config, span),
			ChunkHash:        prefixedHash("semantic-code-chunk", map[string]any{"span_id": span.SpanID, "content_hash": span.ContentHash}),
			SourceHash:       span.ContentHash,
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
		Subject:             subject,
		ObjectRefs:          normalizeObjectRefs(hint.ObjectRefs, span),
		Confidence:          strings.TrimSpace(hint.Confidence),
		ConfidenceRationale: strings.TrimSpace(hint.ConfidenceRationale),
		MissingEvidence:     compactStrings(hint.MissingEvidence),
		UnsupportedReason:   strings.TrimSpace(hint.UnsupportedReason),
		CorroborationState:  corroboration,
		// Not configurable. A code hint is provider output; it becomes a
		// canonical relationship only when deterministic evidence agrees, and
		// letting a caller relax that would make the read surface's
		// non-canonical label a lie.
		PromotionPolicy: facts.SemanticPromotionRequiresDeterministicEvidence,
		PolicyState:     e.config.PolicyState,
		RedactionState:  e.config.RedactionState,
		FreshnessState:  e.config.FreshnessState,
		ObservedAt:      e.observedAt(span).Format(time.RFC3339),
	}, nil
}

// normalizeObjectRefs fills each referenced entity's repository from the span
// when the provider left it off, and drops refs with no entity id — a ref that
// names nothing is not a reference.
func normalizeObjectRefs(refs []facts.SemanticCodeEntityRef, span CodeSpanInput) []facts.SemanticCodeEntityRef {
	if len(refs) == 0 {
		return nil
	}
	out := make([]facts.SemanticCodeEntityRef, 0, len(refs))
	for _, ref := range refs {
		if strings.TrimSpace(ref.EntityID) == "" {
			continue
		}
		if strings.TrimSpace(ref.RepositoryID) == "" {
			ref.RepositoryID = span.RepositoryID
		}
		out = append(out, ref)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func semanticSourceID(span CodeSpanInput) string {
	return prefixedHash("semantic-code-source", map[string]any{
		"repository_id": span.RepositoryID,
		"relative_path": span.RelativePath,
	})
}

func semanticChunkID(config Config, span CodeSpanInput) string {
	return prefixedHash("semantic-code-chunk-id", map[string]any{
		"span_id":           span.SpanID,
		"prompt_version":    config.PromptVersion,
		"redaction_version": config.RedactionVersion,
		"extractor_version": config.ExtractorVersion,
	})
}

func prefixedHash(kind string, identity map[string]any) string {
	return facts.StableID(kind, identity)
}

func compactStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
