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
// The hint id is derived from the hint's CONTENT, not from its position in the
// batch. Position looks like a reasonable disambiguator until a retry returns
// the same hints in a different order, or inserts one ahead of an existing one:
// every following hint would take a new stable fact key, so an unchanged hint
// would read as churn against the previous generation and a retry inside one
// generation could leave duplicate rows behind. Content-derived identity makes
// re-emitting the same hint idempotent regardless of batch ordering.
func (e *Emitter) payload(span CodeSpanInput, hint HintInput, index int) (facts.SemanticCodeHintPayload, error) {
	_ = index
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

	// A provider cannot corroborate its own output. Corroboration is a claim
	// about deterministic evidence — a parser, reducer or read-model agreeing —
	// and none of that has run by the time this emitter sees a hint. Accepting
	// a caller-supplied "corroborated" would let model output be labelled as
	// confirmed evidence on the read surface, which is precisely the boundary
	// this package exists to hold.
	//
	// The two states a provider may legitimately report are the ones that claim
	// LESS: unsupported (it could not answer) and uncorroborated (the default).
	corroboration := strings.TrimSpace(hint.CorroborationState)
	switch corroboration {
	case "", facts.SemanticCorroborationUncorroborated:
		corroboration = facts.SemanticCorroborationUncorroborated
	case facts.SemanticCorroborationUnsupported:
		// Kept: it is weaker than uncorroborated, not stronger.
	default:
		return facts.SemanticCodeHintPayload{}, fmt.Errorf(
			"corroboration_state %q may not be supplied by a producer: deterministic evidence has not run, "+
				"so only %q or %q are emittable here",
			corroboration, facts.SemanticCorroborationUncorroborated, facts.SemanticCorroborationUnsupported)
	}

	objectRefs, err := normalizeObjectRefs(hint.ObjectRefs, span)
	if err != nil {
		return facts.SemanticCodeHintPayload{}, err
	}

	subject := hint.Subject
	if strings.TrimSpace(subject.RepositoryID) == "" {
		subject.RepositoryID = span.RepositoryID
	}
	if strings.TrimSpace(subject.RelativePath) == "" {
		subject.RelativePath = span.RelativePath
	}

	return facts.SemanticCodeHintPayload{
		HintID:           span.SpanID + ":" + hintType + ":" + hintHash,
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
		ObjectRefs:          objectRefs,
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
// when the provider left it off, and REJECTS a ref with no entity id.
//
// Dropping it silently would be worse than failing: a relationship hint would
// ship with a partial target list, or none, and a reader would take that as the
// provider's actual answer rather than as evidence the batch was malformed.
func normalizeObjectRefs(refs []facts.SemanticCodeEntityRef, span CodeSpanInput) ([]facts.SemanticCodeEntityRef, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	out := make([]facts.SemanticCodeEntityRef, 0, len(refs))
	for index, ref := range refs {
		if strings.TrimSpace(ref.EntityID) == "" {
			return nil, fmt.Errorf("object_refs[%d] has a blank entity_id: a reference that names nothing cannot be emitted", index)
		}
		if strings.TrimSpace(ref.RepositoryID) == "" {
			ref.RepositoryID = span.RepositoryID
		}
		out = append(out, ref)
	}
	return out, nil
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
