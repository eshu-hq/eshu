// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"strings"
	"testing"
)

func TestCompareEnvironmentsReturnsGoldenMaterializedStageAndProd(t *testing.T) {
	t.Parallel()

	handler := &CompareHandler{
		Neo4j: fakeCompareGraphReader{
			runSingle: func(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
				switch {
				case strings.Contains(cypher, "MATCH (w:Workload)"):
					return map[string]any{
						"id":      "workload:deployable-source",
						"name":    "deployable-source",
						"kind":    "service",
						"repo_id": "repo-deployable-source",
					}, nil
				case strings.Contains(cypher, "MATCH (i:WorkloadInstance)"):
					environment := params["environment"].(string)
					return map[string]any{
						"id":          "workload-instance:deployable-source:" + environment,
						"name":        "deployable-source",
						"kind":        "service",
						"environment": environment,
						"workload_id": "workload:deployable-source",
					}, nil
				}
				return nil, nil
			},
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "MATCH (i:WorkloadInstance)-[r:USES]->(c:CloudResource)") {
					t.Fatalf("unexpected graph query: %s", cypher)
				}
				return []map[string]any{}, nil
			},
		},
	}

	response := executeCompareEnvironmentsRequest(
		t,
		handler,
		`{"workload_id":"workload:deployable-source","left":"stage","right":"prod","limit":10}`,
	)
	left := requireMap(t, response, "left")
	right := requireMap(t, response, "right")
	for _, side := range []struct {
		name        string
		value       map[string]any
		environment string
	}{
		{name: "left", value: left, environment: "stage"},
		{name: "right", value: right, environment: "prod"},
	} {
		if got, want := side.value["status"], "present"; got != want {
			t.Errorf("%s.status = %#v, want %#v", side.name, got, want)
		}
		instance := requireMap(t, side.value, "instance")
		if got, want := instance["id"], "workload-instance:deployable-source:"+side.environment; got != want {
			t.Errorf("%s.instance.id = %#v, want %#v", side.name, got, want)
		}
	}
	changed := requireMap(t, response, "changed")
	if got := len(requireMapSlice(t, changed, "cloud_resources")); got != 0 {
		t.Fatalf("changed cloud resources = %d, want 0", got)
	}
	if got, want := response["confidence"], float64(1); got != want {
		t.Fatalf("confidence = %#v, want %#v", got, want)
	}
	if got, want := response["reason"], "Environments are identical"; got != want {
		t.Fatalf("reason = %#v, want %#v", got, want)
	}
}
