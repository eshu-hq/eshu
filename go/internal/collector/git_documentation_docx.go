// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/ooxmlpreflight"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

const (
	docxAnnotationSkippedWarning = "annotation_text_skipped"
	docxMalformedWarning         = "malformed_xml"
	docxResourceLimitWarning     = "resource_limit_exceeded"
	docxMaxXMLPartBytes          = 1 << 20
)

func extractWordDocumentation(
	ctx context.Context,
	repo repositoryidentity.Metadata,
	relativePath string,
	digest string,
	commitSHA string,
	body []byte,
) (facts.DocumentationDocumentPayload, []facts.DocumentationSectionPayload, []facts.DocumentationLinkPayload) {
	revisionID := firstNonEmptyString(commitSHA, digest, "unknown")
	documentID := gitDocumentationDocumentID(repo.ID, relativePath)
	document := wordDocumentPayload(repo, documentID, relativePath, revisionID, digest, commitSHA, body)
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
	word, warnings, err := parseDOCXDocument(body)
	addDocumentationWarnings(document.SourceMetadata, warnings...)
	if err != nil {
		addDocumentationWarnings(document.SourceMetadata, docxMalformedWarning)
		return document, nil, nil
	}
	sections := docxSectionPayloads(documentID, revisionID, relativePath, word.sections)
	document.Title = documentationTitle(relativePath, sections)
	document.SourceMetadata["section_count"] = strconv.Itoa(len(sections))
	document.SourceMetadata["paragraph_count"] = strconv.Itoa(word.paragraphCount)
	document.SourceMetadata["table_count"] = strconv.Itoa(word.tableCount)
	document.SourceMetadata["table_row_count"] = strconv.Itoa(word.tableRowCount)
	if word.commentCount > 0 {
		document.SourceMetadata["comment_count"] = strconv.Itoa(word.commentCount)
	}
	if word.trackedChangeCount > 0 {
		document.SourceMetadata["tracked_change_count"] = strconv.Itoa(word.trackedChangeCount)
	}
	if word.commentCount > 0 || word.trackedChangeCount > 0 {
		addDocumentationWarnings(document.SourceMetadata, docxAnnotationSkippedWarning)
	}
	return document, sections, nil
}

func wordDocumentPayload(
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
		DocumentType: documentationDocumentType(relativePath, "document"),
		Format:       "docx",
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

func parseDOCXDocument(body []byte) (docxDocument, []string, error) {
	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return docxDocument{}, nil, err
	}
	parts, warnings := docxPartMap(reader)
	main, ok := parts["word/document.xml"]
	if !ok {
		return docxDocument{}, warnings, fmt.Errorf("docx document part missing")
	}
	document, err := parseDOCXMainDocument(main)
	if err != nil {
		return docxDocument{}, warnings, err
	}
	comments, err := countDOCXComments(parts["word/comments.xml"])
	if err != nil {
		return docxDocument{}, warnings, err
	}
	document.commentCount = comments
	return document, warnings, nil
}

func docxPartMap(reader *zip.Reader) (map[string][]byte, []string) {
	parts := map[string][]byte{}
	warnings := []string{}
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		if file.UncompressedSize64 > docxMaxXMLPartBytes {
			warnings = append(warnings, docxResourceLimitWarning)
			continue
		}
		clean := path.Clean(file.Name)
		if strings.HasPrefix(clean, "../") || clean == "." {
			continue
		}
		if !isDOCXReadableXMLPart(clean) {
			continue
		}
		part, err := readDOCXZipPart(file)
		if err != nil {
			continue
		}
		parts[clean] = part
	}
	return parts, warnings
}

func readDOCXZipPart(file *zip.File) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close() }()
	return io.ReadAll(io.LimitReader(reader, docxMaxXMLPartBytes+1))
}

func isDOCXReadableXMLPart(name string) bool {
	lower := strings.ToLower(name)
	return lower == "[content_types].xml" ||
		lower == "_rels/.rels" ||
		lower == "word/document.xml" ||
		lower == "word/comments.xml" ||
		strings.HasSuffix(lower, ".rels")
}

func docxSectionPayloads(
	documentID string,
	revisionID string,
	relativePath string,
	drafts []docxSectionDraft,
) []facts.DocumentationSectionPayload {
	markdownDrafts := make([]markdownSectionDraft, 0, len(drafts))
	for _, draft := range drafts {
		metadata := map[string]string{
			"paragraph_count": strconv.Itoa(draft.paragraphCount),
			"table_count":     strconv.Itoa(draft.tableCount),
			"table_row_count": strconv.Itoa(draft.tableRowCount),
		}
		markdownDrafts = append(markdownDrafts, markdownSectionDraft{
			level:          draft.level,
			heading:        draft.heading,
			anchor:         draft.anchor,
			startRef:       draft.startRef,
			endRef:         draft.endRef,
			content:        draft.content,
			warnings:       draft.warnings,
			sourceMetadata: metadata,
		})
	}
	return documentationSectionsFromDrafts(documentID, revisionID, relativePath, "docx", markdownDrafts)
}

func isDocumentationOfficePath(relativePath string) bool {
	clean := strings.ToLower(path.Clean(filepathToSourceURI(relativePath)))
	base := strings.TrimSuffix(path.Base(clean), path.Ext(clean))
	for _, segment := range strings.Split(path.Dir(clean), "/") {
		switch segment {
		case "doc", "docs", "documentation", "runbook", "runbooks", "adr", "adrs":
			return true
		}
	}
	for _, token := range []string{
		"architecture", "design", "incident", "migration", "plan", "prd",
		"review", "runbook", "spec",
	} {
		if strings.Contains(base, token) {
			return true
		}
	}
	return false
}
