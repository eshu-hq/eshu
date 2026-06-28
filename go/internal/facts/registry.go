// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import (
	"slices"
	"strings"
)

// CoreFactKinds returns every fact kind owned by the Eshu core data plane.
func CoreFactKinds() []string {
	kinds := make([]string, 0, len(factKindRegistryEntries))
	for _, entry := range factKindRegistryEntries {
		kinds = append(kinds, entry.Kind)
	}
	slices.Sort(kinds)
	return slices.Compact(kinds)
}

// IsCoreFactKind reports whether kind is reserved by Eshu core.
func IsCoreFactKind(kind string) bool {
	trimmed := strings.TrimSpace(kind)
	if trimmed == "" {
		return false
	}
	_, ok := slices.BinarySearch(CoreFactKinds(), trimmed)
	return ok
}
