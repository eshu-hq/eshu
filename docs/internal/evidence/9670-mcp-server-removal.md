# #9670 in-tree eshu-mcp-server removal

Base: `origin/main` at `0b1bb14203`. This record is the tracked
performance/no-regression/observability note for the mechanical removal
of the superseded in-tree MCP server after the satellite takeover.
Satellite `eshu-hq/eshu-mcp` v0.2.0 serves the surface (QA rev 111,
dispatch parity proven); claim and rollback disposition are on #9670
(live rollback proven twice). Refs epic #8873, part of #6707.

## Scope

Deletes the in-tree binary dir (`go/cmd/mcp-server`, 51 files:
entrypoint, transports, auth guards, wiring including
`wiring_router.go`, `reader_surface.go`, `oauth_discovery`,
`ssm_host_bounds`, `status_store`) and repoints every in-tree consumer.
Core-owned kernels stay: `go/internal/mcp` registry/router/dispatch
(771 files), `internal/query` read models, and the api-side wiring
tests. The image no longer builds `/go-bin/eshu-mcp-server`.

## Census honesty note

In-tree registry census is **171 tools** (`censusToolCount` in
`go/internal/mcp/tool_registry_census_test.go`), pinned with order,
schemas, dispatch bounds, and router surface by the census suite, all
green at this head. History: 168 → 169 → 170 → 171 as tools landed.
The deleted `CUTOVER_OWNERSHIP.md` said 169 (stale).
No contract assertion migrates out: the registry census and the
router surface (`go/internal/mcp/router`) stay in-tree as the
reproduction contract the satellite already satisfies. Deleted with the
dir: `wiring_router_completeness_test`,
`tool_surface_wiring_test`, `runtime_surface_test`, and the
`assertRouterFieldsWired` cross-entrypoint sync test (its twin is
gone; the api-side sweep keeps guarding the api entrypoint).

## No-Regression Evidence:

Baseline: `origin/main 0b1bb14203` (go 1.26.10, credential-free).
After: this head. `go build ./...` exit 0. Full affected packages
green: `go/internal/mcp/...`, `go/internal/query`,
`go/internal/query/ask`, `go/internal/askwiring`,
`go/internal/reducer/code/call`, `go/cmd/api`, `go/cmd/eshu`,
`go/internal/cigates`, `go/internal/capabilitycatalog`,
`go/internal/evidencecontinuity`, `go/internal/envregistry`,
`go/internal/telemetry/...`, `go/cmd/capability-inventory` (57 ok
packages, 0 FAIL). `tag_history.go` changed comments only (indented
struct-field comments); `instruments.go` drops only the three
registrations nothing references (verified by
`verify-moved-file-refs` and the telemetry gate). No new Cypher,
queue, lease, batching, or concurrency knobs. No live backend
involved; Docker/credentialed gates stay CI-owned.

## Observability Evidence:

Retired `eshu_dp_governance_audit_allowed_emitted_total`,
`_dropped_total`, and `_persist_failures_total` from the in-tree
registry: their only references were the deleted wiring, confirmed by
`verify-telemetry-coverage` (green after removal) and by zero hits in
dashboards, docs rows, and Go callers. `verify-moved-file-refs`
passes (48 vacated paths, no dangling references). The satellite
owns its own registry for the MCP transport it now serves.

## Left for the api/cli lanes and the orchestrator

`Dockerfile` api/cli lines untouched (api #9669 and cli #9671 own them; no
Go imports cross cmd trees, so this removal compiles cleanly). The satellite
rewiring once sequenced here has landed in this head: service launchers
session-delegate plus satellite Exec (`d65b13c`, `2155a64`), the digest-pinned
Dockerfile satellite stage (`b769fae`), gate serving legs build and start the
pinned satellite, and the latency profile mapping is pinned.
