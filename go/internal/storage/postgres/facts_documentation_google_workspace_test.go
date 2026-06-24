// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

const (
	googleWorkspaceSourceSystem   = "google_workspace"
	googleWorkspaceScopeID        = "doc-source:google_workspace:synthetic"
	googleWorkspaceGenerationID   = "gws-gen-readback"
	googleWorkspaceDocumentID     = "doc:google_workspace:sha256:synthetic"
	googleWorkspaceRevisionID     = "rev-readback"
	googleWorkspaceExportMIMEDOCX = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
)

func TestFactStoreRoundTripsGoogleWorkspaceDocumentationFacts(t *testing.T) {
	t.Parallel()

	envelopes := collectGoogleWorkspaceReadbackFacts(t)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{rows: googleWorkspaceFactRows(t, envelopes)}},
	}
	store := NewFactStore(db)

	if err := store.UpsertFacts(context.Background(), envelopes); err != nil {
		t.Fatalf("UpsertFacts() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if got, want := len(db.execs[0].args), len(envelopes)*columnsPerFactRow; got != want {
		t.Fatalf("upsert arg count = %d, want %d", got, want)
	}
	if encoded := fmt.Sprint(db.execs[0].args); strings.Contains(encoded, "gws-doc-readback") ||
		strings.Contains(encoded, "synthetic-secret") {
		t.Fatalf("upsert args leaked provider identifiers: %s", encoded)
	}

	loaded, err := store.ListFactsByKind(
		context.Background(),
		googleWorkspaceScopeID,
		googleWorkspaceGenerationID,
		[]string{
			facts.DocumentationSourceFactKind,
			facts.DocumentationDocumentFactKind,
			facts.DocumentationSectionFactKind,
			facts.DocumentationLinkFactKind,
		},
	)
	if err != nil {
		t.Fatalf("ListFactsByKind() error = %v, want nil", err)
	}
	assertGoogleWorkspaceFactKinds(t, loaded, map[string]int{
		facts.DocumentationSourceFactKind:   1,
		facts.DocumentationDocumentFactKind: 1,
		facts.DocumentationSectionFactKind:  1,
		facts.DocumentationLinkFactKind:     1,
	})
	section := googleWorkspaceLoadedPayload(t, loaded, facts.DocumentationSectionFactKind)
	if got, want := section["content"], "Synthetic workspace runbook"; got != want {
		t.Fatalf("section content = %#v, want %#v", got, want)
	}
	document := googleWorkspaceLoadedPayload(t, loaded, facts.DocumentationDocumentFactKind)
	metadata := document["source_metadata"].(map[string]any)
	if got, want := metadata["export_mime"], googleWorkspaceExportMIMEDOCX; got != want {
		t.Fatalf("document export_mime = %#v, want %#v", got, want)
	}
}

func collectGoogleWorkspaceReadbackFacts(t *testing.T) []facts.Envelope {
	t.Helper()

	observedAt := time.Date(2026, time.June, 9, 14, 0, 0, 0, time.UTC)
	sourcePayload := facts.DocumentationSourcePayload{
		SourceID:     googleWorkspaceScopeID,
		SourceSystem: googleWorkspaceSourceSystem,
		ExternalID:   "gws-allowlist:sha256:synthetic",
		DisplayName:  "Google Workspace source",
		SourceType:   "file",
		ACLSummary: &facts.DocumentationACLSummary{
			Visibility:    "unknown",
			IsPartial:     true,
			PartialReason: "runtime_not_enabled",
		},
		SourceMetadata: map[string]string{
			"allowlist_kind": "file",
			"file_count":     "1",
			"failure_count":  "0",
			"sync_status":    "completed",
			"runtime_status": "removed_facade",
		},
	}
	documentPayload := facts.DocumentationDocumentPayload{
		SourceID:     googleWorkspaceScopeID,
		DocumentID:   googleWorkspaceDocumentID,
		ExternalID:   "gws-file:sha256:synthetic",
		RevisionID:   googleWorkspaceRevisionID,
		CanonicalURI: "gws://file/sha256:synthetic",
		Title:        "Google Workspace document",
		DocumentType: "workspace_document",
		Format:       "google_workspace_export",
		ACLSummary: &facts.DocumentationACLSummary{
			Visibility:   "restricted",
			ReaderGroups: []string{"group:sha256:synthetic-readers"},
		},
		SourceMetadata: map[string]string{
			"file_kind":    "document",
			"file_id_hash": "sha256:synthetic",
			"export_mime":  googleWorkspaceExportMIMEDOCX,
		},
		ContentHash: "sha256:synthetic-content",
	}
	sectionPayload := facts.DocumentationSectionPayload{
		DocumentID:     googleWorkspaceDocumentID,
		RevisionID:     googleWorkspaceRevisionID,
		SectionID:      "export:body",
		SectionAnchor:  "export-body",
		HeadingText:    "Runbook",
		OrdinalPath:    []int{1},
		Content:        "Synthetic workspace runbook",
		ContentFormat:  "text/plain",
		TextHash:       "sha256:synthetic-section",
		ExcerptHash:    "sha256:synthetic-section",
		SourceStartRef: "export:body",
		SourceEndRef:   "export:body",
		SourceMetadata: map[string]string{
			"file_id_hash": "sha256:synthetic",
			"export_mime":  googleWorkspaceExportMIMEDOCX,
		},
	}
	linkPayload := facts.DocumentationLinkPayload{
		DocumentID:     googleWorkspaceDocumentID,
		RevisionID:     googleWorkspaceRevisionID,
		SectionID:      "export:body",
		LinkID:         "link:service-link",
		TargetURI:      "service://synthetic-workload",
		TargetKind:     "external",
		AnchorTextHash: "sha256:synthetic-anchor",
		SourceMetadata: map[string]string{"redacted": "true"},
	}

	return []facts.Envelope{
		googleWorkspaceEnvelope(
			t,
			facts.DocumentationSourceFactKind,
			facts.DocumentationSourceStableID(sourcePayload),
			sourcePayload,
			"",
			sourcePayload.ExternalID,
			observedAt,
		),
		googleWorkspaceEnvelope(
			t,
			facts.DocumentationDocumentFactKind,
			facts.DocumentationDocumentStableID(documentPayload),
			documentPayload,
			documentPayload.CanonicalURI,
			documentPayload.ExternalID,
			observedAt,
		),
		googleWorkspaceEnvelope(
			t,
			facts.DocumentationSectionFactKind,
			facts.DocumentationSectionStableID(sectionPayload),
			sectionPayload,
			documentPayload.CanonicalURI,
			documentPayload.ExternalID,
			observedAt,
		),
		googleWorkspaceEnvelope(
			t,
			facts.DocumentationLinkFactKind,
			facts.DocumentationLinkStableID(linkPayload),
			linkPayload,
			documentPayload.CanonicalURI,
			documentPayload.ExternalID,
			observedAt,
		),
	}
}

func googleWorkspaceEnvelope(
	t *testing.T,
	kind string,
	key string,
	payload any,
	sourceURI string,
	sourceRecordID string,
	observedAt time.Time,
) facts.Envelope {
	t.Helper()

	return facts.Envelope{
		FactID: facts.StableID("DocumentationReadbackFact", map[string]any{
			"fact_kind":     kind,
			"stable_key":    key,
			"scope_id":      googleWorkspaceScopeID,
			"generation_id": googleWorkspaceGenerationID,
		}),
		ScopeID:          googleWorkspaceScopeID,
		GenerationID:     googleWorkspaceGenerationID,
		FactKind:         kind,
		StableFactKey:    key,
		SchemaVersion:    googleWorkspaceSchemaVersion(kind),
		CollectorKind:    string(scope.CollectorDocumentation),
		SourceConfidence: facts.SourceConfidenceObserved,
		ObservedAt:       observedAt,
		Payload:          googleWorkspacePayloadMap(t, payload),
		SourceRef: facts.Ref{
			SourceSystem:   googleWorkspaceSourceSystem,
			ScopeID:        googleWorkspaceScopeID,
			GenerationID:   googleWorkspaceGenerationID,
			FactKey:        key,
			SourceURI:      sourceURI,
			SourceRecordID: sourceRecordID,
		},
	}
}

func googleWorkspacePayloadMap(t *testing.T, payload any) map[string]any {
	t.Helper()

	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal Google Workspace fixture payload: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(encoded, &out); err != nil {
		t.Fatalf("unmarshal Google Workspace fixture payload: %v", err)
	}
	return out
}

func googleWorkspaceSchemaVersion(kind string) string {
	if kind == facts.DocumentationSectionFactKind {
		return facts.DocumentationSectionFactSchemaVersion
	}
	return facts.DocumentationFactSchemaVersion
}

func googleWorkspaceFactRows(t *testing.T, envelopes []facts.Envelope) [][]any {
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

func assertGoogleWorkspaceFactKinds(t *testing.T, envelopes []facts.Envelope, want map[string]int) {
	t.Helper()

	got := map[string]int{}
	for _, envelope := range envelopes {
		got[envelope.FactKind]++
	}
	for kind, count := range want {
		if got[kind] != count {
			t.Fatalf("fact kind %q count = %d, want %d", kind, got[kind], count)
		}
	}
	for _, forbidden := range []string{
		facts.DocumentationEntityMentionFactKind,
		facts.DocumentationClaimCandidateFactKind,
	} {
		if got[forbidden] != 0 {
			t.Fatalf("fact kind %q count = %d, want 0", forbidden, got[forbidden])
		}
	}
}

func googleWorkspaceLoadedPayload(t *testing.T, envelopes []facts.Envelope, kind string) map[string]any {
	t.Helper()

	for _, envelope := range envelopes {
		if envelope.FactKind == kind {
			return envelope.Payload
		}
	}
	t.Fatalf("missing loaded fact kind %q", kind)
	return nil
}
