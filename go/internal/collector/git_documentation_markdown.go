// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

var markdownLinkPattern = regexp.MustCompile(`!?\[([^\]]+)\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)

func markdownContentLines(body string) []markdownLine {
	rawLines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	start := 0
	if len(rawLines) > 0 && strings.TrimSpace(rawLines[0]) == "---" {
		for i := 1; i < len(rawLines); i++ {
			if strings.TrimSpace(rawLines[i]) == "---" {
				start = i + 1
				break
			}
		}
	}
	lines := make([]markdownLine, 0, len(rawLines)-start)
	inFence := false
	for i := start; i < len(rawLines); i++ {
		line := rawLines[i]
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		lines = append(lines, markdownLine{Number: i + 1, Text: line})
	}
	return lines
}

func markdownSections(
	documentID string,
	revisionID string,
	relativePath string,
	lines []markdownLine,
	contentFormat string,
) []facts.DocumentationSectionPayload {
	var drafts []markdownSectionDraft
	current := -1
	for _, line := range lines {
		level, heading, ok := markdownHeading(line.Text)
		if ok {
			drafts = append(drafts, markdownSectionDraft{
				level:    level,
				heading:  heading,
				anchor:   markdownAnchor(heading),
				startRef: fmt.Sprintf("line:%d", line.Number),
				endRef:   fmt.Sprintf("line:%d", line.Number),
			})
			current = len(drafts) - 1
			continue
		}
		if current >= 0 {
			drafts[current].content = append(drafts[current].content, line.Text)
			drafts[current].endRef = fmt.Sprintf("line:%d", line.Number)
			continue
		}
		if strings.TrimSpace(line.Text) != "" {
			drafts = append(drafts, markdownSectionDraft{
				level:    1,
				heading:  documentationTitle(relativePath, nil),
				anchor:   "body",
				startRef: fmt.Sprintf("line:%d", line.Number),
				endRef:   fmt.Sprintf("line:%d", line.Number),
				content:  []string{line.Text},
			})
			current = len(drafts) - 1
		}
	}
	return documentationSectionsFromDrafts(documentID, revisionID, relativePath, contentFormat, drafts)
}

func documentationSectionsFromDrafts(
	documentID string,
	revisionID string,
	relativePath string,
	contentFormat string,
	drafts []markdownSectionDraft,
) []facts.DocumentationSectionPayload {
	sections := make([]facts.DocumentationSectionPayload, 0, len(drafts))
	parentByLevel := map[int]string{}
	anchorCounts := map[string]int{}
	for i, draft := range drafts {
		content, contentWarnings := boundedDocumentationSectionContent(strings.TrimSpace(strings.Join(draft.content, "\n")))
		warnings := append([]string{}, draft.warnings...)
		warnings = append(warnings, contentWarnings...)
		anchor := firstNonEmptyString(draft.anchor, fmt.Sprintf("%d", i+1))
		anchorCounts[anchor]++
		if anchorCounts[anchor] > 1 {
			anchor = fmt.Sprintf("%s-%d", anchor, anchorCounts[anchor])
		}
		sectionID := "section:" + anchor
		section := facts.DocumentationSectionPayload{
			DocumentID:       documentID,
			RevisionID:       revisionID,
			SectionID:        sectionID,
			SectionAnchor:    anchor,
			HeadingText:      draft.heading,
			OrdinalPath:      []int{i + 1},
			Content:          content,
			ContentFormat:    contentFormat,
			TextHash:         documentationHashText(strings.TrimSpace(draft.heading + "\n" + content)),
			ExcerptHash:      documentationHashText(content),
			SourceStartRef:   draft.startRef,
			SourceEndRef:     draft.endRef,
			SourceMetadata:   documentationSectionMetadata(relativePath, draft.sourceMetadata),
			ContainsWarnings: len(warnings) > 0,
		}
		addDocumentationWarnings(section.SourceMetadata, warnings...)
		for level := draft.level - 1; level >= 1; level-- {
			if parent := parentByLevel[level]; parent != "" {
				section.ParentSectionID = parent
				break
			}
		}
		parentByLevel[draft.level] = sectionID
		for level := draft.level + 1; level <= 6; level++ {
			delete(parentByLevel, level)
		}
		sections = append(sections, section)
	}
	return sections
}

func documentationSectionMetadata(relativePath string, extra map[string]string) map[string]string {
	metadata := map[string]string{"path": relativePath}
	for key, value := range extra {
		value = strings.TrimSpace(value)
		if value != "" {
			metadata[key] = value
		}
	}
	return metadata
}

func markdownLinks(relativePath string, sections []facts.DocumentationSectionPayload) []facts.DocumentationLinkPayload {
	links := []facts.DocumentationLinkPayload{}
	for _, section := range sections {
		for _, match := range markdownLinkPattern.FindAllStringSubmatch(section.Content, -1) {
			if strings.HasPrefix(match[0], "!") {
				continue
			}
			target := strings.TrimSpace(match[2])
			if target == "" {
				continue
			}
			links = append(links, facts.DocumentationLinkPayload{
				DocumentID:     section.DocumentID,
				RevisionID:     section.RevisionID,
				SectionID:      section.SectionID,
				LinkID:         fmt.Sprintf("link:%s:%d", section.SectionID, len(links)+1),
				TargetURI:      target,
				TargetKind:     documentationLinkTargetKind(target),
				AnchorTextHash: documentationHashText(match[1]),
				SourceMetadata: map[string]string{"path": relativePath},
			})
		}
	}
	return links
}

func markdownHeading(line string) (int, string, bool) {
	trimmed := strings.TrimLeft(line, " \t")
	level := 0
	for level < len(trimmed) && level < 6 && trimmed[level] == '#' {
		level++
	}
	if level == 0 || level >= len(trimmed) || trimmed[level] != ' ' {
		return 0, "", false
	}
	heading := strings.TrimSpace(trimmed[level+1:])
	if heading == "" {
		return 0, "", false
	}
	return level, heading, true
}

func documentationTitle(relativePath string, sections []facts.DocumentationSectionPayload) string {
	for _, section := range sections {
		if section.HeadingText != "" {
			return section.HeadingText
		}
	}
	base := path.Base(relativePath)
	ext := path.Ext(base)
	return strings.TrimSuffix(base, ext)
}

func markdownAnchor(heading string) string {
	anchor := strings.ToLower(strings.TrimSpace(heading))
	replacer := strings.NewReplacer("`", "", "'", "", "\"", "", ".", "", ",", "", ":", "", "/", "-")
	anchor = replacer.Replace(anchor)
	fields := strings.Fields(anchor)
	return strings.Join(fields, "-")
}

func documentationDocumentType(relativePath string, fallback string) string {
	base := strings.ToLower(path.Base(relativePath))
	cleanPath := strings.ToLower(relativePath)
	switch {
	case strings.HasPrefix(base, "readme."):
		return "readme"
	case strings.Contains(cleanPath, "/adr") || strings.Contains(cleanPath, "architecture decision"):
		return "adr"
	case strings.Contains(cleanPath, "runbook"):
		return "runbook"
	default:
		return fallback
	}
}

func documentationLinkTargetKind(target string) string {
	parsed, err := url.Parse(target)
	if err == nil && parsed.Scheme != "" {
		return parsed.Scheme
	}
	return "source_path"
}

type markdownLine struct {
	Number int
	Text   string
}

type markdownSectionDraft struct {
	level          int
	heading        string
	anchor         string
	startRef       string
	endRef         string
	content        []string
	warnings       []string
	sourceMetadata map[string]string
}
