# replay/schedulereplay — agent scope

## Owned surface

- `go/internal/replay/schedulereplay/` — the R-13 Layer 3 ordering gate.

## Non-negotiable invariants

- The gate asserts **delivery-order invariance** of the converged graph truth.
  The same work items, in any scripted order (in-order, reverse, rotated,
  duplicates), MUST converge on a byte-identical `Graph.Canonical()` snapshot.
- It MUST drive the **real** `reducer.Service` loop via the in-memory
  `ScheduledWorkSource` (which implements both `reducer.WorkSource` and
  `reducer.BatchWorkSource`). Do not bypass the reducer loop — exercising the
  real claim/execute/ack path (sequential AND concurrent batch) is the point.
- The in-memory canonical graph is the **subject** of the order-invariance
  assertion, NOT a fake of the real backend. Backend-specific projection
  correctness (the #4019 class on real NornicDB) stays owned by
  `replay/offlinetier`'s real-backend live tier. Do not move that concern here,
  and do not claim this gate proves backend correctness.
- The gate MUST keep its teeth: the order-sensitive-applier test MUST observe a
  snapshot divergence. If a refactor makes the buggy applier converge, the gate
  is worthless — fix the harness, do not delete the negative test.
- Inputs MUST come from a real committed cassette through the
  `cassette.Source` → `offlinetier` materialization seam, never synthesized
  inline, so the work items track recorded fact shapes.
- It MUST stay credential-free: no Postgres, no graph backend, no Docker. The
  gate runs in the default `go test` pass.

## Skill routing

- `concurrency-deadlock-rigor` for the work source, the reducer loop drive, and
  any change to the concurrent batch path.
- `eshu-golden-corpus-rigor` for the snapshot/cassette assertions.
- `golang-engineering` for Go edits and tests.

## Verifying a change

```bash
export GOCACHE="$(git rev-parse --show-toplevel)/.gocache"
cd go && go test -race ./internal/replay/schedulereplay/ -count=1
```
