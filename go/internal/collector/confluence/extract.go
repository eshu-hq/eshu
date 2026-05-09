package confluence

import (
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/net/html"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func documentPayload(sourceID string, baseURL string, page Page) facts.DocumentationDocumentPayload {
	return facts.DocumentationDocumentPayload{
		SourceID:         sourceID,
		DocumentID:       "doc:confluence:" + page.ID,
		ExternalID:       page.ID,
		RevisionID:       strconvI(page.Version.Number),
		CanonicalURI:     canonicalURI(baseURL, page),
		Title:            page.Title,
		ParentDocumentID: parentDocumentID(page.ParentID),
		DocumentType:     "page",
		Format:           firstNonEmpty(page.Body.Storage.Representation, "storage"),
		Labels:           labelNames(pageLabels(page)),
		OwnerRefs:        ownerRefs(page),
		ACLSummary: &facts.DocumentationACLSummary{
			Visibility: "viewable",
			IsPartial:  false,
		},
		SourceMetadata: map[string]string{
			"space_id":  page.SpaceID,
			"status":    page.Status,
			"owner_id":  page.OwnerID,
			"author_id": page.AuthorID,
		},
		ContentHash:       hashText(page.Body.Storage.Value),
		DocumentUpdatedAt: page.Version.CreatedAt,
	}
}

func sectionsForPage(page Page) []facts.DocumentationSectionPayload {
	body := page.Body.Storage.Value
	return []facts.DocumentationSectionPayload{{
		DocumentID:     "doc:confluence:" + page.ID,
		RevisionID:     strconvI(page.Version.Number),
		SectionID:      "body",
		SectionAnchor:  "body",
		HeadingText:    page.Title,
		OrdinalPath:    []int{1},
		TextHash:       hashText(body),
		ExcerptHash:    hashText(plainText(body)),
		SourceStartRef: "storage:body",
		SourceEndRef:   "storage:body",
		SourceMetadata: map[string]string{"source_format": "storage"},
	}}
}

func linksForPage(page Page, sections []facts.DocumentationSectionPayload) []facts.DocumentationLinkPayload {
	if len(sections) == 0 {
		return nil
	}
	links := extractLinks(page.Body.Storage.Value)
	out := make([]facts.DocumentationLinkPayload, 0, len(links))
	for index, link := range links {
		out = append(out, facts.DocumentationLinkPayload{
			DocumentID:     "doc:confluence:" + page.ID,
			RevisionID:     strconvI(page.Version.Number),
			SectionID:      sections[0].SectionID,
			LinkID:         "link:" + strconvI(index+1),
			TargetURI:      link.href,
			TargetKind:     targetKind(link.href),
			AnchorTextHash: hashText(link.text),
		})
	}
	return out
}

type extractedLink struct {
	href string
	text string
}

func extractLinks(body string) []extractedLink {
	root, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return nil
	}
	var links []extractedLink
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "a" {
			if href := attr(node, "href"); href != "" {
				links = append(links, extractedLink{href: href, text: strings.TrimSpace(nodeText(node))})
			}
		}
		if node.Type == html.ElementNode && node.Data == "ac:link" {
			if href := confluenceStorageHref(node); href != "" {
				links = append(links, extractedLink{href: href, text: strings.TrimSpace(nodeText(node))})
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return links
}

func confluenceStorageHref(link *html.Node) string {
	resource := firstResourceNode(link)
	if resource == nil {
		return ""
	}
	switch resource.Data {
	case "ri:page":
		if id := attr(resource, "ri:content-id"); id != "" {
			return "confluence:page:" + id
		}
		if title := attr(resource, "ri:content-title"); title != "" {
			return "confluence:page-title:" + title
		}
	case "ri:attachment":
		if filename := attr(resource, "ri:filename"); filename != "" {
			return "confluence:attachment:" + filename
		}
	case "ri:url":
		return attr(resource, "ri:value")
	}
	return ""
}

func firstResourceNode(node *html.Node) *html.Node {
	var resource *html.Node
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if resource != nil {
			return
		}
		if current.Type == html.ElementNode && strings.HasPrefix(current.Data, "ri:") {
			resource = current
			return
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return resource
}

func plainText(body string) string {
	root, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return body
	}
	return strings.TrimSpace(nodeText(root))
}

func nodeText(node *html.Node) string {
	var builder strings.Builder
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current.Type == html.TextNode {
			builder.WriteString(current.Data)
			builder.WriteByte(' ')
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return strings.Join(strings.Fields(builder.String()), " ")
}

func attr(node *html.Node, name string) string {
	for _, attr := range node.Attr {
		if attr.Key == name {
			return attr.Val
		}
	}
	return ""
}

func canonicalURI(baseURL string, page Page) string {
	if strings.HasPrefix(page.Links.WebUI, "http://") || strings.HasPrefix(page.Links.WebUI, "https://") {
		return page.Links.WebUI
	}
	base := firstNonEmpty(page.Links.Base, baseURL)
	parsed, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil {
		return page.Links.WebUI
	}
	if strings.TrimSpace(page.Links.WebUI) == "" {
		parsed.Path = strings.TrimRight(parsed.Path, "/") + "/api/v2/pages/" + page.ID
		return parsed.String()
	}
	relative, err := url.Parse(page.Links.WebUI)
	if err != nil {
		return parsed.String()
	}
	return parsed.ResolveReference(relative).String()
}

func labelNames(labels []Label) []string {
	out := make([]string, 0, len(labels))
	for _, label := range labels {
		if strings.TrimSpace(label.Name) != "" {
			out = append(out, label.Name)
		}
	}
	return out
}

func pageLabels(page Page) []Label {
	if len(page.Labels) > 0 {
		return page.Labels
	}
	return page.LabelSet.Results
}

func ownerRefs(page Page) []facts.DocumentationOwnerRef {
	owner := firstNonEmpty(page.OwnerID, page.AuthorID)
	if owner == "" {
		return nil
	}
	return []facts.DocumentationOwnerRef{{Kind: "confluence_user", ID: owner}}
}

func parentDocumentID(parentID string) string {
	if strings.TrimSpace(parentID) == "" {
		return ""
	}
	return "doc:confluence:" + parentID
}

func targetKind(href string) string {
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		return "external_uri"
	}
	return "confluence_reference"
}

func strconvI(value int) string {
	if value == 0 {
		return "0"
	}
	return strconv.Itoa(value)
}
