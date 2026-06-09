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
// deployment impact. Repository, service, and workload advisory evidence
// scopes derive only CVE/advisory/package anchors from active reducer-owned
// impact findings; provider-alert-only rows stay out of this path. Impact
// reads pair the bounded reducer-owned findings page with a readiness envelope
// so a zero-finding answer can be classified as
// not_configured, target_incomplete, evidence_incomplete, ready_zero_findings,
// ready_with_findings, or unsupported. Unsupported matcher ecosystems, scanner
// worker image warnings, and other unsupported targets are coverage-gap
// evidence, not impact findings. The readiness layer also exposes bounded
// source-snapshot and durable source-state freshness for advisory sources
// scoped by requested CVE, package, repository-owned ecosystem, or image
// component ecosystem, plus scoped package-registry freshness for
// package/repository targets. It strips absent optional fields from the
// Postgres JSON rollup before decoding. SBOM attachment reads expose
// `warning_summary_count` as the reducer-persisted aggregate occurrence count
// while keeping `warning_summaries` as a bounded duplicate-collapsed preview.
// They never invent findings or duplicate reducer matching: supported
// impact-matcher ecosystems are classified from existing source and reducer
// facts, while VCS/path/URL, editable, and other provenance-only dependency rows
// stay unsupported target evidence with stable reason codes. Provider
// security-alert reconciliation list, count, and inventory reads select one
// current reducer row per provider alert identity before applying default
// status/state filters, while each returned row keeps reducer reason and
// evidence fact ids for audit.
// Service-catalog correlation reads resolve repository selectors before reading
// reducer facts, return ambiguous candidate repository IDs when the reducer
// cannot select one repository, and attach missing-evidence classes to empty
// anchored pages instead of treating missing catalog hops as proof of absence.
// SBOM attachment list, count, and inventory reads resolve repository selectors
// before applying repository, workload, and service source anchors to
// reducer-owned attachment facts. Source-scoped reads expose missing image or
// image-to-SBOM evidence without promoting parse-only rows into canonical image
// attachment truth. Service-story image-package enrichment treats
// repository-only deployment image references as candidates, explains
// configured or unconfigured OCI collector state from bounded read models, and
// keeps digest, SBOM, and vulnerability truth absent until source collector
// evidence exists.
// The companion explain route accepts one finding id or an advisory/CVE plus
// package, repository, or image digest scope, then hydrates only the finding's
// referenced evidence facts. It reports advisory, package/version,
// dependency-chain, manifest/SBOM/image/workload anchors, freshness, and
// missing-evidence reasons without adding whole-graph traversal or inventing
// reachability truth. Repository-scoped service-catalog correlation evidence
// remains visible in list and explain evidence paths. Catalog entity refs are
// reported as catalog anchors without becoming service ids; only catalog
// evidence that lacks a service id, workload id, and entity ref reports
// service/workload catalog anchor missing instead of claiming catalog evidence
// is absent.
// Metrics reads expose bounded historical time-series for console trend panels
// through MetricsHandler. The handler accepts a MetricsTimeSeriesSource, with
// PrometheusMetricsTimeSeriesSource providing the Prometheus/Mimir query_range
// implementation when the API runtime is configured with a live metrics source.
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
// vulnerable range, selected fixed-version source, match reason, first patched
// version, every published fixed-version branch, the manifest range, a
// manifest_allows_fix tri-state, the direct/transitive designation, a
// parent_package needed for transitive upgrades, the ecosystem the
// recommendation applies to, an
// exact|partial|unknown confidence label, and a closed reason enum
// (direct_upgrade_allowed, direct_range_blocked,
// transitive_parent_upgrade_required, already_fixed, no_patched_version,
// multiple_patched_branches, package_manager_unsupported,
// manifest_range_missing, manifest_range_malformed,
// installed_version_missing, installed_version_malformed). Eshu does not
// open pull requests from this block; it is strictly advisory so callers
// can decide whether and how to upgrade.
// Pub impact rows use exact hosted pubspec.lock evidence for precise matching;
// pubspec.yaml ranges stay comprehensive until exact installed evidence exists.
//
// Incident-context reads expose PagerDuty incident source facts as bounded
// packets with provider state, timeline events, PagerDuty intended/applied/live
// routing evidence, fallback service/time change candidates, and explicit
// missing evidence slots. They do not call provider APIs. Intended routing comes
// from Terraform-source PagerDutyDeclaration content rows, applied routing from
// Terraform-state incident-routing facts, and live routing from optional
// PagerDuty API configuration facts. Runtime and image slots are promoted only
// when an explicit service-catalog operational link connects the PagerDuty
// service to a catalog correlation and reducer-owned image or Kubernetes
// evidence proves the next hop. Build/deploy and commit slots are promoted only
// from reducer-owned CI/CD run correlations tied to the selected image digest or
// reference; pull-request slots require provider pull-request evidence tied to
// that commit. Jira work-item links enrich the path from explicit remote links
// or issue-key evidence, and active Jira project/status metadata can explain the
// selected work item's project visibility and status category without reading
// raw issue bodies. Jira-only pull-request URLs do not verify pull request
// identity by themselves.
//
// WorkItemHandler, WorkItemEvidenceFilter, and WorkItemEvidenceRow expose
// Jira/work-item source facts directly for ticket-first prompts. They require a
// bounded scope anchor plus an explicit limit, sanitize external URLs to
// fingerprints, and return missing, stale, permission-hidden, unsupported-link,
// and rejected-payload states without promoting Jira-only URLs into
// pull-request, commit, deployment, runtime, image, service, or incident truth.
// The query.work_item_evidence span carries the telemetry package's bounded
// SpanAttrWorkItemEvidence* query, result, evidence-state, and truncation
// counts; raw URLs, issue summaries, users, and tenant values stay out of
// metric labels.
// Service and repository stories attach support evidence only through explicit
// candidate_refs, evidence_refs, or linked_entities entries that identify the
// selected workload/service or repository target. The support read path stays
// bounded to JSONB containment predicates over those structured references and
// never scans Jira summaries, issue titles, PagerDuty service names, or mention
// text to infer target ownership.
//
// Documentation finding and fact reads accept repo, target_id, target_kind, and
// service_id filters. Target-scoped finding responses keep admitted findings
// separate from raw collected documentation facts by returning coverage,
// related_facts, and missing_evidence metadata when facts mention the selected
// target but no admissible finding exists. Explicit target filters count only
// findings whose payload references match the selected target in coverage
// metadata, so unrelated repo-source findings do not hide target correlation
// gaps. Related fact previews use the same explicit target reference before
// falling back to repo-scoped documentation facts. A bare target_kind is not a
// canonical target selector without target_id or service_id, and invalid
// documentation fact reads report every accepted scope or target anchor.
// Documentation fact list responses also expose bounded page metadata
// (`count`, `limit`, `truncated`, `missing_evidence`, `states`, and
// `next_cursor` on truncated pages) so API and MCP clients can distinguish a
// complete scoped page from a continuation page or a scoped page with no
// collected documentation facts.
//
// AnswerPacket composes existing query truth into a user-ready response plan
// without losing structured evidence. It is a view over the canonical
// ResponseEnvelope, not a replacement: it copies the TruthEnvelope, references
// the envelope data, and reuses the evidence_citation handle and
// recommended_next_calls shapes. NewAnswerPacket and NewAnswerPacketFromCitations
// classify the answer into a single AnswerTruthClass derived from the existing
// TruthLevel and TruthBasis (deterministic, derived, fallback,
// semantic_observation, code_hint, or unsupported) and refuse to attach a
// confident summary when the envelope carries an error or an evidence-centric
// capability resolved no evidence, so an unanswerable question never becomes a
// confident sentence. The contract is documented in
// docs/public/reference/answer-packets.md; route and MCP wiring is follow-up
// work.
package query
