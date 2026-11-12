# NVD Recovery Ruling — 2026-11-12 (arbiter-eshu)

Recorded by coordinator; arbiter does not edit files. Source: arbiter
subagent verdict on `nvd-decision-brief-2026-11-12.md` (incl. §10).

## Decisions

1. **Chosen option: A+B together. Neither alone passes.** Measured walk
   floor is `(pages-1) x 6s` pacing plus latency/marshal/commit. Head
   window `totalResults=4566` (arbiter probe ~14:15Z) = 92 pages at the
   live anchor's `results_per_page=50` = ≥546s floor — deterministically
   over the ~300s envelope before any latency. A alone at 12h: worst
   measured slice 1691 records ≈ 260–270s, ~10% headroom. A at **6h**:
   worst slice ≤ ~900 records ≈ 140s, comfortable. B alone: 600s likely
   still fails this window; 900s fits today and breaks at the next
   density spike. So: **A = 6h chunk for NVD derivation only** (planner
   default `nvd_window_planner.go:23` is a const; derivation struct
   `nvd_window_derivation.go:19` has no width config — add config or
   change const, executor's choice) **+ B = raise the NVD window claim
   envelope to 600s** with lease TTL raised coherently above it. B only
   after pinning the exact firing knob: host deadline is
   `LeaseExpiresAt` (`request.go:63` → `source.go:185`); raising an
   unrelated timeout is a no-op and will be rejected at proof.
   Sequencing: if a timed live head-window walk proves ≤ ~750s, B-first
   (900s interim) may unwedge while A ships; otherwise ship A+B in one
   lane. Do not keep interim 900s as steady state once A lands.

2. **Dimension rationale.** Accuracy: A tiles every instant; B changes
   no content. Performance: A multiplies minted scopes 8× — trivial vs
   OSV volume; total pages unchanged except bounded prefix-refetch
   amplification (measure in soak). B costs zero when healthy. Pacer
   ceiling ~30k records/h vs ~1.7k/48h arrival: 100× headroom; envelope
   fit, not rate, is the defect. Reliability: A shrinks units, B
   absorbs spikes to ~4900 records/6h; shed + `ErrNVDNoProgress`
   retryable keeps residual stalls honest. Security: a key IS
   configured but the pacer ignores keyed tier — "key fixes duration"
   is false today; keyed/anonymous must become a boolean log fact,
   never log the key. Concurrency: single-anchor invariant unchanged;
   do NOT parallelize windows on one cursor. A+B also ends the retry
   burn monopolizing the pacer and one worker. No serialization, no
   index: 0 ungranted locks / 0 blocked on primary and replica.

3. **Shed-skip is NOT accepted permanent loss. Backfill owed (E).**
   Conditions: (a) exactly 2 partial NVD windows live — backfill both
   by re-walk + diff, or prove by replay their sheds were
   boundary-duplicate only; (b) going forward, shed instants must be
   durable and replannable, or the soak completes with shed tally
   exactly 0 — until (b), any shed is a soak failure. E is scoped to
   the 2 windows now plus durability work; does not block A+B.

4. **C rejected outright** (timeout-skip would silently skip 4566
   records). **D rejected** (contradicts #9309's parity purpose).
   Durable root-cause follow-on (REQUIRED issue, not a recovery
   blocker): page-size/pacing reform — 2000/page = 3 pages, 6.4MB
   largest page vs 8MiB cap, ~1s/page; needs oversize-page fallback
   proof before adoption. Earlier 500/page rejection on #10446 stands.

5. **D4 soak bar (amended).** (i) pre-soak: timed live walk of the
   current head window recorded + 3(a) disposed; (ii) clock is 48h wall
   time starting only after cursor catches to within one chunk of
   catalog head (lag gauge, not row-state `fresh`); (iii) during soak:
   zero NEW `failed_terminal` nvd/_window items, no 10-attempt
   exhaustion, lag < 2× chunk at ≥hourly samples with two stable end
   reads, shed tally 0 or backfilled in-soak (else reset), p95 ≤ 50%
   of envelope and max < envelope, zero `invalid_result` vuln-intel
   terminals, other feeds fresh; (iv) density: ≥1 soaked window with
   `totalResults ≥ 2500`, or recorded replay of the 4566-record head
   window through QA satellite. Lifting D4 = "QA green" only; #9309
   stays open for the production port/readback.

## Required proof

Theory shim (arbiter-measured 4566/1408/1464/1691/3) re-run at final
settings on rebuilt/deployed binary; failing regression first for A;
shared golden vectors green in both trees; claim-plane trace
grant→deadline naming changed knobs (lease > deadline > walk,
budget unchanged); concurrency proof (fencing, no same-cursor
parallelism, retry/dead-letter unchanged); telemetry (lag gauge,
keyed boolean, shed tally, soak samples with exit codes); mint
cadence + drain ETA stated before soak (6h drain of ~28d lag is days
if tick-bound ~30min — say so honestly).

## Will not do

No C, no D, no cursor hand-edits, no index, no worker serialization,
no page-size raise without oversize fallback, no concurrent
same-cursor windows, no treating the 2 partial sheds as known-safe
without replay diff.

## Brief corrections applied

Retry cadence ≈330s (not ~30s); density-not-slowness (4566/92 pages,
API 1.0–1.7s/page); live digest = v0.1.12 by crane labels (satellite
main 5 ahead, docs/tests only); cursor = 2026-10-15 (Nov 10 is last
commit wall time); pg_stat sample-dependent but 0 locks/0 blocked
holds; 13 = 10 non_retryable + 3 invalid_result; tally counts-only;
300s knob unpinned (activation lacks it; envelope ≈300s
corroborated).
