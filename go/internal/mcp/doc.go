// Package mcp implements the Model Context Protocol tool surface for Eshu.
//
// MCP tools dispatch into the same HTTP query handlers that power the public
// HTTP API, so a tool response and the corresponding HTTP query response
// share truth. Helpers in this package normalize tool arguments, including
// shared slice and identifier helpers in dispatch_args.go, build request bodies
// for the underlying handler, and parse canonical response envelopes. Citation
// tools stay in this transport layer and delegate source hydration to the query
// package rather than reading storage directly; the advertised citation schema
// caps input at 500 handles. Structural inventory and security investigation
// tools also stay transport-only and delegate inventory filtering, import
// dependency investigation, call graph metrics, or redacted finding generation
// to the query package; their tool definitions live in
// tools_structural_inventory.go, tools_import_dependencies.go,
// tools_call_graph_metrics.go, and tools_security.go.
// Package-registry and supply-chain tools follow the same rule and keep their
// route builders in dedicated dispatch files, so bounded package, version,
// dependency, correlation, source-only advisory evidence, vulnerability
// finding, explanation, SBOM, and attestation attachment requests stay thin;
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
// change that alters request or response shape must update the MCP guide, the
// HTTP API reference where the route is shared, and the handler tests in the
// same change.
package mcp
