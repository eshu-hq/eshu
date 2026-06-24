// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"bytes"
	"context"
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/ooxmlpreflight"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

const (
	pptxAnnotationSkippedWarning = "annotation_text_skipped"
	pptxHiddenContentWarning     = "hidden_content_skipped"
	pptxMalformedWarning         = "malformed_presentation"
	pptxResourceLimitWarning     = "resource_limit_exceeded"
	pptxMaxXMLPartBytes          = 1 << 20
	pptxMaxSlides                = 500
)

func extractPresentationDocumentation(
	ctx context.Context,
	repo repositoryidentity.Metadata,
	relativePath string,
	digest string,
	commitSHA string,
	body []byte,
) (facts.DocumentationDocumentPayload, []facts.DocumentationSectionPayload, []facts.DocumentationLinkPayload) {
	revisionID := firstNonEmptyString(commitSHA, digest, "unknown")
	documentID := gitDocumentationDocumentID(repo.ID, relativePath)
	document := presentationDocumentPayload(repo, documentID, relativePath, revisionID, digest, commitSHA, body)
	if ctx == nil {
		ctx = context.Background()
	}
	preflight, err := ooxmlpreflight.Preflight(
		ctx,
		path.Base(relativePath),
		bytes.NewReader(body),
		int64(len(body)),
		ooxmlpreflight.Options{},
	)
	recordOOXMLPreflightMetadata(document.SourceMetadata, preflight)
	addDocumentationWarnings(document.SourceMetadata, ooxmlPreflightWarnings(preflight)...)
	if err != nil || ooxmlPreflightBlocksExtraction(preflight) {
		return document, nil, nil
	}
	deck, err := parsePPTXDeck(body)
	addDocumentationWarnings(document.SourceMetadata, deck.warnings...)
	if err != nil {
		addDocumentationWarnings(document.SourceMetadata, pptxMalformedWarning)
		return document, nil, nil
	}
	sections := pptxSlideSectionPayloads(documentID, revisionID, relativePath, deck.visibleSlides)
	document.Title = documentationTitle(relativePath, sections)
	document.SourceMetadata["slide_count"] = strconv.Itoa(len(deck.slides))
	document.SourceMetadata["visible_slide_count"] = strconv.Itoa(len(deck.visibleSlides))
	document.SourceMetadata["hidden_slide_count"] = strconv.Itoa(deck.hiddenSlides)
	document.SourceMetadata["section_count"] = strconv.Itoa(len(sections))
	document.SourceMetadata["paragraph_count"] = strconv.Itoa(deck.paragraphCount)
	document.SourceMetadata["table_count"] = strconv.Itoa(deck.tableCount)
	if deck.notesSlides > 0 {
		document.SourceMetadata["notes_slide_count"] = strconv.Itoa(deck.notesSlides)
	}
	if deck.commentCount > 0 {
		document.SourceMetadata["comment_count"] = strconv.Itoa(deck.commentCount)
	}
	if deck.hiddenSlides > 0 {
		addDocumentationWarnings(document.SourceMetadata, pptxHiddenContentWarning)
	}
	if deck.notesSlides > 0 || deck.commentCount > 0 {
		addDocumentationWarnings(document.SourceMetadata, pptxAnnotationSkippedWarning)
	}
	return document, sections, nil
}

func presentationDocumentPayload(
	repo repositoryidentity.Metadata,
	documentID string,
	relativePath string,
	revisionID string,
	digest string,
	commitSHA string,
	body []byte,
) facts.DocumentationDocumentPayload {
	document := facts.DocumentationDocumentPayload{
		SourceID:     gitDocumentationSourceID(repo.ID),
		DocumentID:   documentID,
		ExternalID:   relativePath,
		RevisionID:   revisionID,
		CanonicalURI: gitDocumentationCanonicalURI(repo, relativePath, commitSHA),
		Title:        documentationTitle(relativePath, nil),
		DocumentType: "presentation",
		Format:       "pptx",
		Language:     "en",
		ContentHash:  firstNonEmptyString(digest, documentationHashText(string(body))),
		SourceMetadata: map[string]string{
			"path":    relativePath,
			"repo_id": repo.ID,
		},
	}
	if commitSHA != "" {
		document.SourceMetadata["source_revision"] = commitSHA
	}
	return document
}

func pptxSlideSectionPayloads(
	documentID string,
	revisionID string,
	relativePath string,
	slides []pptxSlideSummary,
) []facts.DocumentationSectionPayload {
	sections := make([]facts.DocumentationSectionPayload, 0, len(slides))
	for _, slide := range slides {
		content, contentWarnings := boundedDocumentationSectionContent(pptxSlideSectionContent(slide))
		warnings := append([]string{}, slide.warnings...)
		warnings = append(warnings, contentWarnings...)
		heading := firstNonEmptyString(slide.title, fmt.Sprintf("Slide %d", slide.ordinal))
		metadata := documentationSectionMetadata(relativePath, map[string]string{
			"slide_ordinal":   strconv.Itoa(slide.ordinal),
			"paragraph_count": strconv.Itoa(slide.paragraphCount),
			"table_count":     strconv.Itoa(slide.tableCount),
			"table_row_count": strconv.Itoa(slide.tableRowCount),
		})
		addDocumentationWarnings(metadata, warnings...)
		section := facts.DocumentationSectionPayload{
			DocumentID:       documentID,
			RevisionID:       revisionID,
			SectionID:        fmt.Sprintf("section:slide:%d", slide.ordinal),
			SectionAnchor:    fmt.Sprintf("slide-%d", slide.ordinal),
			HeadingText:      heading,
			OrdinalPath:      []int{slide.ordinal},
			Content:          content,
			ContentFormat:    "pptx",
			TextHash:         documentationHashText(strings.TrimSpace(heading + "\n" + content)),
			ExcerptHash:      documentationHashText(content),
			SourceStartRef:   fmt.Sprintf("slide:%d", slide.ordinal),
			SourceEndRef:     fmt.Sprintf("slide:%d", slide.ordinal),
			SourceMetadata:   metadata,
			ContainsWarnings: len(warnings) > 0,
		}
		sections = append(sections, section)
	}
	return sections
}

func pptxSlideSectionContent(slide pptxSlideSummary) string {
	heading := firstNonEmptyString(slide.title, fmt.Sprintf("Slide %d", slide.ordinal))
	lines := []string{fmt.Sprintf("slide: %s", heading)}
	lines = append(lines, slide.paragraphs...)
	for i, row := range slide.tableRows {
		lines = append(lines, fmt.Sprintf("table row %d: %s", i+1, row))
	}
	return strings.Join(lines, "\n")
}

func resolvePPTXRelationshipTarget(sourcePart string, target string) string {
	target = strings.TrimPrefix(strings.TrimSpace(target), "/")
	if strings.Contains(target, ":") {
		return ""
	}
	if strings.HasPrefix(target, "ppt/") {
		return path.Clean(target)
	}
	return path.Clean(path.Join(path.Dir(sourcePart), target))
}

func isPPTXSlideRelationship(relationship pptxRelationship, resolvedTarget string) bool {
	if !strings.HasSuffix(strings.ToLower(relationship.kind), "/slide") {
		return false
	}
	if !strings.HasPrefix(resolvedTarget, "ppt/slides/") {
		return false
	}
	return strings.HasSuffix(strings.ToLower(resolvedTarget), ".xml")
}
