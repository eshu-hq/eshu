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

## Left for the merge gate and the owners

`Dockerfile` row 11 (satellite-binary swap) waits on the owner-provided
digest coordinates (D11 C4); the in-tree build line stays until then.
Public architecture/service/runtime docs still describe the in-tree
surface (REMOVAL_PR_DOCS_LIST items 2-5 and 7); repointing them to the
satellite is serving-owner follow-up with satellite evidence. The
retired in-tree latency gate family moves to the satellite per
checklist row 12. None of this blocks review of the removal itself.
