// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type docxDocument struct {
	sections           []docxSectionDraft
	paragraphCount     int
	tableCount         int
	tableRowCount      int
	commentCount       int
	trackedChangeCount int
}

type docxSectionDraft struct {
	level          int
	heading        string
	anchor         string
	startRef       string
	endRef         string
	content        []string
	warnings       []string
	paragraphCount int
	tableCount     int
	tableRowCount  int
}

type docxParagraphDraft struct {
	style strings.Builder
	text  strings.Builder
}

func parseDOCXMainDocument(body []byte) (docxDocument, error) {
	decoder := xml.NewDecoder(bytes.NewReader(body))
	document := docxDocument{}
	state := docxParserState{currentSection: -1}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return document, nil
		}
		if err != nil {
			return docxDocument{}, err
		}
		switch typed := token.(type) {
		case xml.StartElement:
			state.handleStart(typed, &document)
		case xml.EndElement:
			state.handleEnd(typed, &document)
		case xml.CharData:
			state.handleCharData([]byte(typed))
		}
	}
}

type docxParserState struct {
	currentSection int
	blockOrdinal   int
	inParagraph    bool
	inTable        bool
	inRow          bool
	inCell         bool
	inText         bool
	skipDepth      int
	paragraph      docxParagraphDraft
	cellParagraphs []string
	rowCells       []string
	tableRows      [][]string
}

func (s *docxParserState) handleStart(start xml.StartElement, document *docxDocument) {
	if isDOCXTrackedChangeElement(start.Name.Local) {
		document.trackedChangeCount++
		s.skipDepth++
	}
	switch strings.ToLower(start.Name.Local) {
	case "tbl":
		s.inTable = true
		s.tableRows = nil
	case "tr":
		if s.inTable {
			s.inRow = true
			s.rowCells = nil
		}
	case "tc":
		if s.inTable && s.inRow {
			s.inCell = true
			s.cellParagraphs = nil
		}
	case "p":
		s.inParagraph = true
		s.paragraph = docxParagraphDraft{}
	case "pstyle":
		if s.inParagraph {
			s.paragraph.style.WriteString(xlsxXMLAttr(start, "val"))
		}
	case "t":
		if s.inParagraph && s.skipDepth == 0 {
			s.inText = true
		}
	}
}

func (s *docxParserState) handleEnd(end xml.EndElement, document *docxDocument) {
	switch strings.ToLower(end.Name.Local) {
	case "t":
		s.inText = false
	case "p":
		s.finishParagraph(document)
	case "tc":
		s.finishCell()
	case "tr":
		s.finishRow()
	case "tbl":
		s.finishTable(document)
	}
	if isDOCXTrackedChangeElement(end.Name.Local) && s.skipDepth > 0 {
		s.skipDepth--
	}
}

func (s *docxParserState) handleCharData(value []byte) {
	if s.inText {
		s.paragraph.text.Write(value)
	}
}

func (s *docxParserState) finishParagraph(document *docxDocument) {
	text := normalizeDOCXText(s.paragraph.text.String())
	style := s.paragraph.style.String()
	s.inParagraph = false
	if text == "" {
		return
	}
	if s.inTable && s.inCell {
		s.cellParagraphs = append(s.cellParagraphs, text)
		return
	}
	s.blockOrdinal++
	if level := docxHeadingLevel(style); level > 0 {
		s.currentSection = len(document.sections)
		document.sections = append(document.sections, docxSectionDraft{
			level:    level,
			heading:  text,
			anchor:   markdownAnchor(text),
			startRef: fmt.Sprintf("block:%d", s.blockOrdinal),
			endRef:   fmt.Sprintf("block:%d", s.blockOrdinal),
		})
		return
	}
	index := s.ensureSection(document)
	section := &document.sections[index]
	section.content = append(section.content, text)
	section.paragraphCount++
	section.endRef = fmt.Sprintf("block:%d", s.blockOrdinal)
	document.paragraphCount++
}

func (s *docxParserState) finishCell() {
	if s.inCell {
		s.rowCells = append(s.rowCells, strings.Join(s.cellParagraphs, " "))
	}
	s.inCell = false
	s.cellParagraphs = nil
}

func (s *docxParserState) finishRow() {
	if s.inRow {
		s.tableRows = append(s.tableRows, append([]string{}, s.rowCells...))
	}
	s.inRow = false
	s.rowCells = nil
}

func (s *docxParserState) finishTable(document *docxDocument) {
	s.inTable = false
	if len(s.tableRows) == 0 {
		return
	}
	s.blockOrdinal++
	index := s.ensureSection(document)
	section := &document.sections[index]
	for i, row := range s.tableRows {
		section.content = append(section.content, "table row "+strconv.Itoa(i+1)+": "+strings.Join(row, " | "))
	}
	section.tableCount++
	section.tableRowCount += len(s.tableRows)
	section.endRef = fmt.Sprintf("block:%d", s.blockOrdinal)
	document.tableCount++
	document.tableRowCount += len(s.tableRows)
	s.tableRows = nil
}

func (s *docxParserState) ensureSection(document *docxDocument) int {
	if s.currentSection >= 0 {
		return s.currentSection
	}
	s.currentSection = len(document.sections)
	document.sections = append(document.sections, docxSectionDraft{
		level:    1,
		heading:  "Document",
		anchor:   "body",
		startRef: fmt.Sprintf("block:%d", s.blockOrdinal),
		endRef:   fmt.Sprintf("block:%d", s.blockOrdinal),
	})
	return s.currentSection
}

func countDOCXComments(body []byte) (int, error) {
	if len(body) == 0 {
		return 0, nil
	}
	decoder := xml.NewDecoder(bytes.NewReader(body))
	count := 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return count, nil
		}
		if err != nil {
			return 0, err
		}
		start, ok := token.(xml.StartElement)
		if ok && strings.EqualFold(start.Name.Local, "comment") {
			count++
		}
	}
}

func docxHeadingLevel(style string) int {
	compact := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(style), " ", ""))
	if !strings.HasPrefix(compact, "heading") {
		return 0
	}
	level, err := strconv.Atoi(strings.TrimPrefix(compact, "heading"))
	if err != nil || level < 1 || level > 6 {
		return 0
	}
	return level
}

func isDOCXTrackedChangeElement(name string) bool {
	switch strings.ToLower(name) {
	case "ins", "del", "movefrom", "moveto":
		return true
	default:
		return false
	}
}

func normalizeDOCXText(value string) string {
	return strings.TrimSpace(strings.Join(strings.Fields(strings.ToValidUTF8(value, "")), " "))
}
