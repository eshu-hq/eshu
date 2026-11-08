<!-- SPDX-License-Identifier: MIT -->
<!-- Copyright (c) 2026 eshu-hq -->

# Removal-tail PREP: in-tree consumer-drain checklist (#9669)

Refs #9669, part of #6707. Status: preparation only — no cutover has
occurred, no deletions in this artifact, and every row below is TODO.
Leaf executors (Leaves B–E) drain these consumers; the removal PR deletes
only when each row proves zero remaining in-tree callers.

Source of truth for served routes stays [CUTOVER_OWNERSHIP.md](CUTOVER_OWNERSHIP.md)
(278 rows; count with the escaped pipe `rg -c '^\| '` — the bare
`'^| '` pattern alternates and overcounts to 364). Counts below were
re-measured on `origin/main` `d59650c757` (D13 lane base; prior base
`475ab11911`): the re-prove command returns 3 files (was the identical
88-file set) — all 3 are the known string-mention false positives with
zero AST-exact imports (`go/internal/facts/docs/documentation.go`,
`go/internal/facts/semantic/semantic.go`,
`go/internal/mcp/kind_real_consumer.go`), so no Go row retains a true
non-test import of the query root. Total root-package references outside
`go/cmd/api` and `go/internal/query` are 174 (was 297): 171 in
`_test.go` files plus those 3 false positives. The exact-package MCP
census is 99 (was 107: the 8 true non-test imports drained to 0, the 99
test files remain; one further test file mentions the path as a string
only). `CUTOVER_OWNERSHIP.md` is byte-identical to the prior base (364
lines; 278 route data rows). Re-run the listed command before the
removal PR — counts drift as sibling lanes land. Drain PRs landed on
main since the prior base: #10348 (row 5), #10355 (row 4), #10359
(row-4 reducer followup), #10362 (auth), #10363 (capinv row 7), #10364
(ask), #10365 + #10372 (cli row 3), #10366 (golden-corpus row 6),
#10378 (row-9 remainder), #10381 (row 2 MCP), #10382 (row 8 remainder),
#10387 (auth tests), #10392 (row 2b). Every Go row below stays TODO:
test-file references still block per the drain rule. The 51-route
disposition (SAT 2, MCP 37, CORE 11, RET 1; 49 need satellite closure or
carve-out) is unchanged — full detail in the D13 lane record at
`docs/internal/9669-removal/lane-record.md`.

Landings since the prior base (dest-side and seam progress; every row
below stays TODO — no drain is claimed): satellite `eshu-api` at
`v0.4.0` (`1f50a05`, D0–D10 waves merged, parity/read-latency/test/race
green at the release commit) and `eshu-mcp` at `v0.2.10` (tools.json
171/171 byte-identical); D11 flip pins live on the QA edges
(flip-good for the pin move, not cutover-good); D12 Plan A rollback
rehearsal complete with evidence on #9669; four satellite P2s filed
(eshu-mcp#60, eshu-mcp#61, eshu-api#65, eshu-api#66), all deferred to
removal review. In-tree admission seams still unwired; `go/internal/mcp`
does NOT move in #9669 (in-tree reproduction contract per #9670).

Re-prove command (whole checklist, non-test Go files importing
`go/internal/query` outside the API tree itself):

```bash
rg -ln 'internal/query"' go --glob '*.go' | rg -v '_test\.go' | rg -v '^(go/cmd/api|go/internal/query)/' | sort
```

Row-2 census fix: from the repo root the MCP path needs the `go/`
prefix — `rg -l 'eshu-hq/eshu/go/internal/query"' --type go
go/internal/mcp/` (the bare `internal/mcp/` form exits 2).

## Consumer rows

| # | Consumer | Current import / target | Replacement path | Status |
| --- | --- | --- | --- | --- |
| 1 | `go/cmd/api` (in-tree handlers, wiring, `newRouter`/`wireAPI`) | Owns all 278 route rows across 66 families (`CUTOVER_OWNERSHIP.md`); reads via reader ports, one bounded suppression mutation | Moves to eshu-api as the native handler set; reads via versioned core contracts (`CoreReadContractsVersion = "v0"`, `go/internal/query/querycontract/api_boundary.go`); mutations via versioned admission contracts | TODO |
| 2 | `go/internal/mcp` — 0 non-test files at `d59650c757` (was 8: `transport_auth.go`, `dispatch_calls.go`, `dispatch_budget.go`, `dispatch_envelope.go`, `run_readonly.go`, `summaries.go`, `summaries_families.go`, `router/access.go`; drained by #10381 + #10392 to `querycontract`/`query/auth` native homes) plus 99 test files (99 exact-package total; `kind_real_consumer.go` and `tools_runtime_test.go` only mention the path in a string, not an import — re-measured with `rg -l 'eshu-hq/eshu/go/internal/query"' --type go go/internal/mcp/`) | `ResponseEnvelope`, filters, views | Adapt to versioned read contracts; MCP calls eshu-api transport (Leaf C/D lane) | TODO |
| 3 | `go/internal/cli` — 0 files at `d59650c757` (was 22 at `475ab11911`, 48 before: admin credential flows, compparity exercises, docs verify, graphinstall, hosted onboard, investigation fetch/packet/refusal/subject, localsupervisor config/graph/host/lifecycle/mcpbridge, report/servicereport, support handler, vulnscan localruntime; drained by #10365 with floor-clean followup #10372 — not even test files reference the root package) | `TruthEnvelope`, `ErrorEnvelope`, packet helpers | eshu-cli HTTP client against eshu-api; `localsupervisor` embedded stores stay core-local; doc/parity assertions re-point at the eshu-api OpenAPI surface | TODO |
| 4 | `go/cmd/reducer` — 0 non-test files at `d59650c757` (was 17 at `475ab11911`, 23 before: canonical_graph_writers, config, cross_scope_readiness_wiring, ecs_binding_lookups, generation_retention_wiring, graph_orphan_sweep_wiring, main, main_helpers, neo4j_wiring, observed_service_wiring, provenance_gauge_wiring, run, search_vector_build_wiring, wiring_handlers, workload/sibling/instance-retraction lookups; drained by #10355 with #10359 followup to `querycontract` ports) plus 6 test files | `GraphQuery` port and related wiring types | Import ports from `querycontract` (core-owned); drop handler imports; reducer stays core | TODO |
| 5 | `go/cmd/eshu` — 0 non-test files at `d59650c757` (was 3: `service.go`, `graph.go`, `investigation_cmd.go`; drained by #10348) plus the same 8 parity/perf test files | Profiles, backends, packet render helpers; local `api`/`graph` service commands | eshu-cli HTTP client against eshu-api; local modes via core contracts | TODO |
| 6 | `go/cmd/golden-corpus-gate` — `query.go` carries no root import at `d59650c757` (was 2 files; #10366 re-pointed it at a `querycontract.EnvelopeMIMEType` alias); remaining refs are `querycontract`/`queryplan`/`visualization` subpackage imports, one flag-default string, and test files | `EnvelopeMIMEType` | Contract constant from eshu-sdk/api client; gate retargets the eshu-api base URL | TODO |
| 7 | `go/cmd/capability-inventory` — 0 non-test root imports at `d59650c757` (was 4: `surfaces.go`, `graph_read_probe_registry.go`, plus tests; probe-registry drained per lane record, #10363) plus 1 test file with string mentions only (`docs_mode_test.go`); `openapi_source.go` still reads the `query/openapi` leaf seam (subpackage import, interim until eshu-api#65) | `OpenAPISpec`, probe registry | Fetch OpenAPI/capabilities from eshu-api; probes run against the satellite | TODO |
| 8 | Shared ask/serviceintel helpers — 0 non-test root imports at `d59650c757` across `go/internal/ask/engine` (was 26: `AnswerPacket`, `ResponseEnvelope`, truth classes; drained by #10364 + #10382), `go/internal/serviceintelhttp`, `go/internal/reportbundle` + `answerquality` + `answernarration`, `go/internal/serviceintel` (was 6); test files remain (ask/engine 17, serviceintel 3, serviceintelhttp/reportbundle/answerquality/answernarration 1 each) | Handler-adjacent shapes and HTTP helpers | Ask engine moves with the Ask family (Native now); pure shapes hoist to eshu-sdk or the eshu-api contract module; `serviceintelhttp` moves with ServiceIntelHTTP (Native now) | TODO |
| 9 | Auth/session/fact helpers — `go/internal/scopedtoken`, `oidcbearer`, `oidclogin`, `githublogin` (zero non-test `internal/query"` matches; 3 test files still import the query root at `d59650c757` (was 10: 4 scopedtoken, 4 oidcbearer, 2 oidclogin; #10387 + #10392 drained the rest): `scopedtoken/identity_postgres_restart_test.go`, `scopedtoken/middleware_integration_test.go`, `oidcbearer/resolver_oauth_integration_test.go`) plus string-mention false positives with zero AST-exact imports, pinned by `TestQueryRootDrained` pin tests (green at this base): `go/internal/facts/docs/documentation.go` + `go/internal/facts/semantic/semantic.go` (`NoReducerEvidence` strings, kept per arbiter); `go/internal/evidencecontinuity/gatetriggers.go` no longer matches (neutralized to a `cigates` example) | Auth/session helpers, provider config, doc shapes, gate triggers | Versioned auth/session contracts; identity stores stay core; fact schemas to eshu-sdk; triggers re-point at eshu-api evidence routes | TODO (3 auth test files) |
| 10 | Doc/closure references — `askwiring`, `cigates`, `cli/support` docs; `cli/admin` credential flows | `cmd/api` doc references; admin credential clients | Re-point docs at eshu-api; credential flows at admission-contract clients | TODO |
| 11 | `Dockerfile` — line 50 builds `/go-bin/eshu-api` from `./cmd/api`; line 290 `CMD ["eshu-api"]`; `HEALTHCHECK` on `:8080/healthz` | Core image ships the in-tree API binary as default command | eshu-api repo Dockerfile owns the api build; core image drops the `eshu-api` binary and retargets `CMD`/healthcheck | TODO |
| 12 | `specs/ci-pr-workflows.d/read-api-latency-gate.yml` — triggers on `go/**`, route-budget files (`testdata/benchmarks/read-api-route-budgets.txt`, `read-api-route-work-budgets.txt`), compose files | In-process latency measurement of the in-tree path | eshu-api repo owns the latency gate against a pinned core image; core keeps the contract-registry and static-contract gates | TODO |
| 13 | Live/e2e gate specs — `e2e-tests.yml`, `live-backend-tests.yml`, `live-postgres-readiness.yml`, `golden-corpus-gate.yml` (all reference `go/cmd/api/**` or `CUTOVER_OWNERSHIP.md`) | Gates exercise the in-tree binary surface | Leaf B cross-repo parity harness replays golden-corpus and replay coverage against an eshu-api deployment instead of the in-tree binary | TODO |

## Drain rule

A row drains only when its re-prove query returns empty (or names only
the replacement import) on the removal-PR base. Test-only references
(row 2's ~100 test files, row 5's parity tests) drain by retarget or
deletion in the same PR — they still block it while present. The removal
itself needs Leaves A–G merged plus a fresh arbiter review; this lane
does not authorize it.
