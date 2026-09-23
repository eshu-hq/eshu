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

## Live golden-corpus gate: RED — real regression, reproduced 3x

**`bash scripts/verify-golden-corpus-gate.sh` FAILS against `fix-6915-c4de1c5c`.**
The default gate ports (Postgres 15432, NornicDB Bolt 7687/HTTP 7474) were held
by a foreign, unrelated stack the whole drive (worktree
`eshu-worktrees/6843-graph-only-labels`'s `par6843-battery` compose project,
and separately a long-lived `eshu-6820-pg` container on 15432), and at times a
genuine concurrent `ci-gates` process was live elsewhere on the machine
(`pgrep -x ci-gates` matched real, short-lived processes from other sessions
more than once). Waiting did not clear the port hold (it is a persistent
container, not a finishing gate run), so every run below used the sanctioned
isolated lane: `ESHU_POSTGRES_PORT=15532 NEO4J_BOLT_PORT=7788
NEO4J_HTTP_PORT=7575 GATE_API_PORT=18081 GATE_MCP_PORT=18092` (all five
verified free with `lsof` immediately before each run),
`ESHU_LIVE_GATE_LOCK_DIR` pointed at a private scratch directory, and a
unique `COMPOSE_PROJECT_NAME` per run so nothing touched another session's
stack. Never used `ESHU_SKIP_LIVE_GATE_LOCK` or `CLAUDE_HOOK_ALLOW`. Every
run's own compose project was confirmed torn down (`docker compose ls`)
before the next one started; `par6843-battery` was never touched.

Three independent runs, same tested commit (`2d4cb9ad6f60e2dc0042307113c89ab5bbddcc97`),
same result each time — this is a stable regression, not a flake:

1. `env -u GOROOT ESHU_POSTGRES_PASSWORD=*** ESHU_NEO4J_PASSWORD=*** ESHU_POSTGRES_PORT=15532 NEO4J_BOLT_PORT=7788 NEO4J_HTTP_PORT=7575 GATE_API_PORT=18081 GATE_MCP_PORT=18092 ESHU_GRAPH_BACKEND=nornicdb bash scripts/verify-golden-corpus-gate.sh` — exit 1, `566 pass, 5 required-fail, 1 advisory-warn`.
2. Same command plus `ESHU_LIVE_GATE_LOCK_DIR=<scratch>/live-gate-lock-7014 COMPOSE_PROJECT_NAME=gate7014b7` (isolated lane) — exit 1, `565 pass, 5 required-fail, 2 advisory-warn`.
3. The `golden-corpus-differential` gate's own local command (`specs/ci-gates.v1.yaml` id `golden-corpus-differential`, `COMPOSE_PROJECT_NAME=gate7014diff`): `rm -rf /tmp/eshu-diff-corpus && ESHU_DIFFERENTIAL_CAPTURE=1 ESHU_DIFFERENTIAL_CAPTURE_DIR=/tmp/diff-capture/nornicdb ESHU_REPOS_DIR=/tmp/eshu-diff-corpus ESHU_GRAPH_BACKEND=nornicdb bash scripts/verify-golden-corpus-gate.sh && ... (neo4j leg) && ... (backend-diff, statement-coverage)` — exit 1, `565 pass, 5 required-fail, 2 advisory-warn` on the NornicDB leg (the `&&` chain short-circuited there: **the Neo4j leg and the `-phase=backend-diff`/`-phase=statement-coverage` steps never ran**).

Every run failed on the identical 5 required checks, all one symptom — a
Dart mutual-recursion CALLS-relationship name lookup (function `mutualPing`,
repo `dart_comprehensive` / `repository:r_ed3a9bab`) that comes back empty or
404 through the API/MCP query layer, while the underlying graph write is
correct:

```
[FAIL] POST /api/v0/code/relationships?assert=direct-callees: "outgoing" has 0 results, want >= 1
[FAIL] POST /api/v0/code/relationships?assert=direct-callers: "incoming" has 0 results, want >= 1
[FAIL] POST /api/v0/code/relationships?assert=transitive-callees: HTTP 404 from /api/v0/code/relationships?assert=transitive-callees
[FAIL] POST /api/v0/code/relationships?assert=transitive-callers: HTTP 404 from /api/v0/code/relationships?assert=transitive-callers
[FAIL] mcp:find_function_call_chain: "chains" has 0 results, want >= 1
```

Evidence this is a query-resolution regression, not data loss: in the same
run, `rc-11: (Function)-[:CALLS]->(Function) count=25, want >= 1` PASSED,
`edge_count_CALLS: 29, snapshot range [29,200000]` PASSED, and
`sl-dart-calls-recursion: (Function {language="dart"})-[:CALLS]->(self)
count=2, want [2,2]` PASSED — the CALLS edges exist and the Dart
self-recursion count is exactly right. Only the name+repo_id-anchored lookup
(`ResolveRelationshipsNameTarget` /
`go/internal/query/codemodel/code_relationships_resolution.go`, calling
`reader.SearchEntitiesByName`) and the transitive/MCP call-chain paths that
depend on it come back empty. The direct and transitive requests hit the
same anchor resolution but fail two different ways (200/empty vs 404),
consistent with a single upstream anchor-match miss surfacing through two
different response-shaping code paths, not two independent bugs.

Advisory-only, not blocking: `phase_maintenance_drains` ran 80s against a
25s baseline / 30s ceiling in both non-differential runs — plausibly this
machine's sustained background load (multiple foreign compose stacks and
intermittent sibling `ci-gates` runs throughout the drive), not evidence of
a NornicDB regression on its own.

**Not proven:** whether this same golden-corpus-gate assertion passed on the
prior `fix-500-e022384c` pin. The assertion entries were added to
`testdata/golden/e2e-20repo-snapshot.json` in `15134de7a1` (the only commit
touching those lines, already an ancestor of this branch) as a *required*
check, so it should have been green in CI before this repin; that was not
independently re-verified against the old image in this drive to avoid
spending more shared-machine gate time chasing a confirmed regression.

## Divergence disposition: REGRESSION, not a stale-allowlist retirement

**This is not a backend-divergence-allowlist item.** It is a same-run,
same-backend required-fail against the golden snapshot, orthogonal to the
`specs/backend-divergence-allowlist.v1.yaml` NornicDB-vs-Neo4j comparison
mechanism. No allowlist entry has been retired and no new divergence entry
has been added — the allowlist file is unmodified — because the
`golden-corpus-differential` gate's own chain never reached its
`-phase=backend-diff`/`-phase=statement-coverage` steps (run 3 above stopped
at the NornicDB leg). The two #6915 allowlist entries this repin was
expected to retire remain unretired; that retirement cannot happen until the
call-chain regression above is fixed and the differential gate can actually
run to completion.

Per the binding disposition rule, this is recorded as a regression to fix,
not suppressed, papered over, or worked around by relaxing an assertion.
**#7014/#6915 is NOT gate-proven and this pin should not roll out until the
call-chain query regression is root-caused and fixed** (most likely
candidate given the timing: the orneryd/NornicDB#492 parser rewrite changing
entity/property-match or Cypher planning behavior in a way that breaks
`SearchEntitiesByName`-style anchor lookups; not independently confirmed in
this drive).

No-Observability-Change: no metric, span, log field, or status contract
changes. The image swap is observable through the existing backend image
assertions and the (blocked) differential gate.

No-Regression Evidence: N/A for this section — the live golden-corpus gate
is RED, not green; see above. The focused Go, replay-tier, k8s governance,
and Ifá fault-injection fixture suites in the previous section are unchanged
in shape and still green on the new pin; that is a narrower proof than the
live golden-corpus/differential gates and does not substitute for them.
