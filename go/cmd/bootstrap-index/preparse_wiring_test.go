// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git"
)

// bootstrapIndexPreparseSelectors pins how many selector structs with a
// Preparse field preparseWiringGaps reaches in the bootstrap-index selector
// tree (#8038): the one native selector. A selector the walk misses, or a new
// one added without updating the pin, fails the guard.
const bootstrapIndexPreparseSelectors = 1

// TestBootstrapIndexSelectorCarriesPreparse (ruling a9-143.3): bootstrap-index
// stamps each stored generation's pre-parse identity, so the first sweep after
// a bootstrap can skip the parse; its selector must carry the configuration.
func TestBootstrapIndexSelectorCarriesPreparse(t *testing.T) {
	t.Parallel()
	deps, err := buildBootstrapCollector(context.Background(), &fakeBootstrapSQLDB{}, func(string) string { return "" }, nil, nil, nil, bootstrapArgs{})
	if err != nil {
		t.Fatalf("buildBootstrapCollector() error = %v", err)
	}
	problems := checkPreparseWiring(deps.source.(*git.GitSource).Selector, bootstrapIndexPreparseSelectors)
	if len(problems) > 0 {
		t.Errorf("preparse wiring problems %v", problems)
	}
}
