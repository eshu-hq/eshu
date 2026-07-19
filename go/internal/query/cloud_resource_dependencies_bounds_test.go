// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"testing"
)

func TestConfigDerivedCloudResourceDependenciesUseUniqueCrossAnchorSentinel(t *testing.T) {
	t.Parallel()

	rowsByAnchor := map[string][]map[string]any{
		"/config/primary": {
			{"id": "cloud:shared", "name": "shared"},
			{"id": "cloud:primary", "name": "primary"},
		},
		"/config/secondary": {
			{"id": "cloud:shared", "name": "shared"},
			{"id": "cloud:secondary", "name": "secondary"},
		},
	}
	got, truncated, err := loadConfigDerivedCloudResourceDependenciesBounded(
		t.Context(),
		fakeGraphReader{run: func(_ context.Context, _ string, params map[string]any) ([]map[string]any, error) {
			rows := rowsByAnchor[StringVal(params, "config_anchor")]
			limit := IntVal(params, "limit")
			if limit < len(rows) {
				rows = rows[:limit]
			}
			return rows, nil
		}},
		map[string]any{
			"artifacts": []map[string]any{
				{"relationship_type": "READS_CONFIG_FROM", "matched_value": "/config/primary/*"},
				{"relationship_type": "READS_CONFIG_FROM", "matched_value": "/config/secondary/*"},
			},
		},
		2,
	)
	if err != nil {
		t.Fatalf("loadConfigDerivedCloudResourceDependenciesBounded() error = %v", err)
	}
	if gotCount, want := len(got), 2; gotCount != want {
		t.Fatalf("returned resources = %#v, want %d bounded unique rows", got, want)
	}
	if !truncated {
		t.Fatalf("truncated = false, want true for third unique cross-anchor resource; rows = %#v", got)
	}
}
