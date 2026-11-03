// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"github.com/eshu-hq/eshu/go/internal/ifa/materializededges"
)

// endpointCodeownerTeamLabel is the one node label endpointID's "ref"
// fallback applies to. Kept as a named constant rather than an inline
// string literal so the scoping is visible at the call site, not buried in
// endpointID's body.
const endpointCodeownerTeamLabel = "CodeownerTeam"

// endpointEnvironmentLabel is the one node label endpointID's "name" fallback
// applies to, kept as a named constant for the same reason
// endpointCodeownerTeamLabel is.
const endpointEnvironmentLabel = "Environment"

// endpointRuntimeDeclarationLabel is the one node label endpointID's
// five-part composite fallback applies to, kept as a named constant for the
// same reason endpointCodeownerTeamLabel is.
const endpointRuntimeDeclarationLabel = "RuntimeDeclaration"

// endpointID extracts a node's canonical identity: its "uid" when present,
// its "id" next, then — each scoped to a single node label — a CodeownerTeam
// endpoint's "ref", an Environment endpoint's "name", or a RuntimeDeclaration
// endpoint's five-part MERGE key. It returns "" when none of the applicable
// checks resolves, which the caller reports as an unmaterialized endpoint.
func endpointID(props map[string]any, labels []string) string {
	if props == nil {
		return ""
	}
	if uid, ok := props["uid"].(string); ok && uid != "" {
		return uid
	}
	// Fall back to "id": the graph keys node labels two different ways and the
	// assert path has to speak both. Content entities (Function, Class, File,
	// the SQL family's endpoints) are uid-keyed, but Repository, Workload,
	// WorkloadInstance and Platform are MERGEd `{id: ...}` and carry no uid at
	// all (canonical_node_cypher.go:98, canonical.go:24/36/50).
	//
	// Reading only "uid" made every repo_dependency and workload_dependency edge
	// look like it had an unmaterialized endpoint, which is both wrong and a
	// misleading diagnosis — the node exists, it is simply keyed by id. uid stays
	// first so uid-bearing endpoints are unaffected.
	if id, ok := props["id"].(string); ok && id != "" {
		return id
	}
	// Fall back to "ref", but ONLY for a CodeownerTeam-labeled endpoint:
	// CodeownerTeam is MERGEd `{ref: row.owner_ref}`
	// (canonical_codeowners_edges.go) and carries neither uid nor id. Without
	// this fallback, the target endpoint of every DECLARES_CODEOWNER edge
	// would report as unmaterialized even though the team node exists and is
	// correctly identified by its ref.
	//
	// Scoped by label, not applied to every endpoint: an unscoped fallback
	// rests entirely on the (unverifiable at this call site) claim that no
	// OTHER label is ever keyed on a top-level "ref" property. A node in a
	// uid/id-keyed family that lost its real identity but happened to carry
	// an incidental "ref" would then read as "identified" instead of
	// "unmaterialized" — the exact false pass this scoping closes. See
	// TestRefFallbackIsScopedToCodeownerTeam.
	if hasLabel(labels, endpointCodeownerTeamLabel) {
		ref, _ := props["ref"].(string)
		return ref
	}
	// Fall back to "name", but ONLY for an Environment-labeled endpoint.
	// Environment is MERGEd `{name: row.environment}`
	// (canonicalKubernetesNamespaceWithEnvironmentUpsertCypher) and carries
	// neither uid nor id, so without this the target endpoint of every
	// TARGETS_ENVIRONMENT edge reports as unmaterialized. Measured, not
	// assumed: a live kubernetes_namespace_environment drive materialized both
	// edges and both Environment nodes correctly, and assert-edges still
	// failed them as unmaterialized endpoints.
	//
	// The label scoping matters more here than it does for "ref" above, not
	// less. "name" is a common display property — Repository, Workload and
	// most content entities carry one beside their real identity — so an
	// unscoped fallback would let any uid/id-keyed node that lost its real
	// identity pass as "identified" on its display name alone. See
	// TestNameFallbackIsScopedToEnvironment.
	if hasLabel(labels, endpointEnvironmentLabel) {
		name, _ := props["name"].(string)
		return name
	}
	// Fall back to the five-part composite, but ONLY for a
	// RuntimeDeclaration-labeled endpoint. RuntimeDeclaration is MERGEd on
	// (repo_id, relative_path, line_number, runtime, order_index)
	// (edge/writer/runtime_declaration.go) and carries neither uid nor id, so
	// without this the target endpoint of every DECLARES_RUNTIME edge reports
	// as unmaterialized. The join is rendered by
	// materializededges.RuntimeDeclarationEntityID, the ONE shared derivation
	// the offline guard renders its expected-set target through -- both sides
	// call it, so the two renders cannot drift. It fails closed on a missing
	// key part, which surfaces here as "" and is reported as an
	// unmaterialized endpoint, never a partial join.
	//
	// Scoped by label for the same reason as the two fallbacks above, and
	// more so: every property in the composite is an ordinary property some
	// other label could carry beside its real identity, so an unscoped
	// fallback would let unrelated nodes resolve on coincidental properties.
	// See TestCompositeFallbackIsScopedToRuntimeDeclaration and
	// TestCompositeFallbackMissingKeyPartIsEndpointDefect.
	if hasLabel(labels, endpointRuntimeDeclarationLabel) {
		return materializededges.RuntimeDeclarationEntityID(props)
	}
	return ""
}

// hasLabel reports whether a graph endpoint carries the required node label.
//
// Endpoints carry a handful of labels in practice (1-3 in this graph), and this
// runs once per edge per gate run, so the linear scan is deliberate — an index or
// set conversion would cost more to build than it saves.
//
// Endpoints commonly carry several labels, so this is membership rather than
// equality. An empty required label would match nothing and silently drop the
// edge type from the assertion; the cypher-side guard rejects blank labels so
// that cannot reach here, and this returns false rather than true to keep the
// failure loud if it ever does.
func hasLabel(labels []string, required string) bool {
	if required == "" {
		return false
	}
	for _, label := range labels {
		if label == required {
			return true
		}
	}
	return false
}
