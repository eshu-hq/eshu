// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
)

type pptxDeck struct {
	slides         []pptxSlideRef
	visibleSlides  []pptxSlideSummary
	hiddenSlides   int
	notesSlides    int
	commentCount   int
	paragraphCount int
	tableCount     int
	warnings       []string
}

type pptxSlideRef struct {
	relID   string
	ordinal int
	hidden  bool
}

type pptxRelationship struct {
	target string
	kind   string
}

type pptxSlideSummary struct {
	title          string
	paragraphs     []string
	tableRows      []string
	hidden         bool
	ordinal        int
	paragraphCount int
	tableCount     int
	tableRowCount  int
	warnings       []string
}

type pptxPackageCounts struct {
	notesSlides  int
	commentParts int
}

func parsePPTXDeck(body []byte) (pptxDeck, error) {
	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return pptxDeck{}, err
	}
	parts, counts, warnings := pptxPartMap(reader)
	presentationBody, ok := parts["ppt/presentation.xml"]
	if !ok {
		return pptxDeck{notesSlides: counts.notesSlides, commentCount: counts.commentParts, warnings: warnings}, fmt.Errorf("pptx presentation part missing")
	}
	refs, err := parsePPTXPresentationSlides(presentationBody)
	if err != nil {
		return pptxDeck{notesSlides: counts.notesSlides, commentCount: counts.commentParts, warnings: warnings}, err
	}
	relationships, err := parsePPTXRelationships(parts["ppt/_rels/presentation.xml.rels"])
	if err != nil {
		return pptxDeck{slides: refs, notesSlides: counts.notesSlides, commentCount: counts.commentParts, warnings: warnings}, err
	}
	deck := pptxDeck{
		slides:       refs,
		hiddenSlides: countHiddenPPTXSlides(refs),
		notesSlides:  counts.notesSlides,
		commentCount: counts.commentParts,
		warnings:     warnings,
	}
	parseRefs := refs
	if len(parseRefs) > pptxMaxSlides {
		parseRefs = parseRefs[:pptxMaxSlides]
		deck.warnings = append(deck.warnings, pptxResourceLimitWarning)
	}
	for _, ref := range parseRefs {
		if ref.hidden {
			continue
		}
		relationship, ok := relationships[ref.relID]
		if !ok {
			return deck, fmt.Errorf("pptx slide relationship %q missing", ref.relID)
		}
		target := resolvePPTXRelationshipTarget("ppt/presentation.xml", relationship.target)
		if !isPPTXSlideRelationship(relationship, target) {
			return deck, fmt.Errorf("pptx slide relationship %q target is not a slide", ref.relID)
		}
		slideBody, ok := parts[target]
		if !ok {
			return deck, fmt.Errorf("pptx slide part missing")
		}
		slide, err := parsePPTXSlide(slideBody)
		if err != nil {
			return deck, err
		}
		slide.ordinal = ref.ordinal
		if slide.hidden {
			deck.hiddenSlides++
			continue
		}
		deck.paragraphCount += slide.paragraphCount
		deck.tableCount += slide.tableCount
		deck.visibleSlides = append(deck.visibleSlides, slide)
	}
	return deck, nil
}

func countHiddenPPTXSlides(refs []pptxSlideRef) int {
	count := 0
	for _, ref := range refs {
		if ref.hidden {
			count++
		}
	}
	return count
}

func pptxPartMap(reader *zip.Reader) (map[string][]byte, pptxPackageCounts, []string) {
	parts := map[string][]byte{}
	counts := pptxPackageCounts{}
	warnings := []string{}
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		clean := path.Clean(file.Name)
		lower := strings.ToLower(clean)
		switch {
		case strings.HasPrefix(lower, "ppt/notesslides/") && strings.HasSuffix(lower, ".xml"):
			counts.notesSlides++
		case strings.HasPrefix(lower, "ppt/comments/") && strings.HasSuffix(lower, ".xml"):
			if strings.HasPrefix(strings.ToLower(path.Base(lower)), "comment") {
				counts.commentParts++
			}
		}
		if file.UncompressedSize64 > pptxMaxXMLPartBytes {
			warnings = append(warnings, pptxResourceLimitWarning)
			continue
		}
		if strings.HasPrefix(clean, "../") || clean == "." || !isPPTXReadableXMLPart(clean) {
			continue
		}
		part, err := readPPTXZipPart(file)
		if err != nil {
			continue
		}
		parts[clean] = part
	}
	return parts, counts, warnings
}

func readPPTXZipPart(file *zip.File) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close() }()
	return io.ReadAll(io.LimitReader(reader, pptxMaxXMLPartBytes+1))
}

func isPPTXReadableXMLPart(name string) bool {
	lower := strings.ToLower(name)
	return lower == "ppt/presentation.xml" ||
		lower == "ppt/_rels/presentation.xml.rels" ||
		strings.HasPrefix(lower, "ppt/slides/") && strings.HasSuffix(lower, ".xml")
}

func parsePPTXPresentationSlides(body []byte) ([]pptxSlideRef, error) {
	decoder := xml.NewDecoder(bytes.NewReader(body))
	refs := []pptxSlideRef{}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return refs, nil
		}
		if err != nil {
			return nil, err
		}
		start, ok := token.(xml.StartElement)
		if !ok || !strings.EqualFold(start.Name.Local, "sldId") {
			continue
		}
		refs = append(refs, pptxSlideRef{
			relID:   pptxRelationshipID(start),
			ordinal: len(refs) + 1,
			hidden:  pptxSlideHidden(start),
		})
	}
}

func parsePPTXRelationships(body []byte) (map[string]pptxRelationship, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("pptx presentation relationships missing")
	}
	decoder := xml.NewDecoder(bytes.NewReader(body))
	rels := map[string]pptxRelationship{}
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
		rels[pptxXMLAttr(start, "Id")] = pptxRelationship{
			target: pptxXMLAttr(start, "Target"),
			kind:   pptxXMLAttr(start, "Type"),
		}
	}
}

func parsePPTXSlide(body []byte) (pptxSlideSummary, error) {
	decoder := xml.NewDecoder(bytes.NewReader(body))
	state := pptxSlideParseState{}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return state.finish(), nil
		}
		if err != nil {
			return pptxSlideSummary{}, err
		}
		switch typed := token.(type) {
		case xml.StartElement:
			state.handleStart(typed)
		case xml.EndElement:
			state.handleEnd(typed)
		case xml.CharData:
			state.handleCharData(typed)
		}
	}
}

type pptxSlideParseState struct {
	summary          pptxSlideSummary
	tableDepth       int
	inParagraph      bool
	inText           bool
	inTableCell      bool
	currentParagraph strings.Builder
	currentCell      strings.Builder
	currentRow       []string
}

func (s *pptxSlideParseState) handleStart(start xml.StartElement) {
	switch strings.ToLower(start.Name.Local) {
	case "sld":
		if pptxSlideHidden(start) {
			s.summary.hidden = true
		}
	case "tbl":
		s.tableDepth++
		s.summary.tableCount++
	case "tr":
		if s.tableDepth > 0 {
			s.currentRow = nil
		}
	case "tc":
		if s.tableDepth > 0 {
			s.inTableCell = true
			s.currentCell.Reset()
		}
	case "p":
		if s.tableDepth == 0 {
			s.inParagraph = true
			s.currentParagraph.Reset()
		}
	case "t":
		s.inText = true
	}
}

func (s *pptxSlideParseState) handleEnd(end xml.EndElement) {
	switch strings.ToLower(end.Name.Local) {
	case "t":
		s.inText = false
	case "p":
		if s.tableDepth == 0 && s.inParagraph {
			s.finishParagraph()
		}
	case "tc":
		if s.tableDepth > 0 && s.inTableCell {
			s.currentRow = append(s.currentRow, normalizePPTXText(s.currentCell.String()))
			s.currentCell.Reset()
			s.inTableCell = false
		}
	case "tr":
		if s.tableDepth > 0 && len(s.currentRow) > 0 {
			s.summary.tableRows = append(s.summary.tableRows, strings.Join(s.currentRow, " | "))
			s.summary.tableRowCount++
			s.currentRow = nil
		}
	case "tbl":
		if s.tableDepth > 0 {
			s.tableDepth--
		}
	}
}

func (s *pptxSlideParseState) handleCharData(value []byte) {
	if !s.inText {
		return
	}
	if s.tableDepth > 0 && s.inTableCell {
		s.currentCell.Write(value)
		return
	}
	if s.tableDepth == 0 && s.inParagraph {
		s.currentParagraph.Write(value)
	}
}

func (s *pptxSlideParseState) finishParagraph() {
	text := normalizePPTXText(s.currentParagraph.String())
	s.currentParagraph.Reset()
	s.inParagraph = false
	if text == "" {
		return
	}
	s.summary.paragraphCount++
	if s.summary.title == "" {
		s.summary.title = text
		return
	}
	s.summary.paragraphs = append(s.summary.paragraphs, text)
}

func (s *pptxSlideParseState) finish() pptxSlideSummary {
	return s.summary
}

func pptxRelationshipID(start xml.StartElement) string {
	for _, attr := range start.Attr {
		if strings.EqualFold(attr.Name.Local, "id") && strings.Contains(strings.ToLower(attr.Name.Space), "relationships") {
			return attr.Value
		}
	}
	for _, attr := range start.Attr {
		if strings.EqualFold(attr.Name.Local, "id") && strings.HasPrefix(attr.Value, "rId") {
			return attr.Value
		}
	}
	return ""
}

func pptxSlideHidden(start xml.StartElement) bool {
	for _, attr := range start.Attr {
		if strings.EqualFold(attr.Name.Local, "show") {
			return attr.Value == "0" || strings.EqualFold(attr.Value, "false")
		}
		if strings.EqualFold(attr.Name.Local, "hidden") {
			hidden, err := strconv.ParseBool(attr.Value)
			return err == nil && hidden
		}
	}
	return false
}

func pptxXMLAttr(start xml.StartElement, name string) string {
	for _, attr := range start.Attr {
		if strings.EqualFold(attr.Name.Local, name) {
			return attr.Value
		}
	}
	return ""
}

func normalizePPTXText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
