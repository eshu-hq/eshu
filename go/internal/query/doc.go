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
// or ready_with_findings. The readiness layer also exposes bounded
// source-snapshot cache metadata for advisory sources, stripping absent optional
// fields from the Postgres JSON rollup before decoding. It never invents
// findings or duplicates reducer matching: it counts existing source and
// reducer facts so the answer is diagnosable without re-querying.
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
package query
