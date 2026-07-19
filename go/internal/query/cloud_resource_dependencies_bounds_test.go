// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestConfigDerivedCloudResourceDependenciesUseUniqueCrossAnchorSentinel(t *testing.T) {
	t.Parallel()

	calls := 0
	got, truncated, err := loadConfigDerivedCloudResourceDependenciesBounded(
		t.Context(),
		fakeGraphReader{run: func(_ context.Context, query string, params map[string]any) ([]map[string]any, error) {
			calls++
			if got, want := StringVal(params, "config_anchor_pattern"), `.*(?:/config/primary|/config/secondary|/config/special\+a).*`; got != want {
				t.Fatalf("config_anchor_pattern = %q, want %q", got, want)
			}
			if got, want := IntVal(params, "limit"), 3; got != want {
				t.Fatalf("limit = %d, want global sentinel %d", got, want)
			}
			if !strings.Contains(query, "=~ $config_anchor_pattern") {
				t.Fatalf("query does not apply a single globally bounded anchor predicate: %s", query)
			}
			return []map[string]any{
				{"config_path": "/config/primary/db", "id": "cloud:primary", "name": "primary"},
				{"config_path": "/config/secondary/db", "id": "cloud:secondary", "name": "secondary"},
				{"config_path": "/config/special+a/db", "id": "cloud:third", "name": "third"},
			}, nil
		}},
		map[string]any{
			"artifacts": []map[string]any{
				{"relationship_type": "READS_CONFIG_FROM", "matched_value": "/config/primary/*"},
				{"relationship_type": "READS_CONFIG_FROM", "matched_value": "/config/secondary/*"},
				{"relationship_type": "READS_CONFIG_FROM", "matched_value": "/config/special+a/*"},
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
	if got, want := []string{StringVal(got[0], "matched_value"), StringVal(got[1], "matched_value")}, []string{"/config/primary", "/config/secondary"}; !slices.Equal(got, want) {
		t.Fatalf("matched anchors = %#v, want %#v", got, want)
	}
	if calls != 1 {
		t.Fatalf("graph calls = %d, want one globally bounded query", calls)
	}
}

func TestConfigReadCloudResourceAnchorsEnforcesQueryKeyBound(t *testing.T) {
	t.Parallel()

	artifacts := make([]map[string]any, 0, serviceCloudResourceDependencyLimit+1)
	for index := range serviceCloudResourceDependencyLimit + 1 {
		artifacts = append(artifacts, map[string]any{
			"relationship_type": "READS_CONFIG_FROM",
			"matched_value":     fmt.Sprintf("/config/%03d/*", index),
		})
	}

	got := configReadCloudResourceAnchors(map[string]any{"artifacts": artifacts})
	if len(got) != serviceCloudResourceDependencyLimit {
		t.Fatalf("anchors = %d, want key bound %d", len(got), serviceCloudResourceDependencyLimit)
	}
}
