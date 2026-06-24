// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ocrdoc

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/imagepreflight"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestExtractRedactsPersonalAndPrivateOCRText(t *testing.T) {
	t.Parallel()

	body := encodePNG(t, 3, 1)
	engine := &fakeEngine{result: EngineResult{Regions: []Region{{
		RegionID:   "personal",
		Text:       "operator@example.invalid https://internal.example.invalid/ticket/CASE-123 visible label /Users/example/private.png",
		Bounds:     Bounds{X: 0, Y: 0, Width: 1, Height: 1},
		Confidence: 0.91,
	}}}}

	result, err := Extract(context.Background(), testRequest("docs/personal.png", body, engine))
	if err != nil {
		t.Fatalf("Extract() error = %v, want nil", err)
	}
	section := payloadByKind(t, result.Envelopes, facts.DocumentationSectionFactKind)
	content := section["content"].(string)
	for _, disallowed := range []string{"operator@example.invalid", "internal.example.invalid", "CASE-123", "/Users/example/private.png"} {
		if strings.Contains(content, disallowed) {
			t.Fatalf("section.content leaked %q: %q", disallowed, content)
		}
	}
	metadata := stringMapValue(t, section, "source_metadata")
	if got, want := metadata["redaction_class"], string(imagepreflight.WarningSensitiveValueRedacted); got != want {
		t.Fatalf("redaction_class = %q, want %q", got, want)
	}
	if !strings.Contains(metadata["warning"], string(imagepreflight.WarningSensitiveValueRedacted)) {
		t.Fatalf("warning = %q, want sensitive redaction", metadata["warning"])
	}
}

func TestExtractRedactsUnsafeSourceLocations(t *testing.T) {
	t.Parallel()

	body := encodePNG(t, 1, 1)
	engine := &fakeEngine{}
	req := testRequest("file:///Users/example/private.png", body, engine)
	req.SourceURI = "file:///Users/example/private.png"
	req.CanonicalURI = "git://private.example.invalid/Users/example/private.png"
	req.DocumentID = "doc:git:/Users/example/private.png"
	req.ExternalID = "media:/Users/example/private.png"
	result, err := Extract(context.Background(), req)
	if err != nil {
		t.Fatalf("Extract() error = %v, want nil", err)
	}
	document := payloadByKind(t, result.Envelopes, facts.DocumentationDocumentFactKind)
	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v, want nil", err)
	}
	documentJSON := string(encoded)
	for _, disallowed := range []string{"private.example.invalid", "/Users/example/private.png", "file:///Users"} {
		if strings.Contains(documentJSON, disallowed) {
			t.Fatalf("document payload leaked %q: %s", disallowed, documentJSON)
		}
	}
	metadata := stringMapValue(t, document, "source_metadata")
	for _, key := range []string{"canonical_uri_redacted", "document_id_redacted", "external_id_redacted"} {
		if got, want := metadata[key], "true"; got != want {
			t.Fatalf("source_metadata[%q] = %q, want %q", key, got, want)
		}
	}
	sourceURI := result.Envelopes[0].SourceRef.SourceURI
	if strings.Contains(sourceURI, "file:///Users") || strings.Contains(sourceURI, "private.png") {
		t.Fatalf("SourceRef.SourceURI leaked unsafe source location: %q", sourceURI)
	}
	if !strings.HasPrefix(sourceURI, "redacted:sha256:") {
		t.Fatalf("SourceRef.SourceURI = %q, want redacted fingerprint", sourceURI)
	}
	if strings.Contains(engine.lastImage.SourceName, "file:///Users") || strings.Contains(engine.lastImage.SourceName, "private.png") {
		t.Fatalf("engine SourceName leaked unsafe source location: %q", engine.lastImage.SourceName)
	}

	req = testRequest("docs/private.png", body, &fakeEngine{})
	req.SourceURI = "docs/private.png"
	req.SourceRecordID = "/Users/example/private.png"
	req.ExternalID = "/Users/example/private.png"
	result, err = Extract(context.Background(), req)
	if err != nil {
		t.Fatalf("Extract() with unsafe source record ID error = %v, want nil", err)
	}
	sourceRef := result.Envelopes[0].SourceRef
	if got, want := sourceRef.SourceURI, "docs/private.png"; got != want {
		t.Fatalf("SourceRef.SourceURI = %q, want %q", got, want)
	}
	if strings.Contains(sourceRef.SourceRecordID, "/Users/example/private.png") {
		t.Fatalf("SourceRef.SourceRecordID leaked unsafe source record ID: %q", sourceRef.SourceRecordID)
	}
	if !strings.HasPrefix(sourceRef.SourceRecordID, "ocr-record:sha256:") {
		t.Fatalf("SourceRef.SourceRecordID = %q, want redacted OCR record fingerprint", sourceRef.SourceRecordID)
	}
}

func TestExtractGeneratesSourceNeutralIdentityFallbacks(t *testing.T) {
	t.Parallel()

	body := encodePNG(t, 1, 1)
	req := testRequest("docs/blank-identity.png", body, &fakeEngine{result: EngineResult{
		Regions: []Region{{
			RegionID:   "body",
			Text:       "Visible OCR text",
			Bounds:     Bounds{X: 0, Y: 0, Width: 1, Height: 1},
			Confidence: 0.88,
		}},
	}})
	req.DocumentID = ""
	req.ExternalID = ""
	req.RevisionID = ""

	result, err := Extract(context.Background(), req)
	if err != nil {
		t.Fatalf("Extract() error = %v, want nil", err)
	}
	document := payloadByKind(t, result.Envelopes, facts.DocumentationDocumentFactKind)
	for key, wantPrefix := range map[string]string{
		"document_id": "doc:ocr:sha256:",
		"external_id": "ocr-source:sha256:",
		"revision_id": "sha256:",
	} {
		got, ok := document[key].(string)
		if !ok {
			t.Fatalf("document[%q] type = %T, want string", key, document[key])
		}
		if !strings.HasPrefix(got, wantPrefix) {
			t.Fatalf("document[%q] = %q, want prefix %q", key, got, wantPrefix)
		}
	}
	metadata := stringMapValue(t, document, "source_metadata")
	for _, key := range []string{"document_id_generated", "external_id_generated", "revision_id_generated"} {
		if got, want := metadata[key], "true"; got != want {
			t.Fatalf("source_metadata[%q] = %q, want %q", key, got, want)
		}
	}
	section := payloadByKind(t, result.Envelopes, facts.DocumentationSectionFactKind)
	if got, want := section["document_id"], document["document_id"]; got != want {
		t.Fatalf("section.document_id = %#v, want %#v", got, want)
	}
	if got, want := section["revision_id"], document["revision_id"]; got != want {
		t.Fatalf("section.revision_id = %#v, want %#v", got, want)
	}
}
