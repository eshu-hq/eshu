// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"fmt"
	"html"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestStreamFactsEmitsPPTXSlideDocumentation(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	body := buildTestPPTX(t, pptxTestDeck{
		Slides: []pptxTestSlide{
			{
				Title: "Release Review",
				Body:  "Review the rollback checklist before release.",
				Table: [][]string{
					{"Service", "Owner"},
					{"payments-api", "platform"},
				},
			},
			{
				Title:  "Private Roadmap",
				Body:   "hidden launch plan",
				Hidden: true,
			},
			{
				Title: "Deployment Steps",
				Body:  "Deploy reducer after API.",
			},
		},
		Notes:    []string{"speaker note private text"},
		Comments: []string{"review comment private text"},
	})
	writeCollectorTestFile(t, filepath.Join(repoPath, "docs", "release-review.pptx"), string(body))

	envelopes := streamDocumentFacts(t, repoPath, "docs/release-review.pptx")
	document := singleFact(t, envelopes, facts.DocumentationDocumentFactKind)
	if got, want := payloadString(document.Payload, "format"), "pptx"; got != want {
		t.Fatalf("document format = %q, want %q", got, want)
	}
	if got, want := payloadString(document.Payload, "document_type"), "presentation"; got != want {
		t.Fatalf("document_type = %q, want %q", got, want)
	}
	if got, want := payloadString(document.Payload, "title"), "Release Review"; got != want {
		t.Fatalf("title = %q, want %q", got, want)
	}
	if got, want := payloadSourceMetadataValue(document.Payload, "preflight_format"), "pptx"; got != want {
		t.Fatalf("preflight_format = %q, want %q", got, want)
	}
	for key, want := range map[string]string{
		"slide_count":         "3",
		"visible_slide_count": "2",
		"hidden_slide_count":  "1",
		"notes_slide_count":   "1",
		"comment_count":       "1",
		"section_count":       "2",
		"table_count":         "1",
	} {
		if got := payloadSourceMetadataValue(document.Payload, key); got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
	assertPayloadWarning(t, document.Payload, "annotation_text_skipped")
	assertPayloadWarning(t, document.Payload, "hidden_content_skipped")
	assertDocumentationFactLinkedRepository(t, document, "repository:r_12345678")

	sections := factsByKind(envelopes, facts.DocumentationSectionFactKind)
	if got, want := len(sections), 2; got != want {
		t.Fatalf("documentation_section count = %d, want %d", got, want)
	}
	review := sectionByHeading(sections, "Release Review")
	if review == nil {
		t.Fatalf("missing Release Review section in %#v", sections)
	}
	if got, want := payloadString(review.Payload, "content_format"), "pptx"; got != want {
		t.Fatalf("content_format = %q, want %q", got, want)
	}
	if got, want := payloadSourceMetadataValue(review.Payload, "slide_ordinal"), "1"; got != want {
		t.Fatalf("slide_ordinal = %q, want %q", got, want)
	}
	if got, want := payloadSourceMetadataValue(review.Payload, "table_count"), "1"; got != want {
		t.Fatalf("table_count = %q, want %q", got, want)
	}
	content := payloadString(review.Payload, "content")
	for _, want := range []string{
		"slide: Release Review",
		"Review the rollback checklist",
		"table row 1: Service | Owner",
		"payments-api | platform",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("section content missing %q in %q", want, content)
		}
	}
	for _, forbidden := range []string{
		"Private Roadmap",
		"hidden launch plan",
		"speaker note private text",
		"review comment private text",
	} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("section content leaked %q in %q", forbidden, content)
		}
	}
	assertDocumentationFactLinkedRepository(t, *review, "repository:r_12345678")
}

func TestStreamFactsHandlesMalformedAndUnsafePPTXPackages(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoPath, "docs", "broken.pptx"), "not a zip")
	unsafeBody := buildTestPPTX(t, pptxTestDeck{
		Slides:                      []pptxTestSlide{{Title: "Unsafe", Body: "should not emit"}},
		ExternalPackageRelationship: true,
	})
	writeCollectorTestFile(t, filepath.Join(repoPath, "docs", "unsafe.pptx"), string(unsafeBody))
	embeddedBody := buildTestZip(t, map[string]string{
		"[Content_Types].xml":        pptxContentTypesXML(1, false, false),
		"_rels/.rels":                pptxPackageRelationshipsXML(false),
		"ppt/presentation.xml":       pptxPresentationXML([]pptxTestSlide{{Title: "Embedded"}}, false),
		"ppt/embeddings/object1.bin": "embedded object bytes",
	})
	writeCollectorTestFile(t, filepath.Join(repoPath, "docs", "embedded.pptx"), string(embeddedBody))

	envelopes := streamMultipleDocumentFacts(t, repoPath, []string{
		"docs/broken.pptx",
		"docs/unsafe.pptx",
		"docs/embedded.pptx",
	})
	documents := factsByKind(envelopes, facts.DocumentationDocumentFactKind)
	if got, want := len(documents), 3; got != want {
		t.Fatalf("documentation_document count = %d, want %d", got, want)
	}
	assertDocumentPathWarning(t, documents, "docs/broken.pptx", "malformed_container")
	assertDocumentPathWarning(t, documents, "docs/unsafe.pptx", "external_relationship")
	assertDocumentPathWarning(t, documents, "docs/embedded.pptx", "embedded_object_present")
	if got, want := len(factsByKind(envelopes, facts.DocumentationSectionFactKind)), 0; got != want {
		t.Fatalf("documentation_section count = %d, want %d", got, want)
	}
}

func TestStreamFactsBoundsLargePPTXDeck(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	slides := make([]pptxTestSlide, 0, pptxMaxSlides+1)
	for i := 0; i < pptxMaxSlides; i++ {
		slides = append(slides, pptxTestSlide{
			Title: fmt.Sprintf("Slide %03d", i+1),
			Body:  "bounded visible slide",
		})
	}
	slides = append(slides, pptxTestSlide{
		Title:  "Hidden Over Limit",
		Body:   "hidden body should not emit",
		Hidden: true,
	})
	body := buildTestPPTX(t, pptxTestDeck{Slides: slides})
	writeCollectorTestFile(t, filepath.Join(repoPath, "docs", "large-review.pptx"), string(body))

	envelopes := streamDocumentFacts(t, repoPath, "docs/large-review.pptx")
	document := singleFact(t, envelopes, facts.DocumentationDocumentFactKind)
	assertPayloadWarning(t, document.Payload, "resource_limit_exceeded")
	assertPayloadWarning(t, document.Payload, "hidden_content_skipped")
	if got, want := payloadSourceMetadataValue(document.Payload, "slide_count"), fmt.Sprintf("%d", pptxMaxSlides+1); got != want {
		t.Fatalf("slide_count = %q, want %q", got, want)
	}
	if got, want := payloadSourceMetadataValue(document.Payload, "visible_slide_count"), fmt.Sprintf("%d", pptxMaxSlides); got != want {
		t.Fatalf("visible_slide_count = %q, want %q", got, want)
	}
	if got, want := payloadSourceMetadataValue(document.Payload, "hidden_slide_count"), "1"; got != want {
		t.Fatalf("hidden_slide_count = %q, want %q", got, want)
	}
	sections := factsByKind(envelopes, facts.DocumentationSectionFactKind)
	if got, want := len(sections), pptxMaxSlides; got != want {
		t.Fatalf("documentation_section count = %d, want %d", got, want)
	}
	for _, section := range sections {
		if content := payloadString(section.Payload, "content"); strings.Contains(content, "hidden body should not emit") {
			t.Fatalf("hidden slide content leaked in %q", content)
		}
	}
}

func TestStreamFactsKeepsPPTXRootHiddenSlidesMetadataOnly(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	body := buildTestPPTX(t, pptxTestDeck{
		Slides: []pptxTestSlide{
			{
				Title: "Visible Review",
				Body:  "visible body",
			},
			{
				Title:      "Root Hidden Review",
				Body:       "root hidden body should not emit",
				RootHidden: true,
			},
		},
	})
	writeCollectorTestFile(t, filepath.Join(repoPath, "docs", "root-hidden-review.pptx"), string(body))

	envelopes := streamDocumentFacts(t, repoPath, "docs/root-hidden-review.pptx")
	document := singleFact(t, envelopes, facts.DocumentationDocumentFactKind)
	assertPayloadWarning(t, document.Payload, "hidden_content_skipped")
	if got, want := payloadSourceMetadataValue(document.Payload, "hidden_slide_count"), "1"; got != want {
		t.Fatalf("hidden_slide_count = %q, want %q", got, want)
	}
	sections := factsByKind(envelopes, facts.DocumentationSectionFactKind)
	if got, want := len(sections), 1; got != want {
		t.Fatalf("documentation_section count = %d, want %d", got, want)
	}
	content := payloadString(sections[0].Payload, "content")
	for _, forbidden := range []string{"Root Hidden Review", "root hidden body should not emit"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("root-hidden slide content leaked %q in %q", forbidden, content)
		}
	}
}

func TestStreamFactsRejectsUnexpectedPPTXSlideRelationshipTarget(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	body := buildTestZip(t, map[string]string{
		"[Content_Types].xml":  pptxContentTypesXML(1, false, false),
		"_rels/.rels":          pptxPackageRelationshipsXML(false),
		"ppt/presentation.xml": pptxPresentationXML([]pptxTestSlide{{Title: "Unexpected"}}, false),
		"ppt/_rels/presentation.xml.rels": `<?xml version="1.0" encoding="UTF-8"?>` +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/customXml" ` +
			`Target="../customXml/item1.xml"/>` +
			`</Relationships>`,
		"customXml/item1.xml": pptxSlideXML(pptxTestSlide{
			Title: "Unexpected Target",
			Body:  "should not emit",
		}),
	})
	writeCollectorTestFile(t, filepath.Join(repoPath, "docs", "unexpected-target.pptx"), string(body))

	envelopes := streamDocumentFacts(t, repoPath, "docs/unexpected-target.pptx")
	documents := factsByKind(envelopes, facts.DocumentationDocumentFactKind)
	if got, want := len(documents), 1; got != want {
		t.Fatalf("documentation_document count = %d, want %d", got, want)
	}
	assertPayloadWarning(t, documents[0].Payload, "malformed_presentation")
	if got, want := len(factsByKind(envelopes, facts.DocumentationSectionFactKind)), 0; got != want {
		t.Fatalf("documentation_section count = %d, want %d", got, want)
	}
}

type pptxTestDeck struct {
	Slides                      []pptxTestSlide
	Notes                       []string
	Comments                    []string
	ExternalPackageRelationship bool
}

type pptxTestSlide struct {
	Title      string
	Body       string
	Hidden     bool
	RootHidden bool
	Table      [][]string
}

func buildTestPPTX(t *testing.T, deck pptxTestDeck) []byte {
	t.Helper()

	files := map[string]string{
		"[Content_Types].xml":  pptxContentTypesXML(len(deck.Slides), len(deck.Notes) > 0, len(deck.Comments) > 0),
		"_rels/.rels":          pptxPackageRelationshipsXML(deck.ExternalPackageRelationship),
		"ppt/presentation.xml": pptxPresentationXML(deck.Slides, true),
	}
	presentationRels := strings.Builder{}
	presentationRels.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	presentationRels.WriteString(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	for i, slide := range deck.Slides {
		id := i + 1
		fmt.Fprintf(
			&presentationRels,
			`<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide%d.xml"/>`,
			id,
			id,
		)
		files[fmt.Sprintf("ppt/slides/slide%d.xml", id)] = pptxSlideXML(slide)
	}
	presentationRels.WriteString(`</Relationships>`)
	files["ppt/_rels/presentation.xml.rels"] = presentationRels.String()
	if len(deck.Notes) > 0 {
		for i, note := range deck.Notes {
			files[fmt.Sprintf("ppt/notesSlides/notesSlide%d.xml", i+1)] = pptxNotesXML(note)
		}
	}
	if len(deck.Comments) > 0 {
		files["ppt/comments/comment1.xml"] = pptxCommentsXML(deck.Comments)
	}
	return buildTestZip(t, files)
}

func pptxContentTypesXML(slideCount int, includeNotes bool, includeComments bool) string {
	var body strings.Builder
	body.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	body.WriteString(`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">`)
	body.WriteString(`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>`)
	body.WriteString(`<Default Extension="xml" ContentType="application/xml"/>`)
	body.WriteString(`<Override PartName="/ppt/presentation.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml"/>`)
	for i := 1; i <= slideCount; i++ {
		fmt.Fprintf(&body, `<Override PartName="/ppt/slides/slide%d.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slide+xml"/>`, i)
	}
	if includeNotes {
		body.WriteString(`<Override PartName="/ppt/notesSlides/notesSlide1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.notesSlide+xml"/>`)
	}
	if includeComments {
		body.WriteString(`<Override PartName="/ppt/comments/comment1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.comments+xml"/>`)
	}
	body.WriteString(`</Types>`)
	return body.String()
}

func pptxPackageRelationshipsXML(external bool) string {
	var body strings.Builder
	body.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	body.WriteString(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	body.WriteString(`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="ppt/presentation.xml"/>`)
	if external {
		body.WriteString(`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="https://example.invalid/external" TargetMode="External"/>`)
	}
	body.WriteString(`</Relationships>`)
	return body.String()
}

func pptxPresentationXML(slides []pptxTestSlide, includeRelationshipsNamespace bool) string {
	var body strings.Builder
	body.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	body.WriteString(`<p:presentation xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"`)
	if includeRelationshipsNamespace {
		body.WriteString(` xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"`)
	}
	body.WriteString(`><p:sldIdLst>`)
	for i, slide := range slides {
		hidden := ""
		if slide.Hidden {
			hidden = ` show="0"`
		}
		fmt.Fprintf(&body, `<p:sldId id="%d" r:id="rId%d"%s/>`, 256+i, i+1, hidden)
	}
	body.WriteString(`</p:sldIdLst></p:presentation>`)
	return body.String()
}

func pptxSlideXML(slide pptxTestSlide) string {
	var body strings.Builder
	body.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	body.WriteString(`<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"`)
	if slide.RootHidden {
		body.WriteString(` show="0"`)
	}
	body.WriteString(`><p:cSld><p:spTree>`)
	pptxWriteParagraph(&body, slide.Title)
	pptxWriteParagraph(&body, slide.Body)
	if len(slide.Table) > 0 {
		body.WriteString(`<a:tbl>`)
		for _, row := range slide.Table {
			body.WriteString(`<a:tr>`)
			for _, cell := range row {
				body.WriteString(`<a:tc><a:txBody>`)
				pptxWriteParagraph(&body, cell)
				body.WriteString(`</a:txBody></a:tc>`)
			}
			body.WriteString(`</a:tr>`)
		}
		body.WriteString(`</a:tbl>`)
	}
	body.WriteString(`</p:spTree></p:cSld></p:sld>`)
	return body.String()
}

func pptxWriteParagraph(body *strings.Builder, text string) {
	if text == "" {
		return
	}
	body.WriteString(`<a:p><a:r><a:t>`)
	body.WriteString(html.EscapeString(text))
	body.WriteString(`</a:t></a:r></a:p>`)
}

func pptxNotesXML(note string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>` +
		`<p:notes xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" ` +
		`xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><p:cSld>` +
		`<a:p><a:r><a:t>` + html.EscapeString(note) + `</a:t></a:r></a:p>` +
		`</p:cSld></p:notes>`
}

func pptxCommentsXML(comments []string) string {
	var body strings.Builder
	body.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	body.WriteString(`<p:cmLst xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main">`)
	for i, comment := range comments {
		fmt.Fprintf(&body, `<p:cm authorId="%d"><p:text>%s</p:text></p:cm>`, i, html.EscapeString(comment))
	}
	body.WriteString(`</p:cmLst>`)
	return body.String()
}
