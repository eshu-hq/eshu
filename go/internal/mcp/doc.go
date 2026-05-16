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
// dependency, correlation, SBOM, and attestation attachment requests stay thin.
// Any change that alters request or response shape must update
// the MCP guide, the HTTP API reference where the route is shared, and the
// handler tests in the same change.
package mcp
