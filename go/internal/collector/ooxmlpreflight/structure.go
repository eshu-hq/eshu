// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ooxmlpreflight

import (
	"encoding/xml"
	"strings"
)

func (r *recorder) classifyStructurePartName(lower string) {
	switch r.result.Format {
	case FormatDOCX:
		switch {
		case isDOCXAnnotationPart(lower):
			r.result.AnnotationPartCount++
			r.warnOnce(WarningAnnotationTextSkipped)
		case strings.HasPrefix(lower, "word/media/"):
			r.result.ImagePartCount++
		}
	case FormatXLSX:
		switch {
		case isXLSXWorksheetPart(lower):
			r.result.WorksheetPartCount++
		case lower == "xl/sharedstrings.xml":
			r.result.SharedStringPartCount++
		case isXLSXAnnotationPart(lower):
			r.result.AnnotationPartCount++
			r.warnOnce(WarningAnnotationTextSkipped)
		}
	case FormatPPTX:
		switch {
		case isPPTXSlidePart(lower):
			r.result.SlidePartCount++
		case isPPTXNotesPart(lower):
			r.result.NotesPartCount++
		case isPPTXAnnotationPart(lower):
			r.result.AnnotationPartCount++
			r.warnOnce(WarningAnnotationTextSkipped)
		case strings.HasPrefix(lower, "ppt/media/"):
			r.result.MediaPartCount++
		}
	}
}

func shouldParseStructureXMLMetadata(lower string) bool {
	switch {
	case lower == "word/document.xml":
		return true
	case lower == "xl/workbook.xml" || isXLSXWorksheetPart(lower):
		return true
	case lower == "ppt/presentation.xml" || isPPTXSlidePart(lower):
		return true
	default:
		return false
	}
}

func (r *recorder) classifyStructureXMLElement(start xml.StartElement) {
	local := strings.ToLower(start.Name.Local)
	switch r.result.Format {
	case FormatDOCX:
		r.classifyDOCXStructureElement(local)
	case FormatXLSX:
		r.classifyXLSXStructureElement(local, start)
	case FormatPPTX:
		r.classifyPPTXStructureElement(local, start)
	}
}

func (r *recorder) classifyDOCXStructureElement(local string) {
	switch local {
	case "tbl":
		r.result.TableMarkerCount++
	case "ins", "del", "movefrom", "moveto":
		r.result.TrackedChangeMarkerCount++
		r.warnOnce(WarningAnnotationTextSkipped)
	case "vanish":
		r.result.HiddenContentCount++
		r.warnOnce(WarningHiddenContentSkipped)
	}
}

func (r *recorder) classifyXLSXStructureElement(local string, start xml.StartElement) {
	switch local {
	case "f":
		r.result.FormulaMarkerCount++
	case "sheet":
		if attrIsAny(start, "state", "hidden", "veryHidden") {
			r.result.HiddenContentCount++
			r.warnOnce(WarningHiddenContentSkipped)
		}
	}
}

func (r *recorder) classifyPPTXStructureElement(local string, start xml.StartElement) {
	switch local {
	case "sld", "sldid":
		if attrIsAny(start, "show", "0", "false") || attrIsAny(start, "hidden", "1", "true") {
			r.result.HiddenContentCount++
			r.warnOnce(WarningHiddenContentSkipped)
		}
	}
}

func isDOCXAnnotationPart(lower string) bool {
	return strings.HasPrefix(lower, "word/comments") && strings.HasSuffix(lower, ".xml")
}

func isXLSXWorksheetPart(lower string) bool {
	return strings.HasPrefix(lower, "xl/worksheets/") && strings.HasSuffix(lower, ".xml")
}

func isXLSXAnnotationPart(lower string) bool {
	return (strings.HasPrefix(lower, "xl/comments") || strings.HasPrefix(lower, "xl/threadedcomments/")) &&
		strings.HasSuffix(lower, ".xml")
}

func isPPTXSlidePart(lower string) bool {
	return strings.HasPrefix(lower, "ppt/slides/slide") && strings.HasSuffix(lower, ".xml")
}

func isPPTXNotesPart(lower string) bool {
	return strings.HasPrefix(lower, "ppt/notesslides/") && strings.HasSuffix(lower, ".xml")
}

func isPPTXAnnotationPart(lower string) bool {
	return (strings.HasPrefix(lower, "ppt/comments/") || lower == "ppt/commentauthors.xml") &&
		strings.HasSuffix(lower, ".xml")
}

func attrIsAny(start xml.StartElement, name string, values ...string) bool {
	for _, attr := range start.Attr {
		if !strings.EqualFold(attr.Name.Local, name) {
			continue
		}
		for _, value := range values {
			if strings.EqualFold(attr.Value, value) {
				return true
			}
		}
	}
	return false
}
