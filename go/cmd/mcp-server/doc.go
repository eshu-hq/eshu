// Package main runs the eshu-mcp-server binary, which serves the Eshu MCP
// tool transport over stdio or HTTP backed by the same query and content
// stores as the HTTP API.
//
// When invoked with --version or -v, it prints the embedded application
// version through the test-covered printMCPServerVersionFlag helper and exits
// before runtime setup. Otherwise the binary boots OTEL telemetry, wires the
// query mux with Postgres-backed package, CI/CD, and supply-chain read models
// plus the shared runtime admin mux, and dispatches MCP tool calls through
// mcp.Server. The
// transport is selected by ESHU_MCP_TRANSPORT (`http` by default, also
// `stdio`); HTTP mode listens on ESHU_MCP_ADDR (default :8080) and exposes
// `/sse`, `/mcp/message`, `/health`, the mounted `/api/*` routes, and the
// shared `/healthz`, `/readyz`, `/metrics`, `/admin/status` admin surface.
// SIGINT and SIGTERM trigger context cancellation and clean shutdown.
//
// When ESHU_PPROF_ADDR is set, the binary also exposes an opt-in
// net/http/pprof endpoint via runtime.NewPprofServer, bound to 127.0.0.1
// for port-only inputs so the default does not reach beyond the local host.
package main
