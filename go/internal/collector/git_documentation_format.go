// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"path"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

const (
	documentationMaxBodyBytes    = 512 * 1024
	apiContractMaxBodyBytes      = 2 * 1024 * 1024
	notebookMaxBodyBytes         = 8 * 1024 * 1024
	documentationMaxSectionChars = 16 * 1024
)

type gitDocumentationFormat struct {
	format   string
	language string
}

func extractGitDocumentation(
	ctx context.Context,
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
	case "openapi", "swagger", "asyncapi", "graphql_sdl":
		return extractAPIContractDocumentation(repo, relativePath, digest, commitSHA, body, format.format)
	case "notebook":
		return extractNotebookDocumentation(repo, relativePath, digest, commitSHA, body)
	case "docx":
		return extractWordDocumentation(ctx, repo, relativePath, digest, commitSHA, body)
	case "csv", "tsv":
		return extractSpreadsheetDocumentation(repo, relativePath, digest, commitSHA, body, format.format)
	case "xlsx", "xls":
		return extractWorkbookDocumentation(ctx, repo, relativePath, digest, commitSHA, body, format.format)
	case "pptx":
		return extractPresentationDocumentation(ctx, repo, relativePath, digest, commitSHA, body)
	case "mermaid", "d2", "plantuml", "drawio", "excalidraw", "svg":
		return extractDiagramDocumentation(ctx, repo, relativePath, digest, commitSHA, body, format.format)
	default:
		return extractTextDocumentation(repo, relativePath, digest, commitSHA, body, format.format)
	}
}

func gitDocumentationFormatEmitsTruth(format gitDocumentationFormat) bool {
	switch format.format {
	case "mermaid", "d2", "plantuml", "drawio", "excalidraw", "svg":
		return false
	default:
		return true
	}
}

func gitDocumentationFormatIsArchive(format gitDocumentationFormat) bool {
	switch format.format {
	case "zip", "tar", "tar.gz":
		return true
	default:
		return false
	}
}

func gitDocumentationFormatForPath(relativePath string) (gitDocumentationFormat, bool) {
	if format, ok := gitDocumentationArchiveFormatForPath(relativePath); ok {
		return format, true
	}
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
	case ".ipynb":
		return gitDocumentationFormat{format: "notebook", language: "python"}, true
	case ".docx":
		if !isDocumentationOfficePath(relativePath) {
			return gitDocumentationFormat{}, false
		}
		return gitDocumentationFormat{format: "docx", language: "docx"}, true
	case ".pptx":
		if !isDocumentationOfficePath(relativePath) {
			return gitDocumentationFormat{}, false
		}
		return gitDocumentationFormat{format: "pptx", language: "pptx"}, true
	case ".mmd", ".mermaid":
		return gitDocumentationFormat{format: "mermaid", language: "mermaid"}, true
	case ".d2":
		return gitDocumentationFormat{format: "d2", language: "d2"}, true
	case ".puml", ".plantuml":
		return gitDocumentationFormat{format: "plantuml", language: "plantuml"}, true
	case ".drawio":
		return gitDocumentationFormat{format: "drawio", language: "drawio"}, true
	case ".excalidraw":
		return gitDocumentationFormat{format: "excalidraw", language: "excalidraw"}, true
	case ".svg":
		return gitDocumentationFormat{format: "svg", language: "svg"}, true
	case ".graphql", ".graphqls":
		return gitDocumentationFormat{format: "graphql_sdl", language: "graphql"}, true
	case ".json", ".yaml", ".yml":
		if format, ok := apiContractFormatForPath(relativePath); ok {
			return format, true
		}
		return gitDocumentationFormat{}, false
	case ".csv":
		if !isDocumentationSpreadsheetPath(relativePath) {
			return gitDocumentationFormat{}, false
		}
		return gitDocumentationFormat{format: "csv", language: "csv"}, true
	case ".tsv":
		if !isDocumentationSpreadsheetPath(relativePath) {
			return gitDocumentationFormat{}, false
		}
		return gitDocumentationFormat{format: "tsv", language: "tsv"}, true
	case ".xlsx":
		if !isDocumentationSpreadsheetPath(relativePath) {
			return gitDocumentationFormat{}, false
		}
		return gitDocumentationFormat{format: "xlsx", language: "xlsx"}, true
	case ".xls":
		if !isDocumentationSpreadsheetPath(relativePath) {
			return gitDocumentationFormat{}, false
		}
		return gitDocumentationFormat{format: "xls", language: "xls"}, true
	default:
		return gitDocumentationFormat{}, false
	}
}

func gitDocumentationArchiveFormatForPath(relativePath string) (gitDocumentationFormat, bool) {
	if !isDocumentationArchivePath(relativePath) {
		return gitDocumentationFormat{}, false
	}
	lower := strings.ToLower(filepathToSourceURI(relativePath))
	switch {
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return gitDocumentationFormat{format: "tar.gz", language: "tar.gz"}, true
	case strings.HasSuffix(lower, ".tar"):
		return gitDocumentationFormat{format: "tar", language: "tar"}, true
	case strings.HasSuffix(lower, ".zip"):
		return gitDocumentationFormat{format: "zip", language: "zip"}, true
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

func gitDocumentationSourceURIAndFormatForBody(relativePath string, body []byte) (string, gitDocumentationFormat, bool) {
	sourceURI, format, ok := gitDocumentationSourceURIAndFormat(relativePath)
	if ok {
		return sourceURI, format, true
	}
	sourceURI, ok = documentationSourceURI(relativePath)
	if !ok || !isPotentialStructuredAPIContractPath(sourceURI) {
		return "", gitDocumentationFormat{}, false
	}
	format, ok = detectStructuredAPIContractFormat(sourceURI, body)
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

func isDocumentationPathOrStructuredAPIContractCandidate(relativePath string) bool {
	if _, ok := gitDocumentationFormatForPath(relativePath); ok {
		return true
	}
	return isPotentialStructuredAPIContractPath(relativePath)
}

func filepathToSourceURI(filePath string) string {
	return path.Clean(strings.ReplaceAll(filePath, "\\", "/"))
}

func boundedDocumentationBody(body []byte) (string, []string) {
	return boundedDocumentationBodyBytes(body, documentationMaxBodyBytes)
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
