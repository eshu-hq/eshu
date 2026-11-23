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

T2's hosted collector is the built `collector-component-extension` running the
offline `collector-synth-zero` emitter on the in-tree claim path: a tagged
seeder installs the component registry and plants pending workflow items through
the real planner, whose `source_system` is the component id. The emitter commits
no facts, so T2 proves claim pacing only.

The ingester runs in `explicit` mode against local bare remotes (a git
`url.insteadOf` rewrite), the production path. The global reducer-admission
marks are raised above the corpus in every arm and recorded in each manifest:
the corpus (8,713 mix rows plus A's 13,000) is past the ingester defaults
(10,000 outstanding, 500 retrying), so the default gate would defer all intents,
B's included, and time them out in the control arm too. That backstop is not what
this replay tests.

## Arm manifest

One JSON file per arm and repetition, written before the arm starts:

- commit, binary digests, arm, repetition, TTL;
- every `ESHU_SOURCE_BACKPRESSURE_*` value (absent for C);
- projector, ingester and reducer worker counts, batch sizes, claim limits;
- PostgreSQL and Neo4j versions, seed digest, host load at start;
- start and end timestamps of each phase.

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
| `run.sh` | Entry point: loads the `lib/` files, walks arms and repetitions. |
| `lib/host.sh` | Load gate, wall-time cap, container start and cleanup. |
| `lib/build.sh` | One-commit binary build and digest record. |
| `lib/arm.sh` | Per-arm manifest, process start and stop, phases. |
| `lib/collect.sh` | Signal collection and snapshot. |
| `census_not_takeable.sql` | The #11187 census, one row per class. |
| `compare.sh` | Final-state equality against arm C. |
| `report.sh` | Turns the manifests and signals into the evidence record. |

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
