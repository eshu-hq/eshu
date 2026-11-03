// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package advisory

import (
	"strings"
	"testing"
)

// TestListCatalogQueryEcosystemFoldsASCIIOnly pins the #9622 SQL contract for
// the advisory catalog: every projection of a stored advisory ecosystem folds
// ASCII letters only (packageidentity.LowerASCII), and the ecosystem filter
// compares against an ASCII-only fold of its bind, never a locale LOWER.
func TestListCatalogQueryEcosystemFoldsASCIIOnly(t *testing.T) {
	t.Parallel()

	const oldFold = `LOWER(TRIM(fact.payload->>'ecosystem'))`
	if strings.Contains(ListCatalogQuery, oldFold) {
		t.Fatalf("ListCatalogQuery keeps locale fold %q; use LOWER(x COLLATE \"C\") COLLATE \"default\"", oldFold)
	}

	const newFold = `LOWER(TRIM(fact.payload->>'ecosystem') COLLATE "C") COLLATE "default"`
	if got := strings.Count(ListCatalogQuery, newFold); got != 3 {
		t.Errorf("ListCatalogQuery holds %d copies of %q, want 3", got, newFold)
	}

	const oldParam = `e = LOWER($2)`
	if strings.Contains(ListCatalogQuery, oldParam) {
		t.Errorf("ListCatalogQuery keeps locale param fold %q; use LOWER(($2) COLLATE \"C\") COLLATE \"default\"", oldParam)
	}
	const newParam = `e = LOWER(($2) COLLATE "C") COLLATE "default"`
	if !strings.Contains(ListCatalogQuery, newParam) {
		t.Errorf("ListCatalogQuery misses ASCII-only param fold %q", newParam)
	}
}
