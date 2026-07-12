# replay/offlinetier

The R-5 offline replay gate tier (epic #4102, issue #4107): an env-gated
`go test` that replays a committed cassette through the **real** canonical
projection writer into a **real** single-container NornicDB — with no Docker
Compose — then reads the graph back over Bolt and asserts node and edge truth.

## Why this exists

Backend-specific projection bugs only surface against a real graph engine:

- **#4019** — the phase-group executor's within-phase read-your-writes gap on
  NornicDB, which silently dropped nested directories (and everything beneath
  them) when the directory node MERGE and the parent-edge MATCH shared one
  transaction.
- commit-time MERGE uniqueness races.
- NornicDB `MATCH` quirks once the schema's `uid` lookup indexes exist.

A fake or in-memory graph cannot reproduce any of these, so this tier never
substitutes one. When no real backend is configured it **skips cleanly** (it
does not silently pass); when a backend is present it **fails on any mismatch**.

## What it proves

The committed cassette `testdata/cassettes/replayoffline/nested-directory-tree.json`
models the #4019 case: a repository with directories at depth 0, 1, and 2. The
tier maps those facts to a `projector.CanonicalMaterialization` and drives the
production `storage/cypher.CanonicalNodeWriter` over the **NornicDB phase-group
write path** (each canonical phase is its own transaction — the same path the
ingester/bootstrap-index wire in production). It then asserts:

- the `Repository` node and all three `Directory` nodes exist, and
- the full `CONTAINS` chain exists: `Repository -> depth0`, `depth0 -> depth1`,
  `depth1 -> depth2`.

The depth-2 edge is the regression guard: if the projector reverted to a single
atomic group across phases, NornicDB would drop the nested-directory edges and
the tier would fail.

## Running

The default `go test` pass runs only the offline half
(`TestCassetteMaterializationMapsNestedTree`) and skips the live tier.

To run the real-backend tier, use the companion script, which starts the lean
NornicDB container with plain `docker run` (not Compose), exports the Bolt
environment, runs the focused test, prints before/after wall-clock, and always
tears the container down:

```bash
scripts/verify-replay-tier.sh
```

CI runs exactly this script in `.github/workflows/verify-replay-tier.yml` on
pushes/PRs that touch the replay, projection, graph, or cypher paths, so the
backend-specific regression class is gated on the real backend in CI — not only
when a developer remembers to run it locally. (The default `go test ./...` run
leaves `ESHU_REPLAY_TIER_LIVE` unset, so the live tier skips there.)

To run it by hand against an already-running backend:

```bash
ESHU_REPLAY_TIER_LIVE=1 \
ESHU_GRAPH_BACKEND=nornicdb \
ESHU_NEO4J_DATABASE=nornic \
NEO4J_URI=bolt://localhost:7687 \
NEO4J_USERNAME=neo4j NEO4J_PASSWORD=change-me \
go test ./internal/replay/offlinetier/ -run TestOfflineReplayTierGraphTruth -count=1 -v
```

## Relationship to the Compose B-7 gate

This tier is the fast, credential-free, single-container backend check. The full
Compose B-7 golden-corpus gate (`scripts/verify-golden-corpus-gate.sh`) is
unchanged and remains the belt-and-suspenders full-corpus assertion.

## Delta / multi-generation / tombstone (R-17, #4126)

`delta.go` extends the tier to multi-generation scenarios. A two-generation
cassette (`testdata/cassettes/replaydelta/multi-generation-tombstone.json`)
records gen1 (alpha, beta, gamma) then gen2 that adds `delta`, supersedes the
repository name, and **tombstones** `gamma` (`is_tombstone: true`).

`DeltaMaterializationFromGenerations` builds the gen2
`CanonicalMaterialization` from the **surviving** (non-tombstoned) facts and
sets `FirstGeneration=false`, so the production retract phase fires and removes
entities absent from gen2 — it never resurrects a tombstoned node by writing it.

- **Offline (every PR):** `delta_tier_test.go` asserts the structural inputs
  that drive retraction — gamma filtered out of the surviving rows, the
  tombstoned-path list, `FirstGeneration=false`, and the superseded repo name.
- **Live (`ESHU_REPLAY_TIER_LIVE=1`):** `delta_tier_live_test.go` writes gen1
  then gen2 against real NornicDB and asserts gamma is **gone** (count=0),
  survivors present, the repo name superseded, and idempotency (gen2 twice).
  Its negative control forces `FirstGeneration=true` and asserts gamma **stays**
  — the #3859 held-pending-retract bug class — then proves the correct gen2
  removes it. This is the retraction proof; the offline half cannot delete a
  node without a backend.

### Entity-label retract coverage (C-14 #4367)

The cassette's gen1 also carries a `content_entity` fact per retractable graph
entity label (Function, Class, HelmChart, ArgoCDApplication, Terraform*, ...),
absent from gen2. `materialization.go` routes these through the production
`projector.ExtractEntityRows` mapping, so the offline tier drives real entity
nodes of each label. `delta_tier_entity_retract_live_test.go` derives the label
set from the cassette itself and asserts each label is created (count=1) after
gen1 and retracted (count=0) after gen2 against real NornicDB.
`TestEntityRetractManifestBinding` binds that cassette-derived set to the
replay-coverage manifest's `retractable_node` delta_tombstone rows, so a
cassette/manifest drift fails without a backend.

Residual labels with base-cassette survivors (GitlabJob, GitlabPipeline) get a
doomed instance in gen1 absent from gen2; `delta_tier_survivor_retract_live_test.go`
proves the doomed instance is retracted (count=0 by uid) while a same-label
survivor remains, so the retract is scoped, not a label wipe. K8sResource is
covered through the content_entity batch. File (structural file-retract path the
offline delta tier does not yet drive) and Variable (reducer-owned semantic
graph subset, skipped by the canonical entity phase) remain follow-ups.

### Edge-retract coverage (C-14 #4367)

`delta_tier_edge_retract_live_test.go` proves DIRECT DEFINES_JOB (GitlabPipeline
-> GitlabJob) edge retraction between endpoints that both survive — the same
standard CONTAINS/NEEDS hold. A mover job reparents from pipeline A's file to
pipeline B's file across generations while both pipelines and the job survive
(pipeline A keeps a stayer job so it stays a reconciled DEFINES_JOB source); the
DEFINES_JOB(A -> mover) edge retracts and DEFINES_JOB(B -> mover) is written. A
probe confirmed DEFINES_JOB is the only still-uncovered edge type the offline
canonical writer creates (CONTAINS/NEEDS already covered); every other
retractable edge type is reducer-materialized (code-call, inheritance,
repository-relationship, cloud, IAM, SQL, taint) and is not reachable through the
offline canonical-writer tier, so covering those needs a reducer delta-replay
harness, tracked separately.

No-Regression Evidence: this change adds cassette gitlab facts, a `go` live test,
and its `-run` wiring only — no production projection code. On the pinned
NornicDB `timothyswt/nornicdb-cpu-bge:v1.1.9`,
`scripts/verify-replay-tier.sh` proved DEFINES_JOB(pipelineA -> mover) retracts
to count=0 while pipelineA, pipelineB, and the mover job all survive (count=1)
and DEFINES_JOB(pipelineB -> mover) is written (count=1), all live tests green.

No-Observability-Change: no metric, span, log, worker, lease, or status field is
added; the tier reuses the existing canonical writer phase-group telemetry.

No-Regression Evidence: this change adds cassette facts and offline-tier live
tests only; no production projection code changes. On the pinned NornicDB
`timothyswt/nornicdb-cpu-bge:v1.1.9`, `scripts/verify-replay-tier.sh` proved the
new K8sResource label and the doomed GitlabJob/GitlabPipeline instances retract
to count=0 (survivor GitlabJob remains count=1) in a 4s tier run, all live tests
green. No queue or row-count contract applies to the single-writer offline tier.

No-Observability-Change: no metric, span, log, worker, lease, or status field is
added; the tier reuses the existing canonical writer phase-group telemetry.

No-Regression Evidence: `projector.ExtractEntityRows` is a thin exported wrapper
over the existing unexported `extractEntities`; it adds no new production
projection logic and is called only by this offline test tier, so the
reducer/projector graph-write path is byte-unchanged. The offline tier's new
`content_entity` case routes those facts through that same mapping. Proof on the
pinned NornicDB `timothyswt/nornicdb-cpu-bge:v1.1.9`: `scripts/verify-replay-tier.sh`
wrote gen1 (81 content_entity labels present, count=1 each) then gen2, and the
production `entity_retract` phase removed all 81 (count=0 each) in 0.11s over 85
statements; total tier wall-clock 5s, all four live tests green. No queue or
row-count contract applies because the tier is a single-writer offline replay.

No-Observability-Change: the tier reuses the existing canonical writer spans,
metrics, and phase-group logs (`phase=entity_retract`); no metric name, label,
span, worker, lease, batch, or status field is added. Operators diagnose entity
retract through the existing canonical phase-group telemetry.

## Cassette fact-kind decode disposition

`materialization.go`'s `rowFromPayload` helpers read the cassette's synthetic
fact kinds (`git.repository`, `git.directory`, `git.file`,
`git.gitlab_pipeline`, `git.gitlab_job`, and `content_entity`) as raw
`map[string]any` payloads
rather than through the `sdk/go/factschema` typed-decode seam other
consumers migrated to under epic #4783 (Contract System — Full Integration).
This is deliberate, not deferred debt: those fact kinds have no real
collector producer and no factschema family, so there is nothing to decode
against. See
[ADR: replay/offlinetier Cassette Fact-Kind Exemption (#4866)](../../../../docs/internal/design/4866-offlinetier-cassette-exemption.md)
for the full rationale, the exempt fact-kind list, and the requirement that
the future W3a raw-payload ratchet gate (#4800) allowlist this package by
name instead of flagging it.

## Files

- `materialization.go` — the cassette `fact` -> `CanonicalMaterialization` seam
  (the only production-facing code in the package).
- `delta.go` — the multi-generation/tombstone delta materialization seam (R-17).
- `offline_tier_test.go` — the offline mapping test plus the env-gated live tier.
- `delta_tier_test.go` / `delta_tier_live_test.go` — the R-17 offline structural
  checks and the env-gated real-backend retraction/supersession/idempotency tests.
- `executor_test.go` — the driver-backed executor adapters (singleton + the
  phase-group write path that mirrors production NornicDB).
