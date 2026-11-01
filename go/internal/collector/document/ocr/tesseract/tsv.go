// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tesseract

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/eshu-hq/eshu/go/internal/collector/document/ocr"
)

// tsvHeader is the column row tesseract 4 and 5 print for the tsv config.
const tsvHeader = "level\tpage_num\tblock_num\tpar_num\tline_num\tword_num\tleft\ttop\twidth\theight\tconf\ttext"

// tsvWordLevel is the TSV level of a single recognized word.
const tsvWordLevel = 5

// lineKey identifies one text line in tesseract's page layout.
type lineKey struct {
	page, block, par, line int
}

// lineAccumulator gathers the words of one line in output order.
type lineAccumulator struct {
	words          []string
	confidenceSum  float64
	x0, y0, x1, y1 int
}

// parseTSV turns tesseract TSV into line regions in reading order.
//
// Regions are lines, not words, on purpose: redaction in ocr.Extract judges
// each region's text as a unit, so "password: <value>" must stay one region
// for the value to be redacted with its label. Parse errors never quote row
// content, so they can not leak OCR text.
func parseTSV(stdout []byte, width, height, maxRegions int) ([]ocr.Region, error) {
	lines := bytes.Split(stdout, []byte("\n"))
	if len(lines) == 0 || strings.TrimRight(string(lines[0]), "\r") != tsvHeader {
		return nil, failure(OutcomeParseFailed, "missing TSV header", nil)
	}
	order := []lineKey{}
	byKey := map[lineKey]*lineAccumulator{}
	for index, raw := range lines[1:] {
		row := strings.TrimRight(string(raw), "\r")
		if row == "" {
			continue
		}
		if !utf8.ValidString(row) {
			return nil, failure(OutcomeParseFailed, fmt.Sprintf("row %d is not UTF-8", index+2), nil)
		}
		fields := strings.Split(row, "\t")
		if len(fields) != 12 {
			return nil, failure(OutcomeParseFailed, fmt.Sprintf("row %d has %d fields, want 12", index+2, len(fields)), nil)
		}
		numbers := make([]int, 10)
		for i := 0; i < 10; i++ {
			value, err := strconv.Atoi(fields[i])
			if err != nil {
				return nil, failure(OutcomeParseFailed, fmt.Sprintf("row %d column %d is not an integer", index+2, i+1), nil)
			}
			numbers[i] = value
		}
		confidence, err := strconv.ParseFloat(fields[10], 64)
		if err != nil {
			return nil, failure(OutcomeParseFailed, fmt.Sprintf("row %d confidence is not a number", index+2), nil)
		}
		text := strings.TrimSpace(fields[11])
		if numbers[0] != tsvWordLevel || text == "" || confidence < 0 {
			continue
		}
		key := lineKey{page: numbers[1], block: numbers[2], par: numbers[3], line: numbers[4]}
		left, top, boxWidth, boxHeight := numbers[6], numbers[7], numbers[8], numbers[9]
		acc, ok := byKey[key]
		if !ok {
			if len(order) == maxRegions {
				return nil, failure(OutcomeRegionCapExceeded, fmt.Sprintf("more than %d line regions", maxRegions), nil)
			}
			acc = &lineAccumulator{x0: left, y0: top, x1: left + boxWidth, y1: top + boxHeight}
			byKey[key] = acc
			order = append(order, key)
		}
		acc.words = append(acc.words, text)
		acc.confidenceSum += confidence
		acc.x0 = min(acc.x0, left)
		acc.y0 = min(acc.y0, top)
		acc.x1 = max(acc.x1, left+boxWidth)
		acc.y1 = max(acc.y1, top+boxHeight)
	}
	regions := make([]ocr.Region, 0, len(order))
	for index, key := range order {
		acc := byKey[key]
		regions = append(regions, ocr.Region{
			RegionID:   fmt.Sprintf("tess-%04d", index+1),
			Text:       strings.Join(acc.words, " "),
			Confidence: clamp01(acc.confidenceSum / float64(len(acc.words)) / 100),
			Bounds: ocr.Bounds{
				X:      clamp01(float64(acc.x0) / float64(width)),
				Y:      clamp01(float64(acc.y0) / float64(height)),
				Width:  clamp01(float64(acc.x1-acc.x0) / float64(width)),
				Height: clamp01(float64(acc.y1-acc.y0) / float64(height)),
			},
		})
	}
	return regions, nil
}

func clamp01(value float64) float64 {
	switch {
	case value < 0:
		return 0
	case value > 1:
		return 1
	default:
		return value
	}
}
