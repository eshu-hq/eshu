# Golden Corpus Gate (B-7)

The golden end-to-end corpus gate is the headline guard against "Eshu stops
working end to end." One command runs the full pipeline — `sync → discover →
parse → collect → reduce → query` — over a fixed repo corpus with every
credentialed collector replayed from cassettes, then diffs the live result
against a committed golden snapshot.

It exists because unit tests pass while the assembled pipeline silently breaks:
a queue that never drains, a correlation edge that stops being written, a query
shape that changes. The gate asserts the whole assembly, not the parts.

## What it proves

The gate asserts the four B-7 acceptance buckets:

| Bucket | Assertion |
| --- | --- |
| (a) drains | `fact_work_items` residual rows and `shared_projection_intents` nonterminal rows both reach their snapshot bound. The `shared_projection_intents` check is the decisive one — a zero `fact_work_items` queue alone misses held projection intents (see #3859). To avoid passing on an *unreduced* pipeline, the drain is **populated-then-drained**: it is accepted only after the reducer has been observed to emit the `repo_dependency` domain (`-require-populated-domains`), so a poll that fires before the reducer starts cannot read an empty `0/0` and pass. The `repo_dependency` subset is reported because it is the primary drain signal. |
| (b) graph truth | Required correlations exist (`rc-1` deployable-unit, `rc-3` cross-repo `DEPENDS_ON`, ...). Per-label node and per-relationship edge counts are reported against the snapshot tolerances. No node or edge property may hold an unresolved `row.<key>` token, the text NornicDB stores when a writer omits an `UNWIND` row key it reads (#6782). |
| (c) query truth | Canonical HTTP responses (`GET /api/v0/repositories`, `GET /api/v0/status/operator-control-plane`) carry their required shape. |
| (d) timing | The total pipeline wall time stays within a budget multiple, and — when the orchestrator supplies per-phase timings (B-11, #3804) — each gated phase stays within its `e2e-baseline.json` baseline. See [Macro per-phase regression (B-11)](#macro-per-phase-regression-b-11). |

Before cassette replay, the orchestrator also runs a bounded Postgres lifecycle
proof for container image identity demotion (#5853). It publishes one current
`tag_resolved` support, follows it with `stale_tag`, and requires the production
support writer to select an explicit empty current set. This transition cannot
be expressed deterministically by the static cassette corpus after collector
settle; the proof runs against the same initialized Postgres and fails B-7
before collection if the retirement path stops biting.

## Moving parts

- **B-10 cassettes** (`testdata/cassettes/<collector>/supply-chain-demo.json`)
  replay every credentialed collector with no cloud credentials.
- **B-12 snapshot** (`testdata/golden/e2e-20repo-snapshot.json`) is the contract
  the live run is diffed against. Update it only under review when graph or query
  behaviour changes intentionally — never to paper over drift.
- **`golden-corpus-gate`** (`go/cmd/golden-corpus-gate`) is the typed,
  unit-tested assertion binary.
- **`scripts/verify-golden-corpus-gate.sh`** is the orchestrator that brings up
  Postgres + the graph backend, runs `bootstrap-index` and the bounded demotion
  lifecycle proof, replays the cassettes, drains the projector and reducer,
  starts `eshu-api`, and invokes the gate.

## Minimal-then-grow

The first landing ran a **minimal corpus** (5 repos) and blocked only on the
drains, the existence of `rc-1`/`rc-3`, the two HTTP query shapes, and the timing
budget. The corpus has since grown one assertion at a time as each correlation
was proven green end to end: `rc-2` (`RUNS_IN`, the code→runtime bridge) and
`rc-4` (`RUNS_IMAGE`, the live workload→OCI manifest edge) are now **required**
alongside `rc-1` (deployable-unit) and `rc-3` (cross-repo `DEPENDS_ON`). The
20-repo node/edge count tolerances remain **advisory** (`WARN`) so latent gaps
surface without blocking.

The blocking correlation set is configurable via `-required-correlations`.
`scripts/verify-golden-corpus-gate.sh` passes `-required-correlations="all"`,
which single-sources the blocking set from the snapshot's own
`required_correlations` ids (#4596): promoting a newly-added `rc-N` to blocking
is a one-file edit to the snapshot, not also a hand-edit to a second,
duplicated comma-separated id list in the script. To stage a new `rc-N` as
advisory-only before it is proven, pass an explicit comma-separated subset
(e.g. `-required-correlations=rc-3,rc-1,rc-2,rc-4`) instead of `all`; an empty
value blocks nothing.

## Macro per-phase regression (B-11)

B-2 (#3795) catches a per-*function* `ns/op` regression with benchstat; B-11
(#3804) catches a per-*phase* wall-clock regression that no single benchmark
would surface. The orchestrator captures the wall-clock of each pipeline phase
(`bootstrap`, `collect`, `first_drain`, `maintenance_drains`, `graph_query`),
emits it as `phase-timings.json`, and the gate compares each phase against the
committed baseline `testdata/golden/e2e-baseline.json`.

A gated phase passes when

```
observed <= baseline * (1 + regression_band)   OR   observed <= baseline + absolute_slack_seconds
```

The dual rule mirrors the reducer claim-latency contract's "1.10x OR +60s": the
relative band catches real regressions on the larger phases, while the absolute
slack absorbs integer-second timing jitter on the small phases.

- **`collect`** is recorded but **not gated** — it is dominated by the collector
  settle poll, which waits until every `(scope_id, generation_id)` pair the
  cassettes launched has committed (rows the gate did not launch, such as the
  bootstrap `eshu:global` seed, are ignored), bounded by
  `GATE_COLLECTOR_SETTLE_SECONDS` as a deadline, not pipeline work.
- **On shared CI runners** the check is **advisory** (`-phase-regression-advisory`):
  GitHub's hosted runners vary run-to-run by more than the band, so a per-PR
  regression is reported as a `WARN` without a false red — the same reasoning
  behind the 2x total-wall-time budget multiplier.
- **On a controlled host** (consistent hardware) set
  `GATE_PHASE_REGRESSION_ADVISORY=false` to make the gated phases blocking; the
  committed baseline is valid there.

Recapture the baseline on the enforcement host after an intentional perf change:

```bash
bash scripts/refresh-e2e-baseline.sh   # runs the gate, folds observed seconds in
```

It updates only `baseline_seconds`; `gated` flags, notes, band, slack, and the
policy blocks are preserved. Review the diff and commit with a before/after
explanation, the same review bar as the B-12 snapshot.

## Running it

Static and unit checks (no Docker):

```bash
cd go && go test ./cmd/golden-corpus-gate -count=1
bash scripts/test-verify-golden-corpus-gate.sh
```

Full live run (needs Docker):

```bash
bash scripts/verify-golden-corpus-gate.sh
# --no-compose  assume Postgres + graph are already up
# --keep        retain services + work dir for debugging a failure
#               (also retains the cross-run lock - see below)
```

### Running it on Neo4j

The same gate runs against the Neo4j compatibility backend. Set
`ESHU_GRAPH_BACKEND=neo4j` and the orchestrator switches to
`docker-compose.neo4j.yml`, the `neo4j` service, and the `neo4j` database; every
other step, and every snapshot assertion, is unchanged:

```bash
ESHU_GRAPH_BACKEND=neo4j bash scripts/verify-golden-corpus-gate.sh
```

The snapshot is one contract for both backends. A Neo4j-only failure is a
backend divergence to diagnose, not a reason to loosen the snapshot: find which
backend matches the documented contract, and fix Eshu (or file the backend
defect) accordingly. The first Neo4j run (#6782) found two such divergences: a
Neo4j-only `shortestPath()` error on a self-recursive call chain, and a
NornicDB-only bogus `source_tool` written from a missing UNWIND row key. See
[Backend Conformance](../backend-conformance.md#b-7-golden-corpus-on-both-backends).
The cross-run lock below applies to both backends, so run them one after the
other on one host.

### Differential oracle (NornicDB vs Neo4j)

Beyond the per-backend snapshot, CI runs a **differential** job that replays
the same corpus on both backends with statement capture on, then diffs the two
recordings (`differential nornicdb vs neo4j` in `golden-corpus-gate.yml`,
blocking via `golden-corpus-differential` in `specs/ci-gates.v1.yaml`). Every
executed graph statement is fingerprinted (normalized text plus bound
parameters) with a digest of its result rows; the gate fails on any statement
whose digest differs, naming the statement so the failure points at its
production source. A side with no
recordings fails instead of passing vacuously, so a half-finished run can never
look green.

CI runs **two leg pairings** and fails only on divergences that reproduce
across both (multi-leg quorum): pairing-local scheduling noise drops out,
while a systematic backend divergence reproduces and still fails.

| Finding | Meaning |
| --- | --- |
| `nornicdb_vs_neo4j_quorum` | Reproduced divergences of a required kind. Failing. (Reproduced advisory divergences stay advisory here; the ceiling below is their tripwire.) |
| `nornicdb_vs_neo4j_executions` | Reproduced execution-count or row-total divergences with agreeing results (scheduling noise: drain passes, retries, extra poll iterations). Advisory. |
| `nornicdb_vs_neo4j_transient` | Divergences on registered transient-state reads, whose digests disagree because the result depends on the drain point. Advisory, always visible. |
| `nornicdb_vs_neo4j_tie_order` | Divergences on registered tie-order reads: ORDER BY over tied keys with no truncation, where delivery order is backend-undefined but the row multiset agrees. Advisory, always visible. |
| `nornicdb_vs_neo4j_executions_ceiling` | Required tripwire: the reproduced advisory total (scheduling-noise plus transient-read plus tie-order) exceeded `-diff-executions-advisory-max` (CI passes 200). |
| `nornicdb_vs_neo4j_nonreproducing` | Pairing-local divergences the quorum dropped. Informational. |

Known, accepted divergences live in
`specs/backend-divergence-allowlist.v1.yaml`. Each `entries` item names the
exact statement fingerprint (plus optional binding narrowing), the divergence
kind it excuses (`missing`, `results`, `failures`, or the whole statement —
there is no `executions` or `rowcount` tier; the parser rejects both), the
reason, the upstream issue, and the owner. Every entry must match at least one
divergence in the run: a stale entry fails even a green run, so a fixed bug
cannot linger as a permanent exemption. Beside entries, a `transient_reads`
section registers transient-state reads (orphan scans over `uid IS NULL`,
orphan-sweep pages on `eshu_orphan_observed_at_unix`): fingerprint-keyed with
the same reason and upstream accountability, no tier, never stale-checked, but
the parse guard requires a transient-state marker in the statement, and only
the observed-noise kinds are held — a backend error or a one-sided recording
on a transient read still fails. A `tie_order_reads` section registers
ORDER BY reads with no `LIMIT` or `SKIP` whose keys can tie
(fingerprint-keyed with the same reason and upstream accountability, no
tier, never stale-checked): tied delivery order agrees on most runs by
design, so stale-checking it fails the gate on exactly the runs where both
backends agree. The parse guard requires `ORDER BY` and rejects `LIMIT` /
`SKIP` — with truncation, tied keys change which rows return, and that is
row truth that must stay required — and only the `results` kind is held.

Replay a CI capture locally with the committed allowlist (single-pair mode,
which reports under the `nornicdb_vs_neo4j` finding name rather than the
quorum table above; quorum needs two pairing dirs):

```bash
cd go && go run ./cmd/golden-corpus-gate -phase=backend-diff \
  -diff-left=/tmp/diff-capture/nornicdb -diff-right=/tmp/diff-capture/neo4j \
  -diff-allowlist=../specs/backend-divergence-allowlist.v1.yaml
```

The static mirror plus the gate's seeded RED/GREEN unit pair are the local
proof (no Docker):

```bash
cd go && go test ./cmd/golden-corpus-gate -run 'TestBackendDiff|TestRunBackendDiff' -count=1
```

### The cross-run lock

The gate binds **fixed host ports** (Postgres, api, mcp) and a compose project
derived from the worktree name, so two runs cannot safely overlap. They do not
fail cleanly on a port bind — they starve each other, and the loser reports
`fact_work_items_residual: residual=1 (dead_letter=1)` after the drain timeout,
which reads exactly like a reducer or queue defect and costs a long
investigation in the wrong place.

The gate therefore takes a lock under the shared git common dir before it does
any work, and a second run refuses immediately, naming the holder's pid and
worktree. The lock covers **this script, within one clone**: port disjointness
is not safety, because the contention is CPU and Docker I/O, and a separate
clone has its own lock.

| Variable | Effect |
| --- | --- |
| `ESHU_SKIP_LIVE_GATE_LOCK=1` | Bypass the lock. For CI, where each job already has an isolated runner and there is no sibling worktree to collide with. |
| `ESHU_LIVE_GATE_LOCK_DIR=<dir>` | Relocate the lock, so tests can exercise it without touching the real one under `.git`. |

**`--keep` retains the lock as well as the stack.** That is deliberate: the
retained containers still hold the fixed ports, so releasing the lock would hand
those ports to the next run, which would then tear the retained stack down with
`docker compose down -v` on its own exit — destroying the thing `--keep` was
for. The versioned marker records the Compose project as the durable resource
owner and an epoch timestamp for elapsed-age diagnostics. The recorded pid and
worktree identify the holder; process liveness does not decide marker validity
because the holder normally exits immediately after retaining the stack.

A later run checks that project with `docker compose -p <project> ps -q` while
it owns the applicable live-gate lock. Compose lists running containers by
default. A successful empty result therefore means the project no longer owns
running containers or fixed host ports, and the run removes the stale marker.
Stopped containers are intentionally reclaimable because they do not bind the
ports; use `docker compose ps --all` separately if you need to inspect them.

Every refusal for a valid marker names its project, worktree, pid, and elapsed
retention age in seconds. The decision fails closed when Docker is unavailable,
the Compose query fails, the marker is unreadable or malformed, the current
clock is unavailable, the timestamp is missing, invalid, or in the future, the
project is empty, or the stale marker cannot be removed. A four-field marker
written by the earlier open #5987 branch also remains fail-closed: it identifies
the holder but reports the retention age as unknown rather than guessing or
reclaiming it.

Tear down the stack **from the worktree that took `--keep`** because its
environment and Compose files define the retained project and backend:

```bash
docker compose -f docker-compose.yaml down -v          # ESHU_GRAPH_BACKEND=neo4j: docker-compose.neo4j.yml
rm -f "$(git rev-parse --path-format=absolute --git-common-dir)/eshu-live-gate.lock" \
      "$(git rev-parse --path-format=absolute --git-common-dir)/eshu-live-gate.lock.keep"
```

Tear the stack down **before** removing the marker. Removing the marker first
frees the lock while the containers still hold the fixed ports, which is exactly
the collision the retention exists to prevent. The refusal message names both
paths, so you do not have to remember them.

The main lock is a deliberately dangling symlink whose link text carries the
owner payload. A shell check using only `[ -e "$lock" ]` reports it absent.
Inspect it with `readlink "$lock"` or `[ -L "$lock" ] || [ -e "$lock" ]`.

This mutex refuses immediately; it is not a FIFO queue. A stale retained marker
self-heals as described above, but an active holder still blocks, and repeated
active holders can make another worktree retry later. Adding queued fairness is
a separate scheduling change with waiter cleanup and cancellation semantics.

### Retained-marker evidence

No-Regression Evidence: The conflict domain is the shared git-common-dir lock
and marker for live gates within one clone. The no-marker fast path checks the
filesystem and makes no Docker query. A valid marker-present liveness decision
makes one `docker compose -p <project> ps -q` query while it owns the main lock
or stale reclaim guard; malformed markers refuse before Docker. The gate refuses
immediately when a holder is active or stack state is unknown. It does not add
FIFO waiting. An isolated Docker Compose 5.1.2 run returned 0 container IDs for
an absent project, 1 for a running project, and after stopping that container
returned 0 from default `ps -q` and 1 from `ps -q --all`. The focused shell race
starts two contenders against one stale marker and proves the second never
queries Docker while the first owns the main lock; the first contender's
replacement marker remains intact.

Observability Evidence: Refusals write the retained Compose project, worktree,
holder pid, elapsed age or an explicit unknown-age state, and the decision
reason to stderr. Reasons distinguish a running project, Docker unavailable,
query failure, malformed or incompatible marker data, clock/timestamp failure,
replacement-marker change, and removal failure. Successful stale-marker
reclaim also reports the project and holder details. This shell coordination
change adds no service OpenTelemetry metric, span, or log field; service runtime
behavior is unchanged.

In CI the gate runs as the **Golden Corpus Gate** workflow, required on any PR
that touches a pipeline phase (collector, parser, projector, reducer, query,
storage, the pipeline command binaries, the cassettes, or the snapshot). Its
`corpus-gate` job is a matrix with one cell per graph backend:
`corpus-gate (nornicdb)` is blocking (`golden-corpus-gate` in
`specs/ci-gates.v1.yaml`), and `corpus-gate (neo4j)` is registered as the
non-blocking `golden-corpus-gate-neo4j` until main is green on it.
