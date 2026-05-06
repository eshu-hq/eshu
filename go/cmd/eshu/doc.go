// Package main runs the eshu binary, the unified Cobra-based CLI and
// MCP/API launcher for Eshu.
//
// The binary registers root flags (`--database`, `--visual`, `--version`,
// `-v`) and a tree of subcommands covering local indexing (`index`, `list`, `watch`, `query`,
// `stats`), service launch (`mcp start`, `api start`, `serve`), authenticated
// local Eshu service commands (`graph`), backend installation (`install`),
// admin/operator workflows (`admin ...`), configuration (`config`, `neo4j`),
// discovery (`find`, `analyze`, `ecosystem`), internal local-service
// orchestration, and the `doctor` diagnostic. Its local-authoritative graph
// path starts embedded or process-mode NornicDB, injects the workspace-scoped
// Bolt credentials into child services, and keeps embedded Bolt database access
// aligned with the HTTP server's RBAC callbacks. It hands off to the Go runtime
// binaries discovered through `PATH`. Exit codes reflect the underlying Cobra
// command result.
package main
