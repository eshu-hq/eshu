---
name: eshu-mcp-call-rigor
description: Use when calling Eshu MCP/API tools, debugging local Eshu MCP connectivity, designing or reviewing MCP tool schemas, or running graph-backed query tools where scope, pagination, timeout, stale local owner ports, truth labels, or payload size affect correctness and performance.
---

# eshu-mcp-call-rigor

Use this skill before any Eshu MCP/API call that might query the graph, read the
content store, or return a list of results. The goal is a correct answer through
a call shape that is bounded, scoped, and diagnosable.

## Call Contract

Every list or search tool MUST have:

- canonical scope such as `repo_id`, `workload_id`, `service_id`,
  `environment`, or an explicit `scope`
- required or defaulted `limit`
- deterministic ordering
- server-side timeout or cancellation path
- `truncated` or continuation metadata when more results exist
- structured envelope metadata: `truth.level`, `truth.profile`,
  `truth.freshness.state`, and `error`

Prefer summary/count/handle calls before payload-heavy drilldowns. MUST NOT fetch
large source bodies, relationship expansions, or whole-graph result sets until a
cheap first call proves they are needed.

Add `eshu-performance-rigor` when the call is part of a latency benchmark,
scaled read proof, or before/after performance claim.

## Response Shape (truth envelope)

Eshu MCP tool results wrap the payload in a truth envelope: the `tools/call`
result's `structuredContent` (and the JSON in the first text content entry) is
`{data, truth, error}`. The canonical query payload lives under **`data`**
(e.g. `data.repositories`, `data.correlations`, `data.resources`), NOT at the top
level — the `truth`/`error` siblings carry the envelope metadata from the Call
Contract. When parsing or asserting a tool response, read `data`; treating the
envelope as the payload (looking for `repositories` at the top level) is a common
mistake.

The http transport serves JSON-RPC at `POST /mcp/message` **standalone** — no SSE
session is required; `handleHTTPMessage` returns the response synchronously, so a
plain HTTP client can `tools/call` directly. A tool registered in `tools/list`
can still fail with an `isError` result wrapping `HTTP 404` when its route is not
mounted on the MCP server's router — advertised is not the same as servable. Fix
the route (mirror `cmd/api/wiring.go` in `cmd/mcp-server/wiring.go`); do not
assume a registered tool works. Tools that need a selector take it in
`arguments` (`get_repo_summary` → `repo_name`/`repo_id`;
`list_kubernetes_correlations` → `cluster_id`/`scope_id`/...).

## Local MCP Preflight

Before blaming Eshu query truth, prove the local transport is reading the
current owner state:

1. Confirm the MCP server is visible to the current client session.
2. Check the local owner record for the active workspace.
3. Confirm the current Postgres and graph/Bolt ports are listening.
4. Confirm the MCP child connected to those current ports, not stale ports from
   a previous owner.
5. Confirm the repo is indexed and the response freshness is not `building` or
   `stale`.

For Codex CLI, repo-local `.mcp.json` is not enough by itself. Use the active
Codex MCP configuration or per-launch `-c mcp_servers.<name>...` override and
verify visibility in the session.

## Failure Classification

When a call is slow, hanging, or unexpectedly broad, stop and classify it before
retrying:

- transport mismatch or closed MCP session
- stale local owner ports
- backend health or connection refusal
- missing or broad scope
- query shape or missing index
- payload size or missing pagination
- runtime mode switched onto a slower path

Do not retry the same unbounded call and hope for a better result.

## Design Rules

- Whole-graph defaults are not acceptable when a repo/workload scope is known.
- Expensive tools should expose cheap analysis metadata even when results are
  empty.
- High-volume per-node metadata belongs in content/query paths unless a measured
  indexed graph query needs it.
- Runtime modes with different performance profiles must be explicit opt-in.
- Results should explain truth level without sounding less confident than the
  actual envelope supports.

## Evidence Gate For New Tools

When adding or changing an MCP/API tool that introduces graph Cypher, a
graph-backed query handler, broad traversal, queue-backed materialization, or a
new runtime stage, run:

```bash
scripts/test-verify-performance-evidence.sh
scripts/verify-performance-evidence.sh
```

Add tracked `Performance Evidence:`, `Benchmark Evidence:`, or
`No-Regression Evidence:` plus `Observability Evidence:` or
`No-Observability-Change:` to a docs/ADR/package note. Name the scope, limit,
timeout, result cardinality, backend/query shape, and the metric/span/log/status
signal that proves the tool is bounded and diagnosable.
