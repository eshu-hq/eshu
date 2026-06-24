// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
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
	xlsxHiddenContentSkippedWarning = "hidden_content_skipped"
	xlsxMalformedWarning            = "malformed_spreadsheet"
	xlsxUnsupportedLegacyWarning    = "unsupported_legacy_binary"
	xlsxMaxXMLPartBytes             = 1 << 20
	xlsxMaxFormulaHashes            = 20
)

func extractWorkbookDocumentation(
	ctx context.Context,
	repo repositoryidentity.Metadata,
	relativePath string,
	digest string,
	commitSHA string,
	body []byte,
	format string,
) (facts.DocumentationDocumentPayload, []facts.DocumentationSectionPayload, []facts.DocumentationLinkPayload) {
	revisionID := firstNonEmptyString(commitSHA, digest, "unknown")
	documentID := gitDocumentationDocumentID(repo.ID, relativePath)
	document := workbookDocumentPayload(repo, documentID, relativePath, revisionID, digest, commitSHA, body, format)
	if format == "xls" {
		addDocumentationWarnings(document.SourceMetadata, xlsxUnsupportedLegacyWarning)
		return document, nil, nil
	}
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
	workbook, err := parseXLSXWorkbook(body)
	if err != nil {
		addDocumentationWarnings(document.SourceMetadata, xlsxMalformedWarning)
		return document, nil, nil
	}
	document.SourceMetadata["sheet_count"] = strconv.Itoa(len(workbook.sheets))
	document.SourceMetadata["visible_sheet_count"] = strconv.Itoa(len(workbook.visibleSheets))
	document.SourceMetadata["hidden_sheet_count"] = strconv.Itoa(workbook.hiddenSheets)
	if workbook.hiddenSheets > 0 {
		addDocumentationWarnings(document.SourceMetadata, xlsxHiddenContentSkippedWarning)
	}
	sections := make([]facts.DocumentationSectionPayload, 0, len(workbook.visibleSheets))
	for i, sheet := range workbook.visibleSheets {
		sections = append(sections, xlsxSheetSectionPayload(documentID, revisionID, relativePath, i+1, sheet))
	}
	return document, sections, nil
}

func workbookDocumentPayload(
	repo repositoryidentity.Metadata,
	documentID string,
	relativePath string,
	revisionID string,
	digest string,
	commitSHA string,
	body []byte,
	format string,
) facts.DocumentationDocumentPayload {
	document := facts.DocumentationDocumentPayload{
		SourceID:     gitDocumentationSourceID(repo.ID),
		DocumentID:   documentID,
		ExternalID:   relativePath,
		RevisionID:   revisionID,
		CanonicalURI: gitDocumentationCanonicalURI(repo, relativePath, commitSHA),
		Title:        documentationTitle(relativePath, nil),
		DocumentType: "spreadsheet",
		Format:       format,
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

func recordOOXMLPreflightMetadata(metadata map[string]string, result ooxmlpreflight.Result) {
	if result.Format != "" {
		metadata["preflight_format"] = result.Format
	}
	metadata["preflight_source_bytes"] = strconv.FormatInt(result.SourceBytes, 10)
	metadata["preflight_part_count"] = strconv.Itoa(result.PartCount)
	metadata["preflight_expanded_bytes"] = strconv.FormatUint(result.ExpandedBytes, 10)
	if result.RelationshipPartCount > 0 {
		metadata["preflight_relationship_part_count"] = strconv.Itoa(result.RelationshipPartCount)
	}
	if result.ExternalRelationshipCount > 0 {
		metadata["preflight_external_relationship_count"] = strconv.Itoa(result.ExternalRelationshipCount)
	}
	if result.ActiveContentCount > 0 {
		metadata["preflight_active_content_count"] = strconv.Itoa(result.ActiveContentCount)
	}
	if result.EmbeddedObjectCount > 0 {
		metadata["preflight_embedded_object_count"] = strconv.Itoa(result.EmbeddedObjectCount)
	}
}

func ooxmlPreflightWarnings(result ooxmlpreflight.Result) []string {
	warnings := make([]string, 0, len(result.Warnings))
	for _, warning := range result.Warnings {
		if warning.Class != "" {
			warnings = append(warnings, warning.Class)
		}
	}
	return warnings
}

type xlsxWorkbook struct {
	sheets        []xlsxSheetRef
	visibleSheets []xlsxSheetSummary
	hiddenSheets  int
}

type xlsxSheetRef struct {
	name   string
	relID  string
	hidden bool
}

type xlsxRelationship struct {
	target string
	kind   string
}

type xlsxSheetSummary struct {
	name          string
	ordinal       int
	dimension     string
	table         spreadsheetTable
	formulaCount  int
	formulaHashes []string
}

func parseXLSXWorkbook(body []byte) (xlsxWorkbook, error) {
	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return xlsxWorkbook{}, err
	}
	parts := xlsxPartMap(reader)
	workbookBody, ok := parts["xl/workbook.xml"]
	if !ok {
		return xlsxWorkbook{}, fmt.Errorf("xlsx workbook part missing")
	}
	refs, err := parseXLSXWorkbookSheetRefs(workbookBody)
	if err != nil {
		return xlsxWorkbook{}, err
	}
	relationships, err := parseXLSXRelationships(parts["xl/_rels/workbook.xml.rels"])
	if err != nil {
		return xlsxWorkbook{}, err
	}
	sharedStrings, err := parseXLSXSharedStrings(parts["xl/sharedStrings.xml"])
	if err != nil {
		return xlsxWorkbook{}, err
	}
	workbook := xlsxWorkbook{sheets: refs}
	for i, ref := range refs {
		if ref.hidden {
			workbook.hiddenSheets++
			continue
		}
		relationship, ok := relationships[ref.relID]
		if !ok {
			return xlsxWorkbook{}, fmt.Errorf("xlsx sheet relationship %q missing", ref.relID)
		}
		target := resolveXLSXRelationshipTarget("xl/workbook.xml", relationship.target)
		if !isXLSXWorksheetRelationship(relationship, target) {
			return xlsxWorkbook{}, fmt.Errorf("xlsx sheet relationship %q target is not a worksheet", ref.relID)
		}
		sheetBody, ok := parts[target]
		if !ok {
			return xlsxWorkbook{}, fmt.Errorf("xlsx worksheet part missing")
		}
		table, dimension, formulaCount, formulaHashes, err := parseXLSXWorksheet(sheetBody, sharedStrings)
		if err != nil {
			return xlsxWorkbook{}, err
		}
		workbook.visibleSheets = append(workbook.visibleSheets, xlsxSheetSummary{
			name:          ref.name,
			ordinal:       i + 1,
			dimension:     dimension,
			table:         table,
			formulaCount:  formulaCount,
			formulaHashes: formulaHashes,
		})
	}
	return workbook, nil
}

func xlsxPartMap(reader *zip.Reader) map[string][]byte {
	parts := map[string][]byte{}
	for _, file := range reader.File {
		if file.FileInfo().IsDir() || file.UncompressedSize64 > xlsxMaxXMLPartBytes {
			continue
		}
		clean := path.Clean(file.Name)
		if strings.HasPrefix(clean, "../") || clean == "." {
			continue
		}
		part, err := readXLSXZipPart(file)
		if err != nil {
			continue
		}
		parts[clean] = part
	}
	return parts
}

func readXLSXZipPart(file *zip.File) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close() }()
	return io.ReadAll(io.LimitReader(reader, xlsxMaxXMLPartBytes+1))
}

func parseXLSXWorkbookSheetRefs(body []byte) ([]xlsxSheetRef, error) {
	decoder := xml.NewDecoder(bytes.NewReader(body))
	refs := []xlsxSheetRef{}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return refs, nil
		}
		if err != nil {
			return nil, err
		}
		start, ok := token.(xml.StartElement)
		if !ok || !strings.EqualFold(start.Name.Local, "sheet") {
			continue
		}
		state := strings.ToLower(xlsxXMLAttr(start, "state"))
		refs = append(refs, xlsxSheetRef{
			name:   firstNonEmptyString(xlsxXMLAttr(start, "name"), fmt.Sprintf("Sheet%d", len(refs)+1)),
			relID:  xlsxXMLAttr(start, "id"),
			hidden: state == "hidden" || state == "veryHidden",
		})
	}
}

func parseXLSXRelationships(body []byte) (map[string]xlsxRelationship, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("xlsx workbook relationships missing")
	}
	decoder := xml.NewDecoder(bytes.NewReader(body))
	rels := map[string]xlsxRelationship{}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return rels, nil
		}
		if err != nil {
			return nil, err
		}
		start, ok := token.(xml.StartElement)
		if !ok || !strings.EqualFold(start.Name.Local, "relationship") {
			continue
		}
		rels[xlsxXMLAttr(start, "Id")] = xlsxRelationship{
			target: xlsxXMLAttr(start, "Target"),
			kind:   xlsxXMLAttr(start, "Type"),
		}
	}
}

func parseXLSXSharedStrings(body []byte) ([]string, error) {
	if len(body) == 0 {
		return nil, nil
	}
	decoder := xml.NewDecoder(bytes.NewReader(body))
	values := []string{}
	var current strings.Builder
	inString := false
	inText := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return values, nil
		}
		if err != nil {
			return nil, err
		}
		switch typed := token.(type) {
		case xml.StartElement:
			switch strings.ToLower(typed.Name.Local) {
			case "si":
				inString = true
				current.Reset()
			case "t":
				if inString {
					inText = true
				}
			}
		case xml.EndElement:
			switch strings.ToLower(typed.Name.Local) {
			case "t":
				inText = false
			case "si":
				values = append(values, current.String())
				inString = false
			}
		case xml.CharData:
			if inText {
				current.Write([]byte(typed))
			}
		}
	}
}

func xlsxSheetSectionPayload(
	documentID string,
	revisionID string,
	relativePath string,
	ordinal int,
	sheet xlsxSheetSummary,
) facts.DocumentationSectionPayload {
	content, contentWarnings := boundedDocumentationSectionContent(xlsxSheetSectionContent(sheet))
	warnings := append([]string{}, sheet.table.warnings...)
	warnings = append(warnings, contentWarnings...)
	metadata := documentationSectionMetadata(relativePath, map[string]string{
		"table_kind":       "xlsx_sheet",
		"sheet_name_hash":  documentationHashText(sheet.name),
		"sheet_ordinal":    strconv.Itoa(sheet.ordinal),
		"row_count":        strconv.Itoa(sheet.table.rows),
		"column_count":     strconv.Itoa(sheet.table.columns),
		"sample_row_count": strconv.Itoa(len(sheet.table.samples)),
	})
	if sheet.dimension != "" {
		metadata["dimension_ref"] = sheet.dimension
	}
	if sheet.formulaCount > 0 {
		metadata["formula_count"] = strconv.Itoa(sheet.formulaCount)
		metadata["formula_hashes"] = strings.Join(sheet.formulaHashes, ",")
	}
	addDocumentationWarnings(metadata, warnings...)
	return facts.DocumentationSectionPayload{
		DocumentID:       documentID,
		RevisionID:       revisionID,
		SectionID:        fmt.Sprintf("section:sheet:%d", ordinal),
		SectionAnchor:    fmt.Sprintf("sheet-%d", ordinal),
		HeadingText:      sheet.name,
		OrdinalPath:      []int{ordinal},
		Content:          content,
		ContentFormat:    "xlsx",
		TextHash:         documentationHashText(sheet.name + "\n" + content),
		ExcerptHash:      documentationHashText(content),
		SourceStartRef:   fmt.Sprintf("sheet:%d", sheet.ordinal),
		SourceEndRef:     fmt.Sprintf("sheet:%d", sheet.ordinal),
		SourceMetadata:   metadata,
		ContainsWarnings: len(warnings) > 0,
	}
}

func xlsxSheetSectionContent(sheet xlsxSheetSummary) string {
	lines := []string{
		fmt.Sprintf("sheet: %s", sheet.name),
		fmt.Sprintf("rows: %d", sheet.table.rows),
		fmt.Sprintf("columns: %s", strings.Join(sheet.table.headers, ", ")),
	}
	for i, row := range sheet.table.samples {
		lines = append(lines, fmt.Sprintf("sample %d: %s", i+1, spreadsheetSampleLine(sheet.table.headers, row)))
	}
	return strings.Join(lines, "\n")
}

func xlsxColumnIndex(ref string) int {
	index := 0
	for _, r := range ref {
		if r >= 'A' && r <= 'Z' {
			index = index*26 + int(r-'A'+1)
			continue
		}
		if r >= 'a' && r <= 'z' {
			index = index*26 + int(r-'a'+1)
			continue
		}
		break
	}
	return index
}

func resolveXLSXRelationshipTarget(sourcePart string, target string) string {
	target = strings.TrimPrefix(strings.TrimSpace(target), "/")
	if strings.Contains(target, ":") {
		return ""
	}
	if strings.HasPrefix(target, "xl/") {
		return path.Clean(target)
	}
	return path.Clean(path.Join(path.Dir(sourcePart), target))
}

func isXLSXWorksheetRelationship(relationship xlsxRelationship, resolvedTarget string) bool {
	if !strings.HasSuffix(strings.ToLower(relationship.kind), "/worksheet") {
		return false
	}
	if !strings.HasPrefix(resolvedTarget, "xl/worksheets/") {
		return false
	}
	return strings.HasSuffix(strings.ToLower(resolvedTarget), ".xml")
}

func xlsxXMLAttr(start xml.StartElement, name string) string {
	for _, attr := range start.Attr {
		if strings.EqualFold(attr.Name.Local, name) {
			return attr.Value
		}
	}
	return ""
}
