---
name: eshu-mcp-call-rigor
description: Use when calling Eshu MCP/API tools, debugging local Eshu MCP connectivity, designing or reviewing MCP tool schemas, or running graph-backed query tools where scope, pagination, timeout, stale local owner ports, truth labels, or payload size affect correctness and performance.
---

# Eshu MCP Call Rigor

Use this skill before any Eshu MCP/API call that might query the graph, read the
content store, or return a list of results. The goal is a correct answer through
a call shape that is bounded, scoped, and diagnosable.

## Call Contract

Every list or search tool should have:

- canonical scope such as `repo_id`, workload id, service id, environment, or an
  explicit `scope`
- required or defaulted `limit`
- deterministic ordering
- server-side timeout or cancellation path
- `truncated` or continuation metadata when more results exist
- structured envelope metadata: `truth.level`, `truth.profile`,
  `truth.freshness.state`, and `error`

Prefer summary/count/handle calls before payload-heavy drilldowns. Do not fetch
large source bodies, relationship expansions, or whole-graph result sets until a
cheap first call proves they are needed.

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
