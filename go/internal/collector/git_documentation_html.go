// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"fmt"
	"strings"

	"golang.org/x/net/html"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

func extractHTMLDocumentation(
	repo repositoryidentity.Metadata,
	relativePath string,
	digest string,
	commitSHA string,
	body []byte,
) (facts.DocumentationDocumentPayload, []facts.DocumentationSectionPayload, []facts.DocumentationLinkPayload) {
	revisionID := firstNonEmptyString(commitSHA, digest, "unknown")
	documentID := gitDocumentationDocumentID(repo.ID, relativePath)
	bodyText, warnings := boundedDocumentationBody(body)
	warnings = append(warnings, htmlMalformedWarnings(bodyText)...)

	root, err := html.Parse(strings.NewReader(bodyText))
	if err != nil {
		warnings = append(warnings, "malformed_html")
	}
	drafts, linkDrafts := htmlSectionDrafts(root, relativePath, warnings)
	sections := documentationSectionsFromDrafts(documentID, revisionID, relativePath, "html", drafts)
	title := documentationTitle(relativePath, sections)
	document := facts.DocumentationDocumentPayload{
		SourceID:     gitDocumentationSourceID(repo.ID),
		DocumentID:   documentID,
		ExternalID:   relativePath,
		RevisionID:   revisionID,
		CanonicalURI: gitDocumentationCanonicalURI(repo, relativePath, commitSHA),
		Title:        title,
		DocumentType: documentationDocumentType(relativePath, "html"),
		Format:       "html",
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
	return document, sections, htmlDocumentationLinks(relativePath, sections, linkDrafts)
}

type htmlLinkDraft struct {
	sectionIndex int
	target       string
	anchorText   string
}

func htmlSectionDrafts(
	root *html.Node,
	relativePath string,
	warnings []string,
) ([]markdownSectionDraft, []htmlLinkDraft) {
	if root == nil {
		return nil, nil
	}
	drafts := []markdownSectionDraft{}
	links := []htmlLinkDraft{}
	current := -1
	headingCount := 0

	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node == nil {
			return
		}
		if node.Type == html.ElementNode {
			name := strings.ToLower(node.Data)
			if name == "script" || name == "style" {
				return
			}
			if level, ok := htmlHeadingLevel(name); ok {
				heading := strings.TrimSpace(htmlNodeText(node))
				if heading != "" {
					headingCount++
					anchor := firstNonEmptyString(htmlNodeAttr(node, "id"), markdownAnchor(heading))
					startRef := fmt.Sprintf("dom:%s:%d", name, headingCount)
					if id := htmlNodeAttr(node, "id"); id != "" {
						startRef = "dom:#" + id
					}
					draftWarnings := []string{}
					if len(warnings) > 0 {
						draftWarnings = append(draftWarnings, warnings...)
					}
					drafts = append(drafts, markdownSectionDraft{
						level:    level,
						heading:  heading,
						anchor:   anchor,
						startRef: startRef,
						endRef:   startRef,
						warnings: draftWarnings,
					})
					current = len(drafts) - 1
				}
				return
			}
			if name == "a" {
				target := strings.TrimSpace(htmlNodeAttr(node, "href"))
				if target != "" {
					if current < 0 {
						drafts = append(drafts, markdownSectionDraft{
							level:    1,
							heading:  documentationTitle(relativePath, nil),
							anchor:   "body",
							startRef: "dom:body",
							endRef:   "dom:body",
							warnings: append([]string{}, warnings...),
						})
						current = len(drafts) - 1
					}
					links = append(links, htmlLinkDraft{
						sectionIndex: current,
						target:       target,
						anchorText:   htmlNodeText(node),
					})
				}
			}
		}
		if node.Type == html.TextNode {
			text := strings.TrimSpace(node.Data)
			if text != "" {
				if current < 0 {
					drafts = append(drafts, markdownSectionDraft{
						level:    1,
						heading:  documentationTitle(relativePath, nil),
						anchor:   "body",
						startRef: "dom:body",
						endRef:   "dom:body",
						warnings: append([]string{}, warnings...),
					})
					current = len(drafts) - 1
				}
				drafts[current].content = append(drafts[current].content, text)
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return drafts, links
}

func htmlDocumentationLinks(
	relativePath string,
	sections []facts.DocumentationSectionPayload,
	drafts []htmlLinkDraft,
) []facts.DocumentationLinkPayload {
	links := make([]facts.DocumentationLinkPayload, 0, len(drafts))
	for _, draft := range drafts {
		if draft.sectionIndex < 0 || draft.sectionIndex >= len(sections) {
			continue
		}
		section := sections[draft.sectionIndex]
		links = append(links, facts.DocumentationLinkPayload{
			DocumentID:     section.DocumentID,
			RevisionID:     section.RevisionID,
			SectionID:      section.SectionID,
			LinkID:         fmt.Sprintf("link:%s:%d", section.SectionID, len(links)+1),
			TargetURI:      draft.target,
			TargetKind:     documentationLinkTargetKind(draft.target),
			AnchorTextHash: documentationHashText(strings.TrimSpace(draft.anchorText)),
			SourceMetadata: map[string]string{"path": relativePath},
		})
	}
	return links
}

func htmlHeadingLevel(name string) (int, bool) {
	if len(name) != 2 || name[0] != 'h' || name[1] < '1' || name[1] > '6' {
		return 0, false
	}
	return int(name[1] - '0'), true
}

func htmlNodeText(node *html.Node) string {
	var parts []string
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current == nil {
			return
		}
		if current.Type == html.ElementNode {
			name := strings.ToLower(current.Data)
			if name == "script" || name == "style" {
				return
			}
		}
		if current.Type == html.TextNode {
			if text := strings.TrimSpace(current.Data); text != "" {
				parts = append(parts, text)
			}
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return strings.Join(parts, " ")
}

func htmlNodeAttr(node *html.Node, name string) string {
	for _, attr := range node.Attr {
		if strings.EqualFold(attr.Key, name) {
			return strings.TrimSpace(attr.Val)
		}
	}
	return ""
}

func htmlMalformedWarnings(body string) []string {
	if strings.Count(body, "<") != strings.Count(body, ">") {
		return []string{"malformed_html"}
	}
	return nil
}
