<!-- SPDX-License-Identifier: MIT -->
<!-- Copyright (c) 2026 eshu-hq -->

# Removal-tail PREP: in-tree consumer-drain checklist (#9669)

Refs #9669, part of #6707. Status: preparation only — no cutover has
occurred, no deletions in this artifact, and every row below is TODO.
Leaf executors (Leaves B–E) drain these consumers; the removal PR deletes
only when each row proves zero remaining in-tree callers.

Source of truth for served routes stays
[READ_TRANSPORT_CENSUS.md](../../../../eshu-9669-census/go/cmd/api/READ_TRANSPORT_CENSUS.md)
(read-only reference in the sibling `eshu-9669-census` worktree) and
[CUTOVER_OWNERSHIP.md](CUTOVER_OWNERSHIP.md). Counts below were measured
in this worktree on `drive/9669-removal-tail`; re-run the listed command
before the removal PR — counts drift as sibling lanes land.

Re-prove command (whole checklist, non-test Go files importing
`go/internal/query` outside the API tree itself):

```bash
rg -ln 'internal/query"' go --glob '*.go' | rg -v '_test\.go' | rg -v '^(go/cmd/api|go/internal/query)/' | sort
```

## Consumer rows

| # | Consumer | Current import / target | Replacement path | Status |
| --- | --- | --- | --- | --- |
| 1 | `go/cmd/api` (in-tree handlers, wiring, `newRouter`/`wireAPI`) | Owns all 278 route rows across 66 families (`CUTOVER_OWNERSHIP.md`); reads via reader ports, one bounded suppression mutation | Moves to eshu-api as the native handler set; reads via versioned core contracts (`CoreReadContractsVersion = "v0"`, `go/internal/query/querycontract/api_boundary.go`); mutations via versioned admission contracts | TODO |
| 2 | `go/internal/mcp` — 8 non-test files (`transport_auth.go`, `dispatch_calls.go`, `dispatch_budget.go`, `dispatch_envelope.go`, `run_readonly.go`, `summaries.go`, `summaries_families.go`, `router/access.go`) plus 99 test files (107 total importing the exact query package; `kind_real_consumer.go` only mentions the path in a string/comment, not an import — re-measured with `rg -l 'eshu-hq/eshu/go/internal/query"' --type go internal/mcp/`) | `ResponseEnvelope`, filters, views | Adapt to versioned read contracts; MCP calls eshu-api transport (Leaf C/D lane) | TODO |
| 3 | `go/internal/cli` — 48 files (investigation fetch/packet/subject/refusal, support handler, report/servicereport, compparity exercises, docs verify, admin credential flows, localsupervisor embedded graph/host lifecycle, vulnscan localruntime) | `TruthEnvelope`, `ErrorEnvelope`, packet helpers | eshu-cli HTTP client against eshu-api; `localsupervisor` embedded stores stay core-local; doc/parity assertions re-point at the eshu-api OpenAPI surface | TODO |
| 4 | `go/cmd/reducer` — 23 files (`wiring_handlers.go`, workload/sibling/instance-retraction/dependency lookups, neo4j/retention/orphan-sweep/readiness/provenance wiring) | `GraphQuery` port and related wiring types | Import ports from `querycontract` (core-owned); drop handler imports; reducer stays core | TODO |
| 5 | `go/cmd/eshu` — 3 non-test files (`service.go`, `graph.go`, `investigation_cmd.go`) plus 8 parity/perf test files | Profiles, backends, packet render helpers; local `api`/`graph` service commands | eshu-cli HTTP client against eshu-api; local modes via core contracts | TODO |
| 6 | `go/cmd/golden-corpus-gate` — 2 files (`query.go`, plus test) | `EnvelopeMIMEType` | Contract constant from eshu-sdk/api client; gate retargets the eshu-api base URL | TODO |
| 7 | `go/cmd/capability-inventory` — 4 files (`surfaces.go`, `graph_read_probe_registry.go`, plus tests) | `OpenAPISpec`, probe registry | Fetch OpenAPI/capabilities from eshu-api; probes run against the satellite | TODO |
| 8 | Shared ask/serviceintel helpers — `go/internal/ask/engine` (26 files: `AnswerPacket`, `ResponseEnvelope`, truth classes), `go/internal/serviceintelhttp` (6 files), `go/internal/reportbundle` + `answerquality` + `answernarration` (pure packet/score shapes), `go/internal/serviceintel` (6 files) | Handler-adjacent shapes and HTTP helpers | Ask engine moves with the Ask family (Native now); pure shapes hoist to eshu-sdk or the eshu-api contract module; `serviceintelhttp` moves with ServiceIntelHTTP (Native now) | TODO |
| 9 | Auth/session/fact helpers — `go/internal/scopedtoken` (7), `oidcbearer` (5), `oidclogin` (4), `githublogin` (2), `go/internal/facts` (2), `evidencecontinuity` (3) | Auth/session helpers, provider config, doc shapes, gate triggers | Versioned auth/session contracts; identity stores stay core; fact schemas to eshu-sdk; triggers re-point at eshu-api evidence routes | TODO |
| 10 | Doc/closure references — `askwiring`, `cigates`, `cli/support` docs; `cli/admin` credential flows | `cmd/api` doc references; admin credential clients | Re-point docs at eshu-api; credential flows at admission-contract clients | TODO |
| 11 | `Dockerfile` — line 50 builds `/go-bin/eshu-api` from `./cmd/api`; line 291 `CMD ["eshu-api"]`; `HEALTHCHECK` on `:8080/healthz` | Core image ships the in-tree API binary as default command | eshu-api repo Dockerfile owns the api build; core image drops the `eshu-api` binary and retargets `CMD`/healthcheck | TODO |
| 12 | `specs/ci-pr-workflows.d/read-api-latency-gate.yml` — triggers on `go/**`, route-budget files (`testdata/benchmarks/read-api-route-budgets.txt`, `read-api-route-work-budgets.txt`), compose files | In-process latency measurement of the in-tree path | eshu-api repo owns the latency gate against a pinned core image; core keeps the contract-registry and static-contract gates | TODO |
| 13 | Live/e2e gate specs — `e2e-tests.yml`, `live-backend-tests.yml`, `live-postgres-readiness.yml`, `golden-corpus-gate.yml` (all reference `go/cmd/api/**` or `CUTOVER_OWNERSHIP.md`) | Gates exercise the in-tree binary surface | Leaf B cross-repo parity harness replays golden-corpus and replay coverage against an eshu-api deployment instead of the in-tree binary | TODO |

## Drain rule

A row drains only when its re-prove query returns empty (or names only
the replacement import) on the removal-PR base. Test-only references
(row 2's ~100 test files, row 5's parity tests) drain by retarget or
deletion in the same PR — they still block it while present. The removal
itself needs Leaves A–G merged plus a fresh arbiter review; this lane
does not authorize it.
