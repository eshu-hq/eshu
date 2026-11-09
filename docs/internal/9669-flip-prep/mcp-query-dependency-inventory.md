<!-- SPDX-License-Identifier: MIT -->
<!-- Copyright (c) 2025-2026 eshu-hq -->

# MCP query-dependency inventory (flip prep, #9669)

Refs #9669, part of #6707. Status: preparation only — no behavior change,
no code moves, no IaC edits. This inventory groups every in-tree
`go/internal/mcp` file that imports `go/internal/query` or one of its
subpackages by what it needs, so the severance step has an exact cut list.

## Method (run in the worktree root)

```bash
rg -ln 'eshu-hq/eshu/go/internal/query"' go/internal/mcp --glob '*.go' | sort
rg -ln 'eshu-hq/eshu/go/internal/query"' go/internal/mcp --glob '*.go' | rg -v '_test\.go' | sort
rg -ln 'eshu-hq/eshu/go/internal/query/' go/internal/mcp --glob '*.go' | sort
```

The module-prefixed exact-package pattern (`query"`) counts only real
imports of the root query package. The subpackage pattern (`query/`)
counts real imports of the versioned contract seams under it
(`querycontract`, `query/auth`, `query/playbook`, `query/search/rank`,
`query/contentread`, …). The bare `go/internal/query"` pattern from the
prior refresh over-counts by 2: it also matches the
`"go/internal/query"` string literal in `kind_real_consumer.go` (Group E
below, not an import) and one test file's string literal.

## Census result

99 files import the root `go/internal/query` package at HEAD `75bc84bd`
(re-measured in this worktree on `origin/main` `75bc84bd` with the
method above; `git grep` corroboration returns the identical
99-file set — all `*_test.go`, zero non-test files):

- 0 non-test files import the root package (the 8 runtime files from the
  prior refresh moved to subpackage imports under drain `e1b8e6e431`).
- 99 `*_test.go` files (the test severance surface, Group F).

Correction to the prior refresh: it reported 107 exact-package importers
(8 non-test + 99 test) at `58629479`. After the drain, the exact-package
count is 99 test-only files — the 107 figure no longer holds and is not
carried forward. The runtime severance surface is now 10 files importing
`query/` subpackages (Groups A–D below):

- 8 files drained off the root package by `e1b8e6e431` (row-2 leaf 2a,
  MCP groups A/D/B; ancestor of this HEAD, verified by merge-base).
- 2 files that already imported subpackages at the prior refresh but were
  invisible to the exact-package method: `server.go` (`querycontract`
  profile gating) and `router/playbook.go` (`query/playbook` catalog).
- 28 `*_test.go` files import subpackages (6 also import the root
  package), so 121 distinct test files touch the query tree; the union
  over root + subpackage importers is 131 distinct files.

The MCP tool census at this HEAD is
`censusToolCount = 171`
(`go/internal/mcp/tool_registry_census_test.go` symbol
`censusToolCount`, re-verified at `75bc84bd` with
`TestToolRegistryCensus*` passing) plus the 2 router
tools (`find_tool`, `call_tool`), giving a 173-tool served surface. The
dest `tools.json` at satellite `eshu-mcp` `origin/main` `a7c6ee6c` carries
`registry.tool_count = 171` with `tool_order` byte-identical to the
in-tree census names (empty diff, verified in this refresh).

## Landings since the prior refresh `58629479` (now `75bc84bd`)

Eshu `origin/main` is at `75bc84bd` (382 commits). Satellite `eshu-api`
`origin/main` is at `90a5a98` (PR #91 SAML termination). Satellite
`eshu-mcp` `origin/main` is at `a7c6ee6c`. No flip, cutover, deploy, or
removal has occurred — preparation only.

- Satellite native dispatch complete and extended: D9 groups terminated
  natively (#85 sign-in-policy/providers reads, #86 OIDC pair, #89 GitHub
  pair, #91 SAML triple), Compare relay leg terminated natively (#87),
  D10 re-proof (#90). Frozen scope at `90a5a98`: 174 mounted (173 native
  + 1 relay leg), 11 disclosed-absent, 163 served
  (`TestSpecScope_Frozen` PASS cited, not weakened). Satellite #44
  closed 2026-11-06.
- Eshu admission seams landed, unwired: prior D1/D0/D8/D7/D6 pins #10259,
  #10273, #10284, #10312, #10303, plus read-admission pins #10412
  (admin-reads), #10425 (compare), #10427 (gap07 record), #10430
  (audit-append), #10432 (sign-in-policy), #10442 (terraform-drift),
  #10443 (semantic-search), #10440 (metrics), #10455 (IaC).
  None severs an in-tree import yet.
- Query-root drain waves landed (`e1b8e6e431` row-2 leaf 2a; row2b,
  row8, row9, auth, ask, capinv, CLI, golden-corpus follow-ups).
- Leaf B read-transport census landed (D0 `58cad548c3`);
  `CoreReadContractsVersion` still `v0`; the cross-repo parity
  harness itself is still pending — Group F replacement stays open
  (decision deferred to the severance lane per the D11 ruling).

## Group A — dispatch envelope, budget, and profile (4 files)

| File | Needs from `go/internal/query/*` |
| --- | --- |
| `go/internal/mcp/dispatch_budget.go` | `querycontract` envelope + `ErrorCode` cast (`mcp_response_over_budget`), `PagePositionKey`, `TruthOmission`; `contentread.ContentSearchMaxOffset`; `query/search/rank` walk trim (`rank.BlockKey`, `rank.TrimWalk`) |
| `go/internal/mcp/dispatch_calls.go` | `querycontract.ErrorCode` comparison only (over-budget classification) |
| `go/internal/mcp/dispatch_envelope.go` | `querycontract` `ResponseEnvelope` / `ErrorEnvelope` construction; `EnvelopeMIMEType`; `parseCanonicalEnvelope` decode |
| `go/internal/mcp/server.go` | `querycontract` `QueryProfile` gating of the `tools/list` surface (`NormalizeQueryProfile`, `ProfileProduction`) |

Severance: replace with the versioned read-contract envelope types owned by
the `eshu-api` contract module. `go/internal/mcp/dispatch.go` itself does
NOT import `query` — it dispatches through `routecontract.Request`
(`go/internal/mcp/contract/route/contract.go`) against an `http.Handler`, so
the dispatch path is already transport-shaped; severance swaps the
handler, not the dispatcher.

## Group B — in-process read seam (2 files)

| File | Needs from `go/internal/query/*` |
| --- | --- |
| `go/internal/mcp/run_readonly.go` | `querycontract.ResponseEnvelope` as the return type of `RunReadOnlyTool`; `InProcessMessageHandler` wraps a query `http.Handler` for replay |
| `go/internal/mcp/router/playbook.go` | `query/playbook` `Catalog` / `Definition` for the call plans `find_tool` attaches on intent match |

Consumers: the Ask Eshu engine and similar programmatic callers
(`RunReadOnlyTool`), plus mcpreplay offline replay (`InProcessMessageHandler`).
Severance: point these callers at the `eshu-api` HTTP transport
(`--core-base-url` seam, see `docs/public/run-locally/mcp-local.md:51`)
and return the contract envelope type instead of the query type; the
playbook plan catalog follows the read seam to the contract side.

## Group C — auth and route policy (2 files)

| File | Needs from `go/internal/query/*` |
| --- | --- |
| `go/internal/mcp/router/access.go` | `query/auth` predicates (`IsSharedKeyOnlyRoute`, `IsPendingRowFilteringRoute`, `ScopedHTTPRouteSupportsTenantFilter`); `AuthContext`, `AuthContextFromContext`, `AuthModeShared`, `AuthModeBrowserSession` |
| `go/internal/mcp/transport_auth.go` | `query/auth` `WithScopedRouteDenialSignal`; `AuthContextFromContext`, `AuthModeShared` |

Severance: these predicates and the auth context must move behind the
versioned auth/session contract. The fail-closed `BrowserSessionRoutePolicy`
semantics (`go/cmd/api/AGENTS.md:37-45`) must be preserved verbatim in that
contract — this group is the highest-risk severance because a dropped
predicate silently widens access.

## Group D — summaries and views (2 files)

| File | Needs from `go/internal/query/*` |
| --- | --- |
| `go/internal/mcp/summaries.go` | `querycontract` `ResponseEnvelope`, `ErrorEnvelope`, `TruthEnvelope`, `TruthFreshness`, `FreshnessFresh`, `AnswerMetadata` (`querycontract/answer`); `StringVal`, `BoolVal` field helpers |
| `go/internal/mcp/summaries_families.go` | `querycontract` `StringVal`, `IntVal`, `BoolVal`, `StringSliceVal` field helpers |

Severance: pure view functions over the envelope shape. They follow Group A:
once the envelope type is the contract type, the helpers move with it or are
re-implemented against contract field accessors.

## Group E — static pointer, not a runtime dep (1 file)

| File | Needs from `go/internal/query` |
| --- | --- |
| `go/internal/mcp/kind_real_consumer.go` | String literal `"go/internal/query"` only (`realConsumerRawSQLDir`); an AST scan directory, no import of query symbols |

Severance: repoint the literal at the new contract source location or retire
the check. No runtime behavior depends on it.

## Group F — tests (99 root + 28 subpackage importers, 121 distinct)

The 99 `*_test.go` root-package importers construct query handlers
in-process (`query.APIRouter`, `*Handler` structs such as
`RepositoryHandler`, `CodeHandler`, `LanguageQueryHandler`,
`QueryPlaybookHandler`), build truth envelopes (`BuildTruthEnvelope`,
`TruthBasis*`, `QueryProfile`), and thread auth (`AuthContext`,
`AuthMiddlewareWithScopedTokens`, `WriteSuccess`). Sampled files:
`answer_parity_workflows_test.go`, `answer_parity_test.go`,
`answer_parity_gen2_test.go`, `dispatch_budget_rerank_test.go`,
`dispatch_language_query_envelope_test.go`,
`dispatch_advisory_evidence_authz_test.go`,
`dispatch_admission_decisions_authz_test.go`, `demo_playbook_parity_test.go`.

A further 28 test files import `query/` subpackages (`querycontract`,
`codequery`, `testutil/*`, `querycontract/code`, …); 6 import both, so the
retirement scope is 121 distinct test files, not 99.

Severance: these tests pin in-process behavior. They are retired or
re-pointed at the `eshu-api` HTTP surface (or the satellite binary) at
severance time — they must NOT be carried over as-is, or they re-pin the
in-tree imports they were meant to sever.

## Severance order proposal

1. **API-native-complete**: all `Native now` / `Native next` / `Native anytime`
   families from `../eshu-9669-census/go/cmd/api/READ_TRANSPORT_CENSUS.md`
   serve from `eshu-api` with parity proof; `Never satellite` families hold
   behind admission contracts; `Dispatch until Leaf B-D` families
   (Status, SupplyChain) resolve their audit-read / split-point open
   questions first.
2. **Flip**: the two per-service IaC pins move (see the flip mechanics
   draft — `extractedApi.image.*` for the API edge, `mcpServer.image`
   for the MCP edge, with the command-switch chart change landing first;
   the old "single pin" claim is retired); MCP
   satellite serves from the dest artifact against `eshu-api`.
3. **MCP severance**: Groups A–D move to versioned contracts in dependency
   order C (auth first, highest risk) → A (envelope + profile) → D (views
   follow the envelope) → B (read seam + playbook catalog last, after
   callers migrate); Group E repointed; Group F retired/re-pointed in the
   same diff.
4. **Removal**: in-tree `go/internal/mcp` query imports deleted; removal
   lands only after production acceptance, per the migration rollback rule
   (exactly as the early `go/cmd/mcp-server` removal at `cf357fcf47` must
   NOT be repeated — see the #8873 evidence sweep).

## Severance cut list (D11 condition C2 — plan, not execution)

Severance runs after the flip in C→A→D→B order (D11 arbiter ruling);
nothing below is executed in this lane.

| Order | Group | Files | Cut |
| --- | --- | --- | --- |
| 1 | C auth/policy | `router/access.go`, `transport_auth.go` | Predicates + auth context behind the versioned auth/session contract, fail-closed `BrowserSessionRoutePolicy` verbatim (owner confirms, C6) |
| 2 | A envelope/budget/profile | `dispatch_budget.go`, `dispatch_calls.go`, `dispatch_envelope.go`, `server.go` | Contract envelope types owned by the `eshu-api` contract module; profile gating moves with the envelope |
| 3 | D views | `summaries.go`, `summaries_families.go` | Follow the envelope; helpers move or re-implement on contract accessors |
| 4 | B read seam | `run_readonly.go`, `router/playbook.go` | Callers to `eshu-api` HTTP transport; return contract envelope; playbook plans resolve against the contract catalog |
| — | E pointer | `kind_real_consumer.go` | Repoint literal or retire the check |
| — | F tests | 121 files (99 root + 28 subpackage, 6 overlap) | Retire or re-point at the `eshu-api` HTTP surface in the same diff — never carry over as-is |

Group F replacement harness: Leaf B cross-repo parity harness vs
satellite-local tests — decision deferred to the severance lane.
Decision owner: the severance lane owner (named when that lane
opens). Acceptance bar: the replacement proves the same in-process
behavior the retired tests pinned (envelope shape, authz denials,
admission decisions) against the flipped edges; a retired test with
no proven replacement blocks removal of its surface.

## Open questions

1. Corrected: the count is 99 exact-package root importers (all tests,
   zero runtime) plus 10 runtime subpackage importers. The prior 107
   (8 + 99) is void after drain `e1b8e6e431`. Re-run the module-prefixed
   method (root `query"` + subpackage `query/`) at flip time.
2. Owner confirms the auth-predicate contract (Group C) carries fail-closed
   semantics verbatim (C6) — recorded in the flip record before execution.
3. Group F harness choice deferred to the severance lane (C2 above);
   the Leaf B skeleton exists, the harness itself is pending.
