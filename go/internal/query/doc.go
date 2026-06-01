// Package query owns Eshu's HTTP read surface and the read models behind API,
// MCP, and CLI query workflows.
//
// The package mounts /api/v0 routes, assembles the static OpenAPI document,
// negotiates the canonical {data, truth, error} response envelope, and enforces
// capability gates for each runtime profile. Handlers read through ports such
// as GraphQuery and ContentStore rather than concrete Neo4j, NornicDB, or
// Postgres drivers, so backend-specific behavior stays behind narrow adapter
// seams.
//
// Handler behavior, OpenAPI fragments, docs/public/reference/http-api.md,
// truth-envelope fields, and MCP tool dispatch must stay aligned whenever a
// public route or response shape changes. Code-quality and dead-code responses
// also preserve language maturity, exactness blockers, modeled roots, and
// source handles so callers can distinguish cleanup-ready findings from
// ambiguous or suppressed evidence.
//
// Supply-chain reads expose source-only advisory evidence separately from
// reducer-owned impact findings. Advisory evidence groups active
// vulnerability source facts under canonical GHSA/CVE/OSV/NVD identities while
// preserving CVSS, EPSS, KEV, CWE, range, fixed-version, withdrawn, and
// disagreement provenance without implying repository, image, workload, or
// deployment impact. Impact reads pair the bounded reducer-owned findings page
// with a readiness envelope so a zero-finding answer can be classified as
// not_configured, target_incomplete, evidence_incomplete, ready_zero_findings,
// ready_with_findings, or unsupported. Unsupported matcher ecosystems and
// other unsupported targets are coverage-gap evidence, not impact findings.
// The readiness layer also exposes bounded source-snapshot cache metadata for
// advisory sources and scoped package-registry freshness for package/repository
// targets, stripping absent optional fields from the Postgres JSON rollup
// before decoding. It never invents findings or duplicates reducer matching:
// supported impact-matcher ecosystems are classified from existing source and
// reducer facts so the answer is diagnosable without re-querying. Provider
// security-alert reconciliation list, count, and inventory reads select one
// current reducer row per provider alert identity before applying default
// status/state filters, while each returned row keeps reducer reason and
// evidence fact ids for audit.
// The companion explain route accepts one finding id or an advisory/CVE plus
// package, repository, or image digest scope, then hydrates only the finding's
// referenced evidence facts. It reports advisory, package/version,
// dependency-chain, manifest/SBOM/image/workload anchors, freshness, and
// missing-evidence reasons without adding whole-graph traversal or inventing
// reachability truth.
//
// Supply-chain impact rows also carry a reducer suppression decision that
// captures the VEX or operator-policy state (active, not_affected,
// accepted_risk, false_positive, ignored, expired, provider_dismissed,
// scope_mismatch) plus source, justification, author, timestamps, reason,
// evidence reference, and VEX document/statement IDs. The listing route
// accepts include_suppressed and suppression_state filters so callers can
// include or exclude operator-suppressed findings and explain why; provider
// dismissals stay evidence, not automatic suppressions.
//
// Each supply-chain impact row and explain payload also carries an
// advisory-only Remediation block (issue #595) with the installed version,
// vulnerable range, first patched version, every published fixed-version
// branch, the manifest range, a manifest_allows_fix tri-state, the
// direct/transitive designation, a parent_package needed for transitive
// upgrades, the ecosystem the recommendation applies to, an
// exact|partial|unknown confidence label, and a closed reason enum
// (direct_upgrade_allowed, direct_range_blocked,
// transitive_parent_upgrade_required, no_patched_version,
// multiple_patched_branches, package_manager_unsupported,
// manifest_range_missing, manifest_range_malformed,
// installed_version_missing, installed_version_malformed). Eshu does not
// open pull requests from this block; it is strictly advisory so callers
// can decide whether and how to upgrade.
//
// Incident-context reads expose PagerDuty incident source facts as bounded
// packets with provider state, timeline events, fallback service/time change
// candidates, and explicit missing evidence slots. They do not call provider
// APIs. Runtime and image slots are promoted only when an explicit
// service-catalog operational link connects the PagerDuty service to a catalog
// correlation and reducer-owned image or Kubernetes evidence proves the next
// hop. Build/deploy and commit slots are promoted only from reducer-owned
// CI/CD run correlations tied to the selected image digest or reference;
// pull-request slots require provider pull-request evidence tied to that
// commit. Jira work-item links enrich the path from explicit remote links or
// issue-key evidence, but Jira-only pull-request URLs do not verify pull
// request identity by themselves.
package query
