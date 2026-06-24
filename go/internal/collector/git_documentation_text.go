// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

const documentationFallbackChunkLines = 80

var (
	asciidocLinkPattern = regexp.MustCompile(`(?:link:)?(https?://[^\s\[]+)\[([^\]]*)\]`)
	bareURLPattern      = regexp.MustCompile(`https?://[^\s<>()\[\]]+`)
)

func extractTextDocumentation(
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
	lines := plainDocumentationLines(bodyText)
	sections := textSections(documentID, revisionID, relativePath, lines, format)
	title := documentationTitle(relativePath, sections)
	document := facts.DocumentationDocumentPayload{
		SourceID:     gitDocumentationSourceID(repo.ID),
		DocumentID:   documentID,
		ExternalID:   relativePath,
		RevisionID:   revisionID,
		CanonicalURI: gitDocumentationCanonicalURI(repo, relativePath, commitSHA),
		Title:        title,
		DocumentType: documentationDocumentType(relativePath, format),
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
	return document, sections, textDocumentationLinks(relativePath, sections)
}

func plainDocumentationLines(body string) []markdownLine {
	rawLines := strings.Split(body, "\n")
	lines := make([]markdownLine, 0, len(rawLines))
	for i, line := range rawLines {
		lines = append(lines, markdownLine{Number: i + 1, Text: line})
	}
	return lines
}

func textSections(
	documentID string,
	revisionID string,
	relativePath string,
	lines []markdownLine,
	format string,
) []facts.DocumentationSectionPayload {
	switch format {
	case "asciidoc":
		return asciidocSections(documentID, revisionID, relativePath, lines, format)
	case "restructuredtext":
		return rstSections(documentID, revisionID, relativePath, lines, format)
	case "text":
		return plainTextSections(documentID, revisionID, relativePath, lines, format)
	default:
		return fallbackTextSections(documentID, revisionID, relativePath, lines, format)
	}
}

func plainTextSections(
	documentID string,
	revisionID string,
	relativePath string,
	lines []markdownLine,
	format string,
) []facts.DocumentationSectionPayload {
	heading := documentationTitle(relativePath, nil)
	headingLine := 0
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line.Text) == "" {
			continue
		}
		if i+1 < len(lines) && strings.TrimSpace(lines[i+1].Text) == "" {
			heading = strings.TrimSpace(line.Text)
			headingLine = line.Number
			start = i + 1
		}
		break
	}
	if start < 0 {
		return fallbackTextSectionsWithHeading(documentID, revisionID, relativePath, lines, format, heading)
	}
	content := []string{}
	endLine := headingLine
	for _, line := range lines[start:] {
		if strings.TrimSpace(line.Text) == "" && len(content) == 0 {
			continue
		}
		content = append(content, line.Text)
		endLine = line.Number
	}
	drafts := []markdownSectionDraft{{
		level:    1,
		heading:  heading,
		anchor:   markdownAnchor(heading),
		startRef: fmt.Sprintf("line:%d", headingLine),
		endRef:   fmt.Sprintf("line:%d", endLine),
		content:  content,
	}}
	return documentationSectionsFromDrafts(documentID, revisionID, relativePath, format, drafts)
}

func asciidocSections(
	documentID string,
	revisionID string,
	relativePath string,
	lines []markdownLine,
	format string,
) []facts.DocumentationSectionPayload {
	drafts := []markdownSectionDraft{}
	current := -1
	for _, line := range lines {
		if level, heading, ok := asciidocHeading(line.Text); ok {
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
		drafts, current = appendTextLineToDraft(drafts, current, relativePath, line)
		drafts = markUnsupportedDirective(drafts, current, line.Text)
	}
	return documentationSectionsFromDrafts(documentID, revisionID, relativePath, format, drafts)
}

func rstSections(
	documentID string,
	revisionID string,
	relativePath string,
	lines []markdownLine,
	format string,
) []facts.DocumentationSectionPayload {
	drafts := []markdownSectionDraft{}
	current := -1
	for i := 0; i < len(lines); i++ {
		if i+1 < len(lines) {
			if level, heading, ok := rstHeading(lines[i].Text, lines[i+1].Text); ok {
				drafts = append(drafts, markdownSectionDraft{
					level:    level,
					heading:  heading,
					anchor:   markdownAnchor(heading),
					startRef: fmt.Sprintf("line:%d", lines[i].Number),
					endRef:   fmt.Sprintf("line:%d", lines[i+1].Number),
				})
				current = len(drafts) - 1
				i++
				continue
			}
		}
		drafts, current = appendTextLineToDraft(drafts, current, relativePath, lines[i])
		drafts = markUnsupportedDirective(drafts, current, lines[i].Text)
	}
	return documentationSectionsFromDrafts(documentID, revisionID, relativePath, format, drafts)
}

func fallbackTextSections(
	documentID string,
	revisionID string,
	relativePath string,
	lines []markdownLine,
	format string,
) []facts.DocumentationSectionPayload {
	return fallbackTextSectionsWithHeading(
		documentID,
		revisionID,
		relativePath,
		lines,
		format,
		documentationTitle(relativePath, nil),
	)
}

func fallbackTextSectionsWithHeading(
	documentID string,
	revisionID string,
	relativePath string,
	lines []markdownLine,
	format string,
	heading string,
) []facts.DocumentationSectionPayload {
	drafts := []markdownSectionDraft{}
	var chunk *markdownSectionDraft
	for _, line := range lines {
		if strings.TrimSpace(line.Text) == "" && chunk == nil {
			continue
		}
		if chunk == nil || len(chunk.content) >= documentationFallbackChunkLines ||
			len(strings.Join(chunk.content, "\n")) >= documentationMaxSectionChars {
			if chunk != nil {
				drafts = append(drafts, *chunk)
			}
			anchor := "body"
			if len(drafts) > 0 {
				anchor = fmt.Sprintf("body-%d", len(drafts)+1)
			}
			chunk = &markdownSectionDraft{
				level:    1,
				heading:  heading,
				anchor:   anchor,
				startRef: fmt.Sprintf("line:%d", line.Number),
				endRef:   fmt.Sprintf("line:%d", line.Number),
			}
		}
		chunk.content = append(chunk.content, line.Text)
		chunk.endRef = fmt.Sprintf("line:%d", line.Number)
		if unsupportedDocumentationDirective(line.Text) {
			chunk.warnings = append(chunk.warnings, "unsupported_directive")
		}
	}
	if chunk != nil {
		drafts = append(drafts, *chunk)
	}
	return documentationSectionsFromDrafts(documentID, revisionID, relativePath, format, drafts)
}

func appendTextLineToDraft(
	drafts []markdownSectionDraft,
	current int,
	relativePath string,
	line markdownLine,
) ([]markdownSectionDraft, int) {
	if current < 0 {
		if strings.TrimSpace(line.Text) == "" {
			return drafts, current
		}
		drafts = append(drafts, markdownSectionDraft{
			level:    1,
			heading:  documentationTitle(relativePath, nil),
			anchor:   "body",
			startRef: fmt.Sprintf("line:%d", line.Number),
			endRef:   fmt.Sprintf("line:%d", line.Number),
			content:  []string{line.Text},
		})
		return drafts, len(drafts) - 1
	}
	drafts[current].content = append(drafts[current].content, line.Text)
	drafts[current].endRef = fmt.Sprintf("line:%d", line.Number)
	return drafts, current
}

func asciidocHeading(line string) (int, string, bool) {
	trimmed := strings.TrimSpace(line)
	level := 0
	for level < len(trimmed) && trimmed[level] == '=' {
		level++
	}
	if level == 0 || level >= len(trimmed) || trimmed[level] != ' ' {
		return 0, "", false
	}
	heading := strings.TrimSpace(trimmed[level+1:])
	return level, heading, heading != ""
}

func rstHeading(textLine string, underlineLine string) (int, string, bool) {
	heading := strings.TrimSpace(textLine)
	underline := strings.TrimSpace(underlineLine)
	if heading == "" || len(underline) < len(heading) {
		return 0, "", false
	}
	marker := rune(underline[0])
	for _, current := range underline {
		if current != marker {
			return 0, "", false
		}
	}
	switch marker {
	case '=':
		return 1, heading, true
	case '-':
		return 2, heading, true
	case '~':
		return 3, heading, true
	case '^':
		return 4, heading, true
	default:
		return 0, "", false
	}
}

func markUnsupportedDirective(
	drafts []markdownSectionDraft,
	current int,
	line string,
) []markdownSectionDraft {
	if current >= 0 && unsupportedDocumentationDirective(line) {
		drafts[current].warnings = append(drafts[current].warnings, "unsupported_directive")
	}
	return drafts
}

func unsupportedDocumentationDirective(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, ".. include::") || strings.HasPrefix(trimmed, "include::")
}

func textDocumentationLinks(
	relativePath string,
	sections []facts.DocumentationSectionPayload,
) []facts.DocumentationLinkPayload {
	links := []facts.DocumentationLinkPayload{}
	seen := map[string]bool{}
	for _, section := range sections {
		for _, match := range asciidocLinkPattern.FindAllStringSubmatch(section.Content, -1) {
			target := normalizeDocumentationURL(match[1])
			if target == "" || seen[section.SectionID+"|"+target] {
				continue
			}
			seen[section.SectionID+"|"+target] = true
			links = append(links, facts.DocumentationLinkPayload{
				DocumentID:     section.DocumentID,
				RevisionID:     section.RevisionID,
				SectionID:      section.SectionID,
				LinkID:         fmt.Sprintf("link:%s:%d", section.SectionID, len(links)+1),
				TargetURI:      target,
				TargetKind:     documentationLinkTargetKind(target),
				AnchorTextHash: documentationHashText(firstNonEmptyString(match[2], target)),
				SourceMetadata: map[string]string{"path": relativePath},
			})
		}
		for _, match := range bareURLPattern.FindAllString(section.Content, -1) {
			target := normalizeDocumentationURL(match)
			if target == "" || seen[section.SectionID+"|"+target] {
				continue
			}
			seen[section.SectionID+"|"+target] = true
			links = append(links, facts.DocumentationLinkPayload{
				DocumentID:     section.DocumentID,
				RevisionID:     section.RevisionID,
				SectionID:      section.SectionID,
				LinkID:         fmt.Sprintf("link:%s:%d", section.SectionID, len(links)+1),
				TargetURI:      target,
				TargetKind:     documentationLinkTargetKind(target),
				AnchorTextHash: documentationHashText(target),
				SourceMetadata: map[string]string{"path": relativePath},
			})
		}
	}
	return links
}

func normalizeDocumentationURL(target string) string {
	return strings.TrimRight(strings.TrimSpace(target), ".,;:")
}
