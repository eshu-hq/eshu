<!-- SPDX-License-Identifier: MIT -->
<!-- Copyright (c) 2025-2026 eshu-hq -->

# Joint API+MCP flip mechanics draft (#9669, arbiter §5 prep lane)

Refs #9669, part of #6707. Status: preparation only — this is a plan, not
an execution. No IaC values are changed by this document; every file below
is named so the flip executor can open it, not edited here. A fresh arbiter
review is required before any real flip.

Refresh at eshu `origin/main` `75bc84bd` (prior refresh `58629479`;
satellite `eshu-api` `origin/main` is at `90a5a98`, prior `1f50a05`;
satellite `eshu-mcp` `origin/main` is at `a7c6ee6c`): satellite native
dispatch waves D0–D10 merged (#49–#61, wiring #62, D10 #63 + follow-up
#64) plus D9 group terminations (#85 sign-in-policy/providers reads, #86
OIDC pair, #89 GitHub pair, #91 SAML triple), Compare relay-leg native
termination (#87), and D10 re-proof (#90); satellite #44 closed 2026-11-06;
eshu admission seams #10233 / #10245 / #10259 (D1) / #10273 (D0) / #10284
(D8) / #10312 (D7) / #10303 (D6) merged unwired, plus read-admission pins
#10412 (admin-reads), #10425 (compare), #10430 (audit-append), #10432
(sign-in-policy), #10442 (terraform-drift), #10443 (semantic-search),
#10440 (metrics), #10455 (IaC) — none severs an in-tree import yet; query
root drain `e1b8e6e431` moved MCP groups A/D/B off the query root package;
Leaf B read-transport census landed (D0 `58cad548c3`);
`CoreReadContractsVersion` still `v0`. The flip
sequence below is unchanged — no flip, cutover, deploy, or removal
has occurred.

Carry-forward rules: the 11-07 flip-window evidence does NOT carry forward
— every count in this refresh is re-measured at the pins named above, with
the exact command recorded beside it. Path B is declined: the plan stands
as the full every-route migration, and no partial-route flip is in scope.

## 1. IaC-pin-both-edges plan (C5 correction: two per-service pins)

Deployment IaC lives in `eshu-hq/eshu-deploy` (moved there under #8871; see
`scripts/README.md:27` and the `eshu-deploy` row in
`docs/internal/design/eshu-ecosystem-repository-migration.md:68`). The
chart is at `helm/eshu/` (not `deploy/helm/eshu/`). The flip moves two
per-service pins — one per edge — plus the untouched shared monolith
pin; "one pin moves both edges" is retired as a claim. Proven by
`helm template` render at `eshu-deploy` `origin/main` `360e0505`
(C5 evidence, D11 lane). No file in this repo is the flip switch.

| Edge | IaC file (in `eshu-deploy`) | Pinned value |
| --- | --- | --- |
| API | `helm/eshu/templates/deployment-extracted-api.yaml` (+ `service-extracted-api.yaml`), image via the `eshu.extractedApiImage` helper from `extractedApi.image.{repository,tag,digest}`; gated by `extractedApi.enabled` (default `false` renders zero API resources) | `extractedApi.image` → fresh dest `ghcr.io/eshu-hq/eshu-api:<tag>@<digest>` (default `tag: 0.2.0` is stale — never flip on it) |
| MCP | `helm/eshu/templates/deployment-mcp-server.yaml`: `image: .Values.mcpServer.image` when set, else the shared `.Values.image.repository:tag` (monolith, currently `v0.0.3-pre-release-18`) | `mcpServer.image` (full `repo:tag[@digest]`) → fresh dest `eshu-mcp` artifact. BLOCKED on the command switch: the template hardcodes the monolith `eshu mcp start --transport http` command with no value override, so a set pin yields a command-mismatched pod. #9521 reads CLOSED but the switch is absent from the chart at `360e0505` — needs a chart change or a reopen, verified before the pin PR |
| E2E shadow | `compose/docker-compose.e2e.yaml` (under `$ESHU_DEPLOY_CHECKOUT`) | In-tree `Dockerfile:78-88` `mcp-satellite` stage shows the pattern: digest-pinned `ghcr.io/eshu-hq/eshu-mcp:0.2.8@sha256:fb37…` copied under the old binary path with satellite flags (`-addr`/`-core-base-url`) |

Render proof (read-only, no cluster contact):

- Default values: zero `extracted-api` resources render; every
  Deployment renders the shared monolith image.
- `--set extractedApi.enabled=true
  --set extractedApi.image.tag=STAGED`: `Deployment/eshu-extracted-api`
  + `Service/eshu-extracted-api` render with image
  `ghcr.io/eshu-hq/eshu-api:STAGED`.

Flip sequence (executor runs, arbiter reviews first):

0. Assert the cluster context explicitly: `kubectl config
   use-context` the QA context named in the flip record, then verify
   with `kubectl config current-context`. A sibling prod context
   sits in the same kubeconfig — never assume the context, and never
   paste context names into the repo (privacy gate).
1. Land the dest `eshu-api` release from ≥ satellite `90a5a98`
   (tag + image + cosign + attestations, same chain as the #8873
   dest `v0.2.9` flow). No flip on `v0.3.0` — it predates D6–D10.
2. Land the dest `eshu-mcp` release at a tag whose `tools.json`
   order is byte-identical to the in-tree census (171 + 2) at the
   pinned core commit `75bc84bd`. No flip on `v0.2.9` as it stands.
3. Open the one IaC diff in `eshu-deploy`: re-pin the shared image values
   (or the per-service overrides, whichever the chart uses at flip time —
   verify by rendering from a clean checkout, do not assume) to the two
   dest artifacts. Render proof precedes the pin PR.
4. Capture complete `repo:tag@digest` coordinates for both edges
   pre-flip (core last-good) into the flip pin record; the flip
   executor owns this capture.
5. ArgoCD self-heals both Deployments from git; verify both roll out
   (`kubectl rollout status` on the API and MCP Deployments).
6. Run the paired parity re-proof (section 2) inside the same window before
   declaring the flip good, then capture the post-flip dest coordinates.

## 2. Paired parity re-proof plan (HTTP + MCP tool census, same window)

Both halves run against the flipped deployment before acceptance, so a skew
between edges cannot hide.

HTTP half (API edge):

- Route census: re-run the `CUTOVER_OWNERSHIP.md` method
  (`rg -c '^\| \`(GET|POST|PATCH|PUT|DELETE)' go/cmd/api/CUTOVER_OWNERSHIP.md`
  reads 278 — re-verified at `75bc84bd`: 228 `read` + 50 `mutation`
  rows, and 247 `newRouter` + 31 `wireAPI` rows with the 31 pinned by
  `cutoverWireAPIProbeExclusions`) and the `cutover_ownership_test.go` drift gate
  against the live spec (`GET /api/v0/openapi.json`).
- Envelope parity: negotiation (`Accept: application.eshu.envelope+json`
  via `WriteSuccess`/`WriteErrorEnvelope`), pagination (bounded `limit`,
  `page_position`, resume cursor, `truncated`, `TruthLevelPartial`),
  capability/profile gating, authz (`auth_scoped_routes_completeness`,
  fail-closed `BrowserSessionRoutePolicy`, per the Leaf B harness scope
  (`READ_TRANSPORT_CENSUS.md` landed via D0 `58cad548c3`; conformance
  skeleton still red-first at `CoreReadContractsVersion = "v0"`).
- Satellite scope cross-check: `TestSpecScope_Frozen` at satellite
  `90a5a98` pins 174 mounted (173 native + 1 relay leg), 11
  disclosed-absent, 163 served — cite the frozen PASS, do not weaken it.

MCP half (MCP edge):

- Served surface: `tools/list` returns exactly the 2 router tools
  (`find_tool`, `call_tool`); registry census
  (`TestToolRegistryCensusCount`, 171, re-verified at
  `75bc84bd` with `TestToolRegistryCensus*` passing) matches the dest
  `tools.json` `tool_count` (171) byte-for-byte in registration order
  (empty diff against the in-tree census names at `eshu-mcp` `a7c6ee6c`).
- Live sweep: every registry tool answers transport HTTP 200 via
  `call_tool` with the canonical `{data, truth, error}` envelope or a
  documented honest error. Re-baseline the class split live at flip
  time — never copy a prior split as targets.
- Hostile inputs: unknown tool → `mcp_router_unknown_tool` + suggestions;
  bad args → `mcp_router_invalid_arguments`; unauthenticated → identical
  401 `unauthenticated` on `/sse`, `/mcp/message`, and `/api/v0/status`.

Agreement bar: reducer, canonical-graph, HTTP-API, and MCP truth agree per
capability, plus the explicit unaffected-surfaces list (per the #8873
acceptance item 6). A 30 s dispatch-timeout probe (`defaultToolDispatchTimeout`,
`go/internal/mcp/dispatch_timeout.go` symbol `defaultToolDispatchTimeout`) is NOT_CHECKED until a query runs
long enough to fire it live — record that instead of manufacturing load on
shared QA.

## 3. In-flight drain rule per route family

Shutdown bounds (code pins, not tunable at flip time):

- Graceful shutdown: `go/cmd/api/main.go:133-141` reads
  `ESHU_API_SHUTDOWN_TIMEOUT` (default 30 s); requests not finished in the
  window are interrupted (`go/cmd/api/AGENTS.md:46-50`).
- Write timeout floor: `go/cmd/api/main.go:188` derives the HTTP write
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
   and investigation POSTs are reads (`read` in
   `CUTOVER_OWNERSHIP.md`) but carry bodies, so load balancers must drain
   them as requests, not cache entries. They are idempotent and safe to
   retry against the new edge on shutdown interrupt.
3. **GET reads last**: pure GET families drain naturally; clients retry on
   the new edge with no state risk.
4. **MCP tools throughout**: each `call_tool` runs under the 30 s dispatch
   bound; in-flight calls at flip either complete (HTTP 200 + envelope) or
   surface the bounded timeout envelope — never a half-write, because tools
   are reads (the suppressions mutation is HTTP-only, not a tool).

## Open questions (D11 arbiter conditions in parentheses)

1. Render-verify the exact IaC filenames/values from a clean
   `eshu-deploy` checkout before the pin PR (C5); shared-vs-override
   resolution is decided by the render, not assumed.
2. Owner names the last-good pin holder and confirms the fail-closed
   `BrowserSessionRoutePolicy` carries verbatim into the auth
   contract (C4, C6) — recorded in the flip record before execution.
3. Reconfirm the 278-route / 171+2-tool counts at flip time; both drift.
   Re-baseline the live sweep split there too (C8).
4. Carry-forward guard: no 11-07 flip-window evidence is cited as flip
   proof — only the re-baselined pins named in the refresh header above.
   Path B stays declined; the full every-route migration stands.
