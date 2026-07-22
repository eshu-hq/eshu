// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package query owns Eshu's HTTP read surface and the read models behind API,
// MCP, and CLI query workflows.
//
// The package mounts /api/v0 routes, assembles the static OpenAPI document,
// negotiates the canonical {data, truth, error} response envelope, and enforces
// capability gates for each runtime profile. Handlers read through ports such
// as GraphQuery and ContentStore rather than concrete Neo4j, NornicDB, or
// Postgres drivers, so backend-specific behavior stays behind narrow adapter
// seams. The static capabilities route exposes the embedded capability catalog,
// including built-in roles, grants, data classes, and per-capability
// authorization metadata, so API and MCP callers see the same grant contract;
// profile rows include the capability matrix's p95 latency and max-scope
// budgets, and its OpenAPI schema includes those budget fields plus the
// restricted and sensitive data-class sensitivity values used by the catalog.
// The static surface inventory route exposes the embedded surface catalog with
// collector source-to-read-surface contracts, so HTTP, MCP, and console callers
// can trace collector fact kinds to projection/read consumers and proof gates
// without hand-maintained provenance lists.
// Global code-name search and typed entity resolution use the narrow
// EntityNameSearcher content-index seam; they never fall back to an unanchored
// graph scan. The seam requires an explicit all-repository or repository-set
// scope so empty grants fail closed before SQL execution. Code search probes
// one row beyond the public limit, trims that probe row, and returns count,
// limit, and truncated consistently for global content, repository content,
// and repository graph branches.
// Import-dependency handlers keep one connected graph pattern per read,
// reconstruct Python cycles from a bounded edge set, and page distinct package
// identities. Internal candidate scans stop at 25,000 rows and return a scope-
// narrowing error instead of extending the request timeout.
// Call-graph metric handlers read the repository's directed CALLS edges in one
// Function.repo_id-indexed pass with a 50,001-edge sentinel, then compute
// distinct hub degree and recursive pairs in Go before deterministic paging.
// Repositories above the 50,000-edge exactness bound fail closed with HTTP 422
// and no partial rows. This bounds materialization without relying on
// backend-specific multi-clause aggregation shortcuts on the read path.
//
// Handler behavior, OpenAPI fragments, docs/public/reference/http-api.md,
// truth-envelope fields, and MCP tool dispatch must stay aligned whenever a
// public route or response shape changes. Code-quality and dead-code responses
// also preserve language maturity, exactness blockers, modeled roots, and
// source handles so callers can distinguish cleanup-ready findings from
// ambiguous or suppressed evidence.
// Repository source routes expose source-backed branch/ref metadata when
// ingestion captured it, and reject selected refs that cannot be served from
// the indexed commit instead of falling back silently.
// Repository relationship rows and relationship evidence drilldowns expose
// confidence_basis for correlation edges so clients can distinguish extractor
// constants, resolver aggregation, and assertion overrides from code-edge
// resolution_method confidence tiers.
// Relationship-story reads accept a bounded min_confidence response floor that
// filters returned rows without changing canonical graph truth.
// Relationship-story rows also include a uniform provenance object so API and
// MCP clients can compare code-edge and correlation-edge confidence at the row
// without changing admission policy or the answer-level TruthEnvelope. The
// provenance object carries a named confidence_tier derived from confidence
// (high/medium/low/unsupported) and the answer coverage carries a
// missing_edge_reason, truncation_state, and evidence_explanation so callers
// see why a result is empty or short; both are descriptive and never upgrade a
// heuristic or unsupported edge into canonical truth.
// Admission-decision reads expose reducer-owned admitted, rejected, ambiguous,
// stale, missing-evidence, permission-hidden, unsupported, and unsafe candidate
// rows under a domain/scope/generation boundary; they explain graph writes but
// are not themselves canonical graph edges.
// SQL-table blast radius traverses only writer-backed SQL edges, bounds
// view-on-view READS_FROM expansion to two hops, and reports any remaining
// unmaterialized relationship family through its coverage envelope.
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
// ready_with_findings, ambiguous_scope, or unsupported. Unsupported matcher
// ecosystems, scanner worker image warnings, and other unsupported targets are
// coverage-gap
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
// Component extension reads expose redacted trust decisions, policy gates,
// missing conformance proof state, scheduler state, and read-model
// availability from local registry readback without consulting graph truth.
// Governance status reads expose only redacted policy mode, state, source kind,
// revision hash, readiness booleans, aggregate counts, and reason codes. When a
// private governance audit sink is wired, the route reads only its aggregate
// summary; raw policy, tenant, workspace, source, detailed audit hashes,
// correlation ids, credential, endpoint, prompt, response, path, and token
// values stay out of API and MCP payloads.
// Hosted readiness also treats collector generation dead letters and unresolved
// replay requests as not-ready source-level replay work without exposing fact
// payloads, source paths, or provider responses.
// AuthContext carries scoped-token request bounds used by hosted isolation
// enforcement. Middleware resolves raw bearer tokens at the edge, stores only
// scoped tenant/workspace/subject hashes on the request context, keeps handlers
// free of token material, and fails closed before unsupported scoped routes
// reach handlers. Browser-session middleware hashes the HttpOnly session cookie
// and the `X-Eshu-CSRF` header proof before resolver calls on unsafe methods,
// then attaches the same scoped tenant/workspace/grant context for dashboard
// cookie reads. The Secure cookie attribute follows CookieSecureMode (env
// ESHU_AUTH_COOKIE_SECURE, default "auto", validated startup-closed by
// ValidateCookieSecureMode): every cookie-issuing handler keeps Secure set
// except for a plain-HTTP loopback request, so a non-loopback plain-HTTP
// deployment never receives a non-Secure cookie (#4964). A relaxed cookie
// switches off the __Host- name prefix (RFC 6265bis requires Secure for it)
// to a bare name every read path also accepts, so the browser does not
// silently reject the relaxed cookie outright. Local identity routes hash
// login, invite, MFA recovery, and
// break-glass proofs before storage calls; bootstrap and break-glass enablement
// stay behind shared-operator auth, while public login, invite acceptance, and
// break-glass session creation succeed only after the storage layer validates
// active hashed credentials or recovery windows. OIDC login and callback routes
// are exact public GET routes that complete provider validation before issuing
// browser-session cookies with bounded provider-proof staleness. SAML browser
// login routes keep metadata, AuthnRequest, ACS, assertion validation, replay
// reservation, and principal mapping behind the SAMLStore port, and they create
// the same hash-only browser-session records after a validated external subject
// resolves to an AuthContext. Repository list and selector canary routes apply
// these bounds before pagination, counts, ambiguity, and not-found decisions so
// out-of-scope repositories do not leak through metadata. Code search, entity
// resolution, content read/search routes, evidence citation packets, entity
// context reads, service/workload context or story reads, and service
// investigation reads apply the same bounds to selector resolution, graph
// predicates, exact content lookups, candidate ambiguity, citation handle
// hydration, repo-identity hydration, coverage metadata, recommended next
// calls, and content fallback calls before allowing scoped-token API or MCP
// reads. Query playbook catalog and resolver routes, the vulnerability scanner
// read-contract route, and the
// redacted hosted governance, semantic extraction, and answer narration status
// routes, bounded
// semantic evidence reads, documentation finding/fact list reads,
// documentation finding aggregate reads, documentation evidence packet and
// packet-freshness reads, service catalog correlation reads, package registry
// correlation reads, CI/CD run correlation list and aggregate reads, component
// extension inventory and diagnostics readbacks, collector status readbacks,
// ingester status readbacks, and hosted readiness readbacks are also available
// to scoped tokens because they read only
// deterministic in-process catalog or contract data, normalized runtime posture
// data, sanitized fact rows, sanitized local registry posture, or aggregate
// runtime counters. They leave live-data playbook step targets, scanner result
// routes, provider payloads,
// local filesystem paths, collector instance details, source-system detail,
// queue conflict keys, and tenant data routes behind their own scoped route
// gates.
// Story and investigation routes also attach additive answer_metadata
// companions for prompt-facing clients. The companion normalizes existing
// evidence handles, missing evidence, limitations, truncation, coverage,
// partial reasons, and recommended next calls without changing the canonical
// route fields or performing additional backend reads.
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
// identity by themselves. Scoped tokens authorize an incident-context read only
// when the incident correlates to a granted repository through the reducer-owned
// durable incident→repository edge (IncidentRepositoryAuthorizer over
// reducer_incident_repository_correlation, exact/derived outcomes only); an
// out-of-grant owner, a missing durable edge, or an empty grant all fail closed
// as not-found with no existence disclosure. Shared, admin, and local callers
// skip the authorizer.
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
// Semantic evidence reads are separate opt-in routes over durable semantic
// fact rows. They expose sanitized documentation observations and non-canonical
// code hints with truth basis, provider profile, prompt version, redaction
// version, policy state, freshness, and admission or corroboration state; they
// do not expose raw prompts, credentials, provider responses, or silently add
// semantic hints to deterministic query routes.
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
//
// Semantic extraction status is exposed as runtime capability metadata rather
// than a graph read: no-provider mode reports unavailable, disables code hints
// and documentation observations, configured provider profiles are redacted and
// source-policy gated, and deterministic query truth paths remain unaffected.
// Component extension inventory and diagnostics routes read the runtime
// component registry when ESHU_COMPONENT_HOME is configured, classify the truth
// basis as runtime_state, return unavailable when the registry is unset, and
// expose component ID, version, manifest digest, lifecycle state, activation
// config handles, and policy diagnostics without server-local manifest or
// activation config paths.
//
// QueryPlaybook is a deterministic, bounded, versioned data description of a
// common starter-prompt or cookbook workflow. A playbook names the ordered
// first-class tool calls (never raw Cypher), their bounded parameters with
// default limits, the expected AnswerTruthClass and evidence per step, optional
// drilldowns, and the declared failure modes with recommended fallbacks. It is
// data, not executable code: Resolve turns a playbook plus declared inputs into
// a ResolvedPlaybook of fully specified, bounded calls without reading any
// external or live-backend state, so equal inputs always yield an equal result.
// PlaybookCatalog is the versioned source of truth, PlaybookCatalogVersions
// pins catalog identity, and PlaybookToolNames lets the mcp package cross-check
// every referenced tool against the read-only registry without an import cycle.
// QueryPlaybookHandler exposes the same catalog and resolver through read-only
// API/MCP/CLI surfaces with workflow-plan truth; those surfaces do not execute
// calls, read graph or Postgres state, or expose raw Cypher.
// The contract and catalog are documented in
// docs/public/reference/query-playbooks.md.
// InvestigationWorkflow is the sibling guided-investigation catalog for
// missing-evidence-driven next calls. It declares the input shape, required and
// optional evidence families, expected output packet, grouped atomic tools,
// starter prompts, and routes from observed missing-evidence keys to bounded
// recommended next calls. InvestigationWorkflowHandler exposes the catalog and
// resolver at /api/v0/investigation-workflows; the resolver reads only caller
// inputs and reported missing-evidence state, never graph, Postgres, providers,
// collectors, or tenant data. The public contract is documented in
// docs/public/reference/investigation-workflows.md.
//
// VisualizationPacket is a sibling derived-view contract: a compact, bounded
// subgraph of an existing service-story, evidence-citation, or incident-context
// response. BuildServiceStoryVisualizationPacket,
// BuildEvidenceCitationVisualizationPacket, and
// BuildIncidentContextVisualizationPacket are pure transformations of data the
// caller already received; they perform no graph access and surface no field
// beyond the source response. Their FromMap adapters decode canonical
// HTTP/MCP/CLI JSON maps into those same builders without adding a new data
// source. Node and edge IDs are derived deterministically from the underlying
// entity/handle identity (never iteration order), the subgraph is sorted by
// stable ID and bounded by VisualizationMaxNodes and VisualizationMaxEdges with
// explicit truncation, the source TruthEnvelope is copied verbatim, and each
// node may reference the evidence_citation handle that hydrates it. Unsupported
// views return an explicit packet with recommended_next_calls rather than
// erroring, so a client can render an explainable subgraph without raw Cypher.
// VisualizationHandler exposes POST /api/v0/visualizations/derive, and the MCP
// derive_visualization_packet tool routes to the same handler. The contract is
// documented in docs/public/reference/visualization-packets.md.
//
// InvestigationEvidencePacket is the portable, source-backed v2 artifact
// (investigation_evidence_packet.v2). Unlike AnswerPacket, which is a view over
// one ResponseEnvelope, it is a self-contained artifact that separates raw
// source facts, reducer decisions, graph/query truth, missing-evidence reasons,
// freshness, and optional semantic observations into independent layers.
// NewInvestigationEvidencePacket composes and validates a packet from
// already-resolved evidence — it reads no store and calls no provider — deriving
// a deterministic packet_id from the identity plus a content digest so a
// no-provider build is reproducible byte-for-byte. Semantic observations are
// permitted only under an explicit AllowSemantic policy gate and the
// semantic_augmented basis; an unrecognized family or unanswerable scope yields a
// valid refusal packet rather than a fabricated answer. Layers are bounded with
// explicit truncation and the artifact declares the share_safe_v2 redaction
// profile. The contract is documented in
// docs/public/reference/investigation-evidence-packet.md; the supply-chain
// (#3141) and deployable-unit/drift (#3142) emitters wire real data into it.
//
// FreshnessHandler serves two bounded freshness drilldowns. The generation
// lifecycle drilldown at GET /api/v0/freshness/generations under the
// freshness.generation_lifecycle capability reads through the
// GenerationLifecycleReader port; it is bounded by limit, ordered
// deterministically, and reports truncated, with scope_not_found or not_found
// for a named selector that matches nothing and a building freshness state when
// a returned scope has a pending or in-flight generation. The changed-since
// summary at GET /api/v0/freshness/changed-since under the
// freshness.changed_since capability reads through the ChangedSinceReader port;
// it diffs a prior generation's fact set against the current active generation's
// fact set keyed by stable_fact_key into per-category (files, content entities,
// facts) added/updated/unchanged/retired/superseded counts plus bounded sample
// handles, returns scope_not_found/not_found for unresolved selectors, and maps
// a scope with no current active generation to an explicit unavailable diff
// rather than zero deltas. Both ports are implemented by the Postgres status
// store so the handler never depends on a concrete database driver.
//
// AdminHandler.listInputInvalidFacts serves the bounded durable per-fact
// quarantine read at POST /api/v0/admin/input-invalid-facts/query (issue
// #4630), mirroring listDeadLetters' required-limit-plus-timeout_ms,
// over-fetch-by-one truncation, and scope-access-filter shape. It requires
// scope_id and generation_id, reads reducer_input_invalid_facts rows the
// reducer's best-effort batch writer persisted at reduction time, and returns
// them deterministically ordered (decided_at DESC, fact_id ASC, missing_field
// ASC) with a schema_version envelope. AdminInputInvalidFactListHandler mounts
// only this read (no admin mutations) for cmd/mcp-server, mirroring
// AdminDeadLetterListHandler.
//
// CodeownersOwnershipHandler serves GET /api/v0/codeowners/ownership (issue
// #5419 Phase 4): a bounded, keyset-paginated read of one repository's Phase 3
// DECLARES_CODEOWNER graph edges, plus an effective_owner field resolved by
// resolveEffectiveRepositoryOwner's manifest-vs-codeowners precedence -- a
// service-catalog manifest declaration with an exact or derived reducer
// outcome wins, otherwise the repository's CODEOWNERS last-match-wins rule
// (the edge with the highest order_index) applies, otherwise effective_owner
// is a zero value rather than an error. A scoped caller not granted the
// requested repository_id gets the same bounded empty-ownership shape a
// genuinely CODEOWNERS-less repository would return, so an out-of-grant probe
// cannot be distinguished from a real empty answer. Initial pages execute one
// indexed graph query. Cursor pages execute three mutually exclusive bounded
// predicates and merge them into the same global keyset order, avoiding a
// pinned NornicDB mixed-OR predicate bug without changing the wire contract.
package query
