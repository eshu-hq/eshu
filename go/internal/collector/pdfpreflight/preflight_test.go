// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pdfpreflight

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestPreflightAcceptsNormalPDFMetadata(t *testing.T) {
	t.Parallel()

	body := minimalPDF("%PDF-1.7\n1 0 obj << /Type /Catalog /Pages 2 0 R >> endobj\n%%EOF\n")
	result, err := Preflight(context.Background(), "runbook.pdf", bytes.NewReader(body), int64(len(body)), Options{})
	if err != nil {
		t.Fatalf("Preflight() error = %v, want nil", err)
	}
	if got, want := result.Format, FormatPDF; got != want {
		t.Fatalf("Format = %q, want %q", got, want)
	}
	if !result.Safe {
		t.Fatalf("Safe = false, want true; warnings=%#v", result.Warnings)
	}
	if result.ObjectMarkerCount == 0 {
		t.Fatal("ObjectMarkerCount = 0, want object markers counted")
	}
}

func TestPreflightClassifiesUnsupportedMalformedAndLimits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sourceName string
		body       []byte
		options    Options
		wantClass  WarningClass
	}{
		{name: "unsupported", sourceName: "runbook.doc", body: []byte("ignored"), wantClass: WarningUnsupportedFormat},
		{name: "missing_header", sourceName: "runbook.pdf", body: []byte("not pdf\n%%EOF\n"), wantClass: WarningMalformedPDF},
		{name: "missing_eof", sourceName: "runbook.pdf", body: []byte("%PDF-1.7\n1 0 obj\n"), wantClass: WarningPartialExtraction},
		{name: "oversized_source", sourceName: "runbook.pdf", body: []byte("%PDF-1.7\n%%EOF\n"), options: Options{MaxSourceBytes: 4}, wantClass: WarningResourceLimitExceeded},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := Preflight(context.Background(), tt.sourceName, bytes.NewReader(tt.body), int64(len(tt.body)), tt.options)
			if err != nil {
				t.Fatalf("Preflight() error = %v, want nil", err)
			}
			assertWarning(t, result, tt.wantClass)
		})
	}
}

func TestPreflightClassifiesUnsafePDFMarkers(t *testing.T) {
	t.Parallel()

	body := minimalPDF(`%PDF-1.7
1 0 obj << /Encrypt 2 0 R /OpenAction 3 0 R /AA 4 0 R /JS 5 0 R /JavaScript 6 0 R /EmbeddedFile 7 0 R /URI (https://example.invalid/reference) /Annots [8 0 R] /Title (redacted) /Author (redacted) >> endobj
%%EOF
`)
	result, err := Preflight(context.Background(), "runbook.pdf", bytes.NewReader(body), int64(len(body)), Options{})
	if err != nil {
		t.Fatalf("Preflight() error = %v, want nil", err)
	}
	assertWarning(t, result, WarningUnsupportedEncrypted)
	assertWarning(t, result, WarningUnsupportedActiveContent)
	assertWarning(t, result, WarningExternalReferenceSkipped)
	assertWarning(t, result, WarningAnnotationTextSkipped)
	assertWarning(t, result, WarningMetadataRedacted)
	if result.ExternalReferenceCount == 0 || result.ActiveContentCount == 0 ||
		result.EmbeddedFileCount == 0 || result.MetadataRedactionCount == 0 {
		t.Fatalf("expected bounded counts, got %#v", result)
	}
}

func TestPreflightClassifiesScannedLikePDF(t *testing.T) {
	t.Parallel()

	body := minimalPDF(`%PDF-1.7
1 0 obj << /Type /Catalog /Pages 2 0 R /XObject 3 0 R /Subtype /Image >> endobj
%%EOF
`)
	result, err := Preflight(context.Background(), "scan.pdf", bytes.NewReader(body), int64(len(body)), Options{})
	if err != nil {
		t.Fatalf("Preflight() error = %v, want nil", err)
	}
	assertWarning(t, result, WarningUnsupportedScannedPDF)
}

func TestPreflightClassifiesCanceledContextAsTimeout(t *testing.T) {
	t.Parallel()

	body := minimalPDF("%PDF-1.7\n%%EOF\n")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := Preflight(ctx, "runbook.pdf", bytes.NewReader(body), int64(len(body)), Options{})
	if err == nil {
		t.Fatal("Preflight() error = nil, want canceled context error")
	}
	assertWarning(t, result, WarningTimeout)
}

func TestPreflightResultJSONOmitsSourceAndPDFContent(t *testing.T) {
	t.Parallel()

	body := minimalPDF(`%PDF-1.7
1 0 obj << /Title (member-name-must-not-leak) /URI (https://example.invalid/link) >> endobj
%%EOF
`)
	result, err := Preflight(context.Background(), "private-source-name.pdf", bytes.NewReader(body), int64(len(body)), Options{})
	if err != nil {
		t.Fatalf("Preflight() error = %v, want nil", err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal() error = %v, want nil", err)
	}
	jsonText := string(encoded)
	for _, disallowed := range []string{"member-name-must-not-leak", "private-source-name", "example.invalid"} {
		if strings.Contains(jsonText, disallowed) {
			t.Fatalf("result JSON leaked %q: %s", disallowed, jsonText)
		}
	}
}

func minimalPDF(body string) []byte {
	return []byte(body)
}

func assertWarning(t *testing.T, result Result, class WarningClass) {
	t.Helper()

	for _, warning := range result.Warnings {
		if warning.Class == class && warning.Count > 0 {
			return
		}
	}
	t.Fatalf("missing warning %q in %#v", class, result.Warnings)
}
