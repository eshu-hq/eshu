// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// TestFetchServiceWorkloadContextStripsHandleSelector is the #5385 flip-wave
// read-side retirement for GET /api/v0/services/{service_name}/context: the
// MCP route forwards a workload: handle whole as the path segment, so the
// name-first lookup must see the stripped bare name. A handle that reaches
// the name probe intact misses every row because node names never carry the
// prefix.
func TestFetchServiceWorkloadContextStripsHandleSelector(t *testing.T) {
	t.Parallel()

	const handle = "workload:api-svc"
	const scoped = "workload:repository:r_d5318f0f:api-svc"
	var nameProbeParam string
	handler := &Handler{
		Neo4j: graph.FakeWorkloadGraphReader{
			RunFn: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				switch {
				case strings.Contains(cypher, "w.name = $service_name"):
					nameProbeParam, _ = params["service_name"].(string)
					return []map[string]any{
						{"id": scoped, "name": "api-svc", "kind": "service", "repo_id": "repository:r_d5318f0f", "defining": []string{"repository:r_d5318f0f"}},
					}, nil
				default:
					return nil, nil
				}
			},
		},
	}

	result, err := handler.fetchServiceWorkloadContext(context.Background(), handle, "service_context")
	if err != nil {
		t.Fatalf("fetchServiceWorkloadContext() error = %v, want nil", err)
	}
	if result == nil {
		t.Fatal("fetchServiceWorkloadContext() = nil, want the scoped api-svc context")
	}
	if nameProbeParam != "api-svc" {
		t.Fatalf("name probe param = %q, want stripped bare name %q", nameProbeParam, "api-svc")
	}
}
