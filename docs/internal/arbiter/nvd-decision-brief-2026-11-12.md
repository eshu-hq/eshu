# NVD Decision Brief — 2026-11-12 (for arbiter ruling)

Goal: pick the best-in-class NVD recovery across ACCURACY, PERFORMANCE,
RELIABILITY, SECURITY, CONCURRENCY. Evidence cutoff ~13:55Z. No production
identifiers; all state is the granted QA cluster acting as the prod surface.

## 1. Live status (measured, read-only)

- `vulnerability_source_states` where source=`nvd`: last success
  2026-11-10 23:20:32Z; 13 `failed_terminal` (last Nov 8), 1
  `failed_retryable` (attempting ~every 30s, `launch_failure`), 33
  `fresh`, 2 `partial`.
- Pinned head window: `vuln-intel://nvd/_window/1792081032213000-1792253832213000`
  (48h, start == stuck NVD cursor 1792081032213000).
- Same 48h width succeeded ~every 30min through Nov 10 (~1.3–1.7k
  results each). GHSA/OSV/EPSS/CISA-KEV/CVE-list all fresh today.
- Live satellite: v0.1.12 (digest 6a48b683, matches origin/main pin,
  ArgoCD app for the QA deployment Synced/Healthy). Contains the #77 shed fix.
- `pg_stat_activity`: no lock waits (idle + short active only). NOT a
  Postgres contention/plan/index problem — see §4.

## 2. Edge map (every hop a window claim traverses)

```text
tick
 └─ coordinator deriveNVDWindowTarget          [1]
 │    48h window minted deterministically from cursor;
 │    same scope re-planned while cursor pinned
 └─ workflow claim (lease, 300s claim timeout, 10-attempt budget)  [2]
 └─ satellite HTTPProvider: NVD walk (6s pacer, 50/page, 30s/req)  [3]
 │    48h walk measured 244s on Nov 9
 └─ output budget gate: 10k facts / 2MiB; over-budget groups shed
 │    whole with tally; all-fat -> ErrNVDNoProgress (retryable)    [4]
 └─ host result validation: reassembled vs declared total          [5]
 └─ commit: source-state row + cursor advance to emitted bound     [6]
 └─ reducer newest-copy projection; readiness NVD-leg query       [7]
```

[1] `go/internal/coordinator/vulnerability/nvd_window_derivation.go`
    (`deriveNVDWindowTarget`, `planNVDWindow`), chunk default 48h
    (`nvd_window_planner.go:23`), max 30d.
[2] `chart/.../app-values.yaml` (`claim_timeout_seconds: 300`);
    attempt-10-exhausted `launch_failure` observed in
    `workflow_claims` (terminal for claim rows).
[3] satellite `nvd_client.go:23` (`nvdDefaultTimeout 30s`),
    `nvd_pagination.go`; Nov 9 measurement: 1553-CVE/32-page walk
    took 244s (issue #9309 comment).
[4] satellite `nvd_output_budget.go` (10k facts / 2MiB),
    `nvd_window_split.go` (`collectNVDWindowSplitFacts`,
    `ErrNVDNoProgress`: retryable, never terminal — #10446).
[5] host `invalid_result` ("reassembled N facts, want declared M")
    seen on `component-work` terminal rows today.
[6] cursor advances ONLY on commit; shed generations commit the
    emitted prefix (`complete=false` + budget warning code).
[7] `newest_copy_10042_test.go` family; readiness GHSA-leg pattern
    mirrored for NVD (`vulnerability_intelligence_nvd_window_validation.go`).

## 3. Failure-mode timeline (what changed)

- Nov 8: 11× `non_retryable` over-budget terminals + `invalid_result`
  rows — the wedge. Fix #77 (shed-with-tally, v0.1.9) addressed it.
- Nov 9–10: flow restored, 48h windows committing ~1.6k results.
- Nov 10 23:20Z → now: head window `launch_failure` (context
  deadline), retried ~30s cadence, zero commits. New mode, not the
  wedge: fetch duration now exceeds the 300s claim envelope
  (244s walk + fan-out/marshal/commit), OR NVD API slowed further.

## 4. Classification (eshu-postgres-rigor + diagnostic-rigor)

Queue retry loop + handler-duration symptom. NOT lock wait (none in
`pg_stat_activity`), NOT plan/index (no slow-query evidence; GHSA
proves the shared path fast). Do NOT add an index; do NOT serialize
workers. Rejected: "Postgres slow", "satellite not deployed"
(v0.1.12 live+synced), "wedge persists" (mode changed — see §3).

## 5. Accuracy cost to rule on

Shed groups advance the cursor past whole lastModified instants whose
facts never commit (tally counts them; no backfill path found in the
files read). GHSA has the same mirror precedent. Question for the
ruling: is shed-skip accepted permanent loss, or is a backfill owed?
Live magnitude is countable from `adoptNVDShedTally` observations.

## 6. Options (tradeoffs in §7)

- A. Narrower NVD windows (e.g. 48h → 12/6h chunk for NVD only).
- B. Longer claim timeout for the NVD scope (300s → measured p99 + headroom).
- C. Cursor-advancing handling for timeout-pinned windows (commit
  empty/tally + advance, mirroring the shed philosophy; needs the
  accuracy answer from §5 first).
- D. Accept NVD-stale: record permanent gap on #9309, keep #10446
  open as known-limitation, close D4 as will-not-lift.
- E. (If §5 says loss is unacceptable) backfill shed instants before
  any of A–C.

## 7. Dimension notes for the arbiter

- ACCURACY: A keeps full fidelity (smaller chunks still cover every
  instant); C risks silent gaps; D/E hinge on §5.
- PERFORMANCE: A multiplies ticks (~4× claims for 12h) — measure
  claim-plane load first; B costs nothing when healthy.
- RELIABILITY: B alone may just move the cliff; A+C is the robust
  pair (smaller units + no pin forever).
- SECURITY: no key rotation involved; anonymous-tier pacing stays;
  note `externalsecret-vuln-intel-nvd-collector.yaml` exists in the
  overlay — confirm whether the live walk even uses a key before
  tuning around rate limits.
- CONCURRENCY: deterministic same-scope re-plan already fences
  duplicates; narrower windows must keep single-anchor invariant
  (`deriveNVDWindowTarget` two-anchor refusal).

## 8. Cheapest first measurements (prove-theory-first)

1. Time one live 48h NVD walk now (throwaway, off-host if possible)
   to confirm duration-vs-300s as the binding constraint.
2. Confirm key-vs-anonymous tier on the live walk (decides whether
   a key fixes duration without code).
3. Count shed-tally magnitude in recent observations (sizes §5 loss).

## 9. Explicit asks

1. Rule A vs B vs C vs D (+E conditional) with dimension rationale.
2. Answer §5 (shed-skip: accepted loss or backfill owed?).
3. State the soak that lifts D4 (48h fresh + zero terminals still
   the bar, or amended?).

## 10. Deep-research fold-in (2026-11-12 ~15:00Z, one workflow, 5 lanes)

CORRECTION 1 — cursor coverage: the cursor VALUE is 2026-10-15, head
2026-11-10 23:20Z, lag ~26d. Earlier lane comments saying "stale since
Nov 10" conflated last-success timestamp with coverage: NVD coverage
is ~26 days behind, not 2. Correction posted on #10446.
CORRECTION 2 — the "300s" figure: no 300s constant in code (per-attempt
bound is `ClaimLeaseTTL`, code default 60s), BUT the live deployment sets
`ESHU_COMPONENT_COLLECTOR_CLAIM_LEASE_TTL=5m` (plus
`ESHU_CANONICAL_WRITE_TIMEOUT=300s`). The brief's 300s envelope stands
for the live env; the code path to verify is lease-expiry SIGKILL.
New edges (inspected, satellite checkout
`/home/ubuntu/personal-repos/eshu-collector-vulnerability-intelligence`):
fetch-side `narrowNVDWindowEnd`/`shrinkNVDChunkWidth` (floor 1h) already
narrows dense windows in-claim — option A must be reframed as
mint-chunk policy (48h default, 30d max) vs the existing fetch
narrowing, and the lease-kill theory must explain why narrowing does
not already save the stuck window; error taxonomy
(`source_state.go:301`, `handler.go`) is coherent: 429 rate-limited,
4xx terminal, 5xx/timeout/decode retryable, `ErrNVDNoProgress`
retryable-never-terminal; CAS-max cursor needs no serialization;
`evidence_sql.go` newest-copy keep predicate is GHSA-only, so
superseded NVD copies stay servable as evidence rows (decision path
guarded via `provenance_selection.go` NVD-rank-last; evidence read
path not) — second accuracy caveat beside §5. `pg_stat_activity`
confirms no lock waits. Unresolved for the ruling: NVD-scoped claim
histogram (claims table has no collector column — needs
`workflow_work_items` join, not run); live lease-kill vs
`expired_lease_budget_exhausted` split; full NVD readiness leg never
executed live.
