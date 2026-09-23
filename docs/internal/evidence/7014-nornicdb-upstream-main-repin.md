# NornicDB re-pin to plain upstream main (#7014)

Change: the default NornicDB backend pin moves from the eshu-hq self-built
`ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-500-e022384c@sha256:74a8ed7b36f37bdd1a7e32d8bc6aa3fa88908b7207bfa6568567ab94e4a4b3b1`
(upstream `6ac958a9` plus the unmerged orneryd/NornicDB#502 patch) to
`ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-6915-c4de1c5c@sha256:76dd5f9b016db047ba867b69b13e4b2dd0f7b90c2764059476821ba4ce52274a`
(amd64 child `sha256:79a171850c586fb495a2d7cc66916c200bff8d27a15ab798fdc83f15e41720fb`,
arm64 child `sha256:3a4649d64116f410d10729091b1e94e05e03e94c01339c7eb1ec12e366d5c372`).
The same 41 files move together as in the prior re-pins
([6915-nornicdb-fix500-repin.md](6915-nornicdb-fix500-repin.md),
[6162-nornicdb-fix499-repin.md](6162-nornicdb-fix499-repin.md)). Prior evidence
notes stay untouched as historical records.

## What the image is

An eshu-hq build of plain upstream orneryd/NornicDB `main` at commit
`c4de1c5c`, with no Eshu-carried patch on top. Built with
`docker buildx build --platform linux/amd64,linux/arm64 -f docker/Dockerfile.amd64-cpu`
and pushed to the public eshu-hq GHCR package `nornicdb-amd64-cpu`.

Provenance chain on `main` as of `c4de1c5c`:

- [orneryd/NornicDB#492](https://github.com/orneryd/NornicDB/pull/492) (parser
  refactor, TCK at 100%) merged 2026-09-23 14:56Z, followed by
  [#512](https://github.com/orneryd/NornicDB/pull/512) (regression fixes)
  merged 2026-09-23 16:43Z.
- Main already carried
  [#491](https://github.com/orneryd/NornicDB/pull/491) (`a427a468`, conjunct
  index-seek fix), [#498](https://github.com/orneryd/NornicDB/pull/498)
  (`3f997e04`, numID counter floor on open), and
  [#501](https://github.com/orneryd/NornicDB/pull/501) (`6ac958a9`, Close
  waits for in-flight durable writes).
- The #500 ORDER BY key fix: the regression file from the closed
  [orneryd/NornicDB#502](https://github.com/orneryd/NornicDB/pull/502), run
  against `c4de1c5c`, passes 26 of 27 subtests. #502 was closed as superseded
  by #492's parser rewrite rather than merged as its own patch.

## #502 regression-file result and the DISTINCT watch item

The one non-matching subtest is
`RETURN DISTINCT e.name AS name ORDER BY e.id` (ordering by a non-projected
key under `DISTINCT`): main at `c4de1c5c` now rejects it with
`variable e is not defined`, matching Neo4j's own behavior of refusing an
unprojected `ORDER BY` key after `DISTINCT`. On the prior `fix-500-e022384c`
pin (`main` + the standalone #502 patch) the same statement was still
accepted and silently sorted on storage order, so this is a strictly more
correct rejection, not a new divergence. A repo-wide search found every
`RETURN DISTINCT … ORDER BY` statement Eshu emits already orders on a
projected alias, so nothing in the shipped query surface hits the new
rejection; the gates below are the authoritative proof for hot-path Cypher
usage.

## Sweep

Repinned all 41 files that referenced `fix-500-e022384c` (`rg -l
fix-500-e022384c` before the sweep): `docker-compose.yaml`, Helm values
(`deploy/helm/eshu/values.yaml`,
`deploy/helm/eshu/ci/governance-two-team-k8s.values.yaml`), the replay-tier
and k8s two-team governance proof scripts and their fixtures, the Ifá
fault-injection diagnostics hard-coded image assert
(`scripts/lib/ifa_fault_injection_diagnostics.sh`), the 13 live-test
docker-run pins under `go/`, `go/internal/runtime/compose_nornicdb_image_test.go`,
and the docs enumerated in the prose-fix commit for #7014 (`docker-compose.md`,
`storage.md`, `helm-routing-and-storage-values.md`,
`docs/internal/design/430-nornicdb-graph-search-split.md`, and this note's
sibling `values.yaml` comment). All now read "eshu-hq build of plain upstream
main `c4de1c5c`", dropping every "#502 not yet merged upstream" /
"self-built ... plus the ORDER BY key fix" phrasing that described the
superseded patch stack.

## Gate results (this change)

Focused, non-live proof (all green, captured 2026-09-23):

- `cd go && env -u GOROOT go test ./internal/runtime/ -run NornicDB -count=1` — PASS.
- `bash scripts/test-verify-replay-tier.sh` — PASS.
- `bash scripts/test-verify-k8s-two-team-governance-proof.sh` — PASS.
- `bash scripts/test-k8s-two-team-governance-provenance.sh` — PASS.
- `bash scripts/test-verify-ifa-fault-injection.sh` (the harness that sources
  `scripts/lib/test-ifa-fault-injection-diagnostics-cases.sh` and
  `scripts/lib/test-ifa-fault-injection-diagnostics-fake-docker.sh` as
  fixtures; those two files are not standalone entry points) — PASS: "49
  cells, 4 shards, exact cover proven", "pin-helper behaviour check: 28
  helper(s) executed", "test-verify-ifa-fault-injection: pass".
- `cd go && env -u GOROOT go vet -tags integration` across the packages
  holding the 13 live-test image pins (`cmd/golden-corpus-gate`,
  `cmd/reducer`, `internal/query/...`, `internal/reducer/code/value/...`,
  `internal/runtime`, `internal/storage/cypher`) — clean.
- `cd go && env -u GOROOT go test ./cmd/golden-corpus-gate/ -count=1` (the
  golden-corpus-mirror static/hermetic contract test, no Docker) — PASS.
- `uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml` — PASS (pre-existing
  unrelated nav warning for `reference/local-testing/pre-pr-execution.md`
  only).
- `git diff --check` — clean.

## Deferred: live golden-corpus gate and backend-divergence differential

**Not run.** Two `ci-gates` processes were already live on this machine when
this drive checked (`pgrep -x ci-gates`: PID 17616, running since 13:09:40,
and PID 56974, running since 13:17:37), and worktree
`eshu-worktrees/6843-graph-only-labels` held the default gate ports
(Postgres 15432, NornicDB Bolt 7687/HTTP 7474) via its `par6843-battery`
compose project. The repo's own `guard-live-gate.sh` pre-tool hook blocked
`bash scripts/verify-golden-corpus-gate.sh` and even the hermetic
`bash scripts/test-verify-golden-corpus-gate.sh` on the `ci-gates`-running
check before evaluating ports. Per the binding cross-worktree live-gate-lock
rule (ceiling is 1 concurrent live gate; port relocation does not make a
second run safe, it still contends for CPU and Docker I/O), this drive did
not override the block and did not run:

- `bash scripts/verify-golden-corpus-gate.sh` (B-7 golden-corpus gate, full
  live run against the new image).
- The `golden-corpus-differential` gate (`specs/ci-gates.v1.yaml` id
  `golden-corpus-differential`): replays the B-7 corpus on both NornicDB and
  Neo4j with differential capture, then diffs against
  `specs/backend-divergence-allowlist.v1.yaml` via
  `go run ./cmd/golden-corpus-gate -phase=backend-diff`, plus the
  `-phase=statement-coverage` check. This is the gate that would retire the
  two #6915 allowlist entries or surface a new divergence; it needs both
  backends live and was not attempted inside the 60-minute drive window
  because the port/process conflict never cleared.

**Divergence disposition: not yet assessed.** No allowlist entry has been
retired and no new divergence has been recorded in this change. The
allowlist file (`specs/backend-divergence-allowlist.v1.yaml`) is unmodified.
Whoever runs the deferred differential gate next should follow the
#6991/#6984 precedent: retire any entry the #492 parser refactor now makes
stale, and treat any new divergence or golden failure as a regression to
report, not paper over.

No-Observability-Change: no metric, span, log field, or status contract
changes. The image swap is observable through the existing backend image
assertions and the (deferred) differential gate.

No-Regression Evidence: the focused Go, replay-tier, k8s governance, and Ifá
fault-injection fixture suites above are unchanged in shape and green on the
new pin; no timing or resource claim is made pending the deferred live
differential/golden-corpus run, which is the authoritative accuracy proof for
this backend swap.
