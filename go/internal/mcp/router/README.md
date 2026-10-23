# MCP router contract

## Purpose

`router` holds the opt-in MCP router surface from epic #8752. With
`ESHU_MCP_TOOL_SURFACE=router`, `tools/list` advertises two tools, `find_tool`
and `call_tool`, instead of the full registry. This package defines the shared
contract the parallel lanes build against and the `Engine` that serves both
tools (#8757). A direct `tools/call` to any registered tool is unchanged, and a
`call_tool` result is byte-identical to the direct call it wraps.

## Ownership boundary

This package owns the contract (`Surface`, the two tool definitions, `Query`,
`Candidate`, `Anchors`, the `Ranker` interface, the `Dispatcher` function type,
the core tool-name list) and the `Engine` that serves `find_tool` and
`call_tool`: argument validation, the per-call credential filter, the playbook
match, recursion refusal, and the router telemetry. It does not rank (the
`Ranker` is injected), dispatch (the `Dispatcher` is injected), or read
configuration.

`internal/mcp` keeps `handleMessage`, `dispatchTool`, authorization, and
transport. Its `Server` embeds one `Host`. In the `tools/call` branch, a name
`Intercepts` accepts goes to `Host.Serve` instead of `dispatchTool`, with a
`Dispatcher` that returns exactly the rendered result a direct call gets, so a
routed `call_tool` is byte-identical to the direct call. The `RouteResolver` is
the root's own `resolveRoute`. The router tools are not in `ReadOnlyTools`.
This package must not import `internal/mcp` (import cycle), and a test enforces
that.

`internal/ask/catalog` owns the per-tool `Intents` and `Canonical` fields this
package ranks against. They are empty until the intent-curation lane fills them.

## Exported surface

- `Surface`, `SurfaceFull`, `SurfaceRouter`, `SurfaceCoreRouter`, `ParseSurface`
- `FindToolName`, `CallToolName`, `Tools`, `DefaultLimit`, `MaxLimit`
- `Anchors`, `Query`, `Candidate`, `Ranker`, `Dispatcher`
- `CoreToolNames`, `Advertise`
- `Host`, `Intercepts`, `RankerBuilder` (`Host.NewRanker`, `Host.Prepare`)
- `Engine`, `NewEngine`, `Config`, `Request`, `CallOutcome`, `ToolResult`,
  `Content`, `RouteResolver`
- `ValidateArguments`, `ValidateArgumentsDetail`, `FieldError`, `UnsupportedSchemaFeatures`,
  `ExampleArguments`, `RouteProbeArguments`

## Engine behavior

- `find_tool` ranks only tools that are in the query profile's visible list,
  are not `find_tool`, `call_tool`, or `ask`, and that the API auth middleware
  does not always refuse for the caller's credential. It never hides a tool the
  caller could call; a tool the middleware admits only under deployment policy
  is offered, and the middleware may still refuse it at dispatch. It re-checks the ranker's output, so a ranker that ignores
  `allowed` still cannot leak a tool. Without an `AuthContext` (stdio or
  `ESHU_MCP_ALLOW_UNAUTHENTICATED`) it does not filter and answers
  `access: {filtered: false, reason: "no_auth_context"}`. With no `Ranker`
  configured it answers `mcp_router_ranker_unavailable`.
- `find_tool` rejects an intent over `MaxIntentBytes` (4096), an anchor value
  over `MaxAnchorValueBytes` (256), and any anchor key other than `repo_id`,
  `service_id`, `workload_id`, or `environment`. The transport has no body cap
  and a ranker may echo anchors into every candidate's example, so these bounds
  keep one request from inflating its response.
- `call_tool` rejects a name over `MaxToolNameBytes` (128) with a fixed error
  that does not echo it and runs no edit distance. `ValidateArguments` returns
  at most `MaxFieldErrors` (20) errors and stops walking once it has them, cuts
  caller-supplied keys in a path at `MaxEchoBytes` (256), and reports no
  per-item errors for an array that fails `maxItems`. Error details carry
  `field_errors_truncated`, true only when errors were dropped
  (`ValidateArgumentsDetail`).
- An example call (in a `find_tool` candidate or a `call_tool` validation
  error) fills required properties, and for a tool whose route needs one of
  several optional selectors (`get_repo_summary`, `get_service_story`,
  `get_service_intelligence_report`) adds the first selector, in name order,
  that makes the route resolve. `Engine.Example` returns it.
- The ranker gets the caller's visible slice and the engine's entries map
  unchanged on every call, so it can cache an index keyed on them; the
  per-call credential filter travels in `allowed`.
- The credential filter refuses only what the API auth middleware always
  refuses: shared-key-only routes for every non-shared caller, and routes off
  the scoped-token allowlist for scoped bearers. Decisions the middleware takes
  from deployment policy are admitted here and enforced at dispatch.
- When a query playbook's title words cover the intent and every step is
  callable, `find_tool` attaches its plan; anchors that name a required input
  add the resolver's fully specified calls, keeping only drilldowns the caller
  may be offered.
- `call_tool` looks targets up in the full registry, so a profile-hidden tool
  stays callable, as it is through `tools/call`. Errors use the
  `{data, truth, error}` envelope with codes `mcp_router_invalid_arguments`
  (with field errors, the input schema, and an example call; a target the
  credential can never call is refused before validation, so its schema is not
  echoed),
  `mcp_router_unknown_tool` (with up to three close names, drawn only from
  tools `find_tool` could offer the same caller), and
  `mcp_router_refused`.
- Validation mirrors dispatch where strict JSON schema would not: a JSON null
  for an optional property is absent, undeclared properties are ignored, a
  blank `date-time` is accepted, and `default` is never applied.

## Request flow

```mermaid
sequenceDiagram
  participant A as Agent
  participant M as Eshu MCP
  participant D as dispatchTool
  A->>M: tools/list
  M-->>A: find_tool, call_tool (plus core tools on core_router)
  A->>M: find_tool(intent, anchors, limit)
  M-->>A: ranked candidates: name, why, cost, inputSchema, example
  A->>M: call_tool(name, arguments)
  M->>M: validate arguments against the target schema
  M->>D: same dispatch as tools/call today
  D-->>A: unchanged envelope and truth labels
```

## Lane ownership

Change `Query`, `Candidate`, or `Ranker` only after commenting on epic #8752;
every lane codes against them.

| Issue | Lane | Owns |
| --- | --- | --- |
| #8753 | Scaffold: this contract and the empty catalog fields | `router/` types and definitions, `Annotation`/`Entry` type fields |
| #8754 | Intent phrases and canonical flags for every tool | new files in `internal/ask/catalog` |
| #8755 | Deterministic lexical ranker | a `Ranker` implementation in `router/rank/` |
| #8756 | Eval corpus and report-only gate | eval corpus and gate, written blind to the intents |
| #8757 | `find_tool` and `call_tool` dispatch, validation, telemetry | `handleMessage` intercept, validator, router telemetry |
| #8758 | `ESHU_MCP_TOOL_SURFACE` switch | env registry entry, `ServerOption`, `/mcp/message` `surface` override |
| #8759 | Core tool set from hosted call counts (21-day QA window, 10 tools) | `core.go` `coreToolNames` |
| #8760 | Hosted validation, assistant docs, default decision | docs and measurement |

## Telemetry

`eshu_dp_mcp_router_find_total{outcome}`, `eshu_dp_mcp_router_call_total{tool,outcome}`,
and `eshu_dp_mcp_router_target_rank`, plus the `mcp.router.find` and
`mcp.router.call` spans. See
[MCP Router Telemetry](../../../../docs/public/reference/telemetry/mcp-router.md).

## Performance and concurrency evidence

No-Regression Evidence (#8757): measured on a 16-core arm64 host (Linux
7.0.0-1006-aws, Go 1.26.10) against the in-process echo handler, so the numbers
isolate MCP overhead from any graph or Postgres backend. The baseline is `main`
at `de1e830767` (before this change), and the after measurement is this branch
with the same harness. The benchmark file was applied through
`go test -overlay` and is not committed. It builds a `Server` over a handler
that answers every route with a canonical envelope, then loops `handleMessage`
on a fixed `tools/call`.

- Direct `tools/call` (the path every non-router call takes, now through
  `callTool` and its shared `run` closure).
  `go test -run '^$' -bench BenchmarkDirectToolsCall -benchtime=20000x -count=6 ./internal/mcp/`,
  median of 6:

  | Call | Before | After |
  | --- | --- | --- |
  | `list_indexed_repositories` `{limit: 50, offset: 0}` | 17.0 us/op, 10,866 B, 150 allocs | 15.9 us/op, 10,901 B, 151 allocs |
  | `find_code` `{query, limit: 10}` | 16.6 us/op, 10,842 B, 164 allocs | 16.7 us/op, 10,881 B, 165 allocs |

  The change is one closure allocation (about 37 B); latency is within
  run-to-run noise. A `find_tool` or `call_tool` adds the router work on top,
  and only those two names reach it.
- `Engine` construction runs once per `Host`, on the first router request, and
  classifies every registered route and precomputes every example:
  1.15 ms and 845 KB with entries supplied, 2.83 ms and 2.9 MB when it also
  loads the embedded surface-inventory catalog (`-benchtime=200x -count=5`,
  median).
- Recent-find store (`recent.go`): one mutex guards the per-session map and its
  FIFO order. The lock is held only to copy at most `MaxLimit` (10) names in
  `remember` (182.6 ns/op, 208 B, 1 alloc, including eviction past capacity)
  or to scan them in `rank` (39.5 ns/op, 0 allocs). It holds at most 1,024
  sessions. Each costs about 250 B (ten 16-byte string headers that share the
  registry's name bytes, plus the map and order entries) plus the bytes of its
  principal key (auth mode, tenant, workspace, and subject hash), so well under
  1 MB at the bound. No I/O or callback runs under the lock.
- Concurrency: `TestHostServeIsSafeUnderConcurrentCalls` runs 64 goroutines x
  200 `Serve` calls over 2,560 sessions, so eviction runs under contention.
  `go test -race -count=1 -run TestHostServeIsSafeUnderConcurrentCalls ./internal/mcp/router/`
  passes (0.41 s), and the store stays at 1,024 entries or fewer with its map
  and order in step. Removing the lock in `remember` makes the same command
  report data races (7 to 8 across runs). The engine's other maps are written once in
  `NewEngine`, and `Host` builds it under `sync.Once`.
- Why the input side is safe: the transport has no body cap, so the router is
  the bound (`routed_call_bounds_test.go`, `go test -run TestCallToolBounds -v ./internal/mcp/`).
  The terminal counts: a 1 MiB `call_tool` name returns a 757 B error in 8 to
  10 ms with no edit-distance work; 200,000 ints in
  `build_evidence_citation_packet.handles` return 4,009 B in 31 to 48 ms;
  2,000 caller keys of 4 to 6 KB under `resolve_query_playbook.inputs` (about
  10 MB) return 15,029 B. Every error result is capped at 20 field errors and
  256 echoed bytes per path.

Observability Evidence (#8757): an operator answers "does routing work" from
`eshu_dp_mcp_router_find_total{outcome}` (`candidates`, `empty`, `error`),
`eshu_dp_mcp_router_call_total{tool,outcome}` (`dispatched`,
`invalid_arguments`, `unknown_tool`, `refused`), and
`eshu_dp_mcp_router_target_rank` (dispatched calls only; 0 = the agent ran a
tool `find_tool` did not offer). Both spans carry `outcome`; `mcp.router.call` also carries
`mcp.router.target`, and a dispatched call's own spans nest inside
`mcp.router.call`. Error results carry typed codes:
`mcp_router_invalid_arguments` (with `field_errors` and
`field_errors_truncated`), `mcp_router_unknown_tool`, `mcp_router_refused`,
`mcp_router_ranker_unavailable`, `mcp_router_rank_failed`, and
`mcp_router_dispatch_failed`. A ranker failure logs `mcp router: ranker
failed` at WARN. See [MCP Router Telemetry](../../../../docs/public/reference/telemetry/mcp-router.md).

## Known divergence from a direct call

`call_tool` validates against the advertised `inputSchema` with no tolerance; a
direct `tools/call` goes straight to the route builder. Five recorded classes
of call can succeed directly but are refused by `call_tool` with a field error
that names the schema's rule (arbiter ruling on #8757). The schemas stay as
they are, because widening them would change `tools/list` for the `full`
surface.

| Class | Direct call | `call_tool` | Handler evidence |
| --- | --- | --- | --- |
| A, undocumented alias | `search_entity_content` / `search_file_content` with `query` and no `pattern`; `resolve_entity` with the legacy `name`/`type` keys | `pattern` or `query` is required | `mcp/content` `contentSearchBody` sends `query` when `pattern` is absent; `mcp/entity/resolution` `resolveEntityBody` sends `name` |
| B, enum case | `execute_language_query` with `entity_type: "Function"` | `entity_type` must be one of the lowercase values | `query/language` `Handler.handleLanguageQuery` lowercases `entity_type` |
| D, off-enum list filter | `list_ci_cd_run_correlations` `outcome`, `list_sbom_attestation_attachments` `attachment_status`, `list_secrets_iam_*` `state` outside the enum | the filter must be one of the enum values | the list handlers pass the filter to the store, which matches no rows |
| E, wrong type defaulted | a wrong JSON type on one of the fields listed per tool, such as `list_indexed_repositories` `limit: "25"` | the field must have the schema type | dispatch's argument helpers (`routecontract` `Arguments.String`, `IntOr`, `BoolOr`, `StringSlice`; root `str`, `intOr`, `boolOr`) drop the value or use the default |
| F, handler-optional required field | `analyze_code_relationships` and `analyze_infra_relationships` without `query_type`, `build_evidence_citation_packet` without `handles`, `list_package_registry_packages` without `limit` | the field is required | the route sends an empty or default value and the handler serves it; each row cites its handler |

Class C is not a divergence: the handler answers 400 for the same call (each
row records the handler's message), or the route answers 404 on an empty path
segment, which direct dispatch reports as an error too. A path id left empty is also class C: `resolveRoute` refuses
the `//` route with `<field> is required` before any request is built, so no
handler runs (#8772; `get_entity_context`, `get_incident_context`,
`get_repository_coverage`, `get_workload_context`, `get_workload_story`, and
five more such tools). `TestDirectCallRefusesEmptyPathSegmentRows` holds those
five rows to that.

`TestValidatorAcceptsEveryDispatchedArgumentMap` in the mcp root holds the
validator to every literal argument map in the dispatch tests and every map in
the catalog sweep corpus, except maps in a class. The classes are enumerated
tables, not keyword matches: `routed_call_divergence_tables_test.go` lists each
tool and field set with its evidence, and a rejection no row lists fails the
test (`TestDifferentialRefusesUnlistedRejections` seeds one of each kind). Each
class has a live witness, A and B have pinning tests that fail once a schema
accepts the alias or case.

Staleness is checked per class, not per row. A row is re-proven only when a
literal dispatch-test map, a sweep map, or a witness hits it; a row seen only
in the runtime capture (most C and E rows) is not re-checked by any test and
can go stale silently. Re-run the capture below after changing a schema or a
route builder, and prune rows it no longer reports.

The rows came from a one-off probe. A runtime capture recorded every map
`resolveRoute` built across `go test ./internal/mcp`, and a throwaway test
replayed each rejected map's route against the real API router with nil
backends. A required-field row was replayed with the tool's catalog-sweep
arguments minus only those fields, so a 400 is attributed to the right field;
a call that got past validation (a 2xx, or a 5xx from the nil store) became a
class F or E row. The capture is a one-off: no CI job runs it. On
`aee7455f6d` it recorded 1345 distinct maps and 253 rejected: A 77, B 2, C 84,
D 9, E 64, F 8, G 7, and the 2 seeded unlisted maps outside any class, by
design. With #8772 applied the 7 G maps are no longer built, and one E map (a
wrong-typed `entity_id`) goes with them; the same capture then recorded 1351
distinct maps (the new #8772 tests add some) and 245 rejected: A 77, B 2, C 84,
D 9, E 63, F 8, and the 2 seeded maps. To repeat it, wrap `resolveRoute` in
`dispatch.go` to record each map it returns a route for (refused maps are not
recorded; recording every call gives different counts), and classify the
rejected ones with `classifyRejection` from a `TestMain`, through
`go test -overlay`.

## Gotchas / invariants

- `Tools` returns fresh definitions; mutating a result must not change a later
  call.
- `ParseSurface("")` is `full`. An unknown value is an error, never a silent
  fallback to `full`.
- `CoreToolNames` holds the 10 measured core tools (#8759). `core_router`
  advertises `find_tool`, `call_tool`, and those tools. Change the list only
  with a new measurement, the decision record, and `measuredCoreToolNames` in
  `go/internal/mcp/core_tool_set_test.go`.
- `find_tool` needs a `Ranker`. `cmd/mcp-server` wires the lexical ranker
  (`router/rank`, #8755) through `mcp.WithRouterRanker` (#8771): `NewServer`
  calls `Host.Prepare` after every option, which loads the catalog entries once
  and builds the ranker once over the profile-filtered tool list. The Engine
  then gets that same entries map, and the root passes that same tool slice as
  `Request.Visible`, so `rank`'s index is reused on every call instead of
  rebuilt. `cmd/mcp-server` refuses to start on `router` or `core_router` when
  no ranker was built. A server built without a ranker (a test, an embedding
  host) still answers the typed `mcp_router_ranker_unavailable` error, counted
  as `eshu_dp_mcp_router_find_total{outcome="error"}`.
- A new keyword, type, or format in a registered tool schema fails
  `TestRegisteredSchemasUseOnlyValidatedKeywords` in the mcp root until
  `ValidateArguments` implements it.
- `Ranker.Rank` must never return a tool for which `allowed` is false.

## Related docs

- [MCP package](../README.md)
- [MCP tool contract](../contract/tool/README.md)
- [Ask catalog](../../ask/catalog/README.md)

## Verification

From `go/`, run `go test ./internal/mcp/router/... ./internal/ask/catalog/...
-count=1` and `go vet ./internal/mcp/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
