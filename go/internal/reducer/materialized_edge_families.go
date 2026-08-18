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
// the 14 reducer-owned shared/edge projection domains that write graph edges
// through the ordering-safe shared-projection intent path (repo_dependency,
// workload_dependency, code_calls, sql_relationships, shell_exec,
// inheritance_edges, documentation_edges, rationale_edges,
// deployable_unit_edges, handles_route, runs_in, invokes_cloud_action,
// codeowners_ownership_edges, submodule_pin_edges).
// The full set contains 14 allProjectionDomains families.
// TestMaterializedEdgeFamiliesLocksToAllProjectionDomains locks the two in
// lockstep so a domain added to or removed from allProjectionDomains moves
// this enumeration in the same change, never a second hand-edit.
//
// This is a narrower set than the reducer's full materialized-edge surface:
// reducer domains that write edges directly (not through the shared
// projection intent path — for example the IAM CAN_PERFORM/CAN_ASSUME,
// S3 LOGS_TO, EC2 USES_PROFILE, Crossplane SATISFIED_BY, and cloud-provider
// relationship-materialization families) are NOT enumerated here. #5351
// landed the gate plus first coverage for sql_relationships, leaving the rest
// waived to per-family child issues. #5991 later added the live code_calls
// baseline/fault proof, #5994 added documentation_edges, #5998 added
// rationale_edges, and #5993/#6158 wired deployable_unit_edges' vacuity
// guard into the resolver dispatch and its Odu into the catalog -- both
// existed with their own tests but neither was reachable from a production
// coverage run. Those changes removed their families' waivers.
// The current 8 not-yet-covered allProjectionDomains families remain tracked
// by #5543 in specs/ifa-materialized-edge-coverage.v1.yaml. Adding direct-materialization
// families to this enumeration remains separate follow-up work under #5543
// because they bypass the shared intent path this function inventories.
func MaterializedEdgeFamilies() []string {
	out := make([]string, 0, len(allProjectionDomains))
	for _, domain := range allProjectionDomains {
		out = append(out, string(domain))
	}
	sort.Strings(out)
	return out
}
