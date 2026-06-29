# replay/offlinetier — agent scope

## Owned surface

- `go/internal/replay/offlinetier/` — the R-5 offline replay gate tier.
- `testdata/cassettes/replayoffline/` — the cassette this tier replays.
- `scripts/verify-replay-tier.sh` — the single-container NornicDB runner.

## Non-negotiable invariants

- The live tier MUST run against a REAL NornicDB via
  `runtimecfg.OpenNeo4jDriver` + real Cypher. NEVER substitute an in-memory
  fake, stub, or mock graph. A fake makes the gate worthless because the
  backend-specific bugs it guards (#4019 nested-directory drop, commit-time
  MERGE races, NornicDB MATCH quirks) only surface on the real engine.
- When the backend env/flag is unset the live tier MUST `t.Skip` cleanly. It
  MUST NOT pass against a fake or pass silently with no backend.
- The tier MUST drive the production `storage/cypher.CanonicalNodeWriter`
  unchanged. Do not reimplement projection logic here; the package only maps
  cassette facts to `projector.CanonicalMaterialization`.
- The writer MUST be driven through the NornicDB phase-group write path
  (`livePhaseGroupExecutor`, which exposes `ExecutePhaseGroup` but NOT
  `ExecuteGroup`). Driving it through the full-atomic `GroupExecutor` path is
  the Neo4j path, not production NornicDB, and silently drops the directory
  CONTAINS edges once the schema's uid indexes exist — the #4019 bug class.
- `livePhaseGroupExecutor` MUST stay a FAITHFUL mirror of the production
  `cmd/ingester` `nornicDBPhaseGroupExecutor`, not just a thin `ExecuteGroup`
  wrapper. Two production behaviors are load-bearing on NornicDB and MUST be
  reproduced (see #4186): (1) strip the `_eshu_*` diagnostic params that
  `annotateCanonicalWritePhases` injects (via `cypher.SanitizeStatement`) before
  any statement reaches the driver — an unreferenced param on a grouped
  `DETACH DELETE` makes the delete silently no-op; (2) run an all-retract phase
  SEQUENTIALLY as per-statement auto-commit `Execute` (mirroring
  `executeSequentialRetractPhase`), NEVER as one grouped transaction. Omitting
  either masks correct gen2 directory retraction and reds
  `TestDeltaTombstoneGraphTruth`.
- Cleanup MUST run before AND after the write (DETACH DELETE by repo identity)
  so re-runs are deterministic.
- The assertion MUST fail when the projection writes nothing (no false green).
  The depth-2 CONTAINS edge is the regression guard.
- Do NOT touch `scripts/verify-golden-corpus-gate.sh`; the Compose B-7 gate
  stays as the full-corpus belt-and-suspenders check.
- For the R-17 delta/tombstone scenarios (`delta.go`): a tombstoned fact MUST be
  removed by the production retract phase (driven by `FirstGeneration=false`),
  NEVER written into the gen2 materialization as a surviving row (that would
  resurrect it). Retraction MUST be proven on the REAL backend
  (`delta_tier_live_test.go` reads back count=0 for tombstoned nodes); the
  offline structural test cannot delete a node and must not claim it does. Keep
  the broken-retraction negative control (`FirstGeneration=true` leaves the node
  present — the #3859 class) intact and honest.

## Skill routing

- `eshu-golden-corpus-rigor` for the cassette + projection-truth assertions.
- `cypher-query-rigor` for any read-back query or backend dialect change.
- `concurrency-deadlock-rigor` if the executor's transaction grouping changes.
- `golang-engineering` for Go edits and tests.
- `eshu-diagnostic-rigor` for backend behavior diagnosis and proof runs.

## Verifying a change

```bash
export GOCACHE="$(git rev-parse --show-toplevel)/.gocache"
cd go && go test ./internal/replay/offlinetier/ -count=1   # offline + skip
# real backend (Docker required):
../scripts/verify-replay-tier.sh
```
