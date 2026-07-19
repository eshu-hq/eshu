// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "net/http"

// This file holds the scoped-token route predicates for the supply-chain and
// infrastructure read surfaces. They are split out of auth_scoped_routes.go to
// keep that file under the repository's file-size cap. Each predicate reports
// whether a request targets a route whose handler is proven to intersect its
// reads with the scoped-token grant set (see each function's contract); routes
// not listed here stay deny-by-default for scoped tokens.

func scopedSBOMAttestationAttachmentRoute(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	switch r.URL.Path {
	case "/api/v0/supply-chain/sbom-attestations/attachments",
		"/api/v0/supply-chain/sbom-attestations/attachments/count",
		"/api/v0/supply-chain/sbom-attestations/attachments/inventory":
		return true
	default:
		return false
	}
}

// scopedSecurityAlertReconciliationRoute reports whether the request targets one
// of the reducer-owned provider security-alert reconciliation read routes.
// Reconciliation facts carry a git repository_id plus provider keys; scoped
// reads intersect those with the grant set and out-of-grant repository
// selectors fail before the reconciliation store read.
func scopedSecurityAlertReconciliationRoute(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	switch r.URL.Path {
	case "/api/v0/supply-chain/security-alerts/reconciliations",
		"/api/v0/supply-chain/security-alerts/reconciliations/count",
		"/api/v0/supply-chain/security-alerts/reconciliations/inventory":
		return true
	default:
		return false
	}
}

// scopedInfraResourceAggregateRoute reports whether the request targets one of
// the graph-backed infra resource aggregate read routes (count, inventory).
// These aggregate over the whole-graph infrastructure corpus; scoped tokens
// bind a repository-anchored predicate (see infraResourceScopePredicate) so
// totals, rollups, and inventory buckets are computed over only the resources
// attributable to granted repositories.
func scopedInfraResourceAggregateRoute(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	switch r.URL.Path {
	case "/api/v0/infra/resources/count",
		"/api/v0/infra/resources/inventory":
		return true
	default:
		return false
	}
}

// scopedInfraSearchRoute reports whether the request targets the graph-backed
// infra resource search route. The search handler runs its own whole-graph
// Cypher; scoped tokens bind the same repository-anchored predicate
// (infraResourceScopePredicate) so the matched rows, counts, limit, and
// truncation are computed over only the resources attributable to granted
// repositories, and an empty-grant scoped token returns a bounded empty page
// without a graph read.
func scopedInfraSearchRoute(r *http.Request) bool {
	return r.Method == http.MethodPost && r.URL.Path == "/api/v0/infra/resources/search"
}

// scopedInfraRelationshipsRoute reports whether the request targets the
// graph-backed infra relationship route. The handler anchors the seed node and
// every returned neighbor to a granted repository via infraResourceScopePredicate
// so a relationship is visible only when both endpoints are attributable to a
// granted repository; an out-of-grant or unanchorable seed returns not_found
// (no existence disclosure) and an empty grant fails closed without a graph read.
func scopedInfraRelationshipsRoute(r *http.Request) bool {
	return r.Method == http.MethodPost && r.URL.Path == "/api/v0/infra/relationships"
}

// scopedIaCResourceListRoute reports whether the request targets the
// graph-backed IaC resource browse read. The handler scans one canonical
// Terraform/IaC label (TerraformResource, TerraformModule, TerraformDataSource),
// each of whose nodes carries a durable `repo_id` property; scoped tokens bind a
// repository-anchored predicate (see iacResourceScopeClause) so the listed rows,
// count, limit+1 truncation, and keyset cursor are computed over only the
// resources attributable to granted repositories, and an empty-grant scoped
// token returns a bounded empty page without a graph read.
func scopedIaCResourceListRoute(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/api/v0/iac/resources"
}

// scopedReplatformingSelectorRoute reports whether the request targets the
// Postgres-backed active AWS selector inventory. The handler passes exact
// granted AWS scope ids to the store before counts, truncation, or readiness
// are computed; repository-only and empty grants return an empty page without
// a store read because this path has no authoritative repository-to-scope map.
func scopedReplatformingSelectorRoute(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/api/v0/replatforming/selectors"
}

// scopedWorkItemEvidenceRoute reports whether the request targets the
// source-only work-item evidence read GET /api/v0/work-items/evidence.
// Work-item facts key on the provider project scope (scope_id, project_key,
// work_item_key), not a git repository, so scoped reads intersect the work
// item's durable linked_repository_id (resolved by the Jira collector from a
// confidently typed GitHub PR or GitLab MR link before redaction, #2160) with
// the grant set. A work item with no durable linked_repository_id — every fact
// kind except a canonicalized external_link, or an out-of-grant project
// selector — stays invisible to scoped tokens (fail-closed), never a
// provider-scope leak. The exact evidence path is matched so adjacent
// work-item sub-resources stay deny-by-default until each is separately proven
// tenant-filtered, and the admin POST query route remains admin-only.
func scopedWorkItemEvidenceRoute(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/api/v0/work-items/evidence"
}

func scopedVulnerabilityScannerContractRoute(r *http.Request) bool {
	return r.Method == http.MethodGet &&
		r.URL.Path == "/api/v0/supply-chain/vulnerability-scanner/contract"
}

// scopedSupplyChainImpactRoute reports whether the request targets one of the
// reducer-owned vulnerability impact read routes that compute counts, limits,
// truncation, aggregate grouping, and offsets over only the scoped-token's
// granted repositories. impact/explain (#5167 W5) intersects the matched
// finding's repository_id/scope_id with the same granted set before a finding
// is ever returned -- see explainSupplyChainImpactFindingQuery's $11/$12
// predicate -- so an out-of-grant finding_id, or a bare advisory/CVE plus
// package/image/workload/service anchor that would otherwise resolve to
// another tenant's finding, returns no_finding instead of leaking it. Adjacent
// supply-chain routes (advisory detail, SBOM attestation attachments,
// container-image identities, and security-alert reconciliations) stay
// fail-closed for scoped tokens until each is separately proven
// tenant-filtered.
func scopedSupplyChainImpactRoute(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	switch r.URL.Path {
	case "/api/v0/supply-chain/impact/findings",
		"/api/v0/supply-chain/impact/findings/count",
		"/api/v0/supply-chain/impact/inventory",
		"/api/v0/supply-chain/impact/explain":
		return true
	default:
		return false
	}
}

// scopedInvestigationPacketRoute reports whether the request targets a portable
// investigation packet route whose handler intersects scoped-token grants before
// data reads. Supply-chain packets require a granted repository selector before
// the impact explanation store is read; deployable-unit packets reuse the
// admission-decision scope/repository grant filter. Drift packets (#5167 W5)
// bind the scoped token's exact AllowedScopeIDs grant against the requested
// cloud ingestion scope_id (getDriftPacket) -- drift findings have no
// repository dimension at all, the same reason GET /api/v0/replatforming/
// selectors binds AllowedScopeIDs directly instead of a repository map -- so
// an empty grant or an out-of-grant scope_id returns a scope_not_found
// refusal packet without a drift finding store read.
func scopedInvestigationPacketRoute(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	switch r.URL.Path {
	case "/api/v0/investigations/supply-chain/impact/packet",
		"/api/v0/investigations/deployable-unit/packet",
		"/api/v0/investigations/drift/packet":
		return true
	default:
		return false
	}
}

// scopedSupplyChainAdvisoryEvidenceRoute reports whether the request targets the
// advisory-evidence read. Advisory facts are global CVE/advisory data with no
// repository of their own, so the bare id path is public; the
// repository/service/workload-anchored path intersects the impact findings that
// derive advisory anchors with the scoped-token grant set, and an out-of-grant
// repository selector fails before any store read.
func scopedSupplyChainAdvisoryEvidenceRoute(r *http.Request) bool {
	return r.Method == http.MethodGet &&
		r.URL.Path == "/api/v0/supply-chain/advisories/evidence"
}

func scopedServiceCatalogCorrelationRoute(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/api/v0/service-catalog/correlations"
}

func scopedPackageRegistryCorrelationRoute(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/api/v0/package-registry/correlations"
}

// scopedPackageRegistryIdentityRoute reports whether the request targets one
// of the 5 graph-backed package-registry identity/aggregate read routes
// (#5167 W5b). Unlike the correlation and dependency-chain routes (which
// anchor on a repository), these routes anchor on package identity or
// nothing at all -- (:Package)/(:PackageVersion)/(:PackageDependency) carry
// no repository/tenant property (package_registry_canonical.go). The
// handlers gate scoped callers on package visibility instead:
// visibility='public' rows are served (registry identity metadata is
// world-readable, the same class of global data the advisory-evidence
// precedent already exposes to scoped tokens), and private/unknown rows
// require a bounded LIMIT-1 correlation-grant probe reusing the exact
// predicate the already-shipped scoped correlations route exposes
// (package_registry_correlations.go, including the
// candidate_repository_ids ?| branch) before ever being returned; a probe
// miss returns the same empty page as a nonexistent package (no existence
// oracle). The aggregate routes (count, inventory) instead force
// visibility='public' onto the caller's filter, or return an empty envelope
// without a store read if the caller explicitly asked for private/unknown.
// See go/internal/query/package_registry_scoped_access.go for the gate
// implementation.
func scopedPackageRegistryIdentityRoute(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	switch r.URL.Path {
	case "/api/v0/package-registry/packages",
		"/api/v0/package-registry/versions",
		"/api/v0/package-registry/dependencies",
		"/api/v0/package-registry/packages/count",
		"/api/v0/package-registry/packages/inventory":
		return true
	default:
		return false
	}
}

// scopedPackageRegistryDependencyChainsRoute reports whether the request
// targets the repo-scoped package dependency chain read. The handler requires
// repository_id, intersects both the consumption and publisher reads with the
// scoped-token grant set via AllowedRepositoryIDs / AllowedScopeIDs, and
// short-circuits on an empty grant, so scoped callers see only chains whose
// consumer repository is within their grant.
func scopedPackageRegistryDependencyChainsRoute(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/api/v0/package-registry/dependency-chains"
}

// scopedAdmissionDecisionRoute reports whether the request targets the
// reducer-owned correlation admission decision read route. The handler requires
// domain, scope_id, and generation_id before reading and intersects scoped-token
// grants with scope_id plus repository anchors, so empty or out-of-grant scoped
// tokens get a bounded empty page without a store read.
func scopedAdmissionDecisionRoute(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/api/v0/evidence/admission-decisions"
}

// scopedContainerImageIdentityRoute reports whether the request targets one of
// the reducer-owned container image identity read routes that intersect
// source_repository_ids with the scoped-token grant set before counts,
// grouping, ordering, limits, offsets, truncation, and the source bridge.
// Identity facts key on the OCI repository_id and an OCI registry ingestion
// scope, so attribution to a granted git repository flows only through the
// source_repository_ids overlap; images with no source correlation stay
// invisible to scoped tokens.
func scopedContainerImageIdentityRoute(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	switch r.URL.Path {
	case "/api/v0/supply-chain/container-images/identities",
		"/api/v0/supply-chain/container-images/identities/count",
		"/api/v0/supply-chain/container-images/identities/inventory":
		return true
	default:
		return false
	}
}

func scopedCICDRunCorrelationRoute(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	switch r.URL.Path {
	case "/api/v0/ci-cd/run-correlations",
		"/api/v0/ci-cd/run-correlations/count",
		"/api/v0/ci-cd/run-correlations/inventory":
		return true
	default:
		return false
	}
}
