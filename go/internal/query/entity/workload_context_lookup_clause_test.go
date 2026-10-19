// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"testing"
)

// TestWorkloadContextLookupClauseLaddersHandles is the #5385 flip-wave
// read-side retirement for GET /api/v0/workloads/{workload_id}/context and
// .../story: a workload: handle matches no post-flip node id, so it must
// read the bounded name-candidate rung with the stripped bare name (grant
// admission and lowest-id-wins come from the existing lookupWorkloadRows
// name branch). Full scoped ids keep the exact id rung; bare inputs keep the
// id rung unchanged (they 404d before the flip and still do).
func TestWorkloadContextLookupClauseLaddersHandles(t *testing.T) {
	t.Parallel()

	where, params := workloadContextLookupClause("workload:api-svc")
	if where != "w.name = $workload_id" {
		t.Fatalf("handle where = %q, want the name-candidate rung", where)
	}
	if params["workload_id"] != "api-svc" {
		t.Fatalf("handle param = %#v, want stripped bare name", params["workload_id"])
	}

	where, params = workloadContextLookupClause("workload:repository:r_d5318f0f:api-svc")
	if where != "w.id = $workload_id" {
		t.Fatalf("scoped-id where = %q, want the exact id rung", where)
	}
	if params["workload_id"] != "workload:repository:r_d5318f0f:api-svc" {
		t.Fatalf("scoped-id param = %#v, want the id unchanged", params["workload_id"])
	}

	where, params = workloadContextLookupClause("api-svc")
	if where != "w.id = $workload_id" {
		t.Fatalf("bare where = %q, want the id rung unchanged", where)
	}
	if params["workload_id"] != "api-svc" {
		t.Fatalf("bare param = %#v, want the input unchanged", params["workload_id"])
	}
}
