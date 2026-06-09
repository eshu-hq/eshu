package googleworkspace

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"path"
	"regexp"
	"strings"
)

var (
	emailPattern  = regexp.MustCompile(`(?i)\b[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}\b`)
	schemePattern = regexp.MustCompile(`(?i)\b[a-z][a-z0-9+\-.]*://\S+`)
	domainPattern = regexp.MustCompile(`(?i)\b[a-z0-9](?:[a-z0-9\-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9\-]{0,61}[a-z0-9])?)+\b`)
)

func safeFingerprint(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func safeURI(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if parsed, err := url.Parse(value); err == nil && parsed.Scheme != "" {
		return "redacted:" + safeFingerprint(value)
	}
	if strings.Contains(value, "://") || schemePattern.MatchString(value) {
		return "redacted:" + safeFingerprint(value)
	}
	cleaned := path.Clean(strings.ReplaceAll(value, "\\", "/"))
	if cleaned == "." || strings.HasPrefix(cleaned, "../") || path.IsAbs(cleaned) {
		return "redacted:" + safeFingerprint(value)
	}
	return cleaned
}

func safePrincipal(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if containsSensitiveText(value) {
		return "principal:" + safeFingerprint(value)
	}
	return value
}

func safeDisplayName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || containsSensitiveText(value) {
		return "Google Workspace source"
	}
	return value
}

func safeReason(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if containsSensitiveText(value) {
		return "redacted:" + safeFingerprint(value)
	}
	return value
}

func containsSensitiveText(value string) bool {
	value = strings.TrimSpace(value)
	lower := strings.ToLower(value)
	return emailPattern.MatchString(value) ||
		schemePattern.MatchString(value) ||
		domainPattern.MatchString(value) ||
		strings.Contains(lower, "token=")
}

func safePrincipalList(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = safePrincipal(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func cleanID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '-' || r == '_':
			builder.WriteRune(r)
		}
	}
	return builder.String()
}
