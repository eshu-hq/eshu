// Package main runs the eshu binary, the unified Cobra-based CLI and
// MCP/API launcher for Eshu.
//
// The binary registers root flags (`--database`, `--visual`, `--version`,
// `-v`) and a tree of subcommands covering local indexing (`index`, `list`, `watch`, `query`,
// `stats`), service launch (`mcp start`, `api start`, `serve`), authenticated
// local Eshu service commands (`graph` — `stop` handles both `local_lightweight`
// and `local_authoritative` profiles; lightweight stop verifies the owner
// socket before signaling and uses owner.lock before stale metadata cleanup;
// Bolt health requires a selected protocol version to avoid a
// TCP-accept/protocol-ready race), backend installation (`install`),
// admin/operator workflows (`admin ...`), configuration (`config`, `neo4j`),
// discovery (`find`, `analyze`, `ecosystem`), internal local-service
// orchestration, and the `doctor` diagnostic. Its local-authoritative graph
// path starts embedded or process-mode NornicDB, injects the workspace-scoped
// Bolt credentials plus CPU-count worker defaults from local_host_config.go
// into child services, and keeps embedded Bolt database access aligned with
// the HTTP server's RBAC callbacks. It hands off to the Go runtime binaries
// discovered through `PATH`. Exit codes reflect the underlying Cobra command
// result.
package main
