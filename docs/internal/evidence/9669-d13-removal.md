# #9669 D13 in-tree API relay-surface removal

Base: `origin/main` at `0b45d549bf` (newer than the Owner-GO triple
core `f9cfd99c` / sat `c041d7e` / sdk `bc350d35`; the removal surface
is byte-identical at all three SHAs). This record is the tracked
performance/no-regression/observability note for the mechanical
removal of the superseded in-tree API relay after the satellite
takeover. Satellite `eshu-hq/eshu-api` at `c041d7e` serves the surface
(G2: 282 rows, 236 native, 27 tier-C, 19 relay-disclosed survivors,
0 absent, 0 nonsurvivor; checker exit 0, mirror 12/12). Rollback is
`git revert` of the merge (Shape-3; revert restores the tree
byte-for-byte). Refs eshu-hq/eshu#9669, part of eshu-hq/eshu#6707.
Merge is NOT authorized here: green CI, resolved threads, owner
acceptance, and a fresh cutover-good ruling still gate it.

## Scope

Deletes the in-tree relay tree (126 tracked files: handlers, wiring,
admission ports, route gates, OpenAPI fragment assembly, tests, and
the removal-tail prep docs that lived beside the code) and repoints
every in-tree consumer. Core-owned kernels stay: `go/internal/query`
pins and read models, auth/session helpers, and the satellite-facing
contracts. No termination wave runs against auth/sessions or any of
the other 21 D8 dispatch routes (declared core-owned survivors).

## Census honesty note

Sorted-list digest of the deletion set is
`903ec84a8ed7831ebf703948dd2e41bb74389f88e61ff3dd14babcd37edaaa9a`,
byte-identical to the G8 Shape-3 proven set, so the dry-run
synthetic-removal plus clean-revert proof carries forward. Post-removal
the non-test query-root importer set is the same 3 string-mention
false positives as before, and no file outside the deleted tree
imports the removed packages (the two test files that did are
retargeted in the same PR).

## No-Regression Evidence:

Baseline: `origin/main 0b45d549bf` (go 1.26.11, credential-free).
After: this head. `go build ./...` exit 0. Full affected packages
green: the 13 `TestQueryRootDrained` pin packages, `cli/support`,
`envregistry`, `workflow`, `cigates` (full suite), `telemetry`,
`read-api-latency-gate` (minus one pre-existing environment failure
proven identical on the pre-removal tree: the sibling deploy checkout
sits at a different SHA than the test pins). Comment-only changes in
the hot files (`tag_history.go`, `identity_local_types.go`, and the
admission-pin wiring comments) touch no logic; `instruments.go` drops
only the five registrations nothing references (verified by the
telemetry gate). The capability inventory regenerates cleanly (the
retired `api` command entry drops; 646 surfaces). No new Cypher,
queue, lease, batching, or concurrency knobs. No live backend
involved; Docker/credentialed gates stay CI-owned.

## Observability Evidence:

Retired `eshu_dp_shutdown_duration_seconds`,
`eshu_dp_auth_bootstrap_seed_total`,
`eshu_dp_auth_bootstrap_credential_generated_total`,
`eshu_dp_auth_secret_seal_total`, and `eshu_dp_auth_secret_open_total`
from the in-tree registry: their only references were the deleted
relay code, confirmed by `verify-telemetry-coverage` (green after
removal) and by zero hits in dashboards, docs rows, and Go callers.
The three relay-only coverage-doc rows and the orphaned histogram
bucket set go with them. `verify-moved-file-refs` passes (115 vacated
paths, no dangling references, `go-cmd-api` allowlist fragment for the
retired-component references). The satellite owns its own registry
for the HTTP surface it now serves; satellite metric/span/log names
are Leaf E follow-up.

## Provenance appendix (coordinator re-runs on the D13 head)

All commands run in the `drive/9669-d13-removal` worktree at the
pushed head, Go 1.26.11, `GOTMPDIR=~/tmp/gotmp-9669`, exit codes as
shown:
- `bash scripts/test-capability-stub-satellite.sh` → exit 0,
  `stub-satellite harness: 11 passed, 0 failed`.
- `cd go && GOWORK=off go test -count=1
  ./cmd/capability-inventory/` → exit 0,
  `ok github.com/eshu-hq/eshu/go/cmd/capability-inventory`.
- `GOWORK=off go vet ./cmd/capability-inventory/` → exit 0, no
  findings. `gofmt -l` on the touched Go files → empty.
- `bash scripts/test-verify-golden-corpus-gate.sh` and
  `bash scripts/test-stage-differential-capture.sh` → exit 0
  (`test-verify-golden-corpus-gate: pass`; `differential capture
  staging is complete, isolated and fail-closed`).
- `bash -n` + `shellcheck -S warning` on the touched shell files →
  clean (one pre-existing SC2154 note class shared with the file's
  older lines: sourced-lib variables).
- Census recounts on this head: `go/cmd/api` tracked files 0 (was
  126); non-test query-root importers outside `go/cmd/api` +
  `go/internal/query` empty; `go/cmd/api` importers outside itself
  unchanged (single test file); zero remaining Go references to the
  five retired telemetry instruments.

## CI repair round, 2026-11-18 (post-rebase to origin/main 87b501a1d)

Four lane-caused failures fixed and locally re-proven (pushed):

- Registry N1 (`verify-ci-gate-registry`): the D13 lane deleted
  `.github/workflows/read-api-latency-gate.yml` but left its name in
  the `main-health.yml` watcher trigger list, which is also the judged
  set. Removed the orphan entry; `test-main-health.sh` 165/165 pass.
- Docs-helm (`test-context-stories-doc-split`): the lane's http-api.md
  retirement notice pushed the file to 455 lines (cap 450). Folded the
  correction into the paragraph in place (450/450, gate passes).
- verify-contracts (`test-verify-golden-corpus-filter-exhaustive`
  case `case_real_state_passes`): the verifier hardcoded the deleted
  `api` binary (`go list -deps ./cmd/api/...` fails on a deleted
  tree). Removed `api` from the binary set following the #9670
  mcp-server precedent; mirror 10/10 pass.
- Docker Hub login gate (`scripts/ci/test-image-overrides.sh`):
  mikefarah-yq `open ... read-api-latency-gate.yml: no such file`
  from the same deleted workflow lingering in the pin-check loop
  list. Dropped from the list; script exit 0.

Pre-existing main breakage (NOT lane-caused, proven against main's own
runs): the #10904/#10907-era postgres test-package compile error
(`supply_chain_impact_winners_keyed_explain_export_test.go:90`,
3-value call vs 4-value helper) failed merge-group-slim, go-race,
bench functional cases, and the live-postgres/corpus legs on the PR's
merge runs AND on main runs 10437/10438 with the identical signature.
Fix #10908 is in the new base; the rebased tree compiles clean
(`go test --no-run` over `./internal/storage/postgres/` ok).
Bench `test-run-go-benchmarks` fails byte-identically on main
(de1a7bb2, no D13 content), so any residual bench red is main's to
own, not this lane's.

Still open (needs owner satellite coordinates): e2e `go build
./cmd/api` + `/healthz`+`/readyz` health leg, corpus `build_bin api`
+ `/readyz` probe, Dockerfile builder `./cmd/api` line (row 11),
frontend SSO/MCP image builds. Ground facts: satellite `cmd/eshu-api`
serves :8080 with `/readyz` (503 dead-core / 200 live-core, no
`/healthz`) and REQUIRES `--core-base-url`; no satellite release
carries the full surface (latest v0.5.0, 2026-11-09, SBOM asset
only). Retarget design (retire legs vs satellite+core in CI) is an
owner call; the P0 thread already holds the digest question.

## Satellite swap round, 2026-11-18 (owner "do it" on the retire-vs-wire question)

Builds now use the new code: satellite source at the frozen G9 triple
SHA c041d7ec95e41cba1c9874c8dc098af6a2f994f8 (no satellite release
carries the full surface yet; latest v0.5.0 ships only an SBOM).
Satellite module is stdlib-only (zero cgo), verified
`go build ./cmd/eshu-api` clean at the pin. Grounded interface facts
(probed against the real pinned binary): no `--core-base-url` flag
exits nonzero; coreless `/readyz` is 503 with zero Retry-After
headers; `/healthz` does not exist (404); liveness lives at `GET
/health` (200, credential-free).

- `Dockerfile`: builder loop drops `./cmd/api`; new satellite-source
  RUN (SHA-pinned ARG) writes `/go-bin/eshu-api`. Production
  HEALTHCHECK moves from `/healthz` (gone) to serving-liveness on
  `/readyz` (any HTTP answer). Row-11 release-digest COPY stays the
  serving-owner follow-up.
- `e2e-tests.yml`: build step clones + builds the satellite; "Verify
  API health" now asserts the live fail-closed contract (`/health`
  200, `/readyz` 503, no Retry-After) instead of the deleted 200s.
- Corpus `build_bin api` builds the satellite via
  `build_satellite_api_bin` (#9670 mcp pattern, SHA form).
- Corpus serve legs narrowed with cutover-lane pointers: satellite
  starts with a dead core + 503-assert; B-7 suppression proof, query +
  demo-answers phases, and api/mcp base-URL flags leave (no live
  query-truth core exists in this gate); graph/timing/delta phases,
  drains, mocks, snapshots, and both MCP start flags stay. Snapshot
  chain re-heads on the static snapshot. Mirror updates: phase-timing
  exclusion checks, pipeline phase/region pins, delta-reader phase
  string, suppression execution pins (all green locally).
- Frontend SSO/MCP runners skip full-suite runs with a loud message
  (`--module` runs proceed to fail honestly; catalog-sweep policy
  failure ordering preserved, `test-ci-image-compose` green).
- Pre-existing breakage untouched: bench functional cases fail
  identically on main; postgres compile fixed by #10908 in the base.

## Provenance appendix (answers the evidence-overclaim P2s)

Every green claim above was measured, not asserted. Environment for all
local runs: repo-pinned Go 1.26.11 first on PATH
(`~/.local/share/mise/installs/go/1.26.11/bin`, `go version` confirms
`go1.26.11`), `GOROOT`/`GOBIN` unset, `GOTMPDIR=~/tmp/gotmp-9669`,
2026-11-18. Exact command, tree, and exit code per claim:

- Registry N1: `bash scripts/test-main-health.sh` → `test-main-health:
  165 passed, 0 failed`, exit 0. Tree: worktree rebased to
  origin/main 87b501a1d (commit 3500100afd) plus the uncommitted
  watcher-entry removal and http-api fold (committed as 858277a06f).
- Docs-helm: `bash scripts/test-context-stories-doc-split.sh` →
  `all checks passed`, exit 0, `wc -l` 450/450 on the same tree.
- verify-contracts filter: `bash
  scripts/test-verify-golden-corpus-filter-exhaustive.sh` → `10/10
  cases pass`, exit 0, after the `api` binary-set edit (committed as
  858277a06f).
- Docker Hub login gate: `bash scripts/ci/test-image-overrides.sh`
  → exit 0, after dropping the deleted workflow from the loop list
  (committed as 09865c4167).
- Merge-group predictors on head e2fb85ba (clean worktree except this
  docs-only appendix): `go build ./...` (go/ module) exit 0;
  `test-verify-collector-entrypoints-generated.sh` + verifier both
  pass; `dev/precommit-go.sh filecap-all` exit 0;
  `test-verify-ci-gates-registry.sh` PASS/PASS.
- Corpus gate mirror: `bash scripts/test-verify-golden-corpus-gate.sh`
  → `test-verify-golden-corpus-gate: pass`, exit 0, on e2fb85ba
  (after the Layer-2 phase/flag/probe edits, before this appendix).
- Satellite source build (proves the Layer-1 pin): `git init` +
  `remote add https://github.com/eshu-hq/eshu-api.git` + `fetch
  --depth 1 origin c041d7ec95e41cba1c9874c8dc098af6a2f994f8` +
  `checkout -q FETCH_HEAD` → `rev-parse HEAD` echoes the same SHA;
  `CGO_ENABLED=0 go build -trimpath -o ... ./cmd/eshu-api` exit 0;
  `--help` lists `-addr` (default `:8080`) and required
  `-core-base-url`.
- Satellite boot contracts (probed against the binary above):
  no-flag run exits nonzero with `eshu-api: --core-base-url is
  required`; with `--core-base-url=http://127.0.0.1:9`,
  `GET /readyz` → 503 with zero `retry-after` headers,
  `GET /healthz` → 404.
- Frontend/auth mirrors on e2fb85ba: `test-auth-e2e-cli.sh` PASS;
  `test-ci-image-compose.sh` PASS; `test-verify-apk-floors.sh`
  52 passed, 0 failed; `test-verify-docs-only-ci-skip.sh` 23/23 and
  docs-set 7/7; `generate-ci-gates-doc.sh` writes 162 gates,
  byte-identical, `test-generate-ci-gates-doc.sh` 31/31.
- Registry drift P2 (ci-gates.md thread): the red mirror the review
  saw was the N1 orphan above (judged name vs deleted workflow file);
  the watcher-entry removal fixes the judged set, the registry test
  passes locally (PASS/PASS), and the regen output is byte-identical,
  so no drift was baked in. Final proof is the fresh CI registry job.

## Left for the merge gate and the owners

`Dockerfile` row 11 (satellite-binary swap) waits on the owner-provided
digest coordinates (D11 C4); the in-tree build line stays until then.
Public architecture/service/runtime docs still describe the in-tree
surface (REMOVAL_PR_DOCS_LIST items 2-5 and 7); repointing them to the
satellite is serving-owner follow-up with satellite evidence. The
retired in-tree latency gate family moves to the satellite per
checklist row 12. None of this blocks review of the removal itself.
