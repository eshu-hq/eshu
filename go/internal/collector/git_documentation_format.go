package collector

import (
	"path"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

const (
	documentationMaxBodyBytes    = 512 * 1024
	documentationMaxSectionChars = 16 * 1024
)

type gitDocumentationFormat struct {
	format   string
	language string
}

func extractGitDocumentation(
	repo repositoryidentity.Metadata,
	relativePath string,
	digest string,
	commitSHA string,
	body []byte,
	format gitDocumentationFormat,
) (facts.DocumentationDocumentPayload, []facts.DocumentationSectionPayload, []facts.DocumentationLinkPayload) {
	switch format.format {
	case "markdown", "quarto":
		return extractMarkdownDocumentationWithFormat(repo, relativePath, digest, commitSHA, body, format.format)
	case "html":
		return extractHTMLDocumentation(repo, relativePath, digest, commitSHA, body)
	default:
		return extractTextDocumentation(repo, relativePath, digest, commitSHA, body, format.format)
	}
}

func gitDocumentationFormatForPath(relativePath string) (gitDocumentationFormat, bool) {
	switch strings.ToLower(path.Ext(relativePath)) {
	case ".md", ".mdx", ".markdown":
		return gitDocumentationFormat{format: "markdown", language: "markdown"}, true
	case ".qmd":
		return gitDocumentationFormat{format: "quarto", language: "quarto"}, true
	case ".txt":
		if isNonDocumentationTextPath(relativePath) {
			return gitDocumentationFormat{}, false
		}
		return gitDocumentationFormat{format: "text", language: "text"}, true
	case ".rst":
		return gitDocumentationFormat{format: "restructuredtext", language: "restructuredtext"}, true
	case ".adoc", ".asciidoc":
		return gitDocumentationFormat{format: "asciidoc", language: "asciidoc"}, true
	case ".html", ".htm":
		return gitDocumentationFormat{format: "html", language: "html"}, true
	default:
		return gitDocumentationFormat{}, false
	}
}

func gitDocumentationSourceURIAndFormat(relativePath string) (string, gitDocumentationFormat, bool) {
	sourceURI, ok := documentationSourceURI(relativePath)
	if !ok {
		return "", gitDocumentationFormat{}, false
	}
	format, ok := gitDocumentationFormatForPath(sourceURI)
	if !ok {
		return "", gitDocumentationFormat{}, false
	}
	return sourceURI, format, true
}

func isNonDocumentationTextPath(relativePath string) bool {
	base := strings.ToLower(path.Base(relativePath))
	return base == "requirements.txt" ||
		strings.HasPrefix(base, "requirements-") ||
		strings.HasPrefix(base, "requirements_") ||
		strings.HasPrefix(base, "constraints-") ||
		strings.HasPrefix(base, "constraints_")
}

func isGitDocumentationPath(filePath string) bool {
	_, ok := gitDocumentationFormatForPath(filepathToSourceURI(filePath))
	return ok
}

func filepathToSourceURI(filePath string) string {
	return path.Clean(strings.ReplaceAll(filePath, "\\", "/"))
}

func boundedDocumentationBody(body []byte) (string, []string) {
	warnings := []string{}
	if len(body) > documentationMaxBodyBytes {
		body = body[:documentationMaxBodyBytes]
		warnings = append(warnings, "body_truncated")
	}
	text := strings.ToValidUTF8(string(body), "")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return text, warnings
}

func boundedDocumentationSectionContent(content string) (string, []string) {
	runes := []rune(content)
	if len(runes) <= documentationMaxSectionChars {
		return content, nil
	}
	return strings.TrimSpace(string(runes[:documentationMaxSectionChars])), []string{"section_truncated"}
}

func addDocumentationWarnings(metadata map[string]string, warnings ...string) {
	if len(warnings) == 0 {
		return
	}
	seen := map[string]bool{}
	current := strings.TrimSpace(metadata["warning"])
	if current != "" {
		for _, item := range strings.Split(current, ",") {
			item = strings.TrimSpace(item)
			if item != "" {
				seen[item] = true
			}
		}
	}
	ordered := []string{}
	for _, warning := range warnings {
		warning = strings.TrimSpace(warning)
		if warning == "" || seen[warning] {
			continue
		}
		seen[warning] = true
		ordered = append(ordered, warning)
	}
	if len(ordered) == 0 {
		return
	}
	if current != "" {
		metadata["warning"] = current + "," + strings.Join(ordered, ",")
		return
	}
	metadata["warning"] = strings.Join(ordered, ",")
}
