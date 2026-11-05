<!-- SPDX-License-Identifier: MIT -->
<!-- Copyright (c) 2025-2026 eshu-hq -->

# Joint API+MCP flip mechanics draft (#9669, arbiter §5 prep lane)

Refs #9669, part of #6707. Status: preparation only — this is a plan, not
an execution. No IaC values are changed by this document; every file below
is named so the flip executor can open it, not edited here. A fresh arbiter
review is required before any real flip.

## 1. Single-IaC-pin-both-edges plan

Deployment IaC lives in `eshu-hq/eshu-deploy` (moved there under #8871; see
`scripts/README.md:27` and the `eshu-deploy` row in
`docs/internal/design/eshu-ecosystem-repository-migration.md:68`). The flip
moves exactly one pin, and both edges (API + MCP) follow it. No file in this
repo is the flip switch.

| Edge | IaC file (in `eshu-deploy`) | Pinned value |
| --- | --- | --- |
| API | Helm values for the API deployment (`image.repository`, `image.tag`) and the ArgoCD Application base that syncs the Helm path | `image.repository` / `image.tag` → last-good core `ghcr.io/eshu-hq/eshu:<tag>` pre-flip; dest `eshu-api` artifact post-flip |
| MCP | `deploy/helm/eshu/templates/deployment-mcp-server.yaml` (renders `{{ .Values.image.repository }}:{{ .Values.image.tag }}`) plus any `mcpServer` value overrides; same ArgoCD Application | Same single pin: pre-flip it resolves to the core image (as observed live at `sha-e4c385f493`, per the #8873 rollback comment); post-flip to the dest `ghcr.io/eshu-hq/eshu-mcp:<tag>@<digest>` artifact |
| E2E shadow | `compose/docker-compose.e2e.yaml` (under `$ESHU_DEPLOY_CHECKOUT`) | In-tree `Dockerfile:71-81` `mcp-satellite` stage shows the pattern: digest-pinned `ghcr.io/eshu-hq/eshu-mcp:0.2.8@sha256:fb37…` copied under the old binary path with satellite flags (`-addr`/`-core-base-url`) |

Flip sequence (executor runs, arbiter reviews first):

1. Land the dest `eshu-api` release (tag + image + cosign + attestations,
   same chain as the #8873 dest `v0.2.9` flow).
2. Land the dest `eshu-mcp` release at a tag whose `tools.json` order is
   byte-identical to the in-tree census at the pinned core commit.
3. Open the one IaC diff in `eshu-deploy`: re-pin the shared image values
   (or the per-service overrides, whichever the chart uses at flip time —
   verify by rendering, do not assume) to the two dest artifacts.
4. ArgoCD self-heals both Deployments from git; verify both roll out
   (`kubectl rollout status` on the API and MCP Deployments).
5. Run the paired parity re-proof (section 2) inside the same window before
   declaring the flip good.

## 2. Paired parity re-proof plan (HTTP + MCP tool census, same window)

Both halves run against the flipped deployment before acceptance, so a skew
between edges cannot hide.

HTTP half (API edge):

- Route census: re-run the `CUTOVER_OWNERSHIP.md` method
  (`rg -c '^\| \`(GET|POST|PATCH|PUT|DELETE)' go/cmd/api/CUTOVER_OWNERSHIP.md`
  reads 278 at this HEAD) and the `cutover_ownership_test.go` drift gate
  against the live spec (`GET /api/v0/openapi.json`).
- Envelope parity: negotiation (`Accept: application.eshu.envelope+json`
  via `WriteSuccess`/`WriteErrorEnvelope`), pagination (bounded `limit`,
  `page_position`, resume cursor, `truncated`, `TruthLevelPartial`),
  capability/profile gating, authz (`auth_scoped_routes_completeness`,
  fail-closed `BrowserSessionRoutePolicy`), per the Leaf B harness scope in
  `../eshu-9669-census/go/cmd/api/READ_TRANSPORT_CENSUS.md:166-184`.

MCP half (MCP edge):

- Served surface: `tools/list` returns exactly the 2 router tools
  (`find_tool`, `call_tool`); registry census
  (`TestToolRegistryCensusCount`, currently 171) matches the dest
  `tools.json` `tool_count` byte-for-byte in registration order.
- Live sweep: every registry tool answers transport HTTP 200 via
  `call_tool` with the canonical `{data, truth, error}` envelope or a
  documented honest error (prior full-sweep classes: 77 success / 30 honest
  error / 7 bare-payload status family / 54 fail-closed text — re-baseline
  at flip time, do not copy these numbers as targets).
- Hostile inputs: unknown tool → `mcp_router_unknown_tool` + suggestions;
  bad args → `mcp_router_invalid_arguments`; unauthenticated → identical
  401 `unauthenticated` on `/sse`, `/mcp/message`, and `/api/v0/status`.

Agreement bar: reducer, canonical-graph, HTTP-API, and MCP truth agree per
capability, plus the explicit unaffected-surfaces list (per the #8873
acceptance item 6). A 30 s dispatch-timeout probe (`defaultToolDispatchTimeout`,
`go/internal/mcp/dispatch_timeout.go:17`) is NOT_CHECKED until a query runs
long enough to fire it live — record that instead of manufacturing load on
shared QA.

## 3. In-flight drain rule per route family

Shutdown bounds (code pins, not tunable at flip time):

- Graceful shutdown: `go/cmd/api/main.go:102-110` reads
  `ESHU_API_SHUTDOWN_TIMEOUT` (default 30 s); requests not finished in the
  window are interrupted (`go/cmd/api/AGENTS.md:46-50`).
- Write timeout floor: `go/cmd/api/main.go:159` derives the HTTP write
  timeout from `reset.DefaultRefinalizeDrainTimeout` plus one minute — a
  hard-killed reducer can retain its lease for the drain window, so the
  server timeout must not be shortened below that bound.

Drain order at flip:

1. **Mutations first (`Never satellite` + `Dispatch` families)**: quiesce
   Admin recovery/reindex/replay writes, admission-contract mutations
   (identity, provider-config, sign-in-policy, setup, OIDC/SAML/GitHub
   login, browser-session, local-identity issuance), and the single
   SupplyChain suppressions write. No new mutations once the pin PR merges;
   in-flight mutations drain inside the shutdown window, then the edge
   flips.
2. **Query-shaped POST reads second**: code, impact, IaC, evidence, content,
   and investigation POSTs are reads (`channel=read` in
   `CUTOVER_OWNERSHIP.md`) but carry bodies, so load balancers must drain
   them as requests, not cache entries. They are idempotent and safe to
   retry against the new edge on shutdown interrupt.
3. **GET reads last**: pure GET families drain naturally; clients retry on
   the new edge with no state risk.
4. **MCP tools throughout**: each `call_tool` runs under the 30 s dispatch
   bound; in-flight calls at flip either complete (HTTP 200 + envelope) or
   surface the bounded timeout envelope — never a half-write, because tools
   are reads (the suppressions mutation is HTTP-only, not a tool).

## Open questions

1. Confirm the exact IaC filenames/values in `eshu-deploy` at flip time by
   rendering the chart — names above come from the #8873 rollback comment
   and may have drifted.
2. Owner confirms the “one pin moves both edges” claim against the live
   chart (shared vs per-service image values).
3. Reconfirm the 278-route / 171+2-tool counts at flip time; both drift.
