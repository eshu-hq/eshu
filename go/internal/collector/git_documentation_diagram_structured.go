// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"encoding/json"
	"encoding/xml"
	"html"
	"io"
	"regexp"
	"strings"
)

var (
	diagramEmailPattern   = regexp.MustCompile(`(?i)\b[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}\b`)
	diagramDomainPattern  = regexp.MustCompile(`(?i)\b[a-z0-9](?:[a-z0-9\-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9\-]{0,61}[a-z0-9])?)+\b`)
	drawIOTagPattern      = regexp.MustCompile(`<[^>]+>`)
	plantUMLLinkPattern   = regexp.MustCompile(`\[\[([^\]\s]+)(?:\s+([^\]]+))?\]\]`)
	plantUMLKeywordPrefix = []string{
		"actor ",
		"agent ",
		"artifact ",
		"boundary ",
		"class ",
		"cloud ",
		"collections ",
		"component ",
		"control ",
		"database ",
		"entity ",
		"folder ",
		"frame ",
		"interface ",
		"node ",
		"package ",
		"participant ",
		"queue ",
		"rectangle ",
		"storage ",
		"usecase ",
	}
)

func extractPlantUMLDiagramContent(body string) diagramExtraction {
	result := diagramExtraction{}
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "'") {
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "@start") || strings.HasPrefix(lower, "@end") {
			result.valid = true
			continue
		}
		if links := plantUMLLinks(line); len(links) > 0 {
			result.valid = true
			result.links = append(result.links, links...)
		}
		if strings.Contains(line, "->") || strings.Contains(line, "<-") || strings.Contains(line, "--") {
			result.valid = true
			if _, label, ok := strings.Cut(line, ":"); ok {
				if label := cleanDiagramLabel(label); label != "" {
					result.labels = append(result.labels, label)
				}
			}
		}
		for _, quoted := range quotedDiagramStrings(line) {
			if label := cleanDiagramLabel(quoted); label != "" {
				result.labels = append(result.labels, label)
			}
		}
		if label := plantUMLDeclarationLabel(line); label != "" {
			result.valid = true
			result.labels = append(result.labels, label)
		}
	}
	result.labels = uniqueDiagramValues(result.labels)
	result.links = uniqueDiagramLinks(result.links)
	return result
}

func plantUMLLinks(line string) []diagramLink {
	matches := plantUMLLinkPattern.FindAllStringSubmatch(line, -1)
	links := make([]diagramLink, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		target := cleanDiagramLinkTarget(match[1])
		if target == "" {
			continue
		}
		anchor := target
		if len(match) > 2 {
			anchor = cleanDiagramLabel(match[2])
		}
		links = append(links, diagramLink{target: target, anchor: firstNonEmptyString(anchor, target)})
	}
	return links
}

func plantUMLDeclarationLabel(line string) string {
	lower := strings.ToLower(line)
	for _, prefix := range plantUMLKeywordPrefix {
		if !strings.HasPrefix(lower, prefix) {
			continue
		}
		value := strings.TrimSpace(line[len(prefix):])
		if strings.HasPrefix(value, "\"") {
			return ""
		}
		if before, _, ok := strings.Cut(value, " as "); ok {
			value = before
		}
		return cleanDiagramLabel(value)
	}
	return ""
}

func extractDrawIODiagramContent(body string) diagramExtraction {
	result := diagramExtraction{}
	decoder := xml.NewDecoder(strings.NewReader(body))
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return diagramExtraction{}
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		if strings.EqualFold(start.Name.Local, "mxfile") || strings.EqualFold(start.Name.Local, "diagram") {
			result.valid = true
		}
		label := ""
		link := ""
		for _, attr := range start.Attr {
			switch strings.ToLower(attr.Name.Local) {
			case "name", "label", "value":
				label = firstNonEmptyString(label, drawIOLabel(attr.Value))
			case "link", "href":
				link = cleanDiagramLinkTarget(attr.Value)
			}
		}
		if label != "" {
			result.labels = append(result.labels, label)
		}
		if link != "" {
			result.links = append(result.links, diagramLink{target: link, anchor: firstNonEmptyString(label, link)})
		}
	}
	result.labels = uniqueDiagramValues(result.labels)
	result.links = uniqueDiagramLinks(result.links)
	return result
}

func drawIOLabel(value string) string {
	value = html.UnescapeString(value)
	value = drawIOTagPattern.ReplaceAllString(value, " ")
	return cleanDiagramLabel(value)
}

type excalidrawDocument struct {
	Type     string              `json:"type"`
	Elements []excalidrawElement `json:"elements"`
}

type excalidrawElement struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Text string `json:"text"`
	Link string `json:"link"`
}

func extractExcalidrawDiagramContent(body string) diagramExtraction {
	var document excalidrawDocument
	if err := json.Unmarshal([]byte(body), &document); err != nil {
		return diagramExtraction{}
	}
	result := diagramExtraction{valid: document.Type == "excalidraw" || len(document.Elements) > 0}
	for _, element := range document.Elements {
		label := cleanDiagramLabel(element.Text)
		if label != "" {
			result.labels = append(result.labels, label)
		}
		if target := cleanDiagramLinkTarget(element.Link); target != "" {
			result.links = append(result.links, diagramLink{target: target, anchor: firstNonEmptyString(label, target)})
		}
	}
	result.labels = uniqueDiagramValues(result.labels)
	result.links = uniqueDiagramLinks(result.links)
	return result
}

func extractSVGDiagramContent(body string) diagramExtraction {
	result := diagramExtraction{}
	decoder := xml.NewDecoder(strings.NewReader(body))
	linkStack := []string{}
	textDepth := 0
	var text strings.Builder
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return diagramExtraction{}
		}
		switch typed := token.(type) {
		case xml.StartElement:
			local := strings.ToLower(typed.Name.Local)
			if local == "svg" {
				result.valid = true
			}
			if local == "a" {
				linkStack = append(linkStack, svgLinkTarget(typed))
			}
			if local == "text" {
				textDepth = 1
				text.Reset()
				continue
			}
			if local == "tspan" && textDepth > 0 {
				textDepth++
			}
		case xml.EndElement:
			local := strings.ToLower(typed.Name.Local)
			if local == "text" && textDepth > 0 {
				label := cleanDiagramLabel(text.String())
				if label != "" {
					result.labels = append(result.labels, label)
					if target := lastDiagramLink(linkStack); target != "" {
						result.links = append(result.links, diagramLink{target: target, anchor: label})
					}
				}
				textDepth = 0
				text.Reset()
				continue
			}
			if local == "tspan" && textDepth > 1 {
				textDepth--
			}
			if local == "a" && len(linkStack) > 0 {
				linkStack = linkStack[:len(linkStack)-1]
			}
		case xml.CharData:
			if textDepth > 0 {
				text.Write([]byte(typed))
				text.WriteByte(' ')
			}
		}
	}
	result.labels = uniqueDiagramValues(result.labels)
	result.links = uniqueDiagramLinks(result.links)
	return result
}

func svgLinkTarget(start xml.StartElement) string {
	for _, attr := range start.Attr {
		if strings.EqualFold(attr.Name.Local, "href") {
			return cleanDiagramLinkTarget(attr.Value)
		}
	}
	return ""
}

func lastDiagramLink(values []string) string {
	for i := len(values) - 1; i >= 0; i-- {
		if values[i] != "" {
			return values[i]
		}
	}
	return ""
}

func containsSensitiveDiagramText(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	lower := strings.ToLower(value)
	if strings.Contains(lower, "://") ||
		strings.Contains(lower, "token=") ||
		strings.Contains(lower, "credential_marker") ||
		strings.Contains(lower, "api_key") ||
		strings.Contains(lower, "access_token") ||
		strings.Contains(lower, "auth_token") ||
		strings.Contains(lower, "password=") ||
		strings.Contains(lower, "passwd=") {
		return true
	}
	return diagramEmailPattern.MatchString(value) || diagramDomainPattern.MatchString(value)
}

func containsSensitiveDiagramLinkTarget(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	for _, marker := range []string{
		"token=",
		"credential_marker",
		"api_key",
		"access_token",
		"auth_token",
		"password=",
		"passwd=",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return diagramEmailPattern.MatchString(value)
}
