<!-- SPDX-License-Identifier: MIT -->
<!-- Copyright (c) 2025-2026 eshu-hq -->

# D13 lane record (#9669 removal, arbiter-authorized prep only)

Lane branch `drive/9669-d13-removal`. Ruling: AUTHORIZED-WITH-CONDITIONS for
consumer-drain and removal preparation only — no deletion, no deletion PR,
no traffic shift; merge of any removal PR needs a separate cutover-good ruling.
First deliverable: refresh the stale removal-tail narrative at this base.

## 51 GETs with 401 gate posture (enumerated live 2026-11-07)

Correction to the flip record's "fallthrough" framing: the 401 is gate
posture, not unmounted-path proof — all 51 are mounted in-tree
(`mounted-by=newRouter`) and relay-recognized satellite-side, with no
satellite-native termination path for 49. Disposition: SAT 2, MCP 37
(dispatch-to-survivor on the MCP edge, own removal #9670), CORE 11,
RET 1 (`GET /api/v0/index-status`, alias of canonical
`GET /api/v0/status/index` — repoint the sole alias consumer, the
`get_index_status` tool, to canonical, then retire). GAP=star on 49
(openapi.json and auth/sessions excluded); every star needs satellite
closure or carve-out before the removal PR opens. Per-route evidence
in the D13 disposition report; headline per family below.

## Drain blockers (recorded, not worked around)

- Row 7 (`go/cmd/capability-inventory`): probe-registry path drained
  (independent review PASS, non-blocking P2s; tests green on re-run).
  Package-level drain incomplete by design: the offline inventory still
  reads the `query/openapi` leaf seam until eshu-api#65 lands a served
  spec document. No PR until #65 closes; the branch holds the progress.
- eshu-api#66 CLOSED by satellite #67 (merged): flat-with-embedded-truth
  recorded as the edge contract on relayed lanes, pinned by byte-identity
  tests, independent review clean. One arbiter pre-removal condition cleared.
- eshu-mcp#60 CLOSED by #62 (merged): unknown-tool path emits the
  canonical code + suggestions, fail-closed, independently reviewed
  clean, full CI green. Second arbiter pre-removal condition cleared.
- Dispatch-host analysis (lane report, needs arbiter confirmation
  before any deletion commit): surviving host is the core-owned
  Postgres-backed admission/read-contract implementations behind
  versioned ports (`querycontract.CoreReadContractsVersion = "v0"` +
  per-family admission contracts) — not an HTTP binary; the
  satellite owns HTTP shape only, calling those contracts natively.
  Relay-forever disqualified (arbiter §4 + two-deploys-one-truth).
  Requires: wiring the unwired seams (D3 identity/recovery, D6
  Status+SupplyChain audit-read pin), resolving TBD5/TBD6, satellite
  natively terminating reads per the transport rules.
- Row 5 (`go/cmd/eshu`) DRAINED and merged (PR #10348 at 466eebde):
  service/graph/investigation off in-tree query imports onto
  querycontract ports + CLI-owned render seam, focused contract tests,
  independent review clean, full CI green. First checklist row closed.
- Row 6 (`go/cmd/golden-corpus-gate/query.go`): blocked — no
  `eshu-sdk/api` module exists and no cached SDK module defines
  `EnvelopeMIMEType`; the value lives only in-repo
  (`querycontract/http.go:15`). Unblock path: publish the constant
  from an SDK module (e.g. `sdk/go/apicontract`), then switch the
  import with a pin test. Row stays TODO until then.

### APIRouter
- `GET /api/v0/openapi.json`
### Admin
- `GET /api/v0/admin/delta-union/completeness`
- `GET /api/v0/admin/reindex/progress`
- `GET /api/v0/admin/shared-projection/tuning-report`
### CloudInventory
- `GET /api/v0/cloud/inventory`
### CloudRuntimeDrift
- `GET /api/v0/investigations/drift/packet`
### CodeownersOwnership
- `GET /api/v0/codeowners/ownership`
### ComponentExtensions
- `GET /api/v0/component-extensions`
- `GET /api/v0/component-extensions/{component_id}/diagnostics`
### Dependencies
- `GET /api/v0/dependencies`
### Entities
- `GET /api/v0/entities/{entity_id}/context`
- `GET /api/v0/investigations/services/{service_name}`
- `GET /api/v0/services/{service_name}/context`
- `GET /api/v0/services/{service_name}/story`
- `GET /api/v0/workloads/{workload_id}/context`
- `GET /api/v0/workloads/{workload_id}/story`
### ExtractionReadiness
- `GET /api/v0/collector-extraction-readiness`
- `GET /api/v0/collector-extraction-readiness/{family}`
### FactSchemaVersions
- `GET /api/v0/fact-schema-versions`
- `GET /api/v0/fact-schema-versions/{fact_kind}`
### Freshness
- `GET /api/v0/freshness/changed-since`
- `GET /api/v0/freshness/generations`
- `GET /api/v0/freshness/services/changed-since`
### GraphEntityInventory
- `GET /api/v0/graph/entities`
### IaC
- `GET /api/v0/iac/resources`
- `GET /api/v0/replatforming/selectors`
### Images
- `GET /api/v0/images`
### Incident
- `GET /api/v0/incidents/{incident_id}/context`
### Kubernetes
- `GET /api/v0/kubernetes/correlations`
### Metrics
- `GET /api/v0/metrics/timeseries`
### ObservabilityCoverage
- `GET /api/v0/observability/coverage/correlations`
### SSMHostInventory
- `GET /api/v0/cloud/ssm-host-inventory`
### SecretsIAM
- `GET /api/v0/secrets-iam/identity-trust-chains`
- `GET /api/v0/secrets-iam/posture-gaps`
- `GET /api/v0/secrets-iam/posture-summary`
- `GET /api/v0/secrets-iam/privilege-posture-observations`
- `GET /api/v0/secrets-iam/secret-access-paths`
### SemanticEvidence
- `GET /api/v0/semantic/code-hints`
- `GET /api/v0/semantic/documentation-observations`
### ServiceCatalog
- `GET /api/v0/service-catalog/correlations`
### SessionList
- `GET /api/v0/auth/sessions`
### Status
- `GET /api/v0/index-status`
- `GET /api/v0/status/answer-narration`
- `GET /api/v0/status/governance`
- `GET /api/v0/status/hosted-readiness`
- `GET /api/v0/status/index`
- `GET /api/v0/status/operations`
### SupplyChain
- `GET /api/v0/investigations/supply-chain/impact/packet`
### SurfaceInventory
- `GET /api/v0/surface-inventory`
### TagHistory
- `GET /api/v0/images/tag-history`
### WorkItems
- `GET /api/v0/work-items/evidence`

## Rebase prep note (2026-11-08, base `d59650c757`)

Branch rebased onto fresh `origin/main` `d59650c757` (prior base
`f528b8ac75`; clean rebase, zero conflicts). Prep only — no PR opened,
no merge, no traffic action. Owner acceptance of the eventual removal is
recorded; cutover-good ruling and row-7 closure still gate the merge,
and a fresh arbiter review in-lane is required before any removal PR.

- Removal scope still exactly the in-tree API surface: `go/cmd/api` is
  125 tracked files at the old base, the new base, and the rebased head
  — zero drift (`git ls-files 'go/cmd/api' | wc -l`; old-to-new diff on
  the path is empty).
- Removal proof re-run at the rebased head (all exit 0): the re-prove
  command returns 3 files (was 88) — only the known string-mention
  false positives (`facts/docs/documentation.go`,
  `facts/semantic/semantic.go`, `mcp/kind_real_consumer.go`); the MCP
  exact-package census is 99 (was 107: 8 non-test drained to 0, 99 test
  remain); `CUTOVER_OWNERSHIP.md` byte-identical (278 route data rows);
  the 13 `TestQueryRootDrained` pin-test packages pass
  (`go test -count=1 -run 'TestQueryRootDrained|TestDrain…'`
  across `answernarration`, `answerquality`, `ask/engine`,
  `evidencecontinuity`, `facts/docs`, `facts/semantic`, `mcp`,
  `oidcbearer`, `oidclogin`, `reportbundle`, `scopedtoken`,
  `serviceintel`, `serviceintelhttp`).
- Drain PRs landed on main since the prior checklist base: #10348 (row
  5), #10355 + #10359 (row 4), #10362 (auth), #10363 (capinv row 7),
  #10364 (ask), #10365 + #10372 (cli row 3), #10366 (golden-corpus row
  6), #10378 (row-9 remainder), #10381 (row 2 MCP), #10382 (row 8
  remainder), #10387 (auth tests), #10392 (row 2b). Per-row counts in
  `go/cmd/api/REMOVAL_CONSUMER_DRAIN_CHECKLIST.md` refreshed for this
  base; every Go row stays TODO (test-file references still block per
  the drain rule).
- Readiness: ready for in-lane arbiter review. NOT merge-ready:
  cutover-good ruling, row-7 closure (offline inventory still reads the
  `query/openapi` leaf seam until eshu-api#65), and the fresh arbiter
  review itself are outstanding, and the test-file retargets plus the
  51-route satellite closures/carve-outs still block the removal PR.

## Rebase prep note (2026-11-08, base `d7de38f49a`)

Branch rebased onto fresh `origin/main` `d7de38f49a` (prior base
`d59650c757`; clean rebase, 9/9 commits, zero conflicts). Prep only —
no PR opened, no merge, no traffic action. Owner acceptance of the
eventual removal is recorded; cutover-good ruling and row-7 closure
still gate the merge, and a fresh arbiter review in-lane is required
before any removal PR.

- Removal scope still exactly the in-tree API surface: `go/cmd/api` is
  125 tracked files at the old base, the new base, and the rebased head
  — zero drift (`git ls-files 'go/cmd/api' | wc -l`; old-to-new
  name-status diff on the path is empty).
- Removal proof re-run at the rebased head (all exit 0): the re-prove
  command returns the same 3 files (was 3 — identical set) — only the
  known string-mention false positives (`facts/docs/documentation.go`,
  `facts/semantic/semantic.go`, `mcp/kind_real_consumer.go`); total
  root-package references outside `go/cmd/api` + `go/internal/query`
  still 174 (171 test + 3 FP); the MCP exact-package census is 99
  (0 non-test, unchanged); `CUTOVER_OWNERSHIP.md` byte-identical across
  old base, new base, and head (md5 `9f5514a7`, 364 lines); the 13
  `TestQueryRootDrained` pin-test packages pass (`go test -count=1
  -run 'TestQueryRootDrained|TestDrain'` across `answernarration`,
  `answerquality`, `ask/engine`, `evidencecontinuity`, `facts/docs`,
  `facts/semantic`, `mcp`, `oidcbearer`, `oidclogin`, `reportbundle`,
  `scopedtoken`, `serviceintel`, `serviceintelhttp`).
- New-caller scan (killer risk) clean: non-test query-root reference set
  identical at old base, new base, and head (same 3 FP files via
  `git grep`; lane adds no `.go` files); `go/cmd/api` importers outside
  itself are the same 3 test files, all unchanged since the old base
  (`naming-glue-gate/candidates_test.go`, `cli/support/closure_test.go`,
  `workflow/claim_truth_scope_test.go`). No new in-tree callers of the
  removal surface landed on main.
- Merges landed on main since the prior base: #10398, #10402, #10403,
  #10404, #10405, #10406, #10412, #10413, #10414, #10420 — none drains a
  checklist row. Seam progress only: #10412 mints D3 admin-read pins v0
  (Gap 02) plus `StatusExtraReadAdmission` v0. Per-row checklist figures
  re-verified identical; every Go row stays TODO.
- Readiness: ready for in-lane arbiter review. NOT merge-ready:
  cutover-good ruling, row-7 closure (offline inventory still reads the
  `query/openapi` leaf seam until eshu-api#65), and the fresh arbiter
  review itself are outstanding, and the test-file retargets plus the
  51-route satellite closures/carve-outs still block the removal PR.

## Rebase prep note (2026-11-16, base `2d4db4ac`, satellite `f4511f84`)

Branch rebased onto frozen `origin/main` `2d4db4ac` (prior base
`d7de38f49a`; clean rebase, 10/10 commits, zero conflicts). G7 refresh
at the frozen pair (satellite `f4511f84` includes Wave-1b #149, Wave-2
#150, Wave-3 #151 — all G4 holds terminated, tracker #10491 at zero
open boxes). Prep only — no PR opened, no merge, no traffic action.
Owner acceptance of the eventual removal is recorded; cutover-good
ruling and row-7 closure still gate the merge, and a fresh arbiter
review in-lane is required before any removal PR.

- Removal scope: `go/cmd/api` is 126 tracked files at the new base
  (+1 vs 125 at the old base: `writer_admission_transport_test.go`
  added; 5 files modified including the C2 leaf move
  `content_relationships*.go` and the downstream-consumers cutover-map
  registration). No scope shrink: every row stays TODO.
- `CUTOVER_OWNERSHIP.md` changed (+6/-2 lines, md5 `0cd14227`,
  368 lines vs `9f5514a7`/364): SignInPolicy row version-tagged,
  manifest FactStore read-only snapshot note added, and 3 read rows
  registered (manifest-dependencies, api-surface/endpoints,
  downstream-consumers). All reads, no mutations.
- Removal proof re-run at the rebased head (all exit 0): the 13
  `TestQueryRootDrained` pin-test packages pass (`go test -count=1
  -run 'TestQueryRootDrained|TestDrain'`); MCP exact-package census
  holds at 0 non-test offenders (TestQueryRootDrained green);
  `CUTOVER_OWNERSHIP.md` values recorded above.
- New-caller scan (killer risk) clean: non-test query-root importers
  outside `go/cmd/api` + `go/internal/query` are byte-identical
  (empty set) at old base, new base, and head; `go/cmd/api`
  importers outside itself are unchanged (single test file,
  `cli/support/closure_test.go`). No new in-tree callers of the
  removal surface landed on main.
- Merges landed on main since the prior base include the G4 finals
  (#10745 C2 leaf, #10779 C2 follow-up, #10747 C5 27->30,
  #10748 C4 no-port, #10749+ mac-seed admissions, #10754 design-B,
  #10775 product severity, #10778 doc citations, #10789 continuation
  verdict) — none drains a checklist row; seams only.
- Readiness: ready for in-lane arbiter review (G9 input). NOT
  merge-ready: cutover-good ruling, row-7 closure, and the fresh
  arbiter review itself are outstanding; test-file retargets are
  unchanged.

## Rebase prep note (2026-11-16, base `e2496962df`, satellite `a3ca83a`)

Branch rebased onto frozen `origin/main` `e2496962df` (prior base
`2d4db4ac`; clean rebase, 11/11 commits, zero conflicts; tree clean at
the rebased head). Satellite `a3ca83a` includes the G2 wave #152 — the
7 MUST-TERMINATE rows terminate natively and G2 passes at the pair
(282/236/27/19/0, checker exit 0). Prep only — no PR opened, no merge,
no traffic action. Owner acceptance of the eventual removal is
recorded; cutover-good ruling and row-7 closure still gate the merge,
and a fresh arbiter review in-lane is required before any removal PR.

- Removal scope: `go/cmd/api` is 126 tracked files at the new base
  (unchanged count, zero drift).
- `CUTOVER_OWNERSHIP.md` unchanged (md5 `0cd14227`, 368 lines, 282
  rows): no rows added or removed at the new base.
- Removal proof re-run at the rebased head (all exit 0): the 13
  `TestQueryRootDrained` pin-test packages pass (`go test -count=1
  -run 'TestQueryRootDrained|TestDrain'`); MCP exact-package census
  holds at 0 non-test offenders (TestQueryRootDrained green).
- New-caller scan (killer risk) clean: non-test query-root importers
  outside `go/cmd/api` + `go/internal/query` are the empty set at old
  base, new base, and head; `go/cmd/api` importers outside itself are
  unchanged (single test file, `cli/support/closure_test.go`). No new
  in-tree callers of the removal surface landed on main.
- Merges landed on main since the prior base (#10795 response-digest
  cassette, #10797 NVD baseline pins, #10796 G2 readmission pins, #10799
  trip-latch durable) — none drains a checklist row; the 5 G2 pins are
  new unwired seams only.
- Readiness: G9 returned NO-GO at this pair (recorded on
  eshu-hq/eshu-api#101): the lane record itself still bars the removal
  PR — row-7 drain (pending eshu-api#65), row-6 SDK module, TBD5/TBD6
  + unwired seams, 51-route satellite closures/carve-outs, test-file
  retargets, cutover-good ruling, and fresh in-lane review all still
  outstanding. NOT merge-ready.
