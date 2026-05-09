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
