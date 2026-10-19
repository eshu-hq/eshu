// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package filters

import "strings"

// CleanFactKinds removes empty fact-kind filters while preserving first-seen
// order so tests and query plans stay stable. Single shared copy for the
// postgres root package and the tfstate subpackage (issue #8552): the root
// imports tfstate, so tfstate cannot import the root back, and two copies
// stayed identical by prose alone until this leaf gave both sides one source.
func CleanFactKinds(factKinds []string) []string {
	cleaned := make([]string, 0, len(factKinds))
	seen := make(map[string]struct{}, len(factKinds))
	for _, factKind := range factKinds {
		factKind = strings.TrimSpace(factKind)
		if factKind == "" {
			continue
		}
		if _, ok := seen[factKind]; ok {
			continue
		}
		seen[factKind] = struct{}{}
		cleaned = append(cleaned, factKind)
	}
	return cleaned
}
