# AGENTS.md - internal/ifa/throughput guidance

## Read first

1. `doc.go` and `README.md` here.
2. `go/internal/ifa/AGENTS.md` — the parent Ifá contract-layer invariants.
3. `go/internal/ifa/amplify.go` and `slots.go` — the amplifier seam and the
   adopted scale-lab slots this runner drives.
4. `go/internal/replay/concurrentreplay` — the P2 driver; `go/cmd/ifa/drive.go`
   is the durable-backend counterpart to this in-memory runner.

## Invariants

- Amplify only through `ifa.AmplifyAtSlot`. Do not call
  `synth/gcp.GenerateMultiScope` directly and do not add a generic
  scope_id/stable_fact_key rewrite — the family-aware disjointness contract lives
  in the amplifier seam.
- The hermetic path stays credential-free: temp-file cassette + in-memory
  committer, no Postgres, graph backend, or network. Keep the small slot in this
  lane so it can run in the `make prove` common path.
- Assert committed counts, not wall time, for the hermetic gate. Wall time is
  reported informationally; a wall-time assertion would flake on CI.
- Committed totals MUST be worker-count invariant. If a change makes them vary
  with `-workers`, that is a driver concurrency defect to root-cause, not a
  tolerance to widen.
- Reuse `ifa.ScaleSlot.Enforcement` for the slot's class. Do not introduce a
  second perf contract or redefine the scale-lab latency thresholds here.
