// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ooxmlpreflight

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestPreflightAcceptsNormalOOXMLPackageMetadata(t *testing.T) {
	t.Parallel()

	archive := buildOOXMLZip(t, map[string]string{
		"[Content_Types].xml": contentTypesXML("word/document.xml", "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"),
		"_rels/.rels":         relationshipsXML(false, "word/document.xml"),
		"word/document.xml":   "<w:document/>",
	})

	result, err := Preflight(context.Background(), "runbook.docx", bytes.NewReader(archive), int64(len(archive)), Options{
		MaxEntries:          10,
		MaxExpandedBytes:    4096,
		MaxCompressionRatio: 100,
		MaxXMLBytes:         2048,
	})
	if err != nil {
		t.Fatalf("Preflight() error = %v, want nil", err)
	}
	if !result.Safe {
		t.Fatalf("Safe = false, want true; warnings=%#v", result.Warnings)
	}
	if got, want := result.Format, FormatDOCX; got != want {
		t.Fatalf("Format = %q, want %q", got, want)
	}
	if got, want := result.PartCount, 3; got != want {
		t.Fatalf("PartCount = %d, want %d", got, want)
	}
	if got, want := result.RelationshipPartCount, 1; got != want {
		t.Fatalf("RelationshipPartCount = %d, want %d", got, want)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("Warnings = %#v, want none", result.Warnings)
	}
}

func TestPreflightRecognizesSupportedOOXMLFormats(t *testing.T) {
	t.Parallel()

	archive := buildOOXMLZip(t, map[string]string{
		"[Content_Types].xml": contentTypesXML("doc/part.xml", "application/vnd.openxmlformats-officedocument"),
	})
	tests := []struct {
		name       string
		sourceName string
		wantFormat string
	}{
		{name: "docx", sourceName: "runbook.docx", wantFormat: FormatDOCX},
		{name: "xlsx", sourceName: "inventory.xlsx", wantFormat: FormatXLSX},
		{name: "pptx", sourceName: "review.pptx", wantFormat: FormatPPTX},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := Preflight(context.Background(), tt.sourceName, bytes.NewReader(archive), int64(len(archive)), Options{})
			if err != nil {
				t.Fatalf("Preflight() error = %v, want nil", err)
			}
			if got := result.Format; got != tt.wantFormat {
				t.Fatalf("Format = %q, want %q", got, tt.wantFormat)
			}
			if !result.Safe {
				t.Fatalf("Safe = false, want true; warnings=%#v", result.Warnings)
			}
		})
	}
}

func TestPreflightRejectsMacroEnabledOfficeExtensions(t *testing.T) {
	t.Parallel()

	archive := buildOOXMLZip(t, map[string]string{
		"[Content_Types].xml": contentTypesXML("word/document.xml", "application/vnd.ms-word.document.macroEnabled.main+xml"),
	})

	result, err := Preflight(context.Background(), "runbook.docm", bytes.NewReader(archive), int64(len(archive)), Options{})
	if err != nil {
		t.Fatalf("Preflight() error = %v, want nil", err)
	}
	if result.Safe {
		t.Fatal("Safe = true, want false for macro-enabled extension")
	}
	assertWarning(t, result, WarningUnsupportedMacroEnabled, 1)
}

func TestPreflightFlagsUnsafePathsExternalRelationshipsAndActiveParts(t *testing.T) {
	t.Parallel()

	archive := buildOOXMLZip(t, map[string]string{
		"[Content_Types].xml":       contentTypesXML("word/document.xml", "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"),
		"_rels/.rels":               relationshipsXML(true, "https://example.invalid/external-target"),
		"../escape.xml":             "<xml/>",
		"word/activeX/activeX1.xml": "<ax/>",
		"word/embeddings/ole.bin":   "embedded",
		"word/vbaProject.bin":       "macro",
	})

	result, err := Preflight(context.Background(), "runbook.docx", bytes.NewReader(archive), int64(len(archive)), Options{
		MaxEntries:          20,
		MaxExpandedBytes:    4096,
		MaxCompressionRatio: 100,
		MaxXMLBytes:         2048,
	})
	if err != nil {
		t.Fatalf("Preflight() error = %v, want nil", err)
	}
	if result.Safe {
		t.Fatal("Safe = true, want false when unsafe package parts are present")
	}
	assertWarning(t, result, WarningArchivePathEscape, 1)
	assertWarning(t, result, WarningExternalRelationship, 1)
	assertWarning(t, result, WarningActiveContent, 2)
	assertWarning(t, result, WarningEmbeddedObject, 1)
	assertWarning(t, result, WarningUnsupportedMacroEnabled, 1)
	if got, want := result.ExternalRelationshipCount, 1; got != want {
		t.Fatalf("ExternalRelationshipCount = %d, want %d", got, want)
	}
	if got, want := result.ActiveContentCount, 2; got != want {
		t.Fatalf("ActiveContentCount = %d, want %d", got, want)
	}

	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal(result) error = %v, want nil", err)
	}
	if strings.Contains(string(encoded), "example.invalid") || strings.Contains(string(encoded), "external-target") {
		t.Fatalf("result leaked relationship target: %s", encoded)
	}
}

func TestPreflightFlagsExternalRelationshipTargetWithoutMode(t *testing.T) {
	t.Parallel()

	archive := buildOOXMLZip(t, map[string]string{
		"[Content_Types].xml": contentTypesXML("word/document.xml", "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"),
		"_rels/.rels":         relationshipsXML(false, "https://private.example.invalid/target"),
	})

	result, err := Preflight(context.Background(), "runbook.docx", bytes.NewReader(archive), int64(len(archive)), Options{
		MaxEntries:       10,
		MaxExpandedBytes: 4096,
		MaxXMLBytes:      2048,
	})
	if err != nil {
		t.Fatalf("Preflight() error = %v, want nil", err)
	}
	if result.Safe {
		t.Fatal("Safe = true, want false for external relationship target")
	}
	assertWarning(t, result, WarningExternalRelationship, 1)
	if got, want := result.ExternalRelationshipCount, 1; got != want {
		t.Fatalf("ExternalRelationshipCount = %d, want %d", got, want)
	}
	assertNoResultLeak(t, result, "private.example.invalid", "target")
}

func TestPreflightClassifiesMalformedContainerAndXML(t *testing.T) {
	t.Parallel()

	result, err := Preflight(context.Background(), "broken.docx", bytes.NewReader([]byte("not a zip")), int64(len("not a zip")), Options{})
	if err != nil {
		t.Fatalf("Preflight(malformed zip) error = %v, want nil", err)
	}
	if result.Safe {
		t.Fatal("Safe = true, want false for malformed container")
	}
	assertWarning(t, result, WarningMalformedContainer, 1)

	archive := buildOOXMLZip(t, map[string]string{
		"[Content_Types].xml": "<Types>",
		"_rels/.rels":         relationshipsXML(false, "word/document.xml"),
	})
	result, err = Preflight(context.Background(), "broken.docx", bytes.NewReader(archive), int64(len(archive)), Options{
		MaxEntries:          10,
		MaxExpandedBytes:    4096,
		MaxCompressionRatio: 100,
		MaxXMLBytes:         2048,
	})
	if err != nil {
		t.Fatalf("Preflight(malformed xml) error = %v, want nil", err)
	}
	if result.Safe {
		t.Fatal("Safe = true, want false for malformed XML")
	}
	assertWarning(t, result, WarningMalformedXML, 1)
}

func TestPreflightClassifiesMalformedStructureXML(t *testing.T) {
	t.Parallel()

	archive := buildOOXMLZip(t, map[string]string{
		"[Content_Types].xml": contentTypesXML("word/document.xml", "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"),
		"word/document.xml":   `<w:document><w:body><w:tbl></w:body></w:document>`,
	})

	result, err := Preflight(context.Background(), "broken.docx", bytes.NewReader(archive), int64(len(archive)), Options{
		MaxEntries:       10,
		MaxExpandedBytes: 4096,
		MaxXMLBytes:      2048,
	})
	if err != nil {
		t.Fatalf("Preflight() error = %v, want nil", err)
	}
	if result.Safe {
		t.Fatal("Safe = true, want false for malformed structure XML")
	}
	assertWarning(t, result, WarningMalformedXML, 1)
}

func TestPreflightClassifiesResourceLimits(t *testing.T) {
	t.Parallel()

	archive := buildOOXMLZip(t, map[string]string{
		"[Content_Types].xml": contentTypesXML("word/document.xml", "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"),
		"_rels/.rels":         relationshipsXML(false, "word/document.xml"),
		"word/document.xml":   strings.Repeat("A", 2048),
	})

	result, err := Preflight(context.Background(), "runbook.docx", bytes.NewReader(archive), int64(len(archive)), Options{
		MaxEntries:          2,
		MaxExpandedBytes:    512,
		MaxCompressionRatio: 2,
		MaxXMLBytes:         2048,
	})
	if err != nil {
		t.Fatalf("Preflight() error = %v, want nil", err)
	}
	if result.Safe {
		t.Fatal("Safe = true, want false for over-budget package")
	}
	assertWarning(t, result, WarningResourceLimitExceeded, 2)
	assertWarning(t, result, WarningCompressionRatioExceeded, 1)
}

func TestPreflightSkipsOversizedStructureXML(t *testing.T) {
	t.Parallel()

	archive := buildOOXMLZip(t, map[string]string{
		"[Content_Types].xml": contentTypesXML("word/document.xml", "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"),
		"word/document.xml":   `<w:document>` + strings.Repeat("private text ", 200) + `<w:tbl/></w:document>`,
	})

	result, err := Preflight(context.Background(), "runbook.docx", bytes.NewReader(archive), int64(len(archive)), Options{
		MaxEntries:       10,
		MaxExpandedBytes: 4096,
		MaxXMLBytes:      512,
	})
	if err != nil {
		t.Fatalf("Preflight() error = %v, want nil", err)
	}
	if result.Safe {
		t.Fatal("Safe = true, want false for oversized structure XML")
	}
	assertWarning(t, result, WarningResourceLimitExceeded, 1)
	if result.TableMarkerCount != 0 {
		t.Fatalf("TableMarkerCount = %d, want 0 when structure part exceeds XML budget", result.TableMarkerCount)
	}
	assertNoResultLeak(t, result, "private text")
}

func TestPreflightClassifiesXMLDepthLimit(t *testing.T) {
	t.Parallel()

	archive := buildOOXMLZip(t, map[string]string{
		"[Content_Types].xml": `<Types><a><b><c><d></d></c></b></a></Types>`,
	})

	result, err := Preflight(context.Background(), "runbook.docx", bytes.NewReader(archive), int64(len(archive)), Options{
		MaxEntries:          10,
		MaxExpandedBytes:    4096,
		MaxCompressionRatio: 100,
		MaxXMLBytes:         2048,
		MaxXMLDepth:         3,
	})
	if err != nil {
		t.Fatalf("Preflight() error = %v, want nil", err)
	}
	if result.Safe {
		t.Fatal("Safe = true, want false for over-depth XML")
	}
	assertWarning(t, result, WarningResourceLimitExceeded, 1)
}

func TestPreflightCountsDOCXStructureMarkers(t *testing.T) {
	t.Parallel()

	archive := buildOOXMLZip(t, map[string]string{
		"[Content_Types].xml":   contentTypesXML("word/document.xml", "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"),
		"word/document.xml":     `<w:document><w:body><w:tbl/><w:ins w:author="private author"/><w:del/></w:body></w:document>`,
		"word/comments.xml":     `<w:comments><w:comment>private comment text</w:comment></w:comments>`,
		"word/media/image1.png": "image-bytes",
	})

	result, err := Preflight(context.Background(), "runbook.docx", bytes.NewReader(archive), int64(len(archive)), Options{
		MaxEntries:       10,
		MaxExpandedBytes: 4096,
		MaxXMLBytes:      2048,
	})
	if err != nil {
		t.Fatalf("Preflight() error = %v, want nil", err)
	}
	if result.TableMarkerCount != 1 || result.TrackedChangeMarkerCount != 2 ||
		result.AnnotationPartCount != 1 || result.ImagePartCount != 1 {
		t.Fatalf("unexpected DOCX structure counts: %#v", result)
	}
	assertWarning(t, result, WarningAnnotationTextSkipped, 1)
	assertNoResultLeak(t, result, "private author", "private comment text")
}

func TestPreflightCountsXLSXStructureMarkers(t *testing.T) {
	t.Parallel()

	archive := buildOOXMLZip(t, map[string]string{
		"[Content_Types].xml":      contentTypesXML("xl/workbook.xml", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"),
		"xl/workbook.xml":          `<workbook><sheets><sheet name="private-sheet" state="hidden"/></sheets></workbook>`,
		"xl/worksheets/sheet1.xml": `<worksheet><sheetData><row><c><f>PRIVATE_FORMULA()</f></c></row></sheetData></worksheet>`,
		"xl/sharedStrings.xml":     `<sst><si><t>private cell text</t></si></sst>`,
		"xl/comments1.xml":         `<comments><comment authorId="private-author">private comment text</comment></comments>`,
	})

	result, err := Preflight(context.Background(), "inventory.xlsx", bytes.NewReader(archive), int64(len(archive)), Options{
		MaxEntries:       10,
		MaxExpandedBytes: 4096,
		MaxXMLBytes:      2048,
	})
	if err != nil {
		t.Fatalf("Preflight() error = %v, want nil", err)
	}
	if result.WorksheetPartCount != 1 || result.SharedStringPartCount != 1 ||
		result.FormulaMarkerCount != 1 || result.HiddenContentCount != 1 ||
		result.AnnotationPartCount != 1 {
		t.Fatalf("unexpected XLSX structure counts: %#v", result)
	}
	assertWarning(t, result, WarningHiddenContentSkipped, 1)
	assertWarning(t, result, WarningAnnotationTextSkipped, 1)
	assertNoResultLeak(t, result, "private-sheet", "PRIVATE_FORMULA", "private cell text", "private-author", "private comment text")
}

func TestPreflightCountsPPTXStructureMarkers(t *testing.T) {
	t.Parallel()

	archive := buildOOXMLZip(t, map[string]string{
		"[Content_Types].xml":             contentTypesXML("ppt/presentation.xml", "application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml"),
		"ppt/presentation.xml":            `<p:presentation><p:sldIdLst><p:sldId show="0"/></p:sldIdLst></p:presentation>`,
		"ppt/slides/slide1.xml":           `<p:sld><p:cSld><p:spTree/></p:cSld></p:sld>`,
		"ppt/notesSlides/notesSlide1.xml": `<p:notes>private notes text</p:notes>`,
		"ppt/comments/comment1.xml":       `<p:cm>private comment text</p:cm>`,
		"ppt/commentAuthors.xml":          `<p:cmAuthor name="private author"/>`,
		"ppt/media/image1.png":            "image-bytes",
	})

	result, err := Preflight(context.Background(), "review.pptx", bytes.NewReader(archive), int64(len(archive)), Options{
		MaxEntries:       10,
		MaxExpandedBytes: 4096,
		MaxXMLBytes:      2048,
	})
	if err != nil {
		t.Fatalf("Preflight() error = %v, want nil", err)
	}
	if result.SlidePartCount != 1 || result.NotesPartCount != 1 ||
		result.AnnotationPartCount != 2 || result.HiddenContentCount != 1 ||
		result.MediaPartCount != 1 {
		t.Fatalf("unexpected PPTX structure counts: %#v", result)
	}
	assertWarning(t, result, WarningHiddenContentSkipped, 1)
	assertWarning(t, result, WarningAnnotationTextSkipped, 1)
	assertNoResultLeak(t, result, "private notes text", "private comment text", "private author")
}

func TestPreflightClassifiesCanceledContextAsTimeout(t *testing.T) {
	t.Parallel()

	archive := buildOOXMLZip(t, map[string]string{
		"[Content_Types].xml": contentTypesXML("word/document.xml", "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"),
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := Preflight(ctx, "runbook.docx", bytes.NewReader(archive), int64(len(archive)), Options{})
	if err != nil {
		t.Fatalf("Preflight() error = %v, want nil", err)
	}
	if result.Safe {
		t.Fatal("Safe = true, want false for canceled preflight")
	}
	assertWarning(t, result, WarningTimeout, 1)
}

func buildOOXMLZip(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	for name, body := range files {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		part, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatalf("CreateHeader(%q) error = %v, want nil", name, err)
		}
		if _, err := io.WriteString(part, body); err != nil {
			t.Fatalf("WriteString(%q) error = %v, want nil", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("zip.Close() error = %v, want nil", err)
	}
	return buf.Bytes()
}

func contentTypesXML(partName, contentType string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>` +
		`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
		`<Override PartName="/` + partName + `" ContentType="` + contentType + `"/>` +
		`</Types>`
}

func relationshipsXML(external bool, target string) string {
	mode := ""
	if external {
		mode = ` TargetMode="External"`
	}
	return `<?xml version="1.0" encoding="UTF-8"?>` +
		`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
		`<Relationship Id="rId1" Type="https://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="` + target + `"` + mode + `/>` +
		`</Relationships>`
}

func assertWarning(t *testing.T, result Result, class string, wantCount int) {
	t.Helper()

	for _, warning := range result.Warnings {
		if warning.Class == class {
			if warning.Count != wantCount {
				t.Fatalf("warning %q count = %d, want %d; warnings=%#v", class, warning.Count, wantCount, result.Warnings)
			}
			return
		}
	}
	t.Fatalf("missing warning %q in %#v", class, result.Warnings)
}

func assertNoResultLeak(t *testing.T, result Result, disallowed ...string) {
	t.Helper()

	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal(result) error = %v, want nil", err)
	}
	for _, text := range disallowed {
		if strings.Contains(string(encoded), text) {
			t.Fatalf("result leaked %q: %s", text, encoded)
		}
	}
}
