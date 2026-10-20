// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package appmesh

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// sensitiveHeaderNames are HTTP header names whose match values App Mesh routes
// may carry that frequently hold credentials, session tokens, or customer
// identifiers. A match value on any of these is always redacted regardless of
// its literal shape, because the header name alone is enough signal that the
// value is sensitive.
var sensitiveHeaderNames = map[string]struct{}{
	"authorization":        {},
	"proxy-authorization":  {},
	"cookie":               {},
	"set-cookie":           {},
	"x-api-key":            {},
	"x-auth-token":         {},
	"x-csrf-token":         {},
	"x-amz-security-token": {},
}

// headerMatchAttributes maps route header matches into emittable attribute
// maps. The header name and match type are non-sensitive routing shape and are
// always preserved. The match value is redacted through the shared redact
// library when the header name is known-sensitive or the value matches a shared
// sensitive-key pattern; otherwise the literal value is preserved.
func (s Scanner) headerMatchAttributes(matches []HeaderMatch) []map[string]any {
	if len(matches) == 0 {
		return nil
	}
	result := make([]map[string]any, 0, len(matches))
	for _, match := range matches {
		name := strings.TrimSpace(match.Name)
		attribute := map[string]any{
			"name":       name,
			"match_type": strings.TrimSpace(match.MatchType),
			"invert":     match.Invert,
		}
		if value := strings.TrimSpace(match.Value); value != "" {
			attribute["value"] = s.headerMatchValue(name, value)
		}
		result = append(result, attribute)
	}
	return result
}

// headerMatchValue returns either the literal match value or a redaction marker
// payload. The decision is fail-safe: a known-sensitive header name always
// redacts, and the shared AWS redaction ruleset (keyed on the header name)
// catches names such as "x-secret-token" or "password" that the explicit list
// does not enumerate.
func (s Scanner) headerMatchValue(headerName, value string) any {
	source := "appmesh.route.header." + strings.ToLower(headerName)
	if headerNameIsSensitive(headerName) {
		return awscloud.RedactString(value, source, s.RedactionKey)
	}
	redacted, marker := awscloud.ClassifyStackOutput(headerName, value, s.RedactionKey)
	if redacted {
		return marker
	}
	return value
}

func headerNameIsSensitive(name string) bool {
	_, ok := sensitiveHeaderNames[strings.ToLower(strings.TrimSpace(name))]
	return ok
}
