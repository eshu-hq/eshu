// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"regexp"
	"sort"
	"strings"
)

var serviceDocsRoutePattern = regexp.MustCompile(`(?i)['"](/[^'"]+)['"]`)

func extractDocsRoutes(content string) []string {
	matches := serviceDocsRoutePattern.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	routes := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		route := strings.TrimSpace(match[1])
		if route == "" {
			continue
		}
		if !looksLikeDocsRoute(route) {
			continue
		}
		if _, ok := seen[route]; ok {
			continue
		}
		seen[route] = struct{}{}
		routes = append(routes, route)
	}
	sort.Strings(routes)
	return routes
}

func looksLikeDocsRoute(route string) bool {
	lower := strings.ToLower(route)
	if strings.Contains(lower, "docs") || strings.Contains(lower, "swagger") || strings.Contains(lower, "openapi") {
		return true
	}
	for _, segment := range strings.FieldsFunc(lower, func(r rune) bool {
		switch r {
		case '/', '_', '-', '.', ':':
			return true
		default:
			return false
		}
	}) {
		if segment == "spec" || segment == "specs" {
			return true
		}
	}
	return false
}
