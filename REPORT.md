# Security Audit Report — Eshu (`eshu-hq/eshu`)

## 1. Run parameters

- **Profile**: `standard` (full six-phase workflow; candidate validation and final record verification kept as separate fresh agents).
- **Scope**: whole repository (`.`), target `/home/eblin/code/eshu`.
- **Source ref**: commit `5766b3592d7975860efcc2b01078b51378bb6ace`, branch `main`.
- **Budget**: not set (`null`). Agents spent versus planned: reconnaissance 4 (4 reserved); hunting waves 1–6: 13 hunter assignments; critics: 4 post-wave + 1 separately reserved final-clean critic; Phase 3: 12 verifiers; Phase 5: 11 record verifiers. No budget stop was triggered.
- **Execution policy**: `sandboxed-source-and-local-only` — static source reading plus bounded local execution (Go tests and scratch programs under `GOTOOLCHAIN=local ~/.local/go/bin/go`, go1.27.3), no external network, scratch-only writes for target-controlled processes, parent-owned promoted artifacts.
- **Prior runs**: none existed; this is the first ledger for this repository, so no carried confirmations, no changed-source revalidations, and no prior states suppressed current work.
- **Deferred / out-of-scope**: no ledger unit ended `deferred`, `blocked`, or `out_of_scope`. Exclusions recorded per unit (see ledger `excluded_blocks`): wildcard attack class (reserved for critic gaps), memory-safety/binary class (stdlib parsers only), DOM client-side rendering (build-time React SPA), Kubernetes/IAM topology and compose/Helm/remote-e2e legs (sibling `eshu-deploy` repo; the `ESHU_DEPLOY_CHECKOUT`-gated contract test stays environmental), native/mobile IPC. This is the complete standard-profile pass for this source ref; one pass still does not exhaust the target.

## 2. Security posture summary

The reviewed plane is small and deliberately disciplined: one HTTP auth gate for the API (`go/internal/query/auth.go:162-295`), a single FOR UPDATE SKIP LOCKED claim loop with a fenced lease/heartbeat, fail-closed env loaders on the pipeline mains, and capped sibling decoders. Eleven source-grounded defects survived independent verification — no critical or high items. The two most consequential are a medium auth-posture gap (headerless requests served open under identity-only wiring) and a medium admission gap (the browser-session context switch rebinds by session hash without a subject/tenant membership predicate). The rest are LOW: missing `ReadTimeout`/byte caps on three request planes, two `.env` parser gaps, an inert first-`LIMIT` cap, an unapplied handler timeout, an unbounded graph-read transaction, and a non-injective queue id. No serialization workaround hid any result; every record was reproduced from source or a bounded local run.

## 3. Confirmed findings

| Severity | Title | Affected boundary | Observed result (one line) |
|---|---|---|---|
| medium | `eshu.query.auth.headerless-open-with-db-only-credentials` — headerless API requests served open under DB-only credentials | `go/cmd/api/auth_enforcement.go:42-48` → `go/internal/query/auth.go:187-190` → `querycontract/repository_authz_context.go:26-27` | Identity-only wiring answers headerless GETs with 200 and `AllScopes:true` unfiltered reads; tokened requests still scope correctly. |
| medium | `eshu.query.browser-session.context-switch-unbound-tenant` — session workspace switch rebinds without a subject/tenant predicate | `go/internal/query/browser_session_handler.go:264-284` → `browser_sessions_schema.go:369-410` | Single UPDATE binds only on `session_hash`; the post-switch anchor can diverge from the row's own membership. |
| medium | `reducer.bootstrap.handler-timeout-not-enforced` — `ESHU_REDUCER_HANDLER_TIMEOUT` loaded but never enforced | `go/cmd/reducer/main.go:218-232` → `service_heartbeat.go:245/:315` | `executeHandler` selects only on done/shutdown; each stuck handler pins its worker, connection, and fenced conflict key until shutdown. |
| low | `api.http-server.no-readtimeout-body-phase-unbounded` — API server lacks `ReadTimeout` | `go/cmd/api/main.go:154-163` | Stalled body reads have no deadline (4s/8s/12s completions vs 3s reap under a bounded control). |
| low | `cli-config.env.load-ignores-scanner-error` — `.env` Load drops `scanner.Err()` | `go/internal/cli/config/env.go:69-81` | A >64 KiB token truncates the parse silently; trailing keys lost on rewrite. |
| low | `cli-config.env.unlocked-whole-file-rewrite` — non-atomic whole-file `SetValue` | `go/internal/cli/config/env.go:141-154` | Two overlapping writers: last writer wins, first writer's keys lost. |
| low | `codequery.bounded-readonly-cypher.first-limit-not-terminal` — first-`LIMIT` acceptance suppresses the terminal cap | `go/internal/query/codequery/cypher.go:297` | Inner-`LIMIT` statements reach the driver byte-identical with no row cap; visualization twin is capped. |
| low | `mcp-jsonrpc-body-no-maxbytesreader` — MCP JSON-RPC decoders uncapped | `go/internal/mcp/server.go:259-306`, `server_sse.go:240`, `querycontract/http.go:105` | 10 MiB body retained in full (200, no 413); sibling intake caps at 1 MiB with 413. |
| low | `reducer.bootstrap.graph-read-no-tx-timeout` — read facet `Run` omits the transaction timeout siblings apply | `go/cmd/reducer/neo4j_wiring.go:181-211` | A wedged read pins worker+connection with no client deadline; heartbeat keeps the lease fresh. |
| low | `reducer.queue.work-item-id-part-boundary-collapse` — `reducerWorkItemID` not prefix-free | `go/internal/storage/postgres/reducer_queue_helpers.go:344-362` | Distinct tuples collapse onto one id; `ON CONFLICT DO NOTHING` silently skips the second intent. |
| low | `runtime.admin.recovery-body-no-maxbytesreader` — recovery admin decodes uncapped while the sibling caps at 64 KiB | `go/internal/runtime/recovery_handler.go:87/:146/:220` | 4.4 MB refinalize body → ~17.7 MB heap delta vs ~0.26 MB capped; `Limit==0` scans the whole queue table. |

## 4. Finding detail

### `eshu.query.auth.headerless-open-with-db-only-credentials` (medium)
- **Source**: predicate `go/cmd/api/auth_enforcement.go:42-48`; identity resolver wired at `go/cmd/api/wiring.go:155`, first in chain at `:211`; fall-through `go/internal/query/auth.go:187-190`; sink `go/internal/query/querycontract/repository_authz_context.go:26-27`.
- **Principal**: any client on the API listener (no credential) in an identity-only deployment.
- **Reproduction**: `go test ./cmd/api -run "AuthEnforcement|Headerless" -count=1` and `go test ./internal/query -run "Headerless|EnforcementConstructor" -count=1`.
- **Conditions**: identity-store-only wiring; request without an `Authorization` header.
- **Result**: 5 + 7 PASS, exit 0; headerless 200 with no `AuthContext`, matching token 200, wrong token 401.
- **Impact**: unfiltered data-plane reads in that supported minimal configuration; per-request, scoped requests keep their filter.
- **Priority**: medium — a real boundary violation, narrow configuration set.
- **Smallest fix**: count the identity resolver in the predicate when `identity_token_metadata` holds rows (keeps `TestAPIAuthDemoConfigServesHeaderlessOpen` green on the empty stub).

### `eshu.query.browser-session.context-switch-unbound-tenant` (medium)
- **Source**: `go/internal/query/browser_session_handler.go:264-284` → `go/internal/storage/postgres/browser_sessions_schema.go:369-410`; in-tree residual `go/internal/query/auth_browser_session_route_policy.go:297-304` (#6450 item 4).
- **Principal**: an authenticated browser-session principal (session id + CSRF token).
- **Reproduction**: `go test ./internal/storage/postgres/ -run TestBrowserSessionStoreSwitchesActiveWorkspaceByHash -count=1` (PASS, exit 0; recording fake pins the session-hash-only UPDATE).
- **Conditions**: valid session pair; target pair active/non-tombstoned in the catalog.
- **Impact**: post-switch anchor for identity-bound reads can diverge from the row's own membership.
- **Smallest fix**: add `sess.tenant_id = $2` plus an `EXISTS` against `tenant_scope_grants` (`subject_class`, `tombstoned_at IS NULL`) to the switch WHERE; extend the recording-fake test.

### `reducer.bootstrap.handler-timeout-not-enforced` (medium)
- **Source**: load at `go/cmd/reducer/main.go:218-232` (WARN `:228`, stored `:455-456`); resolver `go/internal/reducer/service_heartbeat.go:245` (zero non-test callers); sink `executeHandler` select `:315` with only `done` / `shutdownCtx.Done()`.
- **Principal**: the reducer process / queue progress for the operator.
- **Reproduction**: `go test ./internal/reducer -run "TestServiceRun|TestHandlerTimeoutFor" -count=1 -v` — three named tests PASS, exit 0; rg census shows no non-test caller.
- **Impact**: per stuck handler, one worker + one pooled connection + one fenced conflict key pinned until shutdown; heartbeat keeps `claim_until` fresh so reclaim never fires.
- **Priority**: medium — structural gap, bounded blast radius.
- **Smallest fix**: third select case on `time.After(HandlerTimeoutFor(item.Domain))` using the existing abandon path.

### `api.http-server.no-readtimeout-body-phase-unbounded` (low)
- `go/cmd/api/main.go:154-163` sets header/write/idle only; stdlib `server.go:1038-1041`/`:1103` leave the body deadline zero. Harness: 4s/8s/12s completions vs 3s reap (`agents/verifier-readtimeout/artifacts/repro3.go`). Fix: add `ReadTimeout` to the literal.

### `cli-config.env.load-ignores-scanner-error` (low)
- `go/internal/cli/config/env.go:69-81` returns the partial map with nil error; sibling `go/internal/runtime/api_key.go:155` checks `scanner.Err()`. Fixture `K1=1 / LONG=<65537> / AFTER=lost` → map `{K1:1}`, rewrite loses `AFTER`. Fix: check and return `scanner.Err()`.

### `cli-config.env.unlocked-whole-file-rewrite` (low)
- `SetValue` (`env.go:141-154`) Load→`os.WriteFile` with no lock/rename; two snapshots → last writer wins. Fix: same-directory temp + `os.Rename`.

### `codequery.bounded-readonly-cypher.first-limit-not-terminal` (low)
- `explicitCypherLimit` returns at `cypher.go:297` on the first `LIMIT` without terminality; inner-`LIMIT` statements reach the driver uncapped while `boundedVisualizationCypher` appends `LIMIT 6`/`LIMIT 1001`. Fix: use `terminalCypherLimit` at `:152`.

### `mcp-jsonrpc-body-no-maxbytesreader` (low)
- Bare decoders at `go/internal/mcp/server.go:259-306`, `server_sse.go:240` via `querycontract/http.go:105`; zero `MaxBytesReader` hits under `go/internal/mcp`; 10 MiB body → 200. Fix: wrap in `http.MaxBytesReader` at the sibling's 1 MiB.

### `reducer.bootstrap.graph-read-no-tx-timeout` (low)
- `neo4j_wiring.go` `Run` `:181-211` omits the `r.transactionConfigurers()...` that `:56/:102/:168` pass; driver default `math.MinInt` sends no `tx_timeout` meta. Fix: pass the configurers into `session.Run`.

### `reducer.queue.work-item-id-part-boundary-collapse` (low)
- `reducer_queue_helpers.go:344-362` join is not prefix-free; both collapse channels reproduced (`agents/pv-workid/scratch/main.go` self-check: 8 tuples → 8 ids with the length-prefixed join). Fix: length-prefixed join + injectivity test.

### `runtime.admin.recovery-body-no-maxbytesreader` (low)
- Bare decodes `recovery_handler.go:87/:146/:220` vs sibling 64 KiB cap (`legacy_workload_retirement_handler.go:71`); listener default `0.0.0.0:8080` (`config.go:26`); `Limit==0` → no SQL LIMIT (`postgres/recovery.go:184-187`). Measured 17.7 MB vs 0.26 MB heap deltas. Fix: `http.MaxBytesReader(w, r.Body, 64<<10)` at each decode.

## 5. NEEDS VALIDATION

None. Every candidate reached an independent final disposition in Phase 3 and every retained record passed Phase 5; no unresolved lead remains, so `NEEDS-VALIDATION.md` records the empty set.

## 6. Hardening notes and positive source patterns

Hardening notes (non-blocking, retained from hunter/verifier bookkeeping; full text in `coverage-ledger.json` per unit):

- API shutdown: join `shutdownDone` unconditionally (`go/cmd/api/main.go:144-148`); clamp `apiShutdownTimeout` to positive (`config_helpers.go:44-57`).
- Status plane: `http_server.go:66` carries only `ReadHeaderTimeout`; add read/write/idle like the API plane.
- Coordinator: bound the nine startup `EnsureSchema` calls with the ping budget (`cmd/workflow-coordinator/main.go:159-206`); add `runtimecfg.ConfigureMemoryLimit` for parity with projector/ingester.
- Reducer: WARN on malformed numeric env values (`config.go:142-159, :214-241`); a malformed `ESHU_REDUCER_EXPECTED_SOURCE_LOCAL_PROJECTORS` silently disables the drain gate; unify the lease/heartbeat constant (lease/2 vs lease/3).
- Collector plane: wire `DeadLetters` in the nine claimed mains that leave it nil (parity with `cmd/collector-security-alerts/service.go:114`).
- MCP: add `http.MaxBytesReader` (~1 MiB) at the three decode sites; record the stdio embedding contract requiring a pre-wrapped query handler.
- Query plane: single identity-token hasher helper; transactionalize the two-statement stale-revoke + resolve; note the dead `scoped_api_tokens` table for the folder-doc keeper.
- Webhooks: `normalizer_circleci.go:35` branch-first fallback; `trigger_store.go:383` escaped key join; `normalizer_bitbucket.go:90` explicit zero-value state.

Positive source patterns worth preserving: one auth gate for the whole HTTP plane; constant-time shared-key compares; `__Host-` cookie discipline with `SameSite=Strict`; fenced lease quadruple on every queue statement; fail-closed env loaders naming the offending key on the pipeline mains; capped sibling decoders that the uncapped ones can mirror directly.

## 7. Coverage summary

- **Ledger totals (30 units)**: 21 `covered`, 9 `candidate` (each carrying its independently verified fingerprints — 11 unique), 0 `blocked`, 0 `deferred`, 0 `planned`/`in_progress`/`out_of_scope`.
- **Waves**: six coverage-led hunter waves under critic guidance (waves 1–6 accepted 4/1/0/3/2/0 units, 3 reassigns total; both wave-1 toolchain leads closed).
- **Final critic result**: two consecutive clean passes from two distinct critics — the post-wave-6 critic (`stop: true`, no missing units, no reassigns) and the separately reserved final-clean critic (`stop: true`, certifying artifact resolution and the 11-fingerprint ledger match). The standard-profile clean-coverage condition is met for this source ref.
- **Exclusions that matter**: the `eshu-deploy` sibling repo (compose/Helm/remote-e2e legs) and the satellite `eshu-mcp` route placement (`/.well-known/oauth-protected-resource` production mux) are outside this target; `cmd/mcp-server` has no in-repo main (removed under eshu-hq/eshu#9670) and its wiring is owned by the stdio/POST units.
- **Phase 5 disposition**: 11/11 records independently reviewed by fresh agents — 9 `verified` as written, 2 non-material replacements applied (browser-session remediation snippet against the real grant tables; `main_test.go` evidence line 44 → 21). Both validators re-run green after each edit: `validate-findings.cjs` → PASS 11 findings; `validate-coverage-ledger.cjs` → PASS 30 units.
