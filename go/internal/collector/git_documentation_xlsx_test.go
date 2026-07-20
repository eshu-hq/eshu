// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"archive/zip"
	"bytes"
	"fmt"
	"html"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestStreamFactsEmitsXLSXWorkbookDocumentation(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	body := buildTestXLSXWorkbook(t, []xlsxTestSheet{
		{
			Name: "Inventory",
			Rows: [][]xlsxTestCell{
				{{Value: "service"}, {Value: "owner_email"}, {Value: "dependency"}, {Value: "check_count"}},
				{{Value: "payments-api"}, {Value: "ops@example.invalid"}, {Value: "postgres"}, {Value: "1"}},
				{{Value: "billing-worker"}, {Value: "billing-team"}, {Value: "queue"}, {Formula: "COUNTA(A2:A3)", Value: "2"}},
			},
		},
		{
			Name:   "Private Notes",
			Hidden: true,
			Rows: [][]xlsxTestCell{
				{{Value: "do-not-emit"}, {Value: "hidden-only-value"}},
			},
		},
	})
	writeCollectorTestFile(t, filepath.Join(repoPath, "docs", "service-inventory.xlsx"), string(body))

	envelopes := streamSpreadsheetFacts(t, repoPath, "docs/service-inventory.xlsx")
	documents := factsByKind(envelopes, facts.DocumentationDocumentFactKind)
	if got, want := len(documents), 1; got != want {
		t.Fatalf("documentation_document count = %d, want %d", got, want)
	}
	document := documents[0]
	if got, want := payloadString(document.Payload, "format"), "xlsx"; got != want {
		t.Fatalf("document format = %q, want %q", got, want)
	}
	if got, want := payloadString(document.Payload, "document_type"), "spreadsheet"; got != want {
		t.Fatalf("document_type = %q, want %q", got, want)
	}
	if got, want := payloadSourceMetadataValue(document.Payload, "preflight_format"), "xlsx"; got != want {
		t.Fatalf("preflight_format = %q, want %q", got, want)
	}
	if got, want := payloadSourceMetadataValue(document.Payload, "visible_sheet_count"), "1"; got != want {
		t.Fatalf("visible_sheet_count = %q, want %q", got, want)
	}
	if got, want := payloadSourceMetadataValue(document.Payload, "hidden_sheet_count"), "1"; got != want {
		t.Fatalf("hidden_sheet_count = %q, want %q", got, want)
	}
	assertPayloadWarning(t, document.Payload, "hidden_content_skipped")

	sections := factsByKind(envelopes, facts.DocumentationSectionFactKind)
	if got, want := len(sections), 1; got != want {
		t.Fatalf("documentation_section count = %d, want %d", got, want)
	}
	section := sections[0]
	if got, want := payloadString(section.Payload, "heading_text"), "Inventory"; got != want {
		t.Fatalf("heading_text = %q, want %q", got, want)
	}
	if got, want := payloadSourceMetadataValue(section.Payload, "table_kind"), "xlsx_sheet"; got != want {
		t.Fatalf("table_kind = %q, want %q", got, want)
	}
	if got, want := payloadSourceMetadataValue(section.Payload, "row_count"), "2"; got != want {
		t.Fatalf("row_count = %q, want %q", got, want)
	}
	if got, want := payloadSourceMetadataValue(section.Payload, "column_count"), "4"; got != want {
		t.Fatalf("column_count = %q, want %q", got, want)
	}
	if got, want := payloadSourceMetadataValue(section.Payload, "dimension_ref"), "A1:D3"; got != want {
		t.Fatalf("dimension_ref = %q, want %q", got, want)
	}
	if got, want := payloadSourceMetadataValue(section.Payload, "formula_count"), "1"; got != want {
		t.Fatalf("formula_count = %q, want %q", got, want)
	}
	if got := payloadSourceMetadataValue(section.Payload, "formula_hashes"); !strings.HasPrefix(got, "sha256:") {
		t.Fatalf("formula_hashes = %q, want sha256 hash", got)
	}
	content := payloadString(section.Payload, "content")
	for _, want := range []string{
		"sheet: Inventory",
		"columns: service, owner_email, dependency, check_count",
		"payments-api",
		"postgres",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("section content missing %q in %q", want, content)
		}
	}
	for _, forbidden := range []string{
		"ops@example.invalid",
		"COUNTA",
		"Private Notes",
		"hidden-only-value",
	} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("section content leaked %q: %q", forbidden, content)
		}
	}
	assertPayloadWarning(t, section.Payload, "sensitive_cell_redacted")
	assertDocumentationFactLinkedRepository(t, section, "repository:r_12345678")
}

func TestStreamFactsBoundsLargeXLSXWorkbook(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	rows := [][]xlsxTestCell{{{Value: "service"}, {Value: "owner"}, {Value: "dependency"}}}
	for i := 0; i < spreadsheetMaxRows+5; i++ {
		row := make([]xlsxTestCell, 0, spreadsheetMaxColumns+2)
		for col := 0; col < spreadsheetMaxColumns+2; col++ {
			row = append(row, xlsxTestCell{Value: fmt.Sprintf("r%03d-c%02d", i, col)})
		}
		rows = append(rows, row)
	}
	body := buildTestXLSXWorkbook(t, []xlsxTestSheet{{Name: "Inventory", Rows: rows}})
	writeCollectorTestFile(t, filepath.Join(repoPath, "docs", "large-inventory.xlsx"), string(body))

	envelopes := streamSpreadsheetFacts(t, repoPath, "docs/large-inventory.xlsx")
	section := singleFact(t, envelopes, facts.DocumentationSectionFactKind)
	assertPayloadWarning(t, section.Payload, "row_limit_exceeded")
	assertPayloadWarning(t, section.Payload, "column_limit_exceeded")
	if got, want := payloadSourceMetadataValue(section.Payload, "row_count"), fmt.Sprintf("%d", spreadsheetMaxRows); got != want {
		t.Fatalf("row_count = %q, want %q", got, want)
	}
	if got, want := payloadSourceMetadataValue(section.Payload, "column_count"), fmt.Sprintf("%d", spreadsheetMaxColumns); got != want {
		t.Fatalf("column_count = %q, want %q", got, want)
	}
	if got, want := payloadSourceMetadataValue(section.Payload, "sample_row_count"), fmt.Sprintf("%d", spreadsheetSampleRows); got != want {
		t.Fatalf("sample_row_count = %q, want %q", got, want)
	}
	content := payloadString(section.Payload, "content")
	if strings.Contains(content, "r020-c00") {
		t.Fatalf("spreadsheet content includes rows beyond bounded sample: %q", content)
	}
}

func TestStreamFactsHandlesMalformedAndLegacyXLSWorkbooks(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoPath, "docs", "broken.xlsx"), "not a zip")
	writeCollectorTestFile(t, filepath.Join(repoPath, "docs", "legacy.xls"), "legacy workbook bytes")

	collected := buildStreamingGeneration(
		repoPath,
		testCollectorRepositoryMetadata(repoPath),
		"run-1",
		time.Date(2026, time.June, 9, 8, 15, 0, 0, time.UTC),
		RepositorySnapshot{
			FileCount: 2,
			DocumentationFileMetas: []ContentFileMeta{
				{RelativePath: "docs/broken.xlsx", Digest: "sha256:broken", Language: "xlsx"},
				{RelativePath: "docs/legacy.xls", Digest: "sha256:legacy", Language: "xls"},
			},
		},
		false,
		"",
	)
	envelopes := drainFactChannel(collected.Facts)

	documents := factsByKind(envelopes, facts.DocumentationDocumentFactKind)
	if got, want := len(documents), 2; got != want {
		t.Fatalf("documentation_document count = %d, want %d", got, want)
	}
	assertDocumentPathWarning(t, documents, "docs/broken.xlsx", "malformed_container")
	assertDocumentPathWarning(t, documents, "docs/legacy.xls", "unsupported_legacy_binary")
	if got, want := len(factsByKind(envelopes, facts.DocumentationSectionFactKind)), 0; got != want {
		t.Fatalf("documentation_section count = %d, want %d", got, want)
	}
}

func TestReadDocumentationBodySkipsLegacyXLSBytes(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoPath, "docs", "legacy.xls"), "legacy workbook bytes")

	body, ok := readDocumentationBody(repoPath, "docs/legacy.xls", nil)
	if !ok {
		t.Fatal("readDocumentationBody() ok = false, want true")
	}
	if got := len(body); got != 0 {
		t.Fatalf("len(body) = %d, want 0", got)
	}
}

func TestStreamFactsRejectsUnexpectedXLSXWorksheetRelationshipTarget(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	body := buildTestZip(t, map[string]string{
		"[Content_Types].xml": xlsxContentTypesXML(),
		"_rels/.rels":         xlsxPackageRelationshipsXML(),
		"xl/workbook.xml": `<?xml version="1.0" encoding="UTF-8"?>` +
			`<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" ` +
			`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
			`<sheets><sheet name="Inventory" sheetId="1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<?xml version="1.0" encoding="UTF-8"?>` +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/customXml" ` +
			`Target="../customXml/item1.xml"/>` +
			`</Relationships>`,
		"customXml/item1.xml": xlsxWorksheetXML([][]xlsxTestCell{
			{{Value: "service"}},
			{{Value: "should-not-emit"}},
		}),
	})
	writeCollectorTestFile(t, filepath.Join(repoPath, "docs", "unexpected-target.xlsx"), string(body))

	envelopes := streamSpreadsheetFacts(t, repoPath, "docs/unexpected-target.xlsx")
	documents := factsByKind(envelopes, facts.DocumentationDocumentFactKind)
	if got, want := len(documents), 1; got != want {
		t.Fatalf("documentation_document count = %d, want %d", got, want)
	}
	assertPayloadWarning(t, documents[0].Payload, "malformed_spreadsheet")
	if got, want := len(factsByKind(envelopes, facts.DocumentationSectionFactKind)), 0; got != want {
		t.Fatalf("documentation_section count = %d, want %d", got, want)
	}
}

func assertDocumentPathWarning(t *testing.T, documents []facts.Envelope, path string, warning string) {
	t.Helper()
	for _, document := range documents {
		if payloadSourceMetadataValue(document.Payload, "path") != path {
			continue
		}
		assertPayloadWarning(t, document.Payload, warning)
		return
	}
	t.Fatalf("missing document path %q in %#v", path, documents)
}

type xlsxTestSheet struct {
	Name   string
	Hidden bool
	Rows   [][]xlsxTestCell
}

type xlsxTestCell struct {
	Value   string
	Formula string
}

func buildTestXLSXWorkbook(t *testing.T, sheets []xlsxTestSheet) []byte {
	t.Helper()

	files := map[string]string{
		"[Content_Types].xml": xlsxContentTypesXML(),
		"_rels/.rels":         xlsxPackageRelationshipsXML(),
	}
	workbookRels := strings.Builder{}
	workbookRels.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	workbookRels.WriteString(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	workbook := strings.Builder{}
	workbook.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	workbook.WriteString(`<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>`)
	for i, sheet := range sheets {
		id := i + 1
		state := ""
		if sheet.Hidden {
			state = ` state="hidden"`
		}
		fmt.Fprintf(&workbook, `<sheet name="%s" sheetId="%d" r:id="rId%d"%s/>`, html.EscapeString(sheet.Name), id, id, state)
		fmt.Fprintf(&workbookRels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, id, id)
		files[fmt.Sprintf("xl/worksheets/sheet%d.xml", id)] = xlsxWorksheetXML(sheet.Rows)
	}
	workbook.WriteString(`</sheets></workbook>`)
	workbookRels.WriteString(`</Relationships>`)
	files["xl/workbook.xml"] = workbook.String()
	files["xl/_rels/workbook.xml.rels"] = workbookRels.String()
	return buildTestZip(t, files)
}

func xlsxContentTypesXML() string {
	return `<?xml version="1.0" encoding="UTF-8"?>` +
		`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
		`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
		`<Default Extension="xml" ContentType="application/xml"/>` +
		`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>` +
		`</Types>`
}

func xlsxPackageRelationshipsXML() string {
	return `<?xml version="1.0" encoding="UTF-8"?>` +
		`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
		`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>` +
		`</Relationships>`
}

func xlsxWorksheetXML(rows [][]xlsxTestCell) string {
	var body strings.Builder
	body.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	body.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)
	fmt.Fprintf(&body, `<dimension ref="A1:%s%d"/>`, xlsxColumnName(maxXLSXTestColumns(rows)), len(rows))
	body.WriteString(`<sheetData>`)
	for rowIndex, row := range rows {
		fmt.Fprintf(&body, `<row r="%d">`, rowIndex+1)
		for colIndex, cell := range row {
			ref := fmt.Sprintf("%s%d", xlsxColumnName(colIndex+1), rowIndex+1)
			if cell.Formula != "" {
				fmt.Fprintf(&body, `<c r="%s"><f>%s</f><v>%s</v></c>`, ref, html.EscapeString(cell.Formula), html.EscapeString(cell.Value))
				continue
			}
			fmt.Fprintf(&body, `<c r="%s" t="inlineStr"><is><t>%s</t></is></c>`, ref, html.EscapeString(cell.Value))
		}
		body.WriteString(`</row>`)
	}
	body.WriteString(`</sheetData></worksheet>`)
	return body.String()
}

func maxXLSXTestColumns(rows [][]xlsxTestCell) int {
	maxColumns := 1
	for _, row := range rows {
		if len(row) > maxColumns {
			maxColumns = len(row)
		}
	}
	return maxColumns
}

func xlsxColumnName(index int) string {
	if index <= 0 {
		return "A"
	}
	name := ""
	for index > 0 {
		index--
		name = string(rune('A'+index%26)) + name
		index /= 26
	}
	return name
}

func buildTestZip(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	for name, body := range files {
		part, err := writer.Create(name)
		if err != nil {
			t.Fatalf("Create(%q) error = %v, want nil", name, err)
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
