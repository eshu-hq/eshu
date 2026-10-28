// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/impact/deployment"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

func TestFetchCloudResourceResultRetainsECSServiceIdentity(t *testing.T) {
	arn := "arn:aws:ecs:us-east-1:000000000000:service/c/api"
	h := &Handler{Neo4j: graph.FakeWorkloadGraphReader{RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
		if !strings.Contains(cypher, "c.resource_type as resource_type, c.arn as arn") {
			t.Fatalf("cloud resource query drops ECS service identity: %s", cypher)
		}
		return []map[string]any{{"id": "cloud-1", "name": "api", "kind": "ecs.service", "resource_type": "ecs.service", "arn": arn, "observation": map[string]any{"confidence": 0.9}}}, nil
	}}}
	result, err := h.fetchCloudResourceResult(context.Background(), "repo-api", "workload-api")
	if err != nil {
		t.Fatal(err)
	}
	got := deployment.ECSServicesFromCloudResources(result.Rows)
	if len(got) != 1 || got[0] != arn {
		t.Fatalf("graph-backed service anchors = %#v, want %q", got, arn)
	}
}
