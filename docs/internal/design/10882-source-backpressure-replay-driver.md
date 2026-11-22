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

Each arm runs three repetitions.

| Arm | Thresholds | A is | B is | What it covers |
| --- | --- | --- | --- | --- |
| C | unset | git | hosted | control |
| T1 | set | git | hosted | ingester admission, projector pre-write consult, selection stretch. Run at signal cache TTL 0 and at one candidate TTL. |
| T2 | set | hosted, paced at the coordinator claim grant | git | the claim-grant leg |
| T3 | set | git; every aged A row is a parked wait on its own producer, fenced, or readiness-blocked | hosted | #11187: the latch never engages, or releases with no claimable A row |

T2 needs a hosted collector that runs offline. `collector-component-extension`
with the zero-fact `collector-synth-zero` emitter is one: the extension's
config is env-only and its work items carry the component id as source system.
Whether the built binary starts standalone against local PostgreSQL on the
in-tree claim path is the first thing the driver build verifies. If it does
not, the smallest stand-in goes to the arbiter before anything is built.

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

## Forks waiting on the arbiter

- **F1, reducer.** The #10862 mix is synthetic (fake fact ids), so a real
  reducer binary would dead-letter it. Proposed: no reducer binary; the
  harness completes A's aged reducer rows at a fixed time in every arm,
  control included.
- **F2, seeding.** A and B projector work is seeded into PostgreSQL directly.
  Real binaries are the projector (pre-write consult, release, admission
  writer), the ingester for its admission leg and selection stretch, and the
  hosted collector for T2.
- **F3, seeder shape.** A build-tagged Go test program that reuses the #10862
  mix, not a new `cmd` directory.

## Not in scope

Enabling the thresholds on QA. The ruling records that as a comment on #10860
and does not do it in this goal.
