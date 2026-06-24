// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

type notebookDocument struct {
	Cells []notebookCell `json:"cells"`
}

type notebookCell struct {
	CellType    string                                `json:"cell_type"`
	ID          string                                `json:"id"`
	Source      any                                   `json:"source"`
	Attachments map[string]map[string]json.RawMessage `json:"attachments"`
	Outputs     []notebookOutput                      `json:"outputs"`
}

type notebookOutput struct {
	OutputType string                     `json:"output_type"`
	Name       string                     `json:"name"`
	Text       any                        `json:"text"`
	Data       map[string]json.RawMessage `json:"data"`
}

func extractNotebookDocumentation(
	repo repositoryidentity.Metadata,
	relativePath string,
	digest string,
	commitSHA string,
	body []byte,
) (facts.DocumentationDocumentPayload, []facts.DocumentationSectionPayload, []facts.DocumentationLinkPayload) {
	revisionID := firstNonEmptyString(commitSHA, digest, "unknown")
	documentID := gitDocumentationDocumentID(repo.ID, relativePath)
	bodyText, warnings := boundedNotebookBody(body)

	var notebook notebookDocument
	if err := json.Unmarshal([]byte(bodyText), &notebook); err != nil {
		warnings = append(warnings, "malformed_notebook")
		document := notebookDocumentPayload(repo, relativePath, revisionID, digest, commitSHA, bodyText, warnings, nil)
		return document, nil, nil
	}

	drafts, extractionWarnings := notebookSectionDrafts(notebook.Cells)
	warnings = append(warnings, extractionWarnings...)
	if len(drafts) == 0 {
		warnings = append(warnings, "empty_notebook")
	}
	sections := documentationSectionsFromDrafts(documentID, revisionID, relativePath, "notebook", drafts)
	document := notebookDocumentPayload(repo, relativePath, revisionID, digest, commitSHA, bodyText, warnings, sections)
	links := markdownLinks(relativePath, sections)
	return document, sections, links
}

func notebookDocumentPayload(
	repo repositoryidentity.Metadata,
	relativePath string,
	revisionID string,
	digest string,
	commitSHA string,
	bodyText string,
	warnings []string,
	sections []facts.DocumentationSectionPayload,
) facts.DocumentationDocumentPayload {
	document := facts.DocumentationDocumentPayload{
		SourceID:     gitDocumentationSourceID(repo.ID),
		DocumentID:   gitDocumentationDocumentID(repo.ID, relativePath),
		ExternalID:   relativePath,
		RevisionID:   revisionID,
		CanonicalURI: gitDocumentationCanonicalURI(repo, relativePath, commitSHA),
		Title:        documentationTitle(relativePath, sections),
		DocumentType: documentationDocumentType(relativePath, "notebook"),
		Format:       "notebook",
		Language:     "en",
		ContentHash:  firstNonEmptyString(digest, documentationHashText(bodyText)),
		SourceMetadata: map[string]string{
			"path":    relativePath,
			"repo_id": repo.ID,
		},
	}
	if commitSHA != "" {
		document.SourceMetadata["source_revision"] = commitSHA
	}
	addDocumentationWarnings(document.SourceMetadata, warnings...)
	return document
}

func notebookSectionDrafts(cells []notebookCell) ([]markdownSectionDraft, []string) {
	drafts := []markdownSectionDraft{}
	warnings := []string{}
	for cellIndex, cell := range cells {
		cellType := strings.ToLower(strings.TrimSpace(cell.CellType))
		switch cellType {
		case "markdown":
			cellDrafts, cellWarnings := notebookMarkdownDrafts(cellIndex, cell)
			drafts = append(drafts, cellDrafts...)
			warnings = append(warnings, cellWarnings...)
		case "raw":
			if draft, ok := notebookRawDraft(cellIndex, cell); ok {
				drafts = append(drafts, draft)
			}
		case "code":
			cellDrafts, cellWarnings := notebookCodeOutputDrafts(cellIndex, cell)
			drafts = append(drafts, cellDrafts...)
			warnings = append(warnings, cellWarnings...)
		default:
			if cellType != "" {
				warnings = append(warnings, "unsupported_cell_type")
			}
		}
	}
	return drafts, warnings
}

func notebookMarkdownDrafts(cellIndex int, cell notebookCell) ([]markdownSectionDraft, []string) {
	source := notebookText(cell.Source)
	if strings.TrimSpace(source) == "" {
		return nil, nil
	}
	warnings := notebookAttachmentWarnings(cell.Attachments)
	cellMetadata := notebookCellMetadata(cellIndex, cell.ID)
	if len(cell.Attachments) > 0 {
		cellMetadata["attachment_count"] = strconv.Itoa(len(cell.Attachments))
	}
	lines := markdownContentLines(source)
	drafts := []markdownSectionDraft{}
	current := -1
	for _, line := range lines {
		level, heading, ok := markdownHeading(line.Text)
		if ok {
			drafts = append(drafts, notebookDraft(cellIndex, cell.ID, level, heading, "cell-"+strconv.Itoa(cellIndex)+"-"+markdownAnchor(heading), fmt.Sprintf("cell:%d:line:%d", cellIndex, line.Number), cellMetadata, warnings))
			current = len(drafts) - 1
			continue
		}
		if current >= 0 {
			drafts[current].content = append(drafts[current].content, line.Text)
			drafts[current].endRef = fmt.Sprintf("cell:%d:line:%d", cellIndex, line.Number)
			continue
		}
		if strings.TrimSpace(line.Text) == "" {
			continue
		}
		drafts = append(drafts, notebookDraft(cellIndex, cell.ID, 1, fmt.Sprintf("Markdown cell %d", cellIndex), "cell-"+strconv.Itoa(cellIndex)+"-body", fmt.Sprintf("cell:%d:line:%d", cellIndex, line.Number), cellMetadata, warnings))
		current = len(drafts) - 1
		drafts[current].content = append(drafts[current].content, line.Text)
	}
	return drafts, warnings
}

func notebookRawDraft(cellIndex int, cell notebookCell) (markdownSectionDraft, bool) {
	source := strings.TrimSpace(notebookText(cell.Source))
	if source == "" {
		return markdownSectionDraft{}, false
	}
	draft := notebookDraft(
		cellIndex,
		cell.ID,
		1,
		fmt.Sprintf("Raw cell %d", cellIndex),
		fmt.Sprintf("cell-%d-raw", cellIndex),
		fmt.Sprintf("cell:%d", cellIndex),
		notebookCellMetadata(cellIndex, cell.ID),
		nil,
	)
	draft.content = []string{source}
	return draft, true
}

func notebookCodeOutputDrafts(cellIndex int, cell notebookCell) ([]markdownSectionDraft, []string) {
	drafts := []markdownSectionDraft{}
	warnings := []string{}
	selectedOrdinal := 0
	for outputIndex, output := range cell.Outputs {
		text, outputWarnings := notebookOutputText(output)
		warnings = append(warnings, outputWarnings...)
		if strings.TrimSpace(text) == "" {
			continue
		}
		selectedOrdinal++
		sectionWarnings := append([]string{}, outputWarnings...)
		text = strings.TrimSpace(text)
		if len([]rune(text)) > documentationMaxSectionChars {
			text = string([]rune(text)[:documentationMaxSectionChars])
			sectionWarnings = append(sectionWarnings, "output_truncated")
			warnings = append(warnings, "output_truncated")
		}
		metadata := notebookCellMetadata(cellIndex, cell.ID)
		metadata["output_index"] = strconv.Itoa(outputIndex)
		if strings.TrimSpace(output.OutputType) != "" {
			metadata["output_type"] = strings.TrimSpace(output.OutputType)
		}
		draft := notebookDraft(
			cellIndex,
			cell.ID,
			1,
			fmt.Sprintf("Code output %d.%d", cellIndex, selectedOrdinal),
			fmt.Sprintf("cell-%d-output-%d", cellIndex, selectedOrdinal),
			fmt.Sprintf("cell:%d:output:%d", cellIndex, outputIndex),
			metadata,
			sectionWarnings,
		)
		draft.content = []string{text}
		drafts = append(drafts, draft)
	}
	return drafts, warnings
}

func notebookDraft(
	cellIndex int,
	cellID string,
	level int,
	heading string,
	anchor string,
	sourceRef string,
	metadata map[string]string,
	warnings []string,
) markdownSectionDraft {
	if metadata == nil {
		metadata = notebookCellMetadata(cellIndex, cellID)
	}
	return markdownSectionDraft{
		level:          level,
		heading:        heading,
		anchor:         anchor,
		startRef:       sourceRef,
		endRef:         sourceRef,
		warnings:       append([]string{}, warnings...),
		sourceMetadata: metadata,
	}
}

func notebookCellMetadata(cellIndex int, cellID string) map[string]string {
	metadata := map[string]string{"cell_index": strconv.Itoa(cellIndex)}
	if strings.TrimSpace(cellID) != "" {
		metadata["cell_id"] = strings.TrimSpace(cellID)
	}
	return metadata
}

func notebookAttachmentWarnings(attachments map[string]map[string]json.RawMessage) []string {
	if len(attachments) == 0 {
		return nil
	}
	for _, byMime := range attachments {
		for mimeType := range byMime {
			if strings.HasPrefix(strings.ToLower(mimeType), "image/") || strings.HasPrefix(strings.ToLower(mimeType), "application/") {
				return []string{"binary_attachment_omitted"}
			}
		}
	}
	return []string{"attachment_omitted"}
}

func notebookOutputText(output notebookOutput) (string, []string) {
	warnings := []string{}
	if strings.EqualFold(output.OutputType, "stream") {
		if strings.EqualFold(strings.TrimSpace(output.Name), "stderr") {
			return "", []string{"stderr_output_omitted"}
		}
		return notebookText(output.Text), warnings
	}
	if len(output.Data) == 0 {
		return "", warnings
	}
	if raw, ok := output.Data["text/plain"]; ok {
		text := notebookRawJSONText(raw)
		warnings = append(warnings, notebookRichOutputWarnings(output.Data, "text/plain")...)
		return text, warnings
	}
	return "", append(warnings, notebookRichOutputWarnings(output.Data, "")...)
}

func notebookRichOutputWarnings(data map[string]json.RawMessage, selected string) []string {
	if len(data) == 0 {
		return nil
	}
	warnings := []string{}
	for _, mimeType := range sortedNotebookMIMETypes(data) {
		if mimeType == selected {
			continue
		}
		lower := strings.ToLower(mimeType)
		switch {
		case strings.HasPrefix(lower, "image/"), lower == "application/octet-stream":
			warnings = append(warnings, "binary_output_omitted")
		default:
			warnings = append(warnings, "rich_output_omitted")
		}
	}
	return warnings
}

func sortedNotebookMIMETypes(data map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func notebookRawJSONText(raw json.RawMessage) string {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return notebookText(value)
}

func notebookText(raw any) string {
	switch typed := raw.(type) {
	case nil:
		return ""
	case string:
		return typed
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			parts = append(parts, fmt.Sprint(item))
		}
		return strings.Join(parts, "")
	case []string:
		return strings.Join(typed, "")
	default:
		return fmt.Sprint(typed)
	}
}
