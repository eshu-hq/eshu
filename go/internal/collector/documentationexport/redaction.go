// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package documentationexport

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

var schemePattern = regexp.MustCompile(`(?i)\b[a-z][a-z0-9+\-.]*://\S+`)

func safeFingerprint(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func safeFingerprintIfPresent(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return safeFingerprint(value)
}

func safePath(value string) string {
	raw := strings.TrimSpace(value)
	value = strings.ReplaceAll(raw, "\\", "/")
	if raw == "" {
		return ""
	}
	cleaned := path.Clean(value)
	if unsafeLocalPath(raw, cleaned) {
		return "redacted:path:" + safeFingerprint(raw)
	}
	return cleaned
}

func safeTargetURI(value string) (string, map[string]string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if tokenBearingURL(value) {
		return "redacted:target:" + safeFingerprint(value), map[string]string{
			"redacted":         "true",
			"redaction_reason": "token_bearing_url",
		}
	}
	if hasWindowsDrivePrefix(value) || strings.Contains(value, "\\") {
		return safePath(value), map[string]string{
			"redacted":         "true",
			"redaction_reason": "unsafe_local_path",
		}
	}
	if parsed, err := url.Parse(value); err == nil && parsed.Scheme != "" {
		return "redacted:target:" + safeFingerprint(value), map[string]string{"redacted": "true"}
	}
	if strings.Contains(value, "://") || schemePattern.MatchString(value) {
		return "redacted:target:" + safeFingerprint(value), map[string]string{"redacted": "true"}
	}
	return safePath(value), nil
}

func unsafeLocalPath(raw string, cleaned string) bool {
	if strings.ContainsRune(raw, 0) || cleaned == "." || strings.HasPrefix(cleaned, "../") ||
		path.IsAbs(cleaned) || hasWindowsDrivePrefix(cleaned) || credentialPath(cleaned) {
		return true
	}
	for _, part := range strings.Split(cleaned, "/") {
		if part == "" || part == "." || part == ".." {
			return true
		}
	}
	return false
}

func hasWindowsDrivePrefix(name string) bool {
	if len(name) < 2 || name[1] != ':' {
		return false
	}
	return (name[0] >= 'A' && name[0] <= 'Z') || (name[0] >= 'a' && name[0] <= 'z')
}

func credentialPath(name string) bool {
	lower := strings.ToLower(filepath.Base(name))
	full := strings.ToLower(name)
	return lower == ".env" || strings.Contains(full, "secret") ||
		strings.Contains(full, "credential") || strings.Contains(full, "private_key") ||
		strings.Contains(full, "id_rsa") || strings.HasSuffix(lower, ".pem") ||
		strings.HasSuffix(lower, ".p12") || strings.HasSuffix(lower, ".pfx") ||
		strings.Contains(full, "token")
}

func tokenBearingURL(rawURL string) bool {
	if strings.TrimSpace(rawURL) == "" || !strings.Contains(rawURL, "?") {
		return false
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	for key := range parsed.Query() {
		if sensitiveQueryKey(key) {
			return true
		}
	}
	return false
}

func sensitiveQueryKey(key string) bool {
	switch strings.ToLower(key) {
	case "token", "access_token", "id_token", "refresh_token", "signature",
		"x-amz-signature", "sig", "secret", "password", "api_key", "key":
		return true
	default:
		return false
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
