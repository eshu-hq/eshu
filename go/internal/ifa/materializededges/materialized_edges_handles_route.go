// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// handlesRouteFamily is the materialized-edge family key this guard asserts
// (#5995), matching the domain key MaterializedEdgeDomainEdgeTypes switches
// on and the registry entry cypher.SingleTypeMaterializedEdgeTypes resolves
// it through.
const handlesRouteFamily = "handles_route"

// handlesRouteExpectedEdgesPath joins repoRoot onto the family's hand-derived
// expected-edge-set fixture.
func handlesRouteExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, "go/internal/ifa/testdata/handlesroute/ifa-handles-route-family-expected-edges.json")
}

// resolveHandlesRouteMaterializedEdges is handles_route's named vacuity
// guard (#5995).
//
// HANDLES_ROUTE's crux (see materialized_edges_symbol_runtime_shared.go):
// the intent-level dedupe key (functionID, repoID, path, http_method)
// permits two intent rows -- one per HTTP method -- for the same route, but
// the graph-write MERGE identity is only the (Function, HANDLES_ROUTE,
// Endpoint) node pair, so both rows MERGE the SAME relationship instance.
// Converting intent rows to ExpectedEdge one-for-one would therefore report
// a spurious EXTRA the moment a fixture ever carried two methods on one
// path. This guard instead dedupes rows onto their true edge identity
// (functionID, repoID, path) via handlesRouteRowsToExpectedEdges before
// comparing -- collapsing same-identity multi-method rows to exactly one
// edge, matching what a live MERGE actually produces.
//
// The Endpoint target is not a value carried anywhere in the intent row's
// payload: the live Cypher's MATCH resolves it by the (repo_id, path)
// property pair at write time, but the Endpoint node's own MERGE-key
// identity is stableAPIEndpointID(repo_id, workload_id, path), an unexported
// reducer function requiring the workload_id no intent-row payload carries
// either. This guard resolves that same id offline by running the SAME
// reducer.ExtractWorkloadCandidates -> reducer.BuildProjectionRows seam the
// live workload-materialization handler runs over the Odù's own facts,
// reading the resulting EndpointRows (an exported field) rather than
// inventing or hardcoding the hash.
func resolveHandlesRouteMaterializedEdges(odu ifa.Odu, expectedEdgesPath string) (bool, string) {
	expected, err := LoadExpectedEdges(expectedEdgesPath, handlesRouteFamily)
	if err != nil {
		return false, err.Error()
	}
	registry, err := MaterializedEdgeDomainEdgeTypes(handlesRouteFamily)
	if err != nil {
		return false, err.Error()
	}
	if missing := missingSymbolRuntimeExpectedTypes(expected, registry); len(missing) > 0 {
		return false, fmt.Sprintf("odù %q: expected-edge set does not cover all registry types, missing: %v", odu.Name, missing)
	}
	relType, err := singleRegistryEdgeType(handlesRouteFamily, registry)
	if err != nil {
		return false, err.Error()
	}

	if len(odu.Facts) == 0 {
		return false, fmt.Sprintf("odù %q: carries no facts", odu.Name)
	}
	rows := symbolRuntimeUpsertRows(odu.Facts, reducer.DomainHandlesRoute)
	if len(rows) == 0 {
		return false, fmt.Sprintf("odù %q: ExtractSymbolRuntimeIntentRows produced zero HANDLES_ROUTE upsert rows; this fixture cannot prove anything", odu.Name)
	}

	candidates, deploymentEnvironments := reducer.ExtractWorkloadCandidates(odu.Facts)
	projection := reducer.BuildProjectionRows(candidates, deploymentEnvironments)
	endpointIDs := handlesRouteEndpointIDsByRepoPath(projection.EndpointRows)

	actual, unresolved := handlesRouteRowsToExpectedEdges(rows, endpointIDs, relType)
	if len(unresolved) > 0 {
		return false, fmt.Sprintf("odù %q: no workload-materialization Endpoint resolved for %d (repo_id, path) pair(s) HANDLES_ROUTE needs: %v; HANDLES_ROUTE cannot bind without a workload-committed Endpoint at that key", odu.Name, len(unresolved), unresolved)
	}

	if mismatch := compareSymbolRuntimeExpectedEdges(odu.Name, "handles route", expected, actual); mismatch != "" {
		return false, mismatch
	}
	return true, fmt.Sprintf(
		"odù %q: ExtractSymbolRuntimeIntentRows + ExtractWorkloadCandidates/BuildProjectionRows reproduces the expected %d-edge HANDLES_ROUTE set exactly, deduped from %d intent row(s) across %d fact envelope(s)",
		odu.Name, len(expected), len(rows), len(odu.Facts),
	)
}

// handlesRouteEndpointIDsByRepoPath indexes a workload projection's
// EndpointRows by (repo_id, path) so handlesRouteRowsToExpectedEdges can
// resolve each intent row's true Endpoint MERGE-key id, mirroring the same
// (repo_id, path) property pair the live Cypher MATCHes on.
func handlesRouteEndpointIDsByRepoPath(rows []reducer.APIEndpointRow) map[string]string {
	out := make(map[string]string, len(rows))
	for _, row := range rows {
		out[row.RepoID+"\x00"+row.Path] = row.EndpointID
	}
	return out
}

// handlesRouteRowsToExpectedEdges dedupes HANDLES_ROUTE upsert rows onto
// their true edge identity (function_entity_id, repo_id, path) -- the same
// identity the graph MERGE uses, which omits http_method -- resolving each
// surviving edge's Endpoint target through endpointIDs. It returns the
// sorted list of (repo_id, path) keys it could not resolve, so the caller can
// report every gap at once rather than failing on the first.
//
// This is the one function a synthetic unit test drives directly to prove
// the GET+POST same-path collapse deterministically (two rows sharing an
// identity but differing only in http_method must yield exactly one
// ExpectedEdge), without needing a live backend or a second fixture route.
func handlesRouteRowsToExpectedEdges(
	rows []reducer.SharedProjectionIntentRow,
	endpointIDs map[string]string,
	relationshipType string,
) (edges []ExpectedEdge, unresolved []string) {
	seen := make(map[string]struct{}, len(rows))
	unresolvedSet := make(map[string]struct{})
	for _, row := range rows {
		functionID := anyToStringValue(row.Payload["function_entity_id"])
		repoID := anyToStringValue(row.Payload["repo_id"])
		path := anyToStringValue(row.Payload["path"])
		identityKey := functionID + "\x00" + repoID + "\x00" + path
		if _, dup := seen[identityKey]; dup {
			continue
		}
		seen[identityKey] = struct{}{}

		endpointID, ok := endpointIDs[repoID+"\x00"+path]
		if !ok {
			unresolvedSet[repoID+"\x00"+path] = struct{}{}
			continue
		}
		edges = append(edges, ExpectedEdge{
			RelationshipType: relationshipType,
			SourceEntityID:   functionID,
			TargetEntityID:   endpointID,
		})
	}
	for key := range unresolvedSet {
		unresolved = append(unresolved, key)
	}
	sort.Strings(unresolved)
	return edges, unresolved
}
