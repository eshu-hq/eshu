# #10876 theory proof: set-aside waiting work with in-transaction wake

Refs #10876. Part of #10860 (phase 2, step 2).

Prove-the-theory-first shim for #10876. No implementation here: numbers
from a throwaway live-Postgres shim (deleted after the run) plus the
designs of the two proof tests the implementation lane will build.
Method follows the #10863 step-0 gate shim: disposable Postgres 18-alpine
on loopback, rolled-back `EXPLAIN (ANALYZE, BUFFERS)`, 2 warmups + 7 reps,
medians; wall-clock txn timings interleaved 30 reps.

Seed: `seedReducerQueue10862Replay` (8,713 waiting rows: 7,475 pending +
1,238 retrying, 600 scopes) widened to the #10863 QA shape: 4,000 scopes,
7,400 generations, 43,711 `fact_work_items` (35k terminal). QA waiting mix
per domain is in `reducer_queue_10862_replay_test.go`.

## (a) Wake in the provider transaction: cheap when keyed narrowly

The wake shape mirrors `activateWithConsumerWake` (#7597): provider save,
then a waiting-set UPDATE in the same transaction. Provider-shaped save
used here: one phase-commit upsert on `graph_projection_phase_state`
(the save behind the largest deferral family, `canonical_nodes_committed`).

| Wake fan-out | EXPLAIN median | Buffers | Txn wall delta over bare provider save (121 us) |
| --- | --- | --- | --- |
| Scope-pinned (3 waiting rows, one scope) | 0.078 ms | 71 | +107 us (wake), +149 us (wake + #7597-style lock SELECT) |
| Domain-wide (982 advisory rows, all scopes) | 15.8 ms | 22,498 | not run in-txn (see below) |

Verdict: PROVEN for narrowly keyed wakes. A scope-pinned wake adds
~0.1 ms and ~70 buffers to the provider commit, the same order as the
shipped #7597 wake (which touches zero rows in the common case and stays
in every activation transaction today).

The domain-wide row is the design constraint, not a rejection: a coarse
wake (e.g. the advisory winners watermark rebuild waking ~1k rows across
scopes) costs ~16 ms and 22k buffers inside the provider transaction and
widens the lock footprint across scopes. So wait keys must be at least as
narrow as (scope, generation, reason), and coarse providers must wake
through the completion-event loop (`CrossScopeCompletionRunner`), never
with a row-by-row in-transaction UPDATE. This is the concrete form of the
#10861 reject criterion ("starting work in the same step makes saves wait
on each other"): the wake takes no blocking lock outside its keyed waiting
set (`SKIP LOCKED` + `SET LOCAL lock_timeout` per #7597), and the provider
row it shares the transaction with is its own phase/generation row,
disjoint from the claimant work-row to generation to consumer lock order
(`AGENTS.md`, "Activation wake (#7597)").

## (b) Claim cost with waiting rows set aside vs ranked: neutral

Shipped single and batch claim texts, unmodified, over three queue states
(same seed, same-method pairs; 1,238 deferral-shaped rows):

| Waiting-row shape | Single claim ms / buffers | Batch claim ms / buffers |
| --- | --- | --- |
| R (today): retrying, future `visible_at` | 13.1 / 13,027 | 49.4 / 14,446 |
| S1 (status set-aside): `status='waiting'` | 12.8 / 13,036 | 48.9 / 14,464 |
| S2 (separate table): rows moved out | 12.8 / 13,042 | 49.0 / 14,476 |

Verdict: PROVEN neutral. Both claim paths cost the same to within noise in
all three shapes. At QA depth the claim is dominated by fixed
per-statement CTEs (the #10863 stale gate over 4,000 scopes, the readiness
requirements), not by the 1,238 waiting rows, which the status index skips
cheaply either way.

Two consequences for the implementation lane. First, the set-aside wins
nothing on claim cost and loses nothing either: the gain is eliminating
wasted handler runs (32% of runs deferred today, Decision 4 target <5%),
not cheaper hand-out. Second, status-vs-table (arbiter question 2) cannot
be settled on claim cost; settle it on lock contention (a separate table
keeps waiting rows out of every claim-row lock) and DDL parity cost.

NOT_CHECKED: batch fairness ranking under a live drain (the #10863 replay
covers the claim side; the drain side belongs to the implementation lane).

## (c) Proof-test designs (not implemented)

### Both-orders equivalence test

Table-driven over every row of the deferral table below. Live Postgres,
operator-gated like the other live queue tests, real `Claim`/`Fail`/`Ack`
plus the real provider store transaction.

- Order 1 (waiter first): claim C, handler reports the gate miss, C waits
  (set-aside in the new shape). Commit P's providing save. Assert the wake
  reopens C (claimable, `visible_at` NULL, wait not counted as a try).
  Claim C, run to success. Record the terminal row image and a hash of the
  facts C emits.
- Order 2 (input first): commit P's save, then claim C. It runs first try.
  Record the same image and hash.
- Assert byte-equal emitted facts and equal terminal state modulo
  timestamps; `attempt_count` equal across orders (the wait never counts).
  Handler executions: order 1 runs once after the wake (today it burns one
  extra wait-run; the test pins the new shape at zero wasted runs).
- Interleave (lost-wake proof, after `TestActivationWakeInterleaveLive`):
  commit P while C's set-aside transaction is in flight, both orders of
  commit-vs-wait-registration. Assert exactly-once execution: C is never
  stranded and never runs twice. The waiter-registers-after-provider-commits
  race must resolve by re-checking the wait key inside the set-aside
  statement (same pattern as the #7597 Fail-path wake).

### Missed-wake audit test

- Plant a waiting row whose wait key is already satisfied: commit the
  provider save through raw SQL that bypasses the wake path (simulates a
  crash between provider commit and wake), plus a second plant where the
  waiter registers after the provider committed.
- Run the audit sweeper at a short test interval. Assert it reopens both
  plants within N audit cycles, emits the missed-wake counter and the
  per-reason oldest-age gauge, and leaves a normally woken row untouched
  (execution count stays 1: the audit never double-starts).
- Bound proof: worst-case detection is 2 audit cycles plus one claim
  latency. Production interval 5 min gives the 10-minute bound Decision 4
  requires; the test pins the formula with a small interval.
- RED/GREEN guard shape: the audit passes on a clean tree (no false wakes)
  and reopens the plants (seeded violation fails without the audit).

## Deferral table (input to criterion 1)

Grouped from `nonCountingReducerRetryFailureClasses`
(`reducer_queue_readiness_sql.go`, 41 entries; the "#5046 seventeen"
comment counts 19, stale but not load-bearing) plus the timer mechanisms.
Each row: what waits, what it waits for, which save provides it
(mappings marked ~ are code-reading synthesis for the lane to confirm
per pair).

| Waits | Waits for | Providing save (wake site) |
| --- | --- | --- |
| 12 materialization domains on `canonical_nodes_committed` (requirements VALUES) | Producer's canonical nodes + phase commit | Phase-commit upsert txn |
| secretsiam / k8s / gcp-rel / ec2-identity / aws-rel / azure-rel / iam x4 / obscoverage / rds / secgroup / workload-cloud-rel / uses-profile / block-kms / ec2-exposure / s3 x3 (~19 in-handler endpoint/node gates) | Named upstream nodes or endpoint | Respective producer's phase commit or Ack |
| crossscope producer-not-ready (ci_cd_run, supply_chain_impact) | Producer scope activation | Producer Ack txn |
| aws drift write-superseded + state-pending; supply-chain write-superseded | Fresher admission / state snapshot | Drift writer txn / ingester state commit |
| advisory winners watermark (~982 rows, domain-wide) | Winners maintainer rebuild (30 s) | Maintainer commit; coarse fan-out, event loop per (a) |
| cloud admission drain; value-flow inputs fence | Sibling Ack / sink-chain writes terminal | Their Ack / completion |
| crossrepo backward evidence; deployable-unit corr x2; workload mat resolution + deploy-source; shared-edge target; service-catalog x2; security-alert impact | Named evidence commit or activation | Corresponding writer / activation txn |
| generation-activation freshness | Projector Ack activating the generation | Ack txn |
| 3 cross-repo kinds (already event-driven) | Completion event | `CrossScopeCompletionRunner` fan-out (keep) |
| resolved-relationship consumers (already in-txn) | Relationship generation activation | `activateWithConsumerWake` (keep, extend pattern) |
| Stored maintenance obligations (10 s poll) | Time, not an input | Keep the timer; out of scope for wakes |
| `reducer_readiness_waits` ledger (migration 110) | Elapsed-time bound anchor | Ledger, not a wait; keep |

## Environment notes

Disposable `postgres:18-alpine` container on 127.0.0.1:55499, removed
after the run. Read-only QA queries: none needed (the #10862 seed and
evidence notes carried every figure). No shared infra touched. Shim file
`zz_10876_shim_live_test.go` deleted before this note was written; nothing
to review in `go/`.
