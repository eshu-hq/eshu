// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"fmt"
)

// routeServesDataBacking is one closed-map entry mapping a read_surface route
// literal to the reducer-domain-level data it serves.
//
// The invariant this gate enforces: a family whose reducer_domain is
// kubernetes_correlation MUST route through a surface that serves
// kubernetes_correlation data — not CloudResource graph rows, not
// package-registry correlations, not an unrelated read model. This closes the
// #5480 gap class: a route can be live and mounted (the #5359 gate stays
// green) while serving data from an entirely different reducer domain.
type routeServesDataBacking struct {
	// ServedDomains is the set of reducer_domain values whose produced data
	// is surfaced through this route. A family's reducer_domain must be in
	// this set for the route→family pairing to be consistent.
	ServedDomains []string `yaml:"served_domains"`
}

// routeServesDataBackingMap is the closed, hand-maintained map from every
// distinct read_surface literal in specs/fact-kind-registry.v1.yaml to the
// reducer_domain values whose data that route surfaces. This is the #5474
// route-serves-data gate's core anti-misrouting mechanism: the gate fails
// closed for any family whose read_surface route is not in this map, and fails
// for any family whose reducer_domain is not in the route's ServedDomains.
//
// Entry discipline:
//   - Every distinct read_surface literal the registry uses (17 today) must
//     have an entry. A missing route fails closed.
//   - ServedDomains lists every reducer_domain whose data the route surfaces.
//     The relationship is many-to-many, not one route per domain: when a new
//     family shares an EXISTING route with another domain (e.g.
//     "GET /api/v0/cloud/inventory" already lists aws_cloud_runtime_drift,
//     azure_resource_materialization, and gcp_resource_materialization), add
//     its reducer_domain to that route's ServedDomains. The reverse also
//     happens — one domain's data can be surfaced through more than one
//     route (incident_repository_correlation appears in BOTH
//     "GET /api/v0/incidents/{incident_id}/context" and
//     "GET /api/v0/work-items/evidence" below) — so a domain is not assumed
//     to have exactly one route, and adding a domain to a second route's
//     ServedDomains is equally legitimate.
//   - read_surface_overrides (per-kind substitutions) are excluded from v1.
//
// Self-certification caveat (PR #5583 round-3 P1b, codex): this map is
// hand-maintained, not derived from the real handler/read-model wiring, for
// all 17 routes. Nothing here cross-checks a ServedDomains claim against the
// actual Go handler registered for that route, so the documented
// remediation for a genuine mismatch ("add the domain to
// routeServesDataBackingMap[route].ServedDomains") could — if misapplied —
// paper over a real #5480-class misrouting instead of fixing it.
// TestRouteServesData_CloudResourcesStructurallyExcludesKubernetesCorrelation
// (route_serves_data_structural_test.go) closes this gap for the ONE
// historical #5480 pair (kubernetes_live must not resolve to
// GET /api/v0/cloud/resources) by inspecting the real InfraHandler/
// KubernetesHandler source instead of this map, so that specific regression
// stays impossible even if this map is poisoned. Generalizing to a real,
// handler-derived expectation for all 17 routes (removing the
// self-certification gap map-wide) is tracked in issue #5584; until that
// lands, every route other than the kubernetes_live/cloud-resources pair
// remains self-certifying by design-for-now.
var routeServesDataBackingMap = map[string]routeServesDataBacking{
	"GET /api/v0/documentation/facts":                          {ServedDomains: []string{"documentation_materialization"}},
	"GET /api/v0/cloud/inventory":                              {ServedDomains: []string{"aws_cloud_runtime_drift", "azure_resource_materialization", "gcp_resource_materialization"}},
	"GET /api/v0/ci-cd/run-correlations":                       {ServedDomains: []string{"ci_cd_run_correlation"}},
	"GET /api/v0/repositories":                                 {ServedDomains: []string{"code_graph_projection"}},
	"GET /api/v0/supply-chain/impact/findings":                 {ServedDomains: []string{"reducer_derived_findings", "supply_chain_impact"}},
	"GET /api/v0/cloud/resources":                              {ServedDomains: []string{"ec2_instance_node_materialization", "rds_posture_materialization", "s3_internet_exposure_materialization"}},
	"GET /api/v0/incidents/{incident_id}/context":              {ServedDomains: []string{"incident_repository_correlation", "incident_routing_materialization"}},
	"GET /api/v0/kubernetes/correlations":                      {ServedDomains: []string{"kubernetes_correlation"}},
	"GET /api/v0/observability/coverage/correlations":          {ServedDomains: []string{"observability_coverage_correlation"}},
	"GET /api/v0/images":                                       {ServedDomains: []string{"container_image_identity"}},
	"GET /api/v0/package-registry/packages":                    {ServedDomains: []string{"package_source_correlation"}},
	"GET /api/v0/secrets-iam/posture-summary":                  {ServedDomains: []string{"s3_external_principal_grant_materialization", "secrets_iam_trust_chain"}},
	"GET /api/v0/supply-chain/sbom-attestations/attachments":   {ServedDomains: []string{"sbom_attestation_attachment"}},
	"GET /api/v0/supply-chain/security-alerts/reconciliations": {ServedDomains: []string{"security_alert_reconciliation"}},
	"GET /api/v0/semantic/documentation-observations":          {ServedDomains: []string{"semantic_entity_materialization"}},
	"GET /api/v0/service-catalog/correlations":                 {ServedDomains: []string{"service_catalog_correlation"}},
	"GET /api/v0/codeowners/ownership":                         {ServedDomains: []string{"codeowners_ownership"}},
	"GET /api/v0/iac/resources":                                {ServedDomains: []string{"config_state_drift"}},
	"GET /api/v0/work-items/evidence":                          {ServedDomains: []string{"incident_repository_correlation"}},
}

// resolveRouteServesData reports whether a family's read_surface route serves
// data from the family's declared reducer_domain. It consults
// routeServesDataBackingMap — the closed, compile-time mapping — not the live
// API inventory or route templates.
func resolveRouteServesData(family, reducerDomain, readSurface string) (ok bool, reason string) {
	backing, known := routeServesDataBackingMap[readSurface]
	if !known {
		return false, fmt.Sprintf(
			"family %q read_surface %q is not in the closed route-serves-data backing map (routeServesDataBackingMap) — add it",
			family, readSurface,
		)
	}
	for _, d := range backing.ServedDomains {
		if d == reducerDomain {
			return true, ""
		}
	}
	return false, fmt.Sprintf(
		"family %q read_surface %q serves domains %v, but the family's reducer_domain is %q — "+
			"either the family's read_surface is wrong (fix specs/fact-kind-registry.v1.yaml) "+
			"or the backing map is wrong (add %q to routeServesDataBackingMap[%q].ServedDomains)",
		family, readSurface, backing.ServedDomains, reducerDomain, reducerDomain, readSurface,
	)
}
