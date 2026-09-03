// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package mcp implements the Model Context Protocol tool surface for Eshu.
//
// ToolDefinition aliases the dependency-neutral toolcontract definition, and
// routecontract carries neutral argument and internal-request values. The
// ask, cloud, documentation, ecosystem, freshness, investigation, playbooks,
// relationships, semantic, service, and visualization child packages own
// their registration definitions without importing this parent package. The
// ask, relationships, and visualization children own pure dependency-neutral
// family route selectors alongside their definitions, and the
// admissiondecisions, cicd, codeflow, codeintel, codeowners, codequality,
// containerimage, content, deadcode, entityresolution, iacmanagement,
// impact, infrainventory, infrasearch, kubernetes, observabilitycoverage,
// packageregistry, replatforming, secretsiam, securityalert,
// servicecontext, and
// supplychainimpact children own such a selector without owning a
// registration.
// The nine impact-analysis selections (trace_deployment_chain through
// trace_exposure_path) live in the impact child and reach dispatch through
// the impactRoute adapter, consulted in resolveRoute's default case — the
// same point in the chain that the family's own switch occupied before the
// extraction; every POST /api/v0/impact/ path, body key, and dispatcher-side
// default is unchanged, and the advertised definitions stay with the
// ecosystem child and the root reachability registration.
// The four code-flow selections (dispatch_taint_path, dispatch_reaching_def,
// dispatch_cfg_summary, dispatch_pdg_summary) live in the codeflow child and
// reach dispatch through the codeFlowRoute adapter, consulted at the same
// delegation position in resolveRoute that the family's own selector occupied
// before the extraction; the four POST /api/v0/code/flow/ paths, the
// shared six-key body, and the limit 25 and line 0 defaults are unchanged,
// and the four advertised definitions stay at this root in
// tools_code_flow.go.
// The three dead-code selections (find_dead_code, investigate_dead_code,
// find_cross_repo_dead_code) live in the deadcode child and reach dispatch
// through the deadCodeRoute adapter defined in dispatch.go itself — the file
// whose switch held the three arms before the extraction; the delegation is
// consulted with the other route delegations ahead of the switch, which no
// caller can observe because every arm claims tool names exactly. The three
// POST /api/v0/code/dead-code paths, every body key, the limit 100 and
// offset 0 defaults, and the null-versus-[] absent shapes of
// exclude_decorated_with and consumer_repo_ids are unchanged, and the three
// advertised definitions stay at this root in tools_codebase.go,
// tools_dead_code.go, and tools_cross_repo_dead_code.go.
// The three complexity/quality selections (calculate_cyclomatic_complexity,
// find_most_complex_functions, inspect_code_quality) live in the codequality
// child and reach dispatch through the codeQualityRoute adapter defined in
// dispatch.go itself, consulted with the other route delegations ahead of
// the switch that held the three arms. The shared POST
// /api/v0/code/complexity path, the POST /api/v0/code/quality/inspect path,
// every body key, the limit 10 and offset 0 defaults, the conditional
// entity_id, and the absent limit on calculate_cyclomatic_complexity are
// unchanged, and the three advertised definitions stay at this root in
// tools_codebase.go and tools_code_quality.go.
// The three entity-resolution selections (resolve_entity,
// get_entity_context, get_entity_content) live in the entityresolution child
// and reach dispatch through the entityResolutionRoute adapter defined in
// dispatch.go itself, consulted with the other route delegations ahead of
// the switch that held the three arms. The POST /api/v0/entities/resolve
// body with its conditional name, type, and repo_id keys and limit 10
// default, the path-escaped GET /api/v0/entities/{entity_id}/context with
// its conditional environment query parameter, and the POST
// /api/v0/content/entities/read entity_id body are unchanged, and the three
// advertised definitions stay at this root in tools_context.go and
// tools_content.go. search_entity_content is not part of this family; see
// the content family below for where it lives.
// The five content selections (get_file_content, get_file_lines,
// build_evidence_citation_packet, search_file_content, and
// search_entity_content) live in the content child and reach dispatch
// through the contentRoute adapter defined in dispatch.go itself, consulted
// with the other route delegations ahead of the switch that held the five
// arms in its former "── Content ──" section. The POST
// /api/v0/content/files/read body, the verbatim-forwarded POST
// /api/v0/content/files/lines body, the POST /api/v0/evidence/citations
// body with its conditional subject and handles and limit 10 default, and
// the shared contentSearchBody selection behind POST
// /api/v0/content/files/search and POST /api/v0/content/entities/search are
// unchanged, and all five advertised definitions stay at this root in
// tools_content.go alongside get_entity_content, whose own routing stays in
// entityresolution rather than joining this family.
// ReadOnlyTools remains the sole ordered assembler; global route membership,
// family adapters, dispatch,
// transport, authorization, timeouts,
// response budgets, envelopes, and telemetry remain owned here. The query
// package retains Ask execution, visualization derivation, relationship
// validation, graph reads, and response shaping.
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
// labeled by mcp_method and a closed reason: unauthenticated,
// session_principal_mismatch, or route_policy. The last one is a credential
// that authenticated and was then refused by route admission -- an all-scope
// bearer reaching GET /sse or POST /mcp/message under hosted_multi_tenant or
// an unrecognized ESHU_GOVERNANCE_MODE, where its grant predicate would go
// inert and the read would cross tenants. It is kept out of the
// unauthenticated series on purpose: the deployment is behaving as
// configured, and the remedy is a narrower credential or hosted_single_tenant. This wrap does not by itself close the
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
// The one infrastructure-search selector, find_infra_resources, lives in the
// infrasearch child and reaches dispatch through the infraResourceSearchRoute
// adapter, consulted last in resolveRoute's delegation chain, directly ahead
// of the main switch that used to answer it; its POST path, eight-key body,
// and limit default of 50 are unchanged, and the ecosystem child still owns
// the advertised definition.
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
// Package-registry and supply-chain tools follow the same rule, so bounded
// package, version, dependency, correlation, source-only advisory evidence,
// vulnerability finding, explanation, SBOM, and attestation attachment
// requests stay thin. The package-registry selector lives in
// the packageregistry child and reaches dispatch through the
// packageRegistryRoute adapter, the four container-image identity, tag
// history, and aggregate selectors live in the containerimage child and reach
// dispatch through the containerImageRoute adapter, the four
// supply-chain-impact findings, count, inventory, and explanation selectors
// live in the supplychainimpact child and reach dispatch through the
// supplyChainImpactRoute adapter, and the three security-alert reconciliation
// listing, count, and inventory selectors live in the securityalert child and
// reach dispatch through the securityAlertRoute adapter. The one
// admission-decisions listing selector, which the same repository router
// answers, lives in the admissiondecisions child and reaches dispatch through
// the admissionDecisionsRoute adapter, and the one Kubernetes-correlation
// listing selector, answered by that router too, lives in the kubernetes
// child and reaches dispatch through the kubernetesCorrelationsRoute adapter.
// The remaining five supply-chain evidence selectors -- the
// vulnerability-scanner read contract, the advisory-evidence listing, and the
// SBOM/attestation attachment listing, count, and inventory -- live in the
// supplychainevidence child and reach dispatch through the
// supplyChainEvidenceRoute adapter, which reuses dispatch_supply_chain.go's
// filename rather than adding a new one; SBOM attachment tools forward
// repository_id to the query layer so repository scope returns reducer-owned
// image/SBOM missing evidence instead of becoming an unscoped aggregate.
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
// The investigation child package owns only the workflow and evidence-packet
// registration definitions. Workflow and packet routing, authorization, query
// execution, response envelopes, transport, and telemetry remain here.
// The service child package owns only the catalog, context, investigation, and
// intelligence-report registration definitions. Their three assembly
// positions, split routing, authorization, query execution, response envelopes,
// transport, and telemetry remain here.
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
// out-of-grant repository. That selection lives in the codeowners child and
// reaches dispatch through the codeownersRoute adapter; the child formats
// after_order_index only when the caller sent it, so an absent leg stays empty
// rather than defaulting to zero and half-supplying the cursor.
package mcp
