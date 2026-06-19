# Launch-Readiness Verification Log

Internal record of launch-readiness decisions that have been verified against the
codebase. Each entry names the decision, what was checked, the evidence (file,
commit, or test), and whether the decision landed consistently across code,
runtime surfaces, and docs.

This file lives under `docs/internal/` (outside the published `docs/public` mkdocs
tree) and is agent/maintainer-facing only.

## 2026-06-19 — Default `eshu mcp start` owner profile is `local_authoritative` (#3026 / PR #3048)

**Decision.** When `eshu mcp start` boots its own local owner over stdio (no owner
already running), it defaults to the `local_authoritative` profile — embedded
Postgres + NornicDB + reducer + ingester in one binary — so graph-backed MCP
questions (transitive callers, import dependencies, read-only Cypher) work on a
fresh install. This is one step up from `local_lightweight` (Postgres-only) and
deliberately not `local_full_stack`. Tracked by #3026, landed via PR #3048,
commit `71fad840a` ("Default eshu mcp start to local_authoritative (#3026)").

**Status: VERIFIED CONSISTENT.** No stale or contradictory state found.

### Code

- Default selection: [`go/cmd/eshu/local_host.go:147`](../../go/cmd/eshu/local_host.go)
  — `defaultProfileForMode` returns `query.ProfileLocalAuthoritative` for
  `localHostModeMCPStdio` and `query.ProfileLocalLightweight` for watch-mode
  owners (the lightweight indexer stays Postgres-only).
- Explicit opt-out preserved: `resolveLocalHostRuntimeConfigWithDefault` still
  honors an explicit `ESHU_QUERY_PROFILE`, and `eshu mcp start --profile` accepts
  `local_authoritative` or `local_lightweight`
  ([`go/cmd/eshu/service.go`](../../go/cmd/eshu/service.go), `mcpStartProfileOverrides`;
  the flag applies to stdio transport only).
- Profile enum unchanged — `local_lightweight` is still a valid, selectable
  profile (no regression):
  [`go/internal/query/contract.go:21`](../../go/internal/query/contract.go)
  (enum) and `ParseQueryProfile` (`:160`) which still accepts it.

### Runtime surfaces

- `eshu graph start` explicitly sets
  `ESHU_QUERY_PROFILE=local_authoritative` + NornicDB
  ([`go/cmd/eshu/graph.go:226`](../../go/cmd/eshu/graph.go)) — consistent with the
  MCP stdio default.
- Docker Compose provides NornicDB to the graph-using services
  (`ESHU_GRAPH_BACKEND: nornicdb` across api / mcp-server / ingester /
  resolution-engine in `docker-compose.yaml`), so the default Compose stack has
  the infrastructure `local_authoritative` expects.

### Docs (all already correct — no edits required)

- `docs/public/reference/local-host-lifecycle.md` — owner defaults to
  `local_authoritative`; documents the `local_lightweight` opt-out.
- `docs/public/reference/local-performance-envelope.md` — dedicated
  "`eshu mcp start` default owner profile (#3026)" section.
- `docs/public/run-locally/mcp-local.md` — boots `local_authoritative` by default.
- `docs/public/getting-started/first-successful-run.md` — same, with the
  `--profile local_lightweight` note for the faster Postgres-only owner.
- `docs/public/reference/environment-runtime-storage.md` — `ESHU_QUERY_PROFILE`
  row states the per-command defaults (`eshu mcp start` → `local_authoritative`,
  `eshu index` → `local_lightweight`).
- `docs/public/reference/local-lightweight-capability-audit.md` — cross-references
  the default change.

No doc was found claiming the default is `local_lightweight` or
`local_full_stack`. Public positioning copy (`docs/public/index.md`,
`why-eshu.md`, `use-cases.md`, and the site `src/siteContent.ts`) makes no
default-profile claim, so there is nothing to contradict.

**README.** `README.md` "Pick Your First Path" makes no explicit default-profile
claim; it routes first-time users to `getting-started/first-successful-run.md`,
which states the `local_authoritative` default correctly. No README edit needed.

### Tests

`go/cmd/eshu/local_host_profile_test.go` covers the contract:
`TestDefaultProfileForMode`, `TestResolveLocalHostRuntimeConfigWithDefault`,
`TestRunMCPStartStdioProfileFlagInjectsLightweight`,
`TestRunMCPStartStdioProfileFlagInjectsAuthoritative`, and
`TestRunMCPStartStdioRejectsUnknownProfile`.

### Not verified here

- Live boot timing / performance envelope of the `local_authoritative` default on
  a real fresh install (covered separately by the performance-envelope work).
