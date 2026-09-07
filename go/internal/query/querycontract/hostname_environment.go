// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/environment"
)

// Hostname environment inference shared by the query root and the
// handler-family subpackages (#6060 lane B2).
//
// The alias table comes from the environment contract package; detection is a
// pure substring match over a normalized token. These live here rather than in
// package query because the impact family's deployment-trace lineage reader
// infers environments and cannot import the root package back without an
// import cycle. Package query keeps forwarding wrappers under the original
// names.

// environmentAliases caches the shared alias table from the environment
// contract package at init time for substring-based alias detection.
var environmentAliases = environment.Aliases()

// NormalizeEvidenceToken lowercases text and folds separators to underscores,
// wrapped in underscores so alias detection matches whole tokens.
func NormalizeEvidenceToken(text string) string {
	lower := strings.ToLower(text)
	replacer := strings.NewReplacer(
		"/", "_",
		".", "_",
		"-", "_",
		":", "_",
		"@", "_",
		"\n", "_",
		"\t", "_",
		" ", "_",
	)
	return "_" + replacer.Replace(lower) + "_"
}

// DetectEnvironmentAliases returns the sorted canonical environments whose
// alias appears as a whole token in text.
func DetectEnvironmentAliases(text string) []string {
	normalized := NormalizeEvidenceToken(text)
	if normalized == "" {
		return nil
	}
	seen := map[string]struct{}{}
	for _, row := range environmentAliases {
		for _, alias := range row.Aliases {
			if strings.Contains(normalized, "_"+alias+"_") {
				seen[row.Canonical] = struct{}{}
				break
			}
		}
	}
	environments := make([]string, 0, len(seen))
	for environment := range seen {
		environments = append(environments, environment)
	}
	sort.Strings(environments)
	return environments
}

// InferHostnameEnvironment returns the first canonical environment inferred
// from hostname, or "" when none matches.
func InferHostnameEnvironment(hostname string) string {
	matches := DetectEnvironmentAliases(hostname)
	if len(matches) == 0 {
		return ""
	}
	return matches[0]
}
