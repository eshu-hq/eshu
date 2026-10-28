// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dockerfile

import (
	"net/url"
	"regexp"
	"strings"
)

var installCredentialAssignment = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(?:access[_-]?token|api[_-]?key|client[_-]?secret|private[_-]?key|token|secret|password|passwd|auth(?:orization)?|credential)[=:]`)

// credentialBearingText checks the bounded decoded forms a Dockerfile token
// can carry. Malformed or repeatedly encoded escapes are treated as unsafe.
func credentialBearingText(raw string) bool {
	value := raw
	for i := 0; i < 4; i++ {
		if directCredentialMarker(value) {
			return true
		}
		if !strings.Contains(value, "%") {
			return false
		}
		decoded, err := url.PathUnescape(value)
		if err != nil {
			return true
		}
		if decoded == value {
			return false
		}
		value = decoded
	}
	return strings.Contains(value, "%") || directCredentialMarker(value)
}

// directCredentialMarker skips detectors whose required punctuation or prefix
// is absent. Every encoded form is checked again after percent decoding.
func directCredentialMarker(value string) bool {
	return strings.ContainsAny(value, "=:") && installCredentialAssignment.MatchString(value) ||
		strings.ContainsAny(value, "gG") && hasKnownTokenPrefix(value)
}

func hasKnownTokenPrefix(value string) bool {
	lower := strings.ToLower(value)
	for _, prefix := range []string{"ghp_", "gho_", "github_pat_", "glpat-"} {
		if strings.Contains(lower, prefix) {
			return true
		}
	}
	return false
}

func credentialBearingValue(value string) bool {
	if credentialBearingText(value) {
		return true
	}
	if !strings.Contains(value, "://") {
		return strings.Contains(value, ":") && strings.Contains(value, "@")
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return true
	}
	return parsed.User != nil || credentialBearingText(parsed.Path) || unsafeDownloadPath(parsed.Path)
}

func unsafeDownloadPath(path string) bool {
	if strings.ContainsAny(path, "@=:\\") || credentialBearingText(path) {
		return true
	}
	return false
}

func credentialVariableName(name string) bool {
	lower := strings.ToLower(name)
	if lower == "key" || strings.HasSuffix(lower, "_key") {
		return true
	}
	for _, part := range strings.FieldsFunc(lower, func(char rune) bool { return char == '_' || char == '-' }) {
		switch part {
		case "token", "secret", "password", "passwd", "credential", "credentials", "auth", "authorization", "private", "pat":
			return true
		}
	}
	return false
}

func sensitiveInstallValue(name, value string, scope *installScope) bool {
	if credentialVariableName(name) || credentialBearingValue(value) {
		return true
	}
	for index := 0; index < len(value); index++ {
		if value[index] != '$' {
			continue
		}
		index++
		if index < len(value) && value[index] == '{' {
			index++
		}
		start := index
		for index < len(value) && (value[index] == '_' || value[index] >= 'A' && value[index] <= 'Z' || value[index] >= 'a' && value[index] <= 'z' || value[index] >= '0' && value[index] <= '9') {
			index++
		}
		if start == index {
			continue
		}
		reference := value[start:index]
		if credentialVariableName(reference) {
			return true
		}
		if inherited, found := scope.lookup(reference); found && inherited.sensitive {
			return true
		}
		index--
	}
	return false
}
