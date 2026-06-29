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

### R-6 — credentialed cassette refresh workflow (PR #4108)

The refresh workflow keeps committed cassettes fresh against real provider APIs
without a contributor needing live credentials locally. It is label-gated:
applying the `[refresh-cassettes]` label to a PR triggers a hosted-runner job
that builds each collector binary, runs it with `-mode=record`, and opens (or
force-updates) a PR with the canonical diff against the committed fixtures.
`workflow_dispatch` is also supported for operator-initiated one-shot refreshes.

**Label gate rationale.** A credentialed run that contacts real AWS/k8s/GCP
APIs must be an explicit operator action. Triggering on `pull_request: types:
[labeled]` with an `if: github.event.label.name == 'refresh-cassettes'` guard
ensures no credential is consumed by an unlabeled PR push or an accidental
merge. This is consistent with the repo's other credentialed workflows that
are either operator-only or remote-only.

**Canonical-diff legibility.** Because the recorder writes canonical output
(sorted keys, sorted arrays, volatile fields collapsed to sentinels) the diff
against the committed fixture is always minimal: re-recording an unchanged
provider API produces an empty diff; a single fact-field change produces one
changed line. Reviewers can reason about exactly what changed in the provider
API without wading through timestamp churn or ordering noise. This property is
proved offline in `go/internal/replay/refreshworkflow` (no credentials, no
network, no Docker) by `TestCanonicalDiffIsLegible`, which asserts that
altering one payload field produces exactly one changed line in the diff.

**Redaction.** The recorder calls `replay.Canonicalize` with
`WithRedactedKeys` for any collector whose fact payloads can carry credential-
bearing fields; the HTTP boundary is redacted separately by the input tape
(R-4). Committed cassettes therefore never contain live secrets regardless of
what the provider API returned. The redaction property is proved offline by
`TestRedactionNeverLeaksSecrets`, which asserts that the configured secret
value does not appear anywhere in the canonical output and that the redaction
sentinel is present in its place.

**Injection safety.** All GitHub Actions context values that flow into shell
`run:` steps are bound to environment variables at the step boundary, never
interpolated directly into the shell script. The PR-body trigger line is built
from those env vars so an attacker cannot inject shell commands via a PR number
or event name.

**Files shipped by R-6:**

| File | Purpose |
| --- | --- |
| `.github/workflows/refresh-cassettes.yml` | Label-gated CI workflow |
| `go/internal/replay/refreshworkflow/` | Offline proofs (canonical-diff + redaction) |
| `scripts/test-cassette-refresh-workflow.sh` | Static mirror test for the workflow contract |

## 7. Phase 2 — remaining integration levels

R-7 (parser-fixture flavor), R-8 (API/query response replay), R-9 (MCP tool-call
replay).

## 8. Phase 3 — contributor packaging

R-10 (contributor conformance packaging — the 5-command flow), issue #4112.

The framework ships as an out-of-tree onboarding surface so a contributor can
prove a collector's extraction **with zero provider credentials and zero
Docker** before it is ever a candidate for the monorepo split:

- **Starter spec** (`go/conformance/testdata/starter-spec.yaml`) — the
  contributor-facing twin of the B-12 golden snapshot, parsed via
  `sigs.k8s.io/yaml` into the same `goldengate.Snapshot` struct, so one contract
  serves both the in-repo JSON snapshot and the contributor YAML spec.
- **Starter tape** (`go/conformance/testdata/starter-cassette.json`) — a small
  `hello-eshu` cassette, schema-valid against the R-3 cassette JSON Schema,
  regenerated via `replay record` (`-mode=record`).
- **Conformance suite** (`go/conformance`) — `go test ./conformance -count=1`
  replays the tape offline, derives the projected graph observation in memory,
  and asserts it against the spec.
- **5-command README** (`go/conformance/README.md`) — clone → run → edit spec →
  record tape → re-run.

**Shared assertion core, no forked logic.** The pure assertion layer (the
`Snapshot` contract, the `Finding`/`Report` accumulator, and every `Evaluate*`
function) was extracted out of `cmd/golden-corpus-gate`'s `package main` into the
importable `go/internal/goldengate` package. Both the in-repo gate (via
`shared.go` aliases) and the conformance suite import it, so the contributor's
credential-free proof and the in-repo gate assert against the identical logic and
cannot drift.

**#4047 readiness proof.** The conformance suite is the automated proof the
#4047 collector-extraction readiness checklist points to for the "deterministic
local and CI proof passes without provider keys" criterion: a green
`go test ./conformance` run shows a collector's facts project the
node/edge/correlation truth the spec demands, reproducibly, with nothing
installed but Go. The in-repo `golden-corpus-gate` and the
`internal/replay/offlinetier` real-NornicDB tier remain the live-pipeline and
real-backend halves of the same proof.

Convergence scenarios (R-11..R-18) fold into the same shared core as they land:
each is a cassette/spec the conformance suite can carry without new assertion
code.

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
  points prove idempotent restart and recovery. Implemented in
  `go/internal/replay/crashreplay/`: a `DurableStore` (the Postgres
  `fact_work_items` + lease analog) survives a recovered-panic crash injected at
  a clean boundary (`CrashBeforeClaim`) or the dirty post-lease-pre-complete
  window (`CrashAfterApply`); recovery lapses the held lease on the R-12 clock,
  reclaims under a higher fencing token, and replays the remainder, asserting the
  recovered canonical snapshot equals the no-crash snapshot with zero
  double-completions.

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
- **Conflict-key mutual exclusion under simultaneous pending claims.** When two
  *pending* siblings on one key are claimed by genuinely simultaneous workers
  before either lease commits, at most one acquires a live lease.

**Resolved (#4137, completing #3558):** the single-claim path originally had a
TOCTOU window here — under READ COMMITTED the `NOT EXISTS` live-sibling check did
not see a concurrent *uncommitted* sibling claim, and `SKIP LOCKED` locked the
two distinct sibling rows, so both were claimed. The batch claim already avoided
this by restricting its candidate to the per-conflict-key representative row; the
fix gives the single-claim candidate the same restriction, so all claimers
converge on one row and `SKIP LOCKED` serializes it. The gate now asserts this
case directly (looped, `-race`).

## 11. Phase 5 — fidelity and the bug classes unit tests can't see

- **R-16 (#4125) — deterministic cost-counting assertions** (N+1 / quadratic
  blowup net; complements Epic B wall-clock perf).
- **R-17 (#4126) — delta / multi-generation / tombstone correctness** (the
  held-pending-retract class, #3859 / #2340).
- **R-18 (#4127) — schema-version compatibility replay** (old cassette vs new
  code, never silent-wrong).

## 12. Coverage completeness (epic #4172) — the gate that keeps the library full

The framework (this epic, #4102) is the replay *player*; epic
[#4172](https://github.com/eshu-hq/eshu/issues/4172) builds the *tape library* and
the *coverage proof* on top of it. The player can replay anything, but scenarios
have been authored for only a fraction of the supported surface (≈9 of 21
collector binaries have a cassette, ≈2 of 17 parsers an R-7 fixture). The value is
not realized until coverage is complete, and stays complete.

### 12.1 C-1 — the keystone coverage gate (#4173)

`go/cmd/replay-coverage-gate` (logic in `go/internal/replaycoverage`) is the
keystone. It enumerates every surface Eshu claims to support from four
machine-readable source-of-truth registries and reports any surface lacking a
green replay scenario:

| Registry | Required surface | Scenario type |
| --- | --- | --- |
| `surface-inventory.v1.yaml` | collectors on the **implemented** readiness lane | cassette |
| `fact-kind-registry.v1.yaml` | each distinct `read_surface` | api/mcp golden |
| `parser-backing-ledger.v1.yaml` | each parser | parser fixture |
| `capability-matrix.v1.yaml` | each positively-claimed capability | claim / refusal |

A curated manifest (`specs/replay-coverage-manifest.v1.yaml`) maps each surface to
the scenario that exercises it — the natural keys differ across registries and
artifacts (the `collector:aws` surface is exercised by the cassette under
`testdata/cassettes/awscloud`), a mapping no single registry can express. The gate
**composes with** the existing capability-inventory drift gate (it reuses the same
generated registries) and **reuses** the golden-corpus gate's advisory→blocking
`goldengate.Finding`/`Report` machinery.

Coverage is **existence-only**: the gate proves a scenario artifact exists and is
wired, not that it passes. Greenness is proven by the sibling gate named in each
manifest entry's `proof_gate` (golden-corpus-gate, the parser fixture tests). That
split keeps the coverage gate static, credential-free, and Docker-free while never
claiming a green it did not observe.

It ships **advisory**: a coverage gap is reported (and emitted in a JSON
coverage-report artifact for the C-7 dashboard) but does not fail CI. Its red
output **is** the C-2..C-6 backfill worklist; its eventual green **is** "we cover
everything we said we support." A single `-blocking` flag flips every uncovered,
unresolved, and stale finding to required, applied once C-2..C-6 burn the gaps
down so coverage never regresses.

### 12.2 Children

C-1 first (keystone, advisory/red). Then C-2..C-6 in parallel (bulk backfill —
collectors, parsers, correlations/edges, API+MCP surfaces, capability claims; each
flips a slice of C-1 green). C-7 (coverage dashboard) builds alongside C-1 on the
same coverage-report artifact. Flip C-1 to blocking last.
