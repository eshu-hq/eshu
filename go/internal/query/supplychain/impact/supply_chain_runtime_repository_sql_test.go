// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"strings"
	"testing"
)

func TestSupplyChainRuntimeRepositoryDecoderIsSharedByFilterAndHydration(t *testing.T) {
	t.Parallel()

	filterJoin := supplyChainRuntimeRepositoryDecoderJoin(
		"runtime_fact.payload",
		"runtime_fact.scope_id",
		"runtime_repository",
	)
	filterQuery := supplyChainImpactRuntimeFilterCTE("$9", "$10", "$11", "$22", "$23")
	if !strings.Contains(filterQuery, filterJoin) {
		t.Fatal("runtime filter query does not contain the shared repository decoder")
	}

	hydrationJoin := supplyChainRuntimeRepositoryDecoderJoin(
		"fact.payload",
		"fact.scope_id",
		"runtime_repository",
	)
	if !strings.Contains(SelectSupplyChainImpactRuntimeContextQuery, hydrationJoin) {
		t.Fatal("runtime-context hydration query does not contain the shared repository decoder")
	}
	if !strings.Contains(SelectSupplyChainImpactRuntimeContextQuery,
		"runtime_repository.repository_id = ANY($3::text[])") {
		t.Fatal("runtime-context hydration does not authorize the canonical decoded repository")
	}
	for _, want := range []string{
		"jsonb_typeof(runtime_fact.payload->'repository_id') = 'string'",
		"jsonb_typeof(runtime_fact.payload->'repo_id') = 'string'",
		"jsonb_typeof(runtime_fact.payload->'scope_id') = 'string'",
	} {
		if !strings.Contains(filterJoin, want) {
			t.Fatalf("runtime repository decoder missing string-only marker %q:\n%s", want, filterJoin)
		}
	}
}
