// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package amp

import (
	"strings"
	"time"
)

// workspaceResourceID returns the resource_id the workspace node publishes. It
// prefers the workspace ARN (always present from the AMP API) and falls back to
// the workspace id, so rule-groups-namespace-in-workspace and
// scraper-sends-to-workspace edges can key the workspace by the same value the
// node publishes.
func workspaceResourceID(workspace Workspace) string {
	return firstNonEmpty(workspace.ARN, workspace.WorkspaceID)
}

// namespaceResourceID returns the resource_id a rule-groups namespace node
// publishes. It prefers the namespace ARN and falls back to the namespace name,
// so a namespace's own edges are sourced on the same id the namespace node
// publishes.
func namespaceResourceID(namespace RuleGroupsNamespace) string {
	return firstNonEmpty(namespace.ARN, namespace.Name)
}

// scraperResourceID returns the resource_id a scraper node publishes. It prefers
// the scraper ARN and falls back to the scraper id, so a scraper's own edges are
// sourced on the same id the scraper node publishes.
func scraperResourceID(scraper Scraper) string {
	return firstNonEmpty(scraper.ARN, scraper.ScraperID)
}

// isARN reports whether value carries the canonical AWS ARN prefix.
func isARN(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "arn:")
}

// firstNonEmpty returns the first trimmed non-empty value, or "" when none.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// timeOrNil returns the UTC time when value is set, or nil for the zero time so
// the attribute payload omits an unknown timestamp instead of emitting an epoch.
func timeOrNil(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

// cloneStringMap returns a trimmed-key copy of input, or nil when it is empty or
// every key trims to empty, keeping omitempty-style payload behavior consistent.
func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		output[trimmed] = value
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

// cloneStrings returns a trimmed copy of input with empty entries dropped and
// duplicates removed (preserving first-seen order), or nil when nothing
// survives. Subnet and security-group id lists feed graph edges, so dedupe keeps
// the edge set idempotent.
func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, 0, len(input))
	seen := make(map[string]struct{}, len(input))
	for _, value := range input {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		output = append(output, trimmed)
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
