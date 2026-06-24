// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"bytes"
	"context"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/diagrampreflight"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

const (
	malformedDiagramWarning        = "malformed_media"
	incidentMediaClassDiagramLabel = "diagram_label"
)

var (
	mermaidBracketLabelPattern = regexp.MustCompile(`[\[\(\{]([^\]\)\}]+)[\]\)\}]`)
	mermaidEdgeLabelPattern    = regexp.MustCompile(`\|([^|]+)\|`)
	diagramHeaderPrefixes      = []string{
		"flowchart ",
		"graph ",
		"sequencediagram",
		"classdiagram",
		"statediagram",
		"erdiagram",
		"journey",
		"gantt",
		"pie",
		"mindmap",
		"timeline",
	}
)

type diagramExtraction struct {
	labels []string
	links  []diagramLink
	valid  bool
}

type diagramLink struct {
	target string
	anchor string
}

func extractDiagramDocumentation(
	ctx context.Context,
	repo repositoryidentity.Metadata,
	relativePath string,
	digest string,
	commitSHA string,
	body []byte,
	format string,
) (facts.DocumentationDocumentPayload, []facts.DocumentationSectionPayload, []facts.DocumentationLinkPayload) {
	if ctx == nil {
		ctx = context.Background()
	}
	revisionID := firstNonEmptyString(commitSHA, digest, "unknown")
	documentID := gitDocumentationDocumentID(repo.ID, relativePath)
	bodyText, bodyWarnings := boundedDocumentationBody(body)
	document := facts.DocumentationDocumentPayload{
		SourceID:     gitDocumentationSourceID(repo.ID),
		DocumentID:   documentID,
		ExternalID:   relativePath,
		RevisionID:   revisionID,
		CanonicalURI: gitDocumentationCanonicalURI(repo, relativePath, commitSHA),
		Title:        documentationTitle(relativePath, nil),
		DocumentType: "diagram",
		Format:       format,
		Language:     format,
		ContentHash:  firstNonEmptyString(digest, documentationHashText(bodyText)),
		SourceMetadata: map[string]string{
			"path":                        relativePath,
			"repo_id":                     repo.ID,
			"format_family":               "diagram",
			"incident_media_source_class": incidentMediaClassDiagramLabel,
			"diagram_format":              format,
		},
	}
	if commitSHA != "" {
		document.SourceMetadata["source_revision"] = commitSHA
	}
	addDocumentationWarnings(document.SourceMetadata, bodyWarnings...)

	preflight, err := diagrampreflight.Preflight(
		ctx,
		path.Base(relativePath),
		bytes.NewReader(body),
		int64(len(body)),
		diagrampreflight.Options{MaxSourceBytes: int64(documentationMaxBodyBytes)},
	)
	recordDiagramPreflightMetadata(document.SourceMetadata, preflight)
	addDocumentationWarnings(document.SourceMetadata, diagramPreflightWarnings(preflight)...)
	if err != nil || !preflight.Safe {
		return document, nil, nil
	}

	extraction := extractTextDiagramContent(bodyText, format)
	if !extraction.valid || len(extraction.labels) == 0 {
		addDocumentationWarnings(document.SourceMetadata, malformedDiagramWarning)
		return document, nil, nil
	}
	sections := diagramDocumentationSections(documentID, revisionID, relativePath, format, extraction.labels)
	links := diagramDocumentationLinks(relativePath, revisionID, sections[0], extraction.links)
	return document, sections, links
}

func recordDiagramPreflightMetadata(metadata map[string]string, result diagrampreflight.Result) {
	if result.Format != "" {
		metadata["preflight_format"] = result.Format
	}
	metadata["preflight_source_bytes"] = strconv.FormatInt(result.SourceBytes, 10)
	metadata["preflight_element_count"] = strconv.Itoa(result.ElementCount)
	if result.ExternalReferenceCount > 0 {
		metadata["preflight_external_reference_count"] = strconv.Itoa(result.ExternalReferenceCount)
	}
	if result.IncludeCount > 0 {
		metadata["preflight_include_count"] = strconv.Itoa(result.IncludeCount)
	}
	if result.ActiveContentCount > 0 {
		metadata["preflight_active_content_count"] = strconv.Itoa(result.ActiveContentCount)
	}
	if result.SensitiveValueCount > 0 {
		metadata["preflight_sensitive_value_count"] = strconv.Itoa(result.SensitiveValueCount)
	}
}

func diagramPreflightWarnings(result diagrampreflight.Result) []string {
	warnings := make([]string, 0, len(result.Warnings))
	for _, warning := range result.Warnings {
		if warning.Class != "" {
			warnings = append(warnings, string(warning.Class))
		}
	}
	return warnings
}

func extractTextDiagramContent(body string, format string) diagramExtraction {
	switch format {
	case "mermaid":
		return extractMermaidDiagramContent(body)
	case "d2":
		return extractD2DiagramContent(body)
	case "plantuml":
		return extractPlantUMLDiagramContent(body)
	case "drawio":
		return extractDrawIODiagramContent(body)
	case "excalidraw":
		return extractExcalidrawDiagramContent(body)
	case "svg":
		return extractSVGDiagramContent(body)
	default:
		return diagramExtraction{}
	}
}

func extractMermaidDiagramContent(body string) diagramExtraction {
	result := diagramExtraction{}
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "%%") {
			continue
		}
		lower := strings.ToLower(line)
		if hasDiagramHeader(lower) {
			result.valid = true
			continue
		}
		if strings.HasPrefix(lower, "click ") {
			result.valid = true
			if link, ok := mermaidClickLink(line); ok {
				result.links = append(result.links, link)
			}
			continue
		}
		if strings.Contains(line, "--") || strings.Contains(line, "==>") || strings.Contains(line, "-.") {
			result.valid = true
		}
		result.labels = append(result.labels, regexpLabels(line, mermaidBracketLabelPattern)...)
		result.labels = append(result.labels, regexpLabels(line, mermaidEdgeLabelPattern)...)
	}
	result.labels = uniqueDiagramValues(result.labels)
	result.links = uniqueDiagramLinks(result.links)
	return result
}

func extractD2DiagramContent(body string) diagramExtraction {
	result := diagramExtraction{}
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, ".link:") {
			result.valid = true
			if link, ok := d2Link(line); ok {
				result.links = append(result.links, link)
			}
			continue
		}
		if strings.Contains(line, "->") || strings.Contains(line, "<-") {
			result.valid = true
		}
		if before, after, ok := strings.Cut(line, ":"); ok {
			if strings.TrimSpace(before) != "" {
				result.valid = true
			}
			label := cleanDiagramLabel(after)
			if label != "" {
				result.labels = append(result.labels, label)
			}
		}
	}
	result.labels = uniqueDiagramValues(result.labels)
	result.links = uniqueDiagramLinks(result.links)
	return result
}

func hasDiagramHeader(line string) bool {
	for _, prefix := range diagramHeaderPrefixes {
		if strings.HasPrefix(line, prefix) || line == prefix {
			return true
		}
	}
	return false
}

func regexpLabels(line string, pattern *regexp.Regexp) []string {
	matches := pattern.FindAllStringSubmatch(line, -1)
	labels := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		if label := cleanDiagramLabel(match[1]); label != "" {
			labels = append(labels, label)
		}
	}
	return labels
}

func mermaidClickLink(line string) (diagramLink, bool) {
	quoted := quotedDiagramStrings(line)
	if len(quoted) == 0 {
		return diagramLink{}, false
	}
	target := cleanDiagramLinkTarget(quoted[0])
	if target == "" {
		return diagramLink{}, false
	}
	anchor := target
	if len(quoted) > 1 {
		anchor = cleanDiagramLabel(quoted[1])
	}
	return diagramLink{target: target, anchor: firstNonEmptyString(anchor, target)}, true
}

func d2Link(line string) (diagramLink, bool) {
	_, target, ok := strings.Cut(line, ":")
	if !ok {
		return diagramLink{}, false
	}
	target = cleanDiagramLinkTarget(target)
	if target == "" {
		return diagramLink{}, false
	}
	return diagramLink{target: target, anchor: target}, true
}

func quotedDiagramStrings(line string) []string {
	values := []string{}
	rest := line
	for {
		start := strings.Index(rest, "\"")
		if start < 0 {
			return values
		}
		rest = rest[start+1:]
		end := strings.Index(rest, "\"")
		if end < 0 {
			return values
		}
		values = append(values, rest[:end])
		rest = rest[end+1:]
	}
}

func cleanDiagramLabel(raw string) string {
	label := cleanDiagramText(raw)
	if containsSensitiveDiagramText(label) {
		return ""
	}
	return label
}

func cleanDiagramLinkTarget(raw string) string {
	target := normalizeDocumentationURL(cleanDiagramText(raw))
	lower := strings.ToLower(target)
	cleanPath := path.Clean(target)
	if target == "" ||
		containsSensitiveDiagramLinkTarget(target) ||
		strings.Contains(target, "\\") ||
		strings.Contains(target, ":") ||
		strings.HasPrefix(cleanPath, "/") ||
		cleanPath == "." ||
		cleanPath == ".." ||
		strings.HasPrefix(cleanPath, "../") ||
		strings.Contains(lower, "://") ||
		strings.HasPrefix(lower, "//") ||
		strings.HasPrefix(lower, "javascript:") {
		return ""
	}
	return target
}

func cleanDiagramText(raw string) string {
	label := strings.TrimSpace(raw)
	label = strings.Trim(label, "\"'`")
	return strings.Join(strings.Fields(label), " ")
}

func uniqueDiagramValues(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = cleanDiagramLabel(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func uniqueDiagramLinks(links []diagramLink) []diagramLink {
	out := make([]diagramLink, 0, len(links))
	seen := map[string]bool{}
	for _, link := range links {
		link.target = cleanDiagramLinkTarget(link.target)
		link.anchor = cleanDiagramLabel(firstNonEmptyString(link.anchor, link.target))
		key := link.target + "\x00" + link.anchor
		if link.target == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, link)
	}
	return out
}

func diagramDocumentationSections(
	documentID string,
	revisionID string,
	relativePath string,
	format string,
	labels []string,
) []facts.DocumentationSectionPayload {
	draft := markdownSectionDraft{
		level:    1,
		heading:  documentationTitle(relativePath, nil),
		anchor:   "diagram",
		startRef: "diagram:text",
		endRef:   "diagram:text",
		content:  labels,
		sourceMetadata: map[string]string{
			"format_family":               "diagram",
			"incident_media_source_class": incidentMediaClassDiagramLabel,
			"diagram_format":              format,
		},
	}
	return documentationSectionsFromDrafts(documentID, revisionID, relativePath, format, []markdownSectionDraft{draft})
}

func diagramDocumentationLinks(
	relativePath string,
	revisionID string,
	section facts.DocumentationSectionPayload,
	links []diagramLink,
) []facts.DocumentationLinkPayload {
	out := make([]facts.DocumentationLinkPayload, 0, len(links))
	for _, link := range links {
		out = append(out, facts.DocumentationLinkPayload{
			DocumentID:     section.DocumentID,
			RevisionID:     revisionID,
			SectionID:      section.SectionID,
			LinkID:         fmt.Sprintf("link:%s:%d", section.SectionID, len(out)+1),
			TargetURI:      link.target,
			TargetKind:     documentationLinkTargetKind(link.target),
			AnchorTextHash: documentationHashText(firstNonEmptyString(link.anchor, link.target)),
			SourceMetadata: map[string]string{
				"path":          relativePath,
				"format_family": "diagram",
			},
		})
	}
	return out
}
