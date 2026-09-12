// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

func TestCodeCallDefinitionSymbolKeysIgnoreGenerationFields(t *testing.T) {
	t.Parallel()

	first := map[string]any{
		"uid":           "uid:generation-a",
		"fact_id":       "fact:generation-a",
		"generation_id": "generation-a",
		"scip_symbol":   "scip-go gomod example.com/lib request().",
	}
	second := map[string]any{
		"uid":           "uid:generation-b",
		"fact_id":       "fact:generation-b",
		"generation_id": "generation-b",
		"scip_symbol":   "scip-go gomod example.com/lib request().",
	}

	firstKeys := codeCallDefinitionSymbolKeys(first)
	secondKeys := codeCallDefinitionSymbolKeys(second)
	if len(firstKeys) != 1 || len(secondKeys) != 1 {
		t.Fatalf("symbol key counts = %d/%d, want 1/1", len(firstKeys), len(secondKeys))
	}
	if firstKeys[0].key != secondKeys[0].key {
		t.Fatalf("symbol keys differ across generation fields: %q vs %q", firstKeys[0].key, secondKeys[0].key)
	}
	if firstKeys[0].method != codeprovenance.MethodSCIP {
		t.Fatalf("method = %q, want %q", firstKeys[0].method, codeprovenance.MethodSCIP)
	}
}
