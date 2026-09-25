// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestDeploymentOverviewCarriesCountsNotHostnameEntrypointArrays proves the
// story deployment_overview keeps hostname_count and entrypoint_count but no
// longer duplicates the full hostnames and entrypoints arrays that already
// ship at the top level of the context payload (#7169).
func TestDeploymentOverviewCarriesCountsNotHostnameEntrypointArrays(t *testing.T) {
	t.Parallel()

	ctx := map[string]any{
		"id":          "workload:svc",
		"name":        "svc",
		"hostnames":   overviewFixtureRows("hostname", 7),
		"entrypoints": overviewFixtureRows("target", 9),
	}

	overview := buildServiceDeploymentOverviewWithContext(newServiceStoryBuildContext(ctx))

	if got, want := querycontract.IntVal(overview, "hostname_count"), 7; got != want {
		t.Fatalf("hostname_count = %d, want %d", got, want)
	}
	if got, want := querycontract.IntVal(overview, "entrypoint_count"), 9; got != want {
		t.Fatalf("entrypoint_count = %d, want %d", got, want)
	}
	for _, key := range []string{"hostnames", "entrypoints"} {
		if _, ok := overview[key]; ok {
			t.Fatalf("deployment_overview[%q] present, want the duplicate array removed", key)
		}
	}
}

func overviewFixtureRows(field string, n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{field: "row.example.test", "index": i})
	}
	return rows
}
