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
//
// The fixture's GET+POST /widgets route pair, sharing one handler, proves
// the collapse LIVE (not only in a synthetic unit test): a regression that
// added http_method to the graph MERGE identity would split that pair into
// two edges here, and the exact-set comparison below would report it as an
// EXTRA by name. The fixture's second, distinct GET /healthz route (a
// different handler on a different path) additionally proves this is not a
// one-edge assertion that could not tell "processed every route" apart from
// "processed only the first one".
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

// handlesRouteEdgeAccumulator collects every distinct http_method observed
// across the intent rows that collapse onto one edge identity, in
// first-occurrence order (the extractor's own row order is already
// deterministic, so preserving it keeps this function deterministic too).
type handlesRouteEdgeAccumulator struct {
	functionID string
	repoID     string
	path       string
	methods    map[string]struct{}
}

// handlesRouteRowsToExpectedEdges dedupes HANDLES_ROUTE upsert rows onto
// their true edge identity (function_entity_id, repo_id, path) -- the same
// identity the graph MERGE uses, which omits http_method -- resolving each
// surviving edge's Endpoint target through endpointIDs. It returns the
// sorted list of (repo_id, path) keys it could not resolve, so the caller can
// report every gap at once rather than failing on the first.
//
// It also asserts http_method as a SET-only Property, but ONLY when every
// row that collapsed onto an identity agrees on exactly one method: a route
// served by a single method (e.g. GET-only /healthz) has a deterministic
// stored http_method, safe to assert. A route served by two-or-more methods
// on the same path (GET+POST /widgets) has NO documented write-order
// guarantee for which row's SET wins (family-semantics.md Sec.4), so
// asserting a value there would be flaky by construction -- that edge's
// Properties stays nil.
//
// This is the one function a synthetic unit test drives directly to prove
// the GET+POST same-path collapse deterministically (two rows sharing an
// identity but differing only in http_method must yield exactly one
// ExpectedEdge with no asserted http_method), without needing a live backend
// or a second fixture route.
func handlesRouteRowsToExpectedEdges(
	rows []reducer.SharedProjectionIntentRow,
	endpointIDs map[string]string,
	relationshipType string,
) (edges []ExpectedEdge, unresolved []string) {
	order := make([]string, 0, len(rows))
	byIdentity := make(map[string]*handlesRouteEdgeAccumulator, len(rows))
	for _, row := range rows {
		functionID := anyToStringValue(row.Payload["function_entity_id"])
		repoID := anyToStringValue(row.Payload["repo_id"])
		path := anyToStringValue(row.Payload["path"])
		httpMethod := anyToStringValue(row.Payload["http_method"])
		identityKey := functionID + "\x00" + repoID + "\x00" + path

		entry, ok := byIdentity[identityKey]
		if !ok {
			entry = &handlesRouteEdgeAccumulator{functionID: functionID, repoID: repoID, path: path, methods: map[string]struct{}{}}
			byIdentity[identityKey] = entry
			order = append(order, identityKey)
		}
		if httpMethod != "" {
			entry.methods[httpMethod] = struct{}{}
		}
	}

	unresolvedSet := make(map[string]struct{})
	for _, identityKey := range order {
		entry := byIdentity[identityKey]
		endpointID, ok := endpointIDs[entry.repoID+"\x00"+entry.path]
		if !ok {
			unresolvedSet[entry.repoID+"\x00"+entry.path] = struct{}{}
			continue
		}
		edge := ExpectedEdge{
			RelationshipType: relationshipType,
			SourceEntityID:   entry.functionID,
			TargetEntityID:   endpointID,
		}
		if len(entry.methods) == 1 {
			for method := range entry.methods {
				edge.Properties = map[string]string{"http_method": method}
			}
		}
		edges = append(edges, edge)
	}
	for key := range unresolvedSet {
		unresolved = append(unresolved, key)
	}
	sort.Strings(unresolved)
	return edges, unresolved
}
