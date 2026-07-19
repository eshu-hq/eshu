// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestFetchCloudResourceResultUsesUniqueSentinel(t *testing.T) {
	t.Parallel()

	reader := fakeGraphReader{run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
		if !strings.Contains(cypher, "LIMIT $cloud_resource_limit") {
			t.Fatalf("cloud resource query is unbounded: %s", cypher)
		}
		if got, want := IntVal(params, "cloud_resource_limit"), serviceStoryItemLimit+1; got != want {
			t.Fatalf("cloud_resource_limit = %d, want %d", got, want)
		}
		rows := make([]map[string]any, 0, serviceStoryItemLimit+1)
		for index := range serviceStoryItemLimit + 1 {
			rows = append(rows, map[string]any{"id": fmt.Sprintf("resource:%03d", index)})
		}
		return rows, nil
	}}
	handler := &ImpactHandler{Neo4j: reader}

	result, err := handler.fetchCloudResourceResult(t.Context(), "workload:orders")
	if err != nil {
		t.Fatalf("fetchCloudResourceResult() error = %v", err)
	}
	if got, want := len(result.rows), serviceStoryItemLimit; got != want {
		t.Fatalf("rows = %d, want %d", got, want)
	}
	if !BoolVal(result.limits, "truncated") || !BoolVal(result.limits, "observed_count_is_lower_bound") {
		t.Fatalf("limits = %#v, want lower-bound truncation", result.limits)
	}
}
