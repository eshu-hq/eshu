// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestFactStoreUpsertFactsPersistsDocumentationDocument(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewFactStore(db)

	payload := facts.DocumentationDocumentPayload{
		SourceID:     "doc-source:confluence:platform",
		DocumentID:   "doc:confluence:12345",
		ExternalID:   "12345",
		RevisionID:   "17",
		CanonicalURI: "https://example.atlassian.net/wiki/spaces/PLAT/pages/12345",
		Title:        "Payment Service Deployment",
		DocumentType: "runbook",
		Format:       "storage",
		Labels:       []string{"payments", "deployment"},
		OwnerRefs: []facts.DocumentationOwnerRef{
			{Kind: "group", ID: "team:payments", DisplayName: "Payments"},
		},
		ACLSummary: &facts.DocumentationACLSummary{
			Visibility:   "restricted",
			ReaderGroups: []string{"platform"},
		},
		ContentHash:       "sha256:document-content",
		DocumentUpdatedAt: "2026-05-09T12:00:00Z",
	}
	envelope := facts.Envelope{
		FactID:           facts.DocumentationDocumentStableID(payload),
		ScopeID:          "documentation-source-confluence-platform",
		GenerationID:     "confluence-generation-17",
		FactKind:         facts.DocumentationDocumentFactKind,
		StableFactKey:    facts.DocumentationDocumentStableID(payload),
		SchemaVersion:    facts.DocumentationFactSchemaVersion,
		CollectorKind:    string(scope.CollectorDocumentation),
		FencingToken:     7,
		SourceConfidence: facts.SourceConfidenceObserved,
		ObservedAt:       time.Date(2026, time.May, 9, 12, 0, 0, 0, time.UTC),
		Payload: map[string]any{
			"source_id":     payload.SourceID,
			"document_id":   payload.DocumentID,
			"external_id":   payload.ExternalID,
			"revision_id":   payload.RevisionID,
			"canonical_uri": payload.CanonicalURI,
			"title":         payload.Title,
			"document_type": payload.DocumentType,
			"format":        payload.Format,
			"labels":        payload.Labels,
			"owner_refs": []any{
				map[string]any{"kind": "group", "id": "team:payments", "display_name": "Payments"},
			},
			"acl_summary": map[string]any{
				"visibility":    "restricted",
				"reader_groups": []string{"platform"},
			},
			"content_hash":        payload.ContentHash,
			"document_updated_at": payload.DocumentUpdatedAt,
		},
		SourceRef: facts.Ref{
			SourceSystem:   string(scope.CollectorDocumentation),
			ScopeID:        "documentation-source-confluence-platform",
			GenerationID:   "confluence-generation-17",
			FactKey:        "document:12345:17",
			SourceURI:      payload.CanonicalURI,
			SourceRecordID: payload.ExternalID,
		},
	}

	if err := store.UpsertFacts(context.Background(), []facts.Envelope{envelope}); err != nil {
		t.Fatalf("UpsertFacts() error = %v, want nil", err)
	}

	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if got, want := len(db.execs[0].args), columnsPerFactRow; got != want {
		t.Fatalf("arg count = %d, want %d", got, want)
	}
	if got, want := db.execs[0].args[3], facts.DocumentationDocumentFactKind; got != want {
		t.Fatalf("fact_kind arg = %q, want %q", got, want)
	}
	if got, want := db.execs[0].args[5], facts.DocumentationFactSchemaVersion; got != want {
		t.Fatalf("schema_version arg = %q, want %q", got, want)
	}
	if got, want := db.execs[0].args[6], string(scope.CollectorDocumentation); got != want {
		t.Fatalf("collector_kind arg = %q, want %q", got, want)
	}
	payloadJSON, ok := db.execs[0].args[16].([]byte)
	if !ok || !strings.Contains(string(payloadJSON), "Payment Service Deployment") {
		t.Fatalf("payload arg = %#v, want documentation document json payload", db.execs[0].args[16])
	}
}

func TestFactStoreUpsertFactsPersistsDiagramDocumentationFacts(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewFactStore(db)

	section := facts.DocumentationSectionPayload{
		DocumentID:       "doc:git:repository:r_diagram:docs/architecture.mmd",
		RevisionID:       "abc123",
		SectionID:        "section:diagram",
		SectionAnchor:    "diagram",
		HeadingText:      "architecture",
		OrdinalPath:      []int{1},
		Content:          "Repository Docs\nDocumentation API",
		ContentFormat:    "mermaid",
		TextHash:         "sha256:diagram-text",
		ExcerptHash:      "sha256:diagram-excerpt",
		SourceStartRef:   "diagram:text",
		SourceEndRef:     "diagram:text",
		SourceMetadata:   map[string]string{"path": "docs/architecture.mmd", "format_family": "diagram"},
		ContainsWarnings: false,
	}
	link := facts.DocumentationLinkPayload{
		DocumentID:     section.DocumentID,
		RevisionID:     section.RevisionID,
		SectionID:      section.SectionID,
		LinkID:         "link:section:diagram:1",
		TargetURI:      "docs/runbook.md",
		TargetKind:     "source_path",
		AnchorTextHash: "sha256:runbook",
		SourceMetadata: map[string]string{"path": "docs/architecture.mmd", "format_family": "diagram"},
	}
	envelopes := []facts.Envelope{
		{
			FactID:           facts.DocumentationSectionStableID(section),
			ScopeID:          "scope-diagram",
			GenerationID:     "gen-diagram",
			FactKind:         facts.DocumentationSectionFactKind,
			StableFactKey:    facts.DocumentationSectionStableID(section),
			SchemaVersion:    facts.DocumentationSectionFactSchemaVersion,
			CollectorKind:    string(scope.CollectorDocumentation),
			SourceConfidence: facts.SourceConfidenceObserved,
			ObservedAt:       time.Date(2026, time.June, 9, 5, 50, 0, 0, time.UTC),
			Payload: map[string]any{
				"document_id":       section.DocumentID,
				"revision_id":       section.RevisionID,
				"section_id":        section.SectionID,
				"section_anchor":    section.SectionAnchor,
				"heading_text":      section.HeadingText,
				"ordinal_path":      []int{1},
				"content":           section.Content,
				"content_format":    section.ContentFormat,
				"text_hash":         section.TextHash,
				"excerpt_hash":      section.ExcerptHash,
				"source_start_ref":  section.SourceStartRef,
				"source_end_ref":    section.SourceEndRef,
				"source_metadata":   section.SourceMetadata,
				"contains_warnings": section.ContainsWarnings,
			},
			SourceRef: facts.Ref{
				SourceSystem:   string(scope.CollectorDocumentation),
				ScopeID:        "scope-diagram",
				GenerationID:   "gen-diagram",
				FactKey:        "section:diagram",
				SourceURI:      "docs/architecture.mmd",
				SourceRecordID: section.SectionID,
			},
		},
		{
			FactID:           facts.DocumentationLinkStableID(link),
			ScopeID:          "scope-diagram",
			GenerationID:     "gen-diagram",
			FactKind:         facts.DocumentationLinkFactKind,
			StableFactKey:    facts.DocumentationLinkStableID(link),
			SchemaVersion:    facts.DocumentationFactSchemaVersion,
			CollectorKind:    string(scope.CollectorDocumentation),
			SourceConfidence: facts.SourceConfidenceObserved,
			ObservedAt:       time.Date(2026, time.June, 9, 5, 50, 0, 0, time.UTC),
			Payload: map[string]any{
				"document_id":      link.DocumentID,
				"revision_id":      link.RevisionID,
				"section_id":       link.SectionID,
				"link_id":          link.LinkID,
				"target_uri":       link.TargetURI,
				"target_kind":      link.TargetKind,
				"anchor_text_hash": link.AnchorTextHash,
				"source_metadata":  link.SourceMetadata,
			},
			SourceRef: facts.Ref{
				SourceSystem:   string(scope.CollectorDocumentation),
				ScopeID:        "scope-diagram",
				GenerationID:   "gen-diagram",
				FactKey:        "link:diagram",
				SourceURI:      "docs/architecture.mmd",
				SourceRecordID: link.LinkID,
			},
		},
	}

	if err := store.UpsertFacts(context.Background(), envelopes); err != nil {
		t.Fatalf("UpsertFacts() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if got, want := len(db.execs[0].args), columnsPerFactRow*len(envelopes); got != want {
		t.Fatalf("arg count = %d, want %d", got, want)
	}
	payload := string(db.execs[0].args[16].([]byte)) + "\n" + string(db.execs[0].args[columnsPerFactRow+16].([]byte))
	for _, want := range []string{"Documentation API", "docs/runbook.md", "format_family", "diagram"} {
		if !strings.Contains(payload, want) {
			t.Fatalf("payload JSON missing %q: %s", want, payload)
		}
	}
}
