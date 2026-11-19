// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import "testing"

// TestSearchIndexPartitionLabelShortensLeafNames pins the closed p00-p63
// partition label set (#10874): plain and schema-qualified leaf regclass
// text shorten to pNN, while anything unexpected keeps its full name rather
// than colliding with a real partition.
func TestSearchIndexPartitionLabelShortensLeafNames(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"eshu_search_index_terms_p29":        "p29",
		"eshu_search_index_terms_p00":        "p00",
		"eshu_search_index_terms_p63":        "p63",
		"public.eshu_search_index_terms_p29": "p29",
		"eshu_search_index_terms":            "eshu_search_index_terms",
		"eshu_search_index_terms_p7":         "eshu_search_index_terms_p7",
		"":                                   "",
	}
	for leaf, want := range cases {
		if got := searchIndexPartitionLabel(leaf); got != want {
			t.Errorf("searchIndexPartitionLabel(%q) = %q, want %q", leaf, got, want)
		}
	}
}
