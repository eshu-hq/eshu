<!-- SPDX-License-Identifier: MIT -->
<!-- Copyright (c) 2025-2026 eshu-hq -->

# MCP query-dependency inventory (flip prep, #9669)

Refs #9669, part of #6707. Status: preparation only — no behavior change,
no code moves, no IaC edits. This inventory groups every in-tree
`go/internal/mcp` file that imports `go/internal/query` by what it needs,
so the severance step has an exact cut list.

## Method (run in the worktree root)

```bash
rg -ln 'go/internal/query"' go/internal/mcp --glob '*.go' | sort
rg -ln 'go/internal/query"' go/internal/mcp --glob '*.go' | rg -v '_test\.go' | sort
```

## Census result

108 files import `go/internal/query` at HEAD `60f2fe256d`:

- 9 non-test files (the runtime severance surface).
- 99 `*_test.go` files (the test severance surface).

The lane brief said 107; the worktree holds 108. The delta is documented,
not resolved — recount with the method above before cutover. The MCP tool
census at this HEAD is `censusToolCount = 171`
(`go/internal/mcp/tool_registry_census_test.go:36`) plus the 2 router tools
(`find_tool`, `call_tool`), giving a 173-tool served surface.

## Group A — dispatch envelope and budget (3 files)

| File | Needs from `go/internal/query` |
| --- | --- |
| `go/internal/mcp/dispatch_budget.go` | `ResponseEnvelope` type; `ErrorCode` const cast (`mcp_response_over_budget`); `ContentSearchMaxOffset`; `querycontract.PagePositionKey`, `querycontract.TruthOmission`; `query/search/rank` walk trim (`rank.BlockKey`, `rank.TrimWalk`) |
| `go/internal/mcp/dispatch_calls.go` | `query.ErrorCode` comparison only (over-budget classification) |
| `go/internal/mcp/dispatch_envelope.go` | `ResponseEnvelope` / `ErrorEnvelope` construction; `EnvelopeMIMEType`; `parseCanonicalEnvelope` decode |

Severance: replace with the versioned read-contract envelope types owned by
the `eshu-api` contract module. `go/internal/mcp/dispatch.go` itself does
NOT import `query` — it dispatches through `routecontract.Request`
(`go/internal/mcp/contract/route/contract.go`) against an `http.Handler`,
so the dispatch path is already transport-shaped; severance swaps the
handler, not the dispatcher.

## Group B — in-process read seam (1 file)

| File | Needs from `go/internal/query` |
| --- | --- |
| `go/internal/mcp/run_readonly.go` | `*query.ResponseEnvelope` as the return type of `RunReadOnlyTool`; `InProcessMessageHandler` wraps a query `http.Handler` for replay |

Consumers: the Ask Eshu engine and similar programmatic callers
(`RunReadOnlyTool`), plus mcpreplay offline replay (`InProcessMessageHandler`).
Severance: point these callers at the `eshu-api` HTTP transport
(`--core-base-url` seam, see `docs/public/run-locally/mcp-local.md:51`)
and return the contract envelope type instead of `query.ResponseEnvelope`.

## Group C — auth and route policy (2 files)

| File | Needs from `go/internal/query` |
| --- | --- |
| `go/internal/mcp/router/access.go` | `IsSharedKeyOnlyRoute`, `IsPendingRowFilteringRoute`, `ScopedHTTPRouteSupportsTenantFilter`; `AuthContext`, `AuthContextFromContext`, `AuthModeShared`, `AuthModeBrowserSession` |
| `go/internal/mcp/transport_auth.go` | `WithScopedRouteDenialSignal`; `AuthContextFromContext`, `AuthModeShared` |

Severance: these predicates and the auth context must move behind the
versioned auth/session contract. The fail-closed `BrowserSessionRoutePolicy`
semantics (`go/cmd/api/AGENTS.md:37-45`) must be preserved verbatim in that
contract — this group is the highest-risk severance because a dropped
predicate silently widens access.

## Group D — summaries and views (2 files)

| File | Needs from `go/internal/query` |
| --- | --- |
| `go/internal/mcp/summaries.go` | `ResponseEnvelope`, `ErrorEnvelope`, `TruthEnvelope`, `TruthFreshness`, `FreshnessFresh`, `AnswerMetadata`; `StringVal`, `BoolVal` field helpers |
| `go/internal/mcp/summaries_families.go` | `StringVal`, `IntVal`, `BoolVal`, `StringSliceVal` field helpers |

Severance: pure view functions over the envelope shape. They follow Group A:
once the envelope type is the contract type, the helpers move with it or are
re-implemented against contract field accessors.

## Group E — static pointer, not a runtime dep (1 file)

| File | Needs from `go/internal/query` |
| --- | --- |
| `go/internal/mcp/kind_real_consumer.go` | String literal `"go/internal/query"` only (`realConsumerRawSQLDir`); an AST scan directory, no import of query symbols |

Severance: repoint the literal at the new contract source location or retire
the check. No runtime behavior depends on it.

## Group F — tests (99 files)

The 99 `*_test.go` importers construct query handlers in-process
(`query.APIRouter`, `*Handler` structs such as `RepositoryHandler`,
`CodeHandler`, `LanguageQueryHandler`, `QueryPlaybookHandler`),
build truth envelopes (`BuildTruthEnvelope`, `TruthBasis*`, `QueryProfile`),
and thread auth (`AuthContext`, `AuthMiddlewareWithScopedTokens`,
`WriteSuccess`). Sampled files: `answer_parity_workflows_test.go`,
`answer_parity_test.go`, `answer_parity_gen2_test.go`,
`dispatch_budget_rerank_test.go`, `dispatch_language_query_envelope_test.go`,
`dispatch_advisory_evidence_authz_test.go`,
`dispatch_admission_decisions_authz_test.go`, `demo_playbook_parity_test.go`.

Severance: these tests pin in-process behavior. They are retired or
re-pointed at the `eshu-api` HTTP surface (or the satellite binary) at
severance time — they must NOT be carried over as-is, or they re-pin the
in-tree import they were meant to sever.

## Severance order proposal

1. **API-native-complete**: all `Native now` / `Native next` / `Native anytime`
   families from `../eshu-9669-census/go/cmd/api/READ_TRANSPORT_CENSUS.md`
   serve from `eshu-api` with parity proof; `Never satellite` families hold
   behind admission contracts; `Dispatch until Leaf B-D` families
   (Status, SupplyChain) resolve their audit-read / split-point open
   questions first.
2. **Flip**: the single IaC pin moves both edges (see the flip mechanics
   draft); MCP satellite serves from the dest artifact against `eshu-api`.
3. **MCP severance**: Groups A–D move to versioned contracts in dependency
   order C (auth first, highest risk) → A (envelope) → D (views follow the
   envelope) → B (read seam last, after callers migrate); Group E repointed;
   Group F retired/re-pointed in the same diff.
4. **Removal**: in-tree `go/internal/mcp` query imports deleted; removal
   lands only after production acceptance, per the migration rollback rule
   (exactly as the early `go/cmd/mcp-server` removal at `cf357fcf47` must
   NOT be repeated — see the #8873 evidence sweep).

## Open questions

1. Confirm the 107-vs-108 recount at flip time with the method above.
2. Owner confirms the auth-predicate contract (Group C) carries fail-closed
   semantics verbatim.
3. Decide whether Group F parity tests are replaced by the Leaf B cross-repo
   parity harness or by satellite-local tests.
