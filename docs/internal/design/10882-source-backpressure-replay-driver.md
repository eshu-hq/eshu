# Source Backpressure Replay Driver (#10882, #11186, #11187)

Status: skeleton. The corpus, arms, criteria and close rule come from the
arbiter ruling of 2026-11-22 (`.claude/arbiter-remaining-plan.md`, section 1).
Three forks (F1 to F3 below) wait on an arbiter ruling and may change the
build. This page describes what the driver does; it holds no measurements.

## What the replay must show

The per-source reducer backpressure leg ships off. Before any deployment sets
the `ESHU_SOURCE_BACKPRESSURE_*` thresholds, one local run of built binaries
from a single `origin/main` commit has to show that:

- a slowed source (A) loses no facts and resumes (AC4);
- a healthy source (B) keeps its projector queue wait and claim rate (AC5);
- the consult stays cheap enough to run per claim (K and consult p50 gate).

## Venue

Local only. PostgreSQL 18.6 in a container named `replay-pg` and the pinned
Neo4j (`neo4j:2026-community@sha256:eabfbb04…`, the digest in
`scripts/docker-compose.go-reachability-proof-deps.yaml`) in `replay-neo4j`.
Binaries come from one commit, built into `~/.cache/replay/bin`; every arm
records the commit, so a control run and a treatment run can never mix builds.
The harness refuses to start an arm while the 1-minute load is 60 or more,
caps the whole replay at 4 hours of wall time, and removes its containers when
it finishes.

## Corpus

| Part | Shape |
| --- | --- |
| Reducer backlog | The #10862 mix: 8,713 waiting rows, 1,238 retrying (`reducer_queue_10862_replay_test.go`). |
| Source A | About 13,000 claimable reducer rows aged past the engage age (the 10x shape), plus projector items for A. At least as many paced A projector items as projector workers. |
| Source B | Steady fresh projector work for the whole run, on a fixed cadence. |

Worker counts and batch sizes are identical in every arm, and each arm's
manifest records them.

## Arms

Each arm runs three repetitions. A and B swap between git and a hosted family.

| Arm | Thresholds | A is | B is | What it covers |
| --- | --- | --- | --- | --- |
| C | unset | git | hosted | control |
| C0 | unset, no catch-up step | git | hosted | the catch-up step hides nothing: B and the final sets equal C |
| T1 | set, signal cache TTL 0 | git | hosted | ingester admission, projector pre-write consult, selection stretch |
| T1TTL | set, candidate TTL | git | hosted | the same at one candidate TTL |
| C2 | unset | hosted | git | control for T2 |
| T2 | set | hosted, paced at the coordinator claim grant | git | the claim-grant leg |
| T3 | set | git; every aged A row is not takeable (parked wait or readiness-gated) | hosted | #11187: the latch never engages |
| T3TTL | set, candidate TTL | as T3 | hosted | consult p50 and p95 with parked rows present at the candidate TTL |
| CG | unset, default global marks | git | hosted | one repetition, reported not judged: what the default global gate does to the corpus |

T2's hosted collector is the built `collector-component-extension` running the
offline `collector-synth-zero` emitter on the in-tree claim path: a tagged
seeder installs the component registry and plants pending workflow items through
the real planner, whose `source_system` is the component id. The emitter commits
no facts, so T2 proves claim pacing only.

The ingester runs in `explicit` mode against local bare remotes (a git
`url.insteadOf` rewrite), the production path. The global reducer-admission marks
are raised above the corpus on the ingester in every arm except CG, and recorded
in each manifest; the standalone projector keeps its product default (no global
gate). The corpus (8,713 mix rows plus A's 13,000) is past the ingester defaults
(10,000 outstanding, 500 retrying), and the global gates run before the source
leg, so at the defaults the ingester would defer all intents, B's included, with
or without this feature. That backstop is not what this replay tests; CG reports
what it does. The admission-writer leg and AC5 therefore hold only while total
reducer outstanding is below the ingester's high-water mark. The pre-write,
claim-grant and selection legs do not depend on it.

## Arm manifest

One `manifest.json` per run, written before the arm starts. Its fields:
`arm`, `rep`, `ttl`, `completion_step`, `thresholds`, `global_admission` and
`global_marks`, `source_a`, `source_b`, `a_shape`, `a_reducer_rows`,
`a_projector_generations`, `b_generations`, `b_cadence`, `workers` (projector and
ingester-projector), `engaged_window_s`, `selection_base_s`, `load1_at_start`,
`postgres`, `neo4j`, `commit` and `binaries` (sha256 of every product binary at
arm start). `build.json` in the results root pins the build commit and the sha256
of each binary; the checker fails an empty commit or a binary whose sha256 differs
from the build. `go build` stamps no `vcs.revision` in this worktree layout, so the
identity rests on the clean-tree check in `build.sh` plus the sha256 pins.

## Phases of one run

1. Reset: fresh schema, fresh graph, seed the corpus.
2. Start binaries, start the B feeder, start the collectors for the arm.
3. Engaged window: A is aged past the engage age; the latch engages
   (treatment arms). Collect the signals below on a fixed cadence.
4. Catch-up: the harness completes A's aged reducer rows in chunks (F1), so
   the latch releases on the real latch rows and A's paced items drain.
5. Drain: wait for every projector item to settle, then snapshot.
6. Census and compare: classify the not-takeable rows
   (`census_not_takeable.sql`), compare the final state to arm C.

## Signals collected

- Consult p50 and p95 from the new
  `eshu_dp_reducer_admission_source_consult_duration_seconds`, per source and
  per TTL, taken while parked rows are present.
- Deferrals by reason (`source_behind`), the paced-release counter, attempt
  counts, dead letters, admission timeouts.
- Canonical and content writes for A while engaged (expected zero).
- B's projector queue wait p50 and p95 and B's claim rate; busy projector
  workers while A is paced.
- Peak K per source (visible retrying rows), visible parked rows, woken rows
  that still hold a wait row, wait-table row count.
- Final committed generation set, fact set (fact id and payload hash per
  scope) and graph counts per label.

## Pass criteria

AC4, AC5 and T3 are exactly the criteria of the ruling; the report adds, as
numbers and not as pass or fail, the consult p50 and p95 at both TTLs, peak K,
parked rows, woken rows holding a wait row, the wait-table row count, and a
recommended TTL with its staleness bound. AC5 uses the larger of 10% or twice
the spread of arm C. The enablement gate is K at most 20 times the consult p50
in milliseconds, and consult p50 at most 100 ms.

## Files

Everything lives under `scripts/dev/10882-replay/`, each file under 500 lines:

| File | Role |
| --- | --- |
| `run.sh` | Entry point: builds, then walks arms and repetitions with the load gate and the 4 hour cap. |
| `lib/env.sh`, `lib/host.sh` | Venue settings, load gate, wall cap, container checks. |
| `lib/build.sh` | One-commit binary build, `build.txt` and `build.json`. |
| `lib/proc.sh`, `lib/hosted.sh` | Start and stop the product binaries; the T2 hosted collector. |
| `lib/arm.sh` | One run: arm switches, manifest, schedule, events. |
| `lib/sampler.sh` | Latch row, A writes (real Neo4j), K, load1 per sample. |
| `lib/collect.sh` | End-of-run exports (generations, facts, reducer rows, projector items, latch rows). |
| `census_not_takeable.sql`, `census.sh` | The #11187 census. |
| `assemble_result.py`, `replay_state.py`, `replay_facts.py`, `replay_prom.py` | Turn a run directory into `result.json`. |
| `check_arm.py`, `checks_lib.py` | The pass/fail gate. |
| `report.sh` | Runs the gate over every run and prints the numbers. |
| `test_check_arm.py`, `test_replay_readers.py`, `test_census.sh` | Seeded RED/GREEN proofs. |

## Rulings that shaped the driver

The arbiter accepted the three forks with conditions (reducer binary, seeding,
seeder shape): no reducer binary runs; the catch-up step goes through
`ReducerQueue.ClaimBatch` and `AckBatch`, never a hand UPDATE, restricted to a
domain only A uses; seeded generations go through the exported
`IngestionStore.CommitScopeGeneration`; the seeder is a build-tagged test
compiled at the same commit. Real collected facts come from the real ingester on
the git fixtures.

## What the replay does not exercise

- No reducer ran. Release is driven by completing seeded rows, and B's freshness
  is measured to the reducer-queue enqueue, not through reducer handlers.
- K and the parked counts come from the seed.
- T2 commits no hosted facts, and pacing acts before the claim, so no partial
  commit exists to lose.
- Seeded `fact_records` equality holds by construction and is not a no-loss
  proof; the no-loss checks use projection outputs and the real fixture facts.
- The admission-writer leg is reached only by an item claimed before the latch
  engaged: `projector/service.go` releases a paced item before any write, so a
  zero on `source_behind` admission deferrals with non-zero pre-write releases is
  expected, not a gap.
- Fact sets of the real fixtures compare stable fact key plus payload hash.
  `fact_id`, `payload.indexed_at` and `payload.source_run_id` are minted from the
  run's wall clock and are normalised; the checker output names them.

## Filesystem mode and the git selection stretch

The first replay venue ran the ingester in `filesystem` source mode and was
abandoned for a defect it exposed. The schedule leg filters repositories before
the filesystem sync, and that sync stamps one whole-root manifest after copying
only the repositories it was handed, so a repository the leg deferred (by the
stretch or by the healthy base interval) is never selected again until the
corpus changes; copy mode also deletes its managed copy. Explicit, githubOrg and
gitlabGroup mode keep per-repository state and re-select a deferred repository.

The fix refuses the pair at startup, so the leg is undefined for filesystem mode.
`TestWireSelectionBackpressureRefusesFilesystemMode` pins the refusal and
`TestSelectRepositoriesFilesystemModeNeverDefersAnEditedRepository` pins the
selector-level drop (copy and direct mode). The replay ingester runs in
`explicit` mode against local bare remotes, and its AC4 and AC5 claims are scoped
to explicit, githubOrg and gitlabGroup. Filesystem mode keeps the admission and
pre-write pacing legs.

No-Observability-Change: the startup error naming `ESHU_REPO_SOURCE_MODE` and
`ESHU_GIT_SELECTION_MIN_RESYNC_INTERVAL` is the operator signal; the existing
deferral and claim-pacing series are unchanged.

## Not in scope

Enabling the thresholds on QA. The ruling records that as a comment on #10860
and does not do it in this goal.

## Evidence markers for the code changes in this branch

Performance Evidence: the replay is the measurement for the schedule-leg and
consult-instrument changes; its record is written when the arms are accepted.
The code the branch adds to hot paths is two monotonic clock reads and one
histogram record per enabled per-source consult (nanoseconds against the 12 to
37 ms consult p50 measured in the driver smoke (not evidence)), and one string comparison per
selection cycle for the source-mode predicate.

No-Regression Evidence: a disabled per-source leg records no sample and reads no
clock (`TestCheckSourceBackpressureDisabledRecordsNoConsultDuration`); a git
source mode other than filesystem takes the same path as before
(`TestSelectionBackpressureSupportsOnlyPerRepositorySyncModes`, the existing
selection backpressure tests).

Observability Evidence: `eshu_dp_reducer_admission_source_consult_duration_seconds`
is the new operator signal (coverage row in
`telemetry-coverage-reducer-core.md`, metric reference in
`docs/public/reference/telemetry/metrics.md`); the filesystem-mode refusal is a
startup error naming both variables.
