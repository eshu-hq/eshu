// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

const (
	spreadsheetMaxRows      = 200
	spreadsheetMaxColumns   = 40
	spreadsheetSampleRows   = 10
	spreadsheetMaxCellRunes = 160
)

var spreadsheetSensitiveCellPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b[A-Z0-9._%+\-]+@[A-Z0-9.\-]+\.[A-Z]{2,}\b`),
	regexp.MustCompile(`(?i)\bhttps?://(?:localhost|127\.|10\.|192\.168\.|172\.(?:1[6-9]|2[0-9]|3[0-1])|[^/\s]*(?:internal|private|corp|\.local))`),
	regexp.MustCompile(`(?i)\b(?:api[_-]?key|token|secret|password|passwd|credential)\b`),
	regexp.MustCompile(`(?i)\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`),
}

func extractSpreadsheetDocumentation(
	repo repositoryidentity.Metadata,
	relativePath string,
	digest string,
	commitSHA string,
	body []byte,
	format string,
) (facts.DocumentationDocumentPayload, []facts.DocumentationSectionPayload, []facts.DocumentationLinkPayload) {
	revisionID := firstNonEmptyString(commitSHA, digest, "unknown")
	documentID := gitDocumentationDocumentID(repo.ID, relativePath)
	bodyText, warnings := boundedDocumentationBody(body)
	table, parseWarnings := parseDelimitedSpreadsheet(bodyText, format)
	warnings = append(warnings, parseWarnings...)
	table.warnings = append(table.warnings, parseWarnings...)
	document := facts.DocumentationDocumentPayload{
		SourceID:     gitDocumentationSourceID(repo.ID),
		DocumentID:   documentID,
		ExternalID:   relativePath,
		RevisionID:   revisionID,
		CanonicalURI: gitDocumentationCanonicalURI(repo, relativePath, commitSHA),
		Title:        documentationTitle(relativePath, nil),
		DocumentType: documentationDocumentType(relativePath, "spreadsheet"),
		Format:       format,
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
	if table.parseErr != nil {
		addDocumentationWarnings(document.SourceMetadata, "malformed_spreadsheet")
		return document, nil, nil
	}
	if table.rows == 0 && len(table.headers) == 0 {
		addDocumentationWarnings(document.SourceMetadata, "empty_spreadsheet")
		return document, nil, nil
	}
	section := spreadsheetSectionPayload(documentID, revisionID, relativePath, format, table)
	return document, []facts.DocumentationSectionPayload{section}, textDocumentationLinks(relativePath, []facts.DocumentationSectionPayload{section})
}

func parseDelimitedSpreadsheet(body string, format string) (spreadsheetTable, []string) {
	reader := csv.NewReader(strings.NewReader(body))
	reader.FieldsPerRecord = -1
	reader.ReuseRecord = true
	if format == "tsv" {
		reader.Comma = '\t'
	}
	warnings := []string{}
	table := spreadsheetTable{}
	rowNumber := 0
	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			table.parseErr = err
			return table, warnings
		}
		rowNumber++
		if rowNumber == 1 {
			table.headers, table.warnings = boundedSpreadsheetRow(record, true, table.warnings)
			continue
		}
		if table.rows >= spreadsheetMaxRows {
			warnings = append(warnings, "row_limit_exceeded")
			break
		}
		table.rows++
		if len(record) > table.columns {
			table.columns = len(record)
		}
		bounded, cellWarnings := boundedSpreadsheetRow(record, false, nil)
		table.warnings = append(table.warnings, cellWarnings...)
		if len(table.samples) < spreadsheetSampleRows {
			table.samples = append(table.samples, bounded)
		}
	}
	if len(table.headers) > table.columns {
		table.columns = len(table.headers)
	}
	return table, warnings
}

func boundedSpreadsheetRow(row []string, header bool, warnings []string) ([]string, []string) {
	out := make([]string, 0, min(len(row), spreadsheetMaxColumns))
	for i, cell := range row {
		if i >= spreadsheetMaxColumns {
			warnings = append(warnings, "column_limit_exceeded")
			break
		}
		cell, cellWarnings := boundedSpreadsheetCell(cell, header)
		warnings = append(warnings, cellWarnings...)
		out = append(out, cell)
	}
	return out, warnings
}

func boundedSpreadsheetCell(cell string, header bool) (string, []string) {
	cell = strings.TrimSpace(strings.ToValidUTF8(cell, ""))
	warnings := []string{}
	if !header && spreadsheetCellLooksSensitive(cell) {
		return "[redacted]", []string{"sensitive_cell_redacted"}
	}
	runes := []rune(cell)
	if len(runes) > spreadsheetMaxCellRunes {
		cell = string(runes[:spreadsheetMaxCellRunes])
		warnings = append(warnings, "cell_truncated")
	}
	return cell, warnings
}

func spreadsheetCellLooksSensitive(cell string) bool {
	for _, pattern := range spreadsheetSensitiveCellPatterns {
		if pattern.MatchString(cell) {
			return true
		}
	}
	return false
}

func spreadsheetSectionPayload(
	documentID string,
	revisionID string,
	relativePath string,
	format string,
	table spreadsheetTable,
) facts.DocumentationSectionPayload {
	content, contentWarnings := boundedDocumentationSectionContent(spreadsheetSectionContent(relativePath, table))
	warnings := append([]string{}, table.warnings...)
	warnings = append(warnings, contentWarnings...)
	metadata := documentationSectionMetadata(relativePath, map[string]string{
		"table_kind":       "delimited_spreadsheet",
		"row_count":        strconv.Itoa(table.rows),
		"column_count":     strconv.Itoa(table.columns),
		"sample_row_count": strconv.Itoa(len(table.samples)),
	})
	addDocumentationWarnings(metadata, warnings...)
	return facts.DocumentationSectionPayload{
		DocumentID:       documentID,
		RevisionID:       revisionID,
		SectionID:        "section:table",
		SectionAnchor:    "table",
		HeadingText:      documentationTitle(relativePath, nil),
		OrdinalPath:      []int{1},
		Content:          content,
		ContentFormat:    format,
		TextHash:         documentationHashText(documentationTitle(relativePath, nil) + "\n" + content),
		ExcerptHash:      documentationHashText(content),
		SourceStartRef:   "row:1",
		SourceEndRef:     fmt.Sprintf("row:%d", table.rows+1),
		SourceMetadata:   metadata,
		ContainsWarnings: len(warnings) > 0,
	}
}

func spreadsheetSectionContent(relativePath string, table spreadsheetTable) string {
	lines := []string{
		fmt.Sprintf("table: %s", documentationTitle(relativePath, nil)),
		fmt.Sprintf("rows: %d", table.rows),
		fmt.Sprintf("columns: %s", strings.Join(table.headers, ", ")),
	}
	for i, row := range table.samples {
		lines = append(lines, fmt.Sprintf("sample %d: %s", i+1, spreadsheetSampleLine(table.headers, row)))
	}
	return strings.Join(lines, "\n")
}

func spreadsheetSampleLine(headers []string, row []string) string {
	cells := make([]string, 0, len(row))
	for i, value := range row {
		label := fmt.Sprintf("column_%d", i+1)
		if i < len(headers) && strings.TrimSpace(headers[i]) != "" {
			label = headers[i]
		}
		cells = append(cells, label+"="+value)
	}
	return strings.Join(cells, "; ")
}

func isDocumentationSpreadsheetPath(relativePath string) bool {
	clean := strings.ToLower(path.Clean(filepathToSourceURI(relativePath)))
	base := strings.TrimSuffix(path.Base(clean), path.Ext(clean))
	for _, segment := range strings.Split(path.Dir(clean), "/") {
		switch segment {
		case "doc", "docs", "documentation", "runbook", "runbooks", "adr", "adrs":
			return true
		}
	}
	for _, token := range []string{
		"audit", "catalog", "dependency", "dependencies", "inventory", "matrix",
		"migration", "owner", "owners", "ownership", "roster", "support",
	} {
		if strings.Contains(base, token) {
			return true
		}
	}
	return false
}

type spreadsheetTable struct {
	headers  []string
	rows     int
	columns  int
	samples  [][]string
	warnings []string
	parseErr error
}
