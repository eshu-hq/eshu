// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backendconformance

import (
	"encoding/json"
	"fmt"
	"strings"
)

// runScopedGenerationKey is the projector's per-materialization lineage
// stamp. Two runs over the same corpus stamp different generations on
// identical content, so the key identifies the producing run, not the
// content. Excluding it from fingerprints and digests is what makes
// cross-run comparison possible at all (#6782); content drift still
// changes every other key.
const runScopedGenerationKey = "generation_id"

// normalizeComparisonValue returns value with every run-scoped lineage key
// removed, recursing into nested maps and slices. The JSON round-trip first
// normalizes driver value types (map[string]string to map[string]any,
// int64 to float64) so stripping sees every nesting level uniformly; the
// encoding is byte-identical to a direct marshal for values without
// run-scoped keys. The caller's value is never mutated.
func normalizeComparisonValue(value any) (any, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal comparison value: %w", err)
	}
	var normalized any
	if err := json.Unmarshal(raw, &normalized); err != nil {
		return nil, fmt.Errorf("re-decode comparison value: %w", err)
	}
	return stripRunScoped(normalized), nil
}

// stripRunScoped removes runScopedGenerationKey from every map in value.
// Top-level diagnostic metadata ("_"-prefixed) keys are handled separately
// by normalizeComparisonParams to mirror SanitizeStatementParameters.
func stripRunScoped(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if key == runScopedGenerationKey {
				delete(typed, key)
				continue
			}
			typed[key] = stripRunScoped(item)
		}
		return typed
	case []any:
		for i, item := range typed {
			typed[i] = stripRunScoped(item)
		}
		return typed
	default:
		return value
	}
}

// normalizeComparisonParams returns params with top-level diagnostic
// metadata keys (any key prefixed with "_", mirroring
// SanitizeStatementParameters: they never reach either backend's driver)
// and run-scoped lineage keys at every level removed.
func normalizeComparisonParams(params map[string]any) (map[string]any, error) {
	if params == nil {
		return map[string]any{}, nil
	}
	normalized, err := normalizeComparisonValue(params)
	if err != nil {
		return nil, fmt.Errorf("encode differential parameters: %w", err)
	}
	out, ok := normalized.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("encode differential parameters: params encode to %T, not an object", normalized)
	}
	for key := range out {
		if strings.HasPrefix(key, "_") {
			delete(out, key)
		}
	}
	return out, nil
}
