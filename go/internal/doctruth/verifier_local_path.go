package doctruth

import (
	"net/url"
	"path"
	"regexp"
	"strings"
)

var markdownLinkPattern = regexp.MustCompile(`\[[^\]]+\]\(([^)\s]+)\)`)

// LocalPathResolver checks one normalized local path claim against caller-owned truth.
type LocalPathResolver func(DocumentInput, string) LocalPathResolution

// LocalPathResolution is the caller-supplied truth result for a local path claim.
type LocalPathResolution struct {
	Supported bool
	Exists    bool
}

func localPathClaimsFromMarkdownLinks(line string, lineNumber int) []extractedClaim {
	claims := []extractedClaim{}
	for _, match := range markdownLinkPattern.FindAllStringSubmatch(line, -1) {
		if localPath := normalizeLocalPathClaim(match[1]); localPath != "" {
			claims = append(claims, extractedClaim{
				claimType:  ClaimTypeLocalPath,
				text:       match[1],
				normalized: localPath,
				line:       lineNumber,
			})
		}
	}
	return claims
}

func normalizeLocalPathClaim(raw string) string {
	text := strings.Trim(strings.TrimSpace(raw), "'\"")
	if text == "" || strings.ContainsAny(text, " \t\n\r") {
		return ""
	}
	if strings.HasPrefix(text, "#") {
		return ""
	}
	if parsed, err := url.Parse(text); err == nil && parsed.Scheme != "" {
		return ""
	}
	if cut := strings.IndexAny(text, "?#"); cut >= 0 {
		text = text[:cut]
	}
	text = strings.TrimSpace(text)
	if text == "" || strings.HasPrefix(text, "/") {
		return ""
	}
	cleaned := path.Clean(strings.ReplaceAll(text, "\\", "/"))
	cleaned = strings.TrimPrefix(cleaned, "./")
	if cleaned == "." || cleaned == ".." {
		return ""
	}
	if !looksLikeRepoPathClaim(cleaned) {
		return ""
	}
	return cleaned
}

func looksLikeRepoPathClaim(cleaned string) bool {
	lower := strings.ToLower(path.Base(cleaned))
	switch lower {
	case "dockerfile", "chart.yaml", "values.yaml", "kustomization.yaml", "kustomization.yml", "docker-compose.yaml", "docker-compose.yml":
		return true
	}
	switch strings.ToLower(path.Ext(cleaned)) {
	case ".tf", ".tfvars", ".hcl", ".yaml", ".yml", ".json", ".toml":
		return true
	default:
		return false
	}
}
