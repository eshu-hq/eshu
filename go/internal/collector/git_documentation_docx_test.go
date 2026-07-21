// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"fmt"
	"html"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestStreamFactsEmitsDOCXDocumentationSections(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	body := buildTestDOCX(t, docxTestPackage{
		Blocks: []docxTestBlock{
			{Style: "Heading1", Text: "Migration Plan"},
			{Text: "Review the bounded rollout sequence before release."},
			{Style: "Heading2", Text: "Service Inventory"},
			{Table: [][]string{
				{"Service", "Owner", "Dependency"},
				{"payments-api", "platform", "postgres"},
				{"billing-worker", "billing-team", "queue"},
			}},
		},
	})
	writeCollectorTestFile(t, filepath.Join(repoPath, "docs", "migration-plan.docx"), string(body))

	envelopes := streamDocumentFacts(t, repoPath, "docs/migration-plan.docx")
	document := singleFact(t, envelopes, facts.DocumentationDocumentFactKind)
	if got, want := payloadString(document.Payload, "format"), "docx"; got != want {
		t.Fatalf("document format = %q, want %q", got, want)
	}
	if got, want := payloadString(document.Payload, "document_type"), "document"; got != want {
		t.Fatalf("document_type = %q, want %q", got, want)
	}
	if got, want := payloadString(document.Payload, "title"), "Migration Plan"; got != want {
		t.Fatalf("title = %q, want %q", got, want)
	}
	if got, want := payloadSourceMetadataValue(document.Payload, "preflight_format"), "docx"; got != want {
		t.Fatalf("preflight_format = %q, want %q", got, want)
	}
	if got, want := payloadSourceMetadataValue(document.Payload, "section_count"), "2"; got != want {
		t.Fatalf("section_count = %q, want %q", got, want)
	}
	if got, want := payloadSourceMetadataValue(document.Payload, "table_count"), "1"; got != want {
		t.Fatalf("table_count = %q, want %q", got, want)
	}
	assertDocumentationFactLinkedRepository(t, document, "repository:r_12345678")

	sections := factsByKind(envelopes, facts.DocumentationSectionFactKind)
	if got, want := len(sections), 2; got != want {
		t.Fatalf("documentation_section count = %d, want %d", got, want)
	}
	plan := sectionByHeading(sections, "Migration Plan")
	if plan == nil {
		t.Fatalf("missing Migration Plan section in %#v", sections)
	}
	if content := payloadString(plan.Payload, "content"); !strings.Contains(content, "bounded rollout sequence") {
		t.Fatalf("Migration Plan content missing paragraph: %q", content)
	}
	inventory := sectionByHeading(sections, "Service Inventory")
	if inventory == nil {
		t.Fatalf("missing Service Inventory section in %#v", sections)
	}
	content := payloadString(inventory.Payload, "content")
	for _, want := range []string{"table row 1: Service | Owner | Dependency", "payments-api | platform | postgres"} {
		if !strings.Contains(content, want) {
			t.Fatalf("Service Inventory content missing %q in %q", want, content)
		}
	}
	if got, want := payloadString(inventory.Payload, "content_format"), "docx"; got != want {
		t.Fatalf("content_format = %q, want %q", got, want)
	}
	if got, want := payloadSourceMetadataValue(inventory.Payload, "table_count"), "1"; got != want {
		t.Fatalf("table_count metadata = %q, want %q", got, want)
	}
	assertDocumentationFactLinkedRepository(t, *inventory, "repository:r_12345678")
}

func TestStreamFactsKeepsDOCXAnnotationsMetadataOnly(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	body := buildTestDOCX(t, docxTestPackage{
		Blocks: []docxTestBlock{
			{Style: "Heading1", Text: "Security Review"},
			{Text: "Approved release checklist."},
			{TrackedInsert: "tracked reviewer draft"},
		},
		Comments: []string{"review-only comment text"},
	})
	writeCollectorTestFile(t, filepath.Join(repoPath, "docs", "security-review.docx"), string(body))

	envelopes := streamDocumentFacts(t, repoPath, "docs/security-review.docx")
	document := singleFact(t, envelopes, facts.DocumentationDocumentFactKind)
	assertPayloadWarning(t, document.Payload, "annotation_text_skipped")
	if got, want := payloadSourceMetadataValue(document.Payload, "comment_count"), "1"; got != want {
		t.Fatalf("comment_count = %q, want %q", got, want)
	}
	if got, want := payloadSourceMetadataValue(document.Payload, "tracked_change_count"), "1"; got != want {
		t.Fatalf("tracked_change_count = %q, want %q", got, want)
	}
	section := singleFact(t, envelopes, facts.DocumentationSectionFactKind)
	content := payloadString(section.Payload, "content")
	for _, forbidden := range []string{"review-only comment text", "tracked reviewer draft"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("DOCX annotation text leaked %q in %q", forbidden, content)
		}
	}
}

func TestStreamFactsHandlesMalformedAndUnsafeDOCXPackages(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoPath, "docs", "broken.docx"), "not a zip")
	unsafeBody := buildTestDOCX(t, docxTestPackage{
		Blocks:                      []docxTestBlock{{Style: "Heading1", Text: "Unsafe"}},
		ExternalPackageRelationship: true,
	})
	writeCollectorTestFile(t, filepath.Join(repoPath, "docs", "unsafe.docx"), string(unsafeBody))
	embeddedBody := buildTestZip(t, map[string]string{
		"[Content_Types].xml":         docxContentTypesXML(false),
		"_rels/.rels":                 docxPackageRelationshipsXML(false),
		"word/document.xml":           docxDocumentXML([]docxTestBlock{{Style: "Heading1", Text: "Embedded"}}),
		"word/embeddings/object1.bin": "embedded object bytes",
	})
	writeCollectorTestFile(t, filepath.Join(repoPath, "docs", "embedded.docx"), string(embeddedBody))

	envelopes := streamMultipleDocumentFacts(t, repoPath, []string{"docs/broken.docx", "docs/unsafe.docx", "docs/embedded.docx"})
	documents := factsByKind(envelopes, facts.DocumentationDocumentFactKind)
	if got, want := len(documents), 3; got != want {
		t.Fatalf("documentation_document count = %d, want %d", got, want)
	}
	assertDocumentPathWarning(t, documents, "docs/broken.docx", "malformed_container")
	assertDocumentPathWarning(t, documents, "docs/unsafe.docx", "external_relationship")
	assertDocumentPathWarning(t, documents, "docs/embedded.docx", "embedded_object_present")
	if got, want := len(factsByKind(envelopes, facts.DocumentationSectionFactKind)), 0; got != want {
		t.Fatalf("documentation_section count = %d, want %d", got, want)
	}
}

type docxTestPackage struct {
	Blocks                      []docxTestBlock
	Comments                    []string
	ExternalPackageRelationship bool
}

type docxTestBlock struct {
	Style         string
	Text          string
	TrackedInsert string
	Table         [][]string
}

func buildTestDOCX(t *testing.T, pkg docxTestPackage) []byte {
	t.Helper()

	files := map[string]string{
		"[Content_Types].xml":          docxContentTypesXML(len(pkg.Comments) > 0),
		"_rels/.rels":                  docxPackageRelationshipsXML(pkg.ExternalPackageRelationship),
		"word/_rels/document.xml.rels": docxDocumentRelationshipsXML(len(pkg.Comments) > 0),
		"word/document.xml":            docxDocumentXML(pkg.Blocks),
	}
	if len(pkg.Comments) > 0 {
		files["word/comments.xml"] = docxCommentsXML(pkg.Comments)
	}
	return buildTestZip(t, files)
}

func docxContentTypesXML(includeComments bool) string {
	var body strings.Builder
	body.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	body.WriteString(`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">`)
	body.WriteString(`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>`)
	body.WriteString(`<Default Extension="xml" ContentType="application/xml"/>`)
	body.WriteString(`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>`)
	if includeComments {
		body.WriteString(`<Override PartName="/word/comments.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.comments+xml"/>`)
	}
	body.WriteString(`</Types>`)
	return body.String()
}

func docxPackageRelationshipsXML(external bool) string {
	var body strings.Builder
	body.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	body.WriteString(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	body.WriteString(`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>`)
	if external {
		body.WriteString(`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="https://example.invalid/external" TargetMode="External"/>`)
	}
	body.WriteString(`</Relationships>`)
	return body.String()
}

func docxDocumentRelationshipsXML(includeComments bool) string {
	var body strings.Builder
	body.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	body.WriteString(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	if includeComments {
		body.WriteString(`<Relationship Id="rIdComments" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/comments" Target="comments.xml"/>`)
	}
	body.WriteString(`</Relationships>`)
	return body.String()
}

func docxDocumentXML(blocks []docxTestBlock) string {
	var body strings.Builder
	body.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	body.WriteString(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">`)
	body.WriteString(`<w:body>`)
	for _, block := range blocks {
		switch {
		case len(block.Table) > 0:
			body.WriteString(`<w:tbl>`)
			for _, row := range block.Table {
				body.WriteString(`<w:tr>`)
				for _, cell := range row {
					body.WriteString(`<w:tc><w:p><w:r><w:t>`)
					body.WriteString(html.EscapeString(cell))
					body.WriteString(`</w:t></w:r></w:p></w:tc>`)
				}
				body.WriteString(`</w:tr>`)
			}
			body.WriteString(`</w:tbl>`)
		case block.TrackedInsert != "":
			body.WriteString(`<w:p><w:ins><w:r><w:t>`)
			body.WriteString(html.EscapeString(block.TrackedInsert))
			body.WriteString(`</w:t></w:r></w:ins></w:p>`)
		default:
			body.WriteString(`<w:p>`)
			if block.Style != "" {
				fmt.Fprintf(&body, `<w:pPr><w:pStyle w:val="%s"/></w:pPr>`, html.EscapeString(block.Style))
			}
			body.WriteString(`<w:r><w:t>`)
			body.WriteString(html.EscapeString(block.Text))
			body.WriteString(`</w:t></w:r></w:p>`)
		}
	}
	body.WriteString(`</w:body></w:document>`)
	return body.String()
}

func docxCommentsXML(comments []string) string {
	var body strings.Builder
	body.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	body.WriteString(`<w:comments xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">`)
	for i, comment := range comments {
		fmt.Fprintf(&body, `<w:comment w:id="%d"><w:p><w:r><w:t>%s</w:t></w:r></w:p></w:comment>`, i, html.EscapeString(comment))
	}
	body.WriteString(`</w:comments>`)
	return body.String()
}

func streamDocumentFacts(t *testing.T, repoPath string, relativePath string) []facts.Envelope {
	t.Helper()

	return streamMultipleDocumentFacts(t, repoPath, []string{relativePath})
}

func streamMultipleDocumentFacts(t *testing.T, repoPath string, relativePaths []string) []facts.Envelope {
	t.Helper()

	metas := make([]ContentFileMeta, 0, len(relativePaths))
	for _, relativePath := range relativePaths {
		metas = append(metas, ContentFileMeta{
			RelativePath: relativePath,
			Digest:       "sha256:docx",
			Language:     filepath.Ext(relativePath)[1:],
			ArtifactType: "documentation",
			CommitSHA:    "abc123",
		})
	}
	collected := buildStreamingGeneration(
		repoPath,
		testCollectorRepositoryMetadata(repoPath),
		"run-1",
		time.Date(2026, time.June, 9, 8, 45, 0, 0, time.UTC),
		RepositorySnapshot{
			FileCount:              len(relativePaths),
			DocumentationFileMetas: metas,
		},
		false,
		"",
	)
	return drainFactChannel(collected.Facts)
}
