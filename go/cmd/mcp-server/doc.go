// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package main runs the eshu-mcp-server binary, which serves the Eshu MCP
// tool transport over stdio or HTTP backed by the same query and content
// stores as the HTTP API. `ESHU_SEMANTIC_SEARCH_LOCAL_EMBEDDER=hash` or
// `local_hash` explicitly forces deterministic no-network local
// semantic/hybrid retrieval from ready persisted vector rows over active
// curated search documents. `auto_hash` selects one governed search_documents
// provider profile when configured and otherwise falls back to local hash query
// embeddings.
//
// When invoked with --version or -v, it prints the embedded application
// version through the test-covered printMCPServerVersionFlag helper and exits
// before runtime setup. Otherwise the binary boots OTEL telemetry, wires the
// query mux with Postgres-backed package, CI/CD, supply-chain attachment,
// advisory evidence, work-item evidence, impact finding, impact explanation
// read models, admission decision readback, cloud runtime drift readback,
// generation freshness drilldowns, optional redacted semantic provider profile
// status, optional semantic extraction source policy, optional hosted governance
// status readback from safe ESHU_GOVERNANCE_* metadata, optional
// component-extension registry diagnostics when ESHU_COMPONENT_HOME is set, and
// the shared runtime admin mux, and dispatches MCP tool calls through
// mcp.Server. The transport is selected by ESHU_MCP_TRANSPORT (`http` by
// default, also `stdio`); HTTP mode listens on ESHU_MCP_ADDR (default :8080)
// and exposes `/sse`, `/mcp/message`, `/health`, the mounted `/api/*` routes,
// and the shared `/healthz`, `/readyz`, `/metrics`, `/admin/status` admin
// surface.
// SIGINT and SIGTERM trigger context cancellation and clean shutdown.
//
// When ESHU_PPROF_ADDR is set, the binary also exposes an opt-in
// net/http/pprof endpoint via runtime.NewPprofServer, bound to 127.0.0.1
// for port-only inputs so the default does not reach beyond the local host.
package main
