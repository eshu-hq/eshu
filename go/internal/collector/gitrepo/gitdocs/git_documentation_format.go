// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitdocs

import (
	"context"
	"path"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

const (
	DocumentationMaxBodyBytes    = 512 * 1024
	apiContractMaxBodyBytes      = 2 * 1024 * 1024
	notebookMaxBodyBytes         = 8 * 1024 * 1024
	DocumentationMaxSectionChars = 16 * 1024
)

type GitDocumentationFormat struct {
	Format   string
	Language string
}

func extractGitDocumentation(
	ctx context.Context,
	repo repositoryidentity.Metadata,
	relativePath string,
	digest string,
	commitSHA string,
	body []byte,
	format GitDocumentationFormat,
) (facts.DocumentationDocumentPayload, []facts.DocumentationSectionPayload, []facts.DocumentationLinkPayload) {
	switch format.Format {
	case "markdown", "quarto":
		return extractMarkdownDocumentationWithFormat(repo, relativePath, digest, commitSHA, body, format.Format)
	case "html":
		return extractHTMLDocumentation(repo, relativePath, digest, commitSHA, body)
	case "openapi", "swagger", "asyncapi", "graphql_sdl":
		return extractAPIContractDocumentation(repo, relativePath, digest, commitSHA, body, format.Format)
	case "notebook":
		return extractNotebookDocumentation(repo, relativePath, digest, commitSHA, body)
	case "docx":
		return extractWordDocumentation(ctx, repo, relativePath, digest, commitSHA, body)
	case "csv", "tsv":
		return extractSpreadsheetDocumentation(repo, relativePath, digest, commitSHA, body, format.Format)
	case "xlsx", "xls":
		return extractWorkbookDocumentation(ctx, repo, relativePath, digest, commitSHA, body, format.Format)
	case "pptx":
		return extractPresentationDocumentation(ctx, repo, relativePath, digest, commitSHA, body)
	case "mermaid", "d2", "plantuml", "drawio", "excalidraw", "svg":
		return extractDiagramDocumentation(ctx, repo, relativePath, digest, commitSHA, body, format.Format)
	default:
		return extractTextDocumentation(repo, relativePath, digest, commitSHA, body, format.Format)
	}
}

func gitDocumentationFormatEmitsTruth(format GitDocumentationFormat) bool {
	switch format.Format {
	case "mermaid", "d2", "plantuml", "drawio", "excalidraw", "svg":
		return false
	default:
		return true
	}
}

func gitDocumentationFormatIsArchive(format GitDocumentationFormat) bool {
	switch format.Format {
	case "zip", "tar", "tar.gz":
		return true
	default:
		return false
	}
}

func gitDocumentationFormatForPath(relativePath string) (GitDocumentationFormat, bool) {
	if format, ok := gitDocumentationArchiveFormatForPath(relativePath); ok {
		return format, true
	}
	switch strings.ToLower(path.Ext(relativePath)) {
	case ".md", ".mdx", ".markdown":
		return GitDocumentationFormat{Format: "markdown", Language: "markdown"}, true
	case ".qmd":
		return GitDocumentationFormat{Format: "quarto", Language: "quarto"}, true
	case ".txt":
		if isNonDocumentationTextPath(relativePath) {
			return GitDocumentationFormat{}, false
		}
		return GitDocumentationFormat{Format: "text", Language: "text"}, true
	case ".rst":
		return GitDocumentationFormat{Format: "restructuredtext", Language: "restructuredtext"}, true
	case ".adoc", ".asciidoc":
		return GitDocumentationFormat{Format: "asciidoc", Language: "asciidoc"}, true
	case ".html", ".htm":
		return GitDocumentationFormat{Format: "html", Language: "html"}, true
	case ".ipynb":
		return GitDocumentationFormat{Format: "notebook", Language: "python"}, true
	case ".docx":
		if !isDocumentationOfficePath(relativePath) {
			return GitDocumentationFormat{}, false
		}
		return GitDocumentationFormat{Format: "docx", Language: "docx"}, true
	case ".pptx":
		if !isDocumentationOfficePath(relativePath) {
			return GitDocumentationFormat{}, false
		}
		return GitDocumentationFormat{Format: "pptx", Language: "pptx"}, true
	case ".mmd", ".mermaid":
		return GitDocumentationFormat{Format: "mermaid", Language: "mermaid"}, true
	case ".d2":
		return GitDocumentationFormat{Format: "d2", Language: "d2"}, true
	case ".puml", ".plantuml":
		return GitDocumentationFormat{Format: "plantuml", Language: "plantuml"}, true
	case ".drawio":
		return GitDocumentationFormat{Format: "drawio", Language: "drawio"}, true
	case ".excalidraw":
		return GitDocumentationFormat{Format: "excalidraw", Language: "excalidraw"}, true
	case ".svg":
		return GitDocumentationFormat{Format: "svg", Language: "svg"}, true
	case ".graphql", ".graphqls":
		return GitDocumentationFormat{Format: "graphql_sdl", Language: "graphql"}, true
	case ".json", ".yaml", ".yml":
		if format, ok := apiContractFormatForPath(relativePath); ok {
			return format, true
		}
		return GitDocumentationFormat{}, false
	case ".csv":
		if !isDocumentationSpreadsheetPath(relativePath) {
			return GitDocumentationFormat{}, false
		}
		return GitDocumentationFormat{Format: "csv", Language: "csv"}, true
	case ".tsv":
		if !isDocumentationSpreadsheetPath(relativePath) {
			return GitDocumentationFormat{}, false
		}
		return GitDocumentationFormat{Format: "tsv", Language: "tsv"}, true
	case ".xlsx":
		if !isDocumentationSpreadsheetPath(relativePath) {
			return GitDocumentationFormat{}, false
		}
		return GitDocumentationFormat{Format: "xlsx", Language: "xlsx"}, true
	case ".xls":
		if !isDocumentationSpreadsheetPath(relativePath) {
			return GitDocumentationFormat{}, false
		}
		return GitDocumentationFormat{Format: "xls", Language: "xls"}, true
	default:
		return GitDocumentationFormat{}, false
	}
}

func gitDocumentationArchiveFormatForPath(relativePath string) (GitDocumentationFormat, bool) {
	if !isDocumentationArchivePath(relativePath) {
		return GitDocumentationFormat{}, false
	}
	lower := strings.ToLower(filepathToSourceURI(relativePath))
	switch {
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return GitDocumentationFormat{Format: "tar.gz", Language: "tar.gz"}, true
	case strings.HasSuffix(lower, ".tar"):
		return GitDocumentationFormat{Format: "tar", Language: "tar"}, true
	case strings.HasSuffix(lower, ".zip"):
		return GitDocumentationFormat{Format: "zip", Language: "zip"}, true
	default:
		return GitDocumentationFormat{}, false
	}
}

func GitDocumentationSourceURIAndFormat(relativePath string) (string, GitDocumentationFormat, bool) {
	sourceURI, ok := documentationSourceURI(relativePath)
	if !ok {
		return "", GitDocumentationFormat{}, false
	}
	format, ok := gitDocumentationFormatForPath(sourceURI)
	if !ok {
		return "", GitDocumentationFormat{}, false
	}
	return sourceURI, format, true
}

func gitDocumentationSourceURIAndFormatForBody(relativePath string, body []byte) (string, GitDocumentationFormat, bool) {
	sourceURI, format, ok := GitDocumentationSourceURIAndFormat(relativePath)
	if ok {
		return sourceURI, format, true
	}
	sourceURI, ok = documentationSourceURI(relativePath)
	if !ok || !isPotentialStructuredAPIContractPath(sourceURI) {
		return "", GitDocumentationFormat{}, false
	}
	format, ok = detectStructuredAPIContractFormat(sourceURI, body)
	if !ok {
		return "", GitDocumentationFormat{}, false
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

func IsGitDocumentationPath(filePath string) bool {
	_, ok := gitDocumentationFormatForPath(filepathToSourceURI(filePath))
	return ok
}

func filepathToSourceURI(filePath string) string {
	return path.Clean(strings.ReplaceAll(filePath, "\\", "/"))
}

func boundedDocumentationBody(body []byte) (string, []string) {
	return boundedDocumentationBodyBytes(body, DocumentationMaxBodyBytes)
}

func boundedNotebookBody(body []byte) (string, []string) {
	return boundedDocumentationBodyBytes(body, notebookMaxBodyBytes)
}

func boundedDocumentationBodyBytes(body []byte, maxBytes int) (string, []string) {
	warnings := []string{}
	if len(body) > maxBytes {
		body = body[:maxBytes]
		warnings = append(warnings, "body_truncated")
	}
	text := strings.ToValidUTF8(string(body), "")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return text, warnings
}

func boundedDocumentationSectionContent(content string) (string, []string) {
	runes := []rune(content)
	if len(runes) <= DocumentationMaxSectionChars {
		return content, nil
	}
	return strings.TrimSpace(string(runes[:DocumentationMaxSectionChars])), []string{"section_truncated"}
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
