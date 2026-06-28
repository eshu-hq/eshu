# 4102 — Deterministic Replay Framework

Status: in progress (epic [#4102](https://github.com/eshu-hq/eshu/issues/4102)).
This document is the design reference for the deterministic replay framework. The
epic issue remains the authoritative source for scope and child tracking; this
file captures the durable design so the in-repo children (#4103–#4127) have a
stable §-anchored reference. Sections 1–9 summarize the model the epic
describes; §10 and §11 detail the convergence and fidelity phases that the
later children build on.

## 1. Goal

Build one deterministic replay framework that lets any contributor — internal or
open-source — prove a change works across every integration boundary with a
single credential-free, Docker-free command, and run that same proof in CI to
catch wiring breakage before merge. It is the golden integration gate for both
Eshu development and the end user.

## 2. The model: two tapes, one chain

```
                    ┌──────────── INPUT TAPE (R-4) ─────────────┐
  real AWS/k8s/git ─┤ recorded HTTP/SDK responses (RoundTripper) │
                    └───────────────────┬───────────────────────┘
                                        ▼
                                  ┌───────────┐
                                  │ COLLECTOR │  ← contributor's new code runs here
                                  └─────┬─────┘
                                        ▼
                    ┌────────── OUTPUT CASSETTE (R-2) ──────────┐
                    │  recorded fact envelopes (canonicalized)   │
                    └───────────────────┬───────────────────────┘
                                        ▼
            reducer → graph (R-5) → API (R-8) → MCP (R-9)   [B-7 golden gate]
```

The output of the input-tape stage (facts) is the input of the cassette stage.
Chained, a contributor proves the whole path end to end with zero credentials
and zero Docker.

## 3. Integration levels covered

| Level | Boundary | Child |
| --- | --- | --- |
| Boundary | external world → collector | R-4 input tape |
| Collect | collector → facts | R-2 cassette recorder |
| Format | fact envelope contract | R-1 core + R-3 schema/validator |
| Project | facts → reducer → graph | R-5 offline replay gate tier |
| Read (API) | graph → HTTP API | R-8 API response replay |
| Read (MCP) | graph → MCP tools | R-9 MCP tool-call replay |
| Parse | source → parser → facts | R-7 parser-fixture flavor |
| Ops | fixture freshness | R-6 credentialed refresh workflow |
| Contributor | the 5-command flow | R-10 conformance packaging |

## 4. Convergence thesis (the golden standard)

A cassette is not just recorded data — it is a scenario: recorded inputs plus a
scripted delivery schedule, scripted faults, and scripted crash points, replayed
deterministically. Layered onto the data plane (Phases 0–2), this brings
boundary failure, retry/idempotency, ordering races, and crash recovery onto the
same golden gate (Phase 4 / §10), credential-free and Docker-free in CI. The
only integration-surface proof that stays outside replay is the real
`FOR UPDATE SKIP LOCKED` claim path under genuine contention (R-15), which cannot
be both deterministic and real — it is kept small and named, not hidden (§10).

## 5. Phase 0 — foundation (is Epic B's B-10)

R-1 (replay core + canonical serialization), R-2 (cassette recorder), R-3
(cassette JSON Schema + offline validator). On Epic B's critical path because
B-10 is a prerequisite for the B-7 golden-corpus gate.

## 6. Phase 1 — boundary + middle tier

R-4 (input tape, `http.RoundTripper` record/replay), R-5 (offline replay gate
tier — runs real NornicDB, never an in-memory fake, so backend-specific bugs
surface), R-6 (credentialed cassette refresh workflow).

## 7. Phase 2 — remaining integration levels

R-7 (parser-fixture flavor), R-8 (API/query response replay), R-9 (MCP tool-call
replay).

## 8. Phase 3 — contributor packaging

R-10 (contributor conformance packaging — the 5-command flow).

## 9. Sequencing

The monorepo split (#4047) happens after all of Epic B (#3741). Epic B's finish
line is the split gate, so the entire framework must land before Epic B closes.
There is no separate split timeline to pace against.

## 10. Phase 4 — convergence: failure + concurrency onto the same gate

Phase 4 turns a cassette into a *scenario* — recorded inputs plus a scripted
fault/delivery/crash schedule — so the same golden gate exercises failure,
retry/idempotency, ordering races, and crash recovery deterministically. This is
Layered Deterministic Simulation Testing for Eshu:

- **R-11 (#4120) — fault-injection tape (Layer 1).** Scripted timeouts and
  partial responses drive retry/idempotency on the boundary; folds the boundary
  half of B-6 (#3799).
- **R-12 (#4121) — deterministic clock seam (Layer 2 enabler).** An injectable
  `clock.Clock` across the reducer queue/lease/reap path replaces raw
  `time.Now()` so lease expiry, claim visibility, and retry backoff are
  controllable. Production injects `clock.System()` (== `time.Now()`); replay
  injects a `clock.Simulated` whose `Advance`/`Set` move time without sleeping.
  Latency-measurement spans deliberately stay on the real monotonic clock.
- **R-13 (#4122) — schedule replay via deterministic `BatchWorkSource`
  (Layer 3).** A deterministic in-memory work source replaces the real claim
  path so ordering races are reproducible. By construction this stops exercising
  the real SQL claim path (see §10.1).
- **R-14 (#4123) — crash-point replay (Layer 3).** Scripted mid-run crash
  points prove idempotent restart and recovery.

### 10.1 The irreducible remainder (R-15, #4124)

Layer 3 substitutes a deterministic in-memory work source for the reducer claim
path. That substitution is what makes ordering and crash replay deterministic —
and it is exactly why replay can no longer test the **real** claim path. The
real path is the production `FOR UPDATE SKIP LOCKED` claim SQL running against
real Postgres under genuinely concurrent workers. It cannot be both
deterministic and real: a deterministic in-memory source does not run
`SKIP LOCKED`, does not expire leases on the wall clock, and does not increment
the per-claim attempt counter at the database.

So one small, nondeterministic, real-Postgres contention check is kept
deliberately as belt-and-suspenders. It is the only integration-surface proof
the golden replay gate does **not** subsume, and it is named here rather than
hidden. The gate (`go/internal/storage/postgres/reducer_queue_contention_gate_test.go`,
CI workflow `reducer-contention-gate.yml`) asserts, with ≥4 concurrent workers
on independent connections against real Postgres:

- **No double-claim.** Distinct work items drained by concurrent claimers are
  each claimed by exactly one worker — the real row-level `SKIP LOCKED` fence.
- **Fencing-token monotonicity.** The per-item `attempt_count` strictly
  increases across claim → expiry → reclaim cycles.
- **Stale-lease reaping under the real wall clock.** A lease left to lapse is
  not claimable while live and becomes reclaimable once it expires in real time
  (distinct from R-12's simulated clock — this is the real-time path replay
  cannot cover).
- **Conflict-key mutual exclusion with a committed holder.** While one item on a
  `(conflict_domain, conflict_key)` holds a committed live lease, concurrent
  workers cannot claim a sibling on the same key.

**Deliberately out of scope of this gate:** conflict-key mutual exclusion when
two *pending* siblings are claimed by genuinely simultaneous workers before
either lease commits. The current `NOT EXISTS` + `SKIP LOCKED` fence has a
TOCTOU window there (the live-sibling check reads a snapshot that may not yet see
a concurrent sibling claim), so asserting it would make the gate flaky. That gap
is tracked as #4137 (a completion of #3558); the committed-holder
case above is the deterministic conflict guarantee. Keeping the gate green and
honest matters more than over-claiming a property the production fence does not
yet guarantee under true simultaneity.

## 11. Phase 5 — fidelity and the bug classes unit tests can't see

- **R-16 (#4125) — deterministic cost-counting assertions** (N+1 / quadratic
  blowup net; complements Epic B wall-clock perf).
- **R-17 (#4126) — delta / multi-generation / tombstone correctness** (the
  held-pending-retract class, #3859 / #2340).
- **R-18 (#4127) — schema-version compatibility replay** (old cassette vs new
  code, never silent-wrong).
