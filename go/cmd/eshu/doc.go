// Package main runs the eshu binary, the unified Cobra-based CLI and
// MCP/API launcher for Eshu.
//
// The binary registers root flags (`--database`, `--visual`, `--version`,
// `-v`) and a tree of subcommands covering local indexing (`index`, `list`, `watch`, `query`,
// `stats`), service launch (`mcp start`, `api start`, `serve`), authenticated
// local Eshu service commands (`graph` — `stop` handles both `local_lightweight`
// and `local_authoritative` profiles; lightweight stop verifies the owner
// socket before signaling; stale lightweight and authoritative stops use
// owner.lock before stopping recorded Postgres children and removing metadata;
// Bolt health requires a selected protocol version to avoid a
// TCP-accept/protocol-ready race), backend installation (`install`),
// admin/operator workflows (`admin ...`), configuration (`config`, `neo4j`),
// discovery (`find`, `analyze`, `ecosystem`), internal local-service
// orchestration, and the `doctor` diagnostic. Its local-authoritative graph
// path first acquires owner.lock, reclaims ownerless live Postgres only after
// PID, socket, and protocol probes agree, clears rebuildable local
// authoritative Postgres, graph, and filesystem-selector state, starts embedded
// NornicDB by default, allows external process mode only when
// ESHU_NORNICDB_RUNTIME=process is explicit, injects the workspace-scoped Bolt
// credentials plus
// CPU-count worker defaults from local_host_config.go into child services,
// captures embedded NornicDB startup output in the workspace graph log, keeps
// noisy child runtime logs in workspace log files by default while rendering a
// branded animated Bubble Tea known-work progress panel from the shared status
// store on terminals, includes explicit stage states so collector generations
// and projector/reducer work items are not conflated, pads styled progress
// columns by visible display width so counts stay aligned, keeps the panel
// verdict at `Indexing` while collector generations are pending, treats the
// active collector generation as the current snapshot rather than a running
// worker, and keeps embedded Bolt database access aligned with the HTTP
// server's RBAC callbacks. It hands off to the Go runtime binaries discovered
// through `PATH`. Exit codes reflect the underlying Cobra command result.
package main
