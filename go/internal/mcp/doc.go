// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
//
// In HTTP mode the transport is wrapped with the caller-supplied credential
// middleware when the server is constructed with WithTransportAuth (issue
// #5168): the GET /sse and POST /mcp/message endpoints run every JSON-RPC
// method (initialize, tools/list, tools/call, ping) through the same middleware
// that guards /api/v0/*, instead of being mounted with none. When that
// middleware fails a request closed -- for example a shared-token (ESHU_API_KEY)
// deployment receiving a request with no or a wrong credential -- the caller
// gets a bare 401 that discloses nothing about the tool catalog or server
// metadata. Each SSE session is also bound to the credential that opened it; a
// POST to that session whose credential resolves to a different principal is
// rejected with 403. Denials increment eshu_dp_mcp_transport_auth_denied_total,
// labeled by mcp_method and reason. This wrap does not by itself close the
// headerless bypass for a scoped-token-only or OIDC-only deployment (shared
// ESHU_API_KEY unset): the shared credential middleware still passes a
// headerless request through on an empty shared token, and that per-request
// enforcement is finished by the companion auth-headerless-bypass hardening
// (under #5161). The stdio transport is never wrapped -- it keeps its process
// and filesystem trust boundary.
// Helpers in this package normalize tool arguments, including shared
// slice and identifier helpers in
// dispatch_args.go, build request bodies for the underlying handler, and parse
// canonical response envelopes. Citation tools stay in this transport layer and
// delegate source hydration to the query package rather than reading storage
// directly; the advertised citation schema caps input at 500 handles.
// The find_code adapter forwards the caller's public page limit unchanged and
// preserves count, limit, and truncated from /api/v0/code/search inside the
// canonical data envelope; only the query layer performs the limit-plus-one
// probe used to determine truncation.
// Structural inventory and security investigation tools also stay
// transport-only and delegate inventory filtering, import dependency
// investigation, call graph metrics, or redacted finding generation to the
// query package; their tool definitions live in
// tools_structural_inventory.go, tools_import_dependencies.go,
// tools_call_graph_metrics.go, and tools_security.go.
// Import-dependency dispatch forwards scope and paging unchanged; the query
// handler owns the 25,000-row candidate ceiling and scope-narrowing response.
// The get_capability_catalog tool forwards to /api/v0/capabilities and
// preserves the embedded role, grant, data-class, permission-family, and
// per-capability authorization metadata from the query response.
// The get_surface_inventory tool forwards to /api/v0/surface-inventory and
// preserves collector source-to-read-surface contracts so MCP callers see the
// same fact-kind provenance, proof gates, fixture refs, and truth profiles as
// HTTP and console callers.
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
// list_reducer_input_invalid_facts (issue #4630) resolves 1:1 to
// POST /api/v0/admin/input-invalid-facts/query, mirroring
// list_dead_letter_work_items: scope_id, generation_id, limit, and
// timeout_ms are required, optional domain/fact_kind filters pass through
// unchanged, and the response is the durable reducer_input_invalid_facts
// read model rather than a raw fact payload.
// list_codeowners_ownership (issue #5419 Phase 4) resolves 1:1 to
// GET /api/v0/codeowners/ownership: it forwards repository_id, limit, and the
// three-part after_order_index/after_pattern/after_ref keyset cursor
// unchanged and preserves the effective_owner field the HTTP handler resolves
// from manifest-vs-codeowners precedence, so a scoped caller sees the same
// bounded empty-ownership shape over MCP that the HTTP route returns for an
// out-of-grant repository.
package mcp
