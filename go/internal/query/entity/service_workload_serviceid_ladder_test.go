// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// TestResolveServiceWorkloadCandidateFallsBackToNameOnHandleServiceID is the
// #5385 flip-wave read-side retirement for the service_id branch: the MCP
// service story and intelligence-report routes forward a workload: handle as
// service_id (canonicalWorkloadIdentifier), so an id-only probe misses every
// post-flip node. A handle service_id must fall back to the stripped-name
// probe, mirroring the ServiceName ladder, and resolve the scoped node id.
func TestResolveServiceWorkloadCandidateFallsBackToNameOnHandleServiceID(t *testing.T) {
	t.Parallel()

	const handle = "workload:api-svc"
	const scoped = "workload:repository:r_d5318f0f:api-svc"
	handler := &Handler{
		Neo4j: graph.FakeWorkloadGraphReader{
			RunFn: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				switch {
				case strings.Contains(cypher, "w.id = $service_id"):
					if got := params["service_id"]; got != handle {
						t.Fatalf("id probe param = %#v, want full selector %q", got, handle)
					}
					return nil, nil
				case strings.Contains(cypher, "w.name = $service_id"):
					if got := params["service_id"]; got != "api-svc" {
						t.Fatalf("name probe param = %#v, want stripped bare name", got)
					}
					return []map[string]any{
						{"id": scoped, "name": "api-svc", "kind": "service", "repo_id": "repository:r_d5318f0f"},
					}, nil
				case strings.Contains(cypher, "r.id IN $repo_ids"):
					return []map[string]any{
						{"repo_id": "repository:r_d5318f0f", "repo_name": "api"},
					}, nil
				default:
					return nil, nil
				}
			},
		},
	}

	candidate, err := handler.resolveServiceWorkloadCandidate(
		context.Background(),
		ServiceWorkloadSelector{ServiceName: handle, ServiceID: handle},
		"platform_impact.context_overview",
	)
	if err != nil {
		t.Fatalf("resolveServiceWorkloadCandidate() error = %v, want nil", err)
	}
	if candidate == nil {
		t.Fatal("resolveServiceWorkloadCandidate() = nil, want the scoped api-svc candidate")
	}
	if candidate.ID != scoped {
		t.Fatalf("candidate.ID = %q, want scoped %q", candidate.ID, scoped)
	}
}
