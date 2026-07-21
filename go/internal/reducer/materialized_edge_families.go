// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "sort"

// MaterializedEdgeFamilies returns the drift-proof enumeration of reducer
// domains that materialize graph edges directly from parser/collector facts,
// for the Ifá `materialized_edges:<domain>` exhaustiveness gate (#5351). The
// gate binds an Odù expectation to each returned family so a reducer
// materialization silently ceasing to produce an edge family is caught,
// mirroring the P2/P4 graph-determinism and fault-injection proof this
// package already backs (go/internal/ifa/graphdump, scripts/verify-ifa-
// determinism.sh, scripts/verify-ifa-fault-injection.sh).
//
// The result is exactly allProjectionDomains (shared_projection.go), sorted:
// the 12 reducer-owned shared/edge projection domains that write graph edges
// through the ordering-safe shared-projection intent path (repo_dependency,
// workload_dependency, code_calls, sql_relationships, shell_exec,
// inheritance_edges, documentation_edges, rationale_edges,
// deployable_unit_edges, handles_route, runs_in, invokes_cloud_action).
// TestMaterializedEdgeFamiliesLocksToAllProjectionDomains locks the two in
// lockstep so a domain added to or removed from allProjectionDomains moves
// this enumeration in the same change, never a second hand-edit.
//
// This is a narrower set than the reducer's full materialized-edge surface:
// reducer domains that write edges directly (not through the shared
// projection intent path — for example the IAM CAN_PERFORM/CAN_ASSUME,
// S3 LOGS_TO, EC2 USES_PROFILE, Crossplane SATISFIED_BY, and cloud-provider
// relationship-materialization families) are NOT enumerated here. #5351
// lands the gate plus first coverage for the sql_relationships family only,
// waiving the other 11 allProjectionDomains members to child issues; adding
// the direct-materialization families to this enumeration is deliberately
// deferred follow-up work, tracked under the umbrella follow-up #5543 (the
// same issue the 11 not-yet-covered allProjectionDomains families are waived
// to in specs/ifa-materialized-edge-coverage.v1.yaml).
func MaterializedEdgeFamilies() []string {
	out := make([]string, 0, len(allProjectionDomains))
	for _, domain := range allProjectionDomains {
		out = append(out, string(domain))
	}
	sort.Strings(out)
	return out
}
