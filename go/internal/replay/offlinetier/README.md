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

## Files

- `materialization.go` — the cassette `fact` -> `CanonicalMaterialization` seam
  (the only production-facing code in the package).
- `delta.go` — the multi-generation/tombstone delta materialization seam (R-17).
- `offline_tier_test.go` — the offline mapping test plus the env-gated live tier.
- `delta_tier_test.go` / `delta_tier_live_test.go` — the R-17 offline structural
  checks and the env-gated real-backend retraction/supersession/idempotency tests.
- `executor_test.go` — the driver-backed executor adapters (singleton + the
  phase-group write path that mirrors production NornicDB).
