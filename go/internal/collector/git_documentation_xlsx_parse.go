// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"bytes"
	"encoding/xml"
	"io"
	"strconv"
	"strings"
)

func parseXLSXWorksheet(body []byte, sharedStrings []string) (spreadsheetTable, string, int, []string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(body))
	table := spreadsheetTable{}
	rowNumber := 0
	dimension := ""
	formulaCount := 0
	formulaHashes := []string{}
	var row []string
	var cell xlsxCellDraft
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			if len(table.headers) > table.columns {
				table.columns = len(table.headers)
			}
			return table, dimension, formulaCount, formulaHashes, nil
		}
		if err != nil {
			return spreadsheetTable{}, "", 0, nil, err
		}
		switch typed := token.(type) {
		case xml.StartElement:
			switch strings.ToLower(typed.Name.Local) {
			case "dimension":
				dimension = xlsxXMLAttr(typed, "ref")
			case "row":
				row = nil
			case "c":
				cell = xlsxCellDraft{ref: xlsxXMLAttr(typed, "r"), cellType: xlsxXMLAttr(typed, "t")}
			case "v":
				cell.captureValue = true
			case "t":
				if cell.cellType == "inlineStr" {
					cell.captureInline = true
				}
			case "f":
				cell.captureFormula = true
			}
		case xml.EndElement:
			switch strings.ToLower(typed.Name.Local) {
			case "v":
				cell.captureValue = false
			case "t":
				cell.captureInline = false
			case "f":
				cell.captureFormula = false
			case "c":
				row = appendXLSXCell(row, cell, sharedStrings)
				if cell.formula != "" {
					formulaCount++
					if len(formulaHashes) < xlsxMaxFormulaHashes {
						formulaHashes = append(formulaHashes, documentationHashText(cell.formula))
					}
				}
			case "row":
				rowNumber++
				if rowNumber == 1 {
					table.headers, table.warnings = boundedSpreadsheetRow(row, true, table.warnings)
					continue
				}
				if table.rows >= spreadsheetMaxRows {
					table.warnings = append(table.warnings, "row_limit_exceeded")
					continue
				}
				table.rows++
				bounded, warnings := boundedSpreadsheetRow(row, false, nil)
				table.warnings = append(table.warnings, warnings...)
				if len(bounded) > table.columns {
					table.columns = len(bounded)
				}
				if len(table.samples) < spreadsheetSampleRows {
					table.samples = append(table.samples, bounded)
				}
			}
		case xml.CharData:
			cell.writeCharData([]byte(typed))
		}
	}
}

type xlsxCellDraft struct {
	ref            string
	cellType       string
	value          strings.Builder
	inline         strings.Builder
	formula        string
	captureValue   bool
	captureInline  bool
	captureFormula bool
}

func (c *xlsxCellDraft) writeCharData(value []byte) {
	switch {
	case c.captureFormula:
		c.formula += string(value)
	case c.captureInline:
		c.inline.Write(value)
	case c.captureValue:
		c.value.Write(value)
	}
}

func appendXLSXCell(row []string, cell xlsxCellDraft, sharedStrings []string) []string {
	index := xlsxColumnIndex(cell.ref)
	if index <= 0 {
		index = len(row) + 1
	}
	for len(row) < index-1 {
		row = append(row, "")
	}
	value := cell.inline.String()
	if value == "" {
		value = cell.value.String()
	}
	if cell.cellType == "s" {
		if parsed, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && parsed >= 0 && parsed < len(sharedStrings) {
			value = sharedStrings[parsed]
		}
	}
	return append(row, value)
}
