# #5393 Phase 0 Measurement Results

Phase 0 hard gate for the branch-aware coverage epic (#5393), required by
the owner decision (2026-09-08) before any Phase 1 build. A throwaway shim
injects N synthetic refs and records the fact-count and projection-time
delta with the per-repo and fleet caps applied.

Shim (throwaway, delete after the caps are decided):

- `go/internal/collector/repo/git/phase0_pinned_ref_shim_test.go`
  (collection lane: real git fixture, production snapshot + fact stream)
- `go/internal/projector/runtime/phase0_candidate_projection_cost_shim_test.go`
  (projection lane: candidate cost on default-kind scopes with fake writers)

The projector `ref_scope_gated` refusal is observed, never tripped: the
measurement lane uses default-kind scopes only, and the fail-closed gate
stays pinned by `TestRuntimeProjectRejectsRefScope`.

## How to reproduce

```bash
cd go
go test ./internal/collector/repo/git/ \
  -run 'TestPhase0DefaultOutputIgnoresPinningConfig|TestPhase0RefFactDeltaAtN|TestPhase0CapAdmission' \
  -count=1 -v
go test ./internal/projector/runtime/ \
  -run 'TestPhase0CandidateProjectionCost' -count=1 -v
```

Measured 2026-10-17 on the Phase 0 worktree over base `2efe709bc`.
Wall times are host-local observations, not budgets.

## Collection lane: fact rows at N refs

Fixture: one git repo, 12 Go files on `main`, 4 synthetic branches each
with small drift (one appended function, one added file). Full snapshot
per checkout through `SnapshotRepository` + drained fact stream.

| N (extra refs) | fact rows | delta vs default |
|---|---|---|
| 0 (default) | 77 | baseline |
| 1 | 158 | +81 (1.05x default) |
| 2 | 239 | +162 (2.10x default) |
| 4 | 401 | +324 (4.21x default) |

Each ref costs one full default-sized snapshot: totals are exactly
additive (`total(N) = default + sum(ref rows)`), so the N× cost model
behind the cap holds with no superlinear blowup on this fixture.

Parse (snapshot) wall time: ~15 ms default, ~61 ms for 4 refs total.
Emit (stream drain) wall time: ~5 ms default, ~20 ms for 4 refs total.
Snapshot + emit CPU is millisecond-scale here; the fleet cost driver is
graph writes, not collection CPU (see budget arithmetic below).

## Projection lane: candidate cost per fact volume

Input mix mirrors the real envelope shapes at 1x/2x/4x volume
(1x = 161 facts: 1 repository + 40 file + 40 content + 80 entity,
one quarter of entities documented).

| facts in | intents out | content records | canonical rows | wall |
|---|---|---|---|---|
| 161 | 20 | 40 | 122 | ~0.4-1.0 ms |
| 321 | 40 | 80 | 242 | ~1.1-2.4 ms |
| 641 | 80 | 160 | 482 | ~2.4-3.2 ms |

Exact scaling asserted in the test: content records equal content facts;
intents grow with constant second differences (20/40/80); canonical rows
grow as `120m + 2`. Queue load comes only from callable-metadata
entities (documented functions, 1 intent each); undocumented code adds
zero queue load. Zero quarantined facts at all volumes.

Projection CPU is ~5 us/fact: negligible next to graph writes.

## No-Regression marker (default path)

With pinning configured for the repo, the default-branch generation is
byte-identical (modulo `indexed_at`, see below) to pinning unset:
same 77 fact rows, same scope identity (`KindRepository`, same scope
ID, no `ref` metadata key).

- Default fact rows: 77
- Default output fingerprint: `2588629bd087995e...`
  (sha256 over FactID-sorted envelopes, `indexed_at` normalized out,
  checkout path scrubbed; run-local otherwise)

Finding for future proofs: `Payload.indexed_at` is stamped at parse
time, so two runs seconds apart differ ONLY in that field (verified by
diffing full envelopes: FactID, GenerationID, scope, and all other
payload content are deterministic). Any Phase 1 byte-identity proof
must normalize `indexed_at` the same way.

## Budget arithmetic (uses the epic's stated constants)

The epic states the bottleneck is graph writes at ~2,800 facts/sec and
~3,900 facts/repo on average. Applying the measured 1.05x-per-ref
factor to those fleet constants:

- One extra ref per repo: ~+3,900 facts, ~+1.4 s projection per repo
  per cycle on the bottleneck. This confirms the epic's "roughly
  doubles projection time" estimate for one extra branch.
- Per-repo cap 3 (current default): worst case ~+11,700 facts,
  ~+4.2 s per fully-pinned repo per cycle.
- Projection CPU at ~5 us/fact adds ~20 ms per ref at fleet scale:
  irrelevant next to graph-write seconds. The cap decision rests on
  the graph-write budget, not CPU.
- Queue: only callable-metadata entities enqueue (~25% of entities
  here); content writes are 1 record per content fact, linear.

## Cap recommendation

- Keep per-repo cap 3 as the Phase 1 default. The data supports it:
  cost is linear and additive with no superlinear term, and 3 refs
  bound the worst case to ~4x rows per repo.
- Leave the fleet cap at 0 (unlimited) for now. Setting it needs two
  inputs Phase 0 does not have: fleet size x deployment-named refs
  (only Phase 1 ref extraction reveals real ref demand) and the
  fleet's spare graph-write budget per cycle.

## What remains (not measured here)

- Real-corpus scale: the fixture is 13 files / ~80 facts vs the
  ~3,900-fact average repo. Linearity is proven; absolute fleet
  seconds above are arithmetic, not a fleet run.
- Live graph-write timing against a real backend (needs the
  live-gate lock protocol and fixed-port lanes).
- Fetch + worktree-add cost per ref per cycle (network; the shim
  snapshots offline worktrees, so fetch is excluded by design).
- Steady-state anchor-skip savings (unchanged branch tips skip
  re-indexing); only full-snapshot cost is measured here.
- Webhook/polling cost for the tracked-ref set (Phase 1 scope).
