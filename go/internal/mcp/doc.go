// Package mcp implements the Model Context Protocol tool surface for Eshu.
//
// MCP tools dispatch into the same HTTP query handlers that power the public
// HTTP API, so a tool response and the corresponding HTTP query response
// share truth. Dispatch gives each handler request a bounded child context with
// a deterministic 30s default and propagates parent cancellation, so handlers
// must honor r.Context rather than starting unbounded work; dispatch timeout
// and cancellation failures are returned as MCP error results with structured
// content. Dispatch also enforces a response-size budget
// (defaultToolResponseByteBudget) as a tool-agnostic hub throttle: a response
// whose serialized size exceeds the budget is replaced with a small bounded
// envelope carrying error code mcp_response_over_budget plus budget accounting
// and narrowing guidance, so a single heavy graph-returning tool cannot blow the
// model context budget. Per-route token budgets still apply first.
// Helpers in this package normalize tool arguments, including shared
// slice and identifier helpers in
// dispatch_args.go, build request bodies for the underlying handler, and parse
// canonical response envelopes. Citation tools stay in this transport layer and
// delegate source hydration to the query package rather than reading storage
// directly; the advertised citation schema caps input at 500 handles.
// Structural inventory and security investigation tools also stay
// transport-only and delegate inventory filtering, import dependency
// investigation, call graph metrics, or redacted finding generation to the
// query package; their tool definitions live in
// tools_structural_inventory.go, tools_import_dependencies.go,
// tools_call_graph_metrics.go, and tools_security.go.
// The get_capability_catalog tool forwards to /api/v0/capabilities and
// preserves the embedded role, grant, data-class, permission-family, and
// per-capability authorization metadata from the query response.
// Package-registry and supply-chain tools follow the same rule and keep their
// route builders in dedicated dispatch files, so bounded package, version,
// dependency, correlation, source-only advisory evidence, vulnerability
// finding, explanation, SBOM, and attestation attachment requests stay thin;
// SBOM attachment tools forward repository_id to the query layer so repository
// scope returns reducer-owned image/SBOM missing evidence instead of becoming
// an unscoped aggregate.
// repository, service, and workload advisory scopes are forwarded to HTTP so
// the query layer can derive advisory anchors from reducer-owned impact
// findings without promoting provider-alert-only evidence;
// SBOM attachment tools forward repository_id to the query layer so unsupported
// repository scope is rejected there instead of becoming an unscoped aggregate.
// Supply-chain schemas preserve ambiguous-subject outcomes instead of hiding
// non-canonical evidence. The supply-chain impact tool exposes
// include_suppressed and suppression_state inputs so callers can opt in to
// VEX/operator-suppressed findings and audit suppression reason, source,
// justification, expiration, and evidence reference per row. Work-item tools
// expose bounded Jira source facts as ticket-first evidence while preserving the
// source-only boundary around PR, commit, deploy, runtime, image, and service
// truth. Documentation tools forward repo, target_kind, target_id, and
// service_id filters to the HTTP read models and preserve coverage,
// related_facts, and missing_evidence fields so raw documentation target facts
// are not collapsed into admissible findings. Documentation fact lists also
// preserve bounded page metadata (`count`, `limit`, `truncated`,
// `missing_evidence`, `states`, and `next_cursor` on truncated pages). Any
// hosted governance or semantic capability status tool stays transport-only
// too: governance forwards to the redacted policy-mode and aggregate readback
// route, while semantic status reports no-provider mode as unavailable and
// configured provider profiles as redacted, source-policy-gated metadata.
// Deterministic indexing, reducer, API, MCP, and documentation fact paths remain
// unaffected by either optional status readback.
// Semantic evidence tools follow the same transport-only boundary: they forward
// to HTTP routes that list durable documentation observations or non-canonical
// code hints with truth basis, provider profile, prompt version, redaction
// version, policy state, freshness, and admission or corroboration state. They
// do not expose raw prompts, credentials, provider responses, or inject code
// hints into deterministic graph-truth tools.
// Component extension tools follow the same transport-only boundary: they
// forward inventory and diagnostics requests to HTTP registry readback routes,
// preserve the canonical envelope, and do not expose server-local manifest
// paths or activation config paths.
// Query playbook and investigation workflow tools are catalog-only dispatch
// surfaces: they forward to HTTP static resolvers that describe bounded call
// plans and missing-evidence-driven next calls without executing those calls or
// reading tenant data.
// Relationship-story tools forward min_confidence unchanged to the query layer
// so the HTTP handler owns confidence-floor validation and filtering.
// Relationship-story responses preserve the HTTP per-row provenance block in
// structuredContent; MCP does not reinterpret confidence, truth, freshness, or
// bounded-result metadata.
// Any change that alters request or response shape must update the MCP guide,
// the HTTP API reference where the route is shared, and the handler tests in
// the same change.
package mcp
