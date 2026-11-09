// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package advisory

import (
	"strings"
	"testing"
)

// TestAdvisoryReadsServeNewestGHSACopy pins eshu-hq/eshu#10464: the catalog
// and evidence reads serve the newest GHSA copy of an advisory (the
// reducer's newest-copy rule in SQL, DISTINCT ON the payload source and
// advisory id), so an older window copy's severity and packages stop
// shadowing the record its newer copy already replaced.
func TestAdvisoryReadsServeNewestGHSACopy(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"ghsa_newest_copy AS (",
		"SELECT DISTINCT ON",
		"modified_ts DESC NULLS LAST",
	} {
		if !strings.Contains(ListCatalogQuery, want) {
			t.Errorf("ListCatalogQuery missing newest-copy marker %q", want)
		}
		if !strings.Contains(ListEvidenceQuery, want) {
			t.Errorf("ListEvidenceQuery missing newest-copy marker %q", want)
		}
	}
}
