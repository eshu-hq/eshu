// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"regexp"
	"slices"
	"strings"
)

// ContentStorePathPattern returns a regular expression, valid in Postgres ARE
// and Go RE2 and meant to be applied case-insensitively (~* or (?i)), that
// matches the repository-relative paths a non-store content-store policy
// governs: each governed definition's exact names, extensions, prefix names,
// and "*<suffix>" path dispatches. It returns "" when no definition is
// governed. The eshu_dp_content_policy_raw_rows gauge (#8833) runs it in SQL.
// A path a higher-precedence dispatch claims first (META-INF/services/x.tfvars)
// still matches; the gauge over-counts such a path, never under-counts.
func (r Registry) ContentStorePathPattern() string {
	if !r.governed {
		return ""
	}
	governed := map[string]bool{}
	var parts []string
	for _, definition := range r.definitions {
		if definition.ContentStore.Policy == ContentStoreStore || definition.ContentStore.Policy == "" {
			continue
		}
		governed[definition.ParserKey] = true
		for _, name := range definition.ExactNames {
			parts = append(parts, `(^|/)`+regexp.QuoteMeta(name)+`$`)
		}
		for _, extension := range definition.Extensions {
			parts = append(parts, regexp.QuoteMeta(extension)+`$`)
		}
		for _, prefix := range definition.PrefixNames {
			parts = append(parts, `(^|/)`+regexp.QuoteMeta(prefix)+`[^/]*$`)
		}
	}
	for _, dispatch := range PathDispatches() {
		if !governed[dispatch.ParserKey] {
			continue
		}
		for _, pattern := range dispatch.Patterns {
			if suffix, ok := strings.CutPrefix(pattern, "*"); ok && !strings.ContainsAny(suffix, "*<>") {
				parts = append(parts, regexp.QuoteMeta(suffix)+`$`)
				continue
			}
			parts = append(parts, `(^|/)`+regexp.QuoteMeta(pattern)+`$`)
		}
	}
	slices.Sort(parts)
	return strings.Join(slices.Compact(parts), "|")
}
