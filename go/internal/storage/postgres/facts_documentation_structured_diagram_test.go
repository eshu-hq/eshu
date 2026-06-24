// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestFactStoreRoundTripsStructuredDiagramDocumentationFacts(t *testing.T) {
	t.Parallel()

	envelopes := structuredDiagramDocumentationEnvelopes(t)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{rows: structuredDiagramFactRows(t, envelopes)}},
	}
	store := NewFactStore(db)

	if err := store.UpsertFacts(context.Background(), envelopes); err != nil {
		t.Fatalf("UpsertFacts() error = %v, want nil", err)
	}
	loaded, err := store.ListFactsByKind(
		context.Background(),
		"scope-structured-diagram",
		"gen-structured-diagram",
		[]string{facts.DocumentationSectionFactKind, facts.DocumentationLinkFactKind},
	)
	if err != nil {
		t.Fatalf("ListFactsByKind() error = %v, want nil", err)
	}
	if got, want := len(loaded), 2; got != want {
		t.Fatalf("ListFactsByKind() len = %d, want %d", got, want)
	}
	for _, envelope := range loaded {
		metadata := envelope.Payload["source_metadata"].(map[string]any)
		if got, want := metadata["diagram_format"], "svg"; got != want {
			t.Fatalf("diagram_format = %#v, want %#v", got, want)
		}
	}
}

func structuredDiagramDocumentationEnvelopes(t *testing.T) []facts.Envelope {
	t.Helper()

	section := facts.DocumentationSectionPayload{
		DocumentID:       "doc:git:repository:r_diagram:docs/architecture.svg",
		RevisionID:       "abc123",
		SectionID:        "section:diagram",
		SectionAnchor:    "diagram",
		HeadingText:      "architecture",
		OrdinalPath:      []int{1},
		Content:          "Documentation Graph\nSVG Runbook",
		ContentFormat:    "svg",
		TextHash:         "sha256:structured-diagram-text",
		ExcerptHash:      "sha256:structured-diagram-excerpt",
		SourceStartRef:   "diagram:text",
		SourceEndRef:     "diagram:text",
		SourceMetadata:   map[string]string{"path": "docs/architecture.svg", "format_family": "diagram", "diagram_format": "svg"},
		ContainsWarnings: false,
	}
	link := facts.DocumentationLinkPayload{
		DocumentID:     section.DocumentID,
		RevisionID:     section.RevisionID,
		SectionID:      section.SectionID,
		LinkID:         "link:section:diagram:1",
		TargetURI:      "docs/svg-runbook.md",
		TargetKind:     "source_path",
		AnchorTextHash: "sha256:svg-runbook",
		SourceMetadata: map[string]string{"path": "docs/architecture.svg", "format_family": "diagram", "diagram_format": "svg"},
	}
	observedAt := time.Date(2026, time.June, 9, 7, 50, 0, 0, time.UTC)
	return []facts.Envelope{
		structuredDiagramEnvelope(t, facts.DocumentationSectionFactKind, facts.DocumentationSectionStableID(section), section, observedAt),
		structuredDiagramEnvelope(t, facts.DocumentationLinkFactKind, facts.DocumentationLinkStableID(link), link, observedAt),
	}
}

func structuredDiagramEnvelope(t *testing.T, kind string, key string, payload any, observedAt time.Time) facts.Envelope {
	t.Helper()

	payloadMap, err := structuredDiagramPayloadMap(payload)
	if err != nil {
		t.Fatalf("structuredDiagramPayloadMap() error = %v, want nil", err)
	}
	version := facts.DocumentationFactSchemaVersion
	if kind == facts.DocumentationSectionFactKind {
		version = facts.DocumentationSectionFactSchemaVersion
	}
	return facts.Envelope{
		FactID:           key,
		ScopeID:          "scope-structured-diagram",
		GenerationID:     "gen-structured-diagram",
		FactKind:         kind,
		StableFactKey:    key,
		SchemaVersion:    version,
		CollectorKind:    string(scope.CollectorDocumentation),
		SourceConfidence: facts.SourceConfidenceObserved,
		ObservedAt:       observedAt,
		Payload:          payloadMap,
		SourceRef: facts.Ref{
			SourceSystem:   string(scope.CollectorDocumentation),
			ScopeID:        "scope-structured-diagram",
			GenerationID:   "gen-structured-diagram",
			FactKey:        key,
			SourceURI:      "docs/architecture.svg",
			SourceRecordID: key,
		},
	}
}

func structuredDiagramPayloadMap(payload any) (map[string]any, error) {
	encoded, err := marshalPayload(structuredDiagramAnyMap(payload))
	if err != nil {
		return nil, err
	}
	return unmarshalPayload(encoded)
}

func structuredDiagramAnyMap(payload any) map[string]any {
	switch typed := payload.(type) {
	case facts.DocumentationSectionPayload:
		return map[string]any{
			"document_id":       typed.DocumentID,
			"revision_id":       typed.RevisionID,
			"section_id":        typed.SectionID,
			"section_anchor":    typed.SectionAnchor,
			"heading_text":      typed.HeadingText,
			"ordinal_path":      typed.OrdinalPath,
			"content":           typed.Content,
			"content_format":    typed.ContentFormat,
			"text_hash":         typed.TextHash,
			"excerpt_hash":      typed.ExcerptHash,
			"source_start_ref":  typed.SourceStartRef,
			"source_end_ref":    typed.SourceEndRef,
			"source_metadata":   typed.SourceMetadata,
			"contains_warnings": typed.ContainsWarnings,
		}
	case facts.DocumentationLinkPayload:
		return map[string]any{
			"document_id":      typed.DocumentID,
			"revision_id":      typed.RevisionID,
			"section_id":       typed.SectionID,
			"link_id":          typed.LinkID,
			"target_uri":       typed.TargetURI,
			"target_kind":      typed.TargetKind,
			"anchor_text_hash": typed.AnchorTextHash,
			"source_metadata":  typed.SourceMetadata,
		}
	default:
		return map[string]any{}
	}
}

func structuredDiagramFactRows(t *testing.T, envelopes []facts.Envelope) [][]any {
	t.Helper()

	rows := make([][]any, 0, len(envelopes))
	for _, envelope := range envelopes {
		payload, err := marshalPayload(envelope.Payload)
		if err != nil {
			t.Fatalf("marshalPayload() error = %v, want nil", err)
		}
		rows = append(rows, []any{
			envelope.FactID,
			envelope.ScopeID,
			envelope.GenerationID,
			envelope.FactKind,
			envelope.StableFactKey,
			envelope.SchemaVersion,
			envelope.CollectorKind,
			envelope.FencingToken,
			envelope.SourceConfidence,
			envelope.SourceRef.SourceSystem,
			envelope.SourceRef.FactKey,
			envelope.SourceRef.SourceURI,
			envelope.SourceRef.SourceRecordID,
			envelope.ObservedAt,
			false,
			payload,
		})
	}
	return rows
}
