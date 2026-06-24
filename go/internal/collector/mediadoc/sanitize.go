// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mediadoc

import (
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	emailPattern       = regexp.MustCompile(`(?i)\b[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}\b`)
	schemePattern      = regexp.MustCompile(`(?i)\b[a-z][a-z0-9+\-.]*://\S+`)
	ticketPattern      = regexp.MustCompile(`\b[A-Z][A-Z0-9]+-[0-9]+\b`)
	unixPathPattern    = regexp.MustCompile(`(?i)(^|[^a-z0-9])/(users|home|var|etc|private|volumes|tmp)/\S+`)
	windowsPathPattern = regexp.MustCompile(`(?i)\b[a-z]:\\[^\s]+`)
)

func containsSensitiveMarker(text string) bool {
	lower := strings.ToLower(text)
	for _, marker := range []string{"credential_marker", "secret_marker", "token_marker", "password", "api_key", "private_key"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return emailPattern.MatchString(text) ||
		schemePattern.MatchString(text) ||
		ticketPattern.MatchString(text) ||
		unixPathPattern.MatchString(text) ||
		windowsPathPattern.MatchString(text)
}

func safeIdentity(value string, prefix string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	if containsSensitiveMarker(value) || isUnsafeSourcePath(value) {
		return prefix + ":" + hashText(value), true
	}
	return value, false
}

func safeCanonicalURI(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	if isSafeRelativeSource(value) {
		return value, false
	}
	return "", true
}

func safeSourceURI(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if isSafeRelativeSource(value) {
		return path.Clean(filepath.ToSlash(value))
	}
	return "redacted:" + hashText(value)
}

func isSafeRelativeSource(value string) bool {
	raw := strings.TrimSpace(value)
	if strings.Contains(raw, "://") || schemePattern.MatchString(raw) {
		return false
	}
	cleaned := path.Clean(filepath.ToSlash(raw))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || path.IsAbs(cleaned) {
		return false
	}
	if containsSensitiveMarker(cleaned) {
		return false
	}
	return true
}

func isUnsafeSourcePath(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if strings.Contains(value, "://") {
		return true
	}
	if strings.HasPrefix(value, "/") || strings.HasPrefix(value, "~/") {
		return true
	}
	if len(value) > 2 && value[1] == ':' && (value[2] == '\\' || value[2] == '/') {
		return true
	}
	return false
}
