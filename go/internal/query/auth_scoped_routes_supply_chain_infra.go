package query

import "net/http"

// scopedSBOMAttestationAttachmentRoute reports whether the request targets one
// of the reducer-owned SBOM/attestation attachment read routes. Attachment
// facts key on an image subject_digest but carry git repository_ids, so scoped
// reads intersect repository_ids (and the missing-evidence probe) with the
// grant set; attachments with no granted-repo correlation stay invisible.
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
// granted repositories. Adjacent supply-chain routes (impact explain, advisory
// detail, SBOM attestation attachments, container-image identities, and
// security-alert reconciliations) stay fail-closed for scoped tokens until each
// is separately proven tenant-filtered.
func scopedSupplyChainImpactRoute(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	switch r.URL.Path {
	case "/api/v0/supply-chain/impact/findings",
		"/api/v0/supply-chain/impact/findings/count",
		"/api/v0/supply-chain/impact/inventory":
		return true
	default:
		return false
	}
}

// scopedInvestigationPacketRoute reports whether the request targets a portable
// investigation packet route whose handler intersects scoped-token grants before
// data reads. Supply-chain packets require a granted repository selector before
// the impact explanation store is read; deployable-unit packets reuse the
// admission-decision scope/repository grant filter. Drift packets stay
// fail-closed for scoped tokens until drift rows carry repository-grant proof.
func scopedInvestigationPacketRoute(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	switch r.URL.Path {
	case "/api/v0/investigations/supply-chain/impact/packet",
		"/api/v0/investigations/deployable-unit/packet":
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

func scopedServiceCatalogCorrelationRoute(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/api/v0/service-catalog/correlations"
}

func scopedPackageRegistryCorrelationRoute(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/api/v0/package-registry/correlations"
}

// scopedAdmissionDecisionRoute reports whether the request targets the
// reducer-owned correlation admission decision read route. The handler requires
// domain, scope_id, and generation_id before reading and intersects scoped-token
// grants with scope_id plus repository anchors, so empty or out-of-grant scoped
// tokens get a bounded empty page without a store read.
func scopedAdmissionDecisionRoute(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/api/v0/evidence/admission-decisions"
}
