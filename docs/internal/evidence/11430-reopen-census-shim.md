# #11430 reopen census: wake-site split (waiting-wake and supply-chain fanout convicted)

Date: 2026-11-25. Lane: drive/11430-drain-bottleneck (leaf child, read-only).
Tree: branch `drive/11430-drain-bottleneck`; no production code touched.
All clocks UTC; census window ~13:11-14:11 plus a ~14:15 follow-up.
Reads: `sh /tmp/qa-read.sh <file>` (15s statement timeout, read-only txn);
SQL files lived in /tmp only, never in the tree.

## Verdict

Two wake sites clear the ~150/hr suppression-shim gate; the shim is
warranted for both. Everything else is exonerated by volume.

- **CONVICTED (1): waiting-wake-shaped churn on `advisory_group_projection` —
  ~750-820/hr.** Retry-ing rows with `reopened_at` in the window: 655 (first
  pass) / 819 (follow-up); 508/819 still hold rows in
  `reducer_waiting_items`. Zero `fanout_origin` stamps, zero fingerprint
  rows. Mechanism caveat: both waiting wakes and retouch write
  `reopened_at` on retrying rows, so this bucket is wake-or-retouch; the
  park-row join (62% still parked) favors wake-reclaim-repark cycling, but
  the split needs the suppression-count shim to settle.
- **CONVICTED (2): supply-chain fanout on `supply_chain_impact` — ~240-290/hr.**
  291 reopened/hr, of which 250 carry the fanout stamp
  (`payload ? 'fanout_origin'`, follow-up: 241/264 pending rows stamped,
  0 parked). The stamp is written only by
  `reopenSupplyChainImpactHolderQuery`, so attribution is exact.
- **EXONERATED: maintenance replay.** Gated domains barely reopened this
  hour (aws_drift 2, container_identity 1, deployable_unit 0), and the
  changed-input rate on that set is 100% changed, 0% wasted: both drift
  rows recorded a stale generation vs the scope's active generation; the
  one identity row is a single-epoch set. Small n — gate verification, not
  proof — but there is no wasted-reopen signal here.
- **EXONERATED: producer-activation settle.** Same tiny rows as above
  (drift 2 + identity 1, all gated-changed); nothing at volume.
- **NOT CONVICTED: completion wake.** 8 `cross_scope_replay_required`
  debt rows point-in-time (all `supply_chain_impact` claimed) plus an
  unattributed `advisory_group_projection` pending-90 remainder (no stamp,
  no park, no fingerprint). Below the gate; remains the residual bucket.

## Wake paths and their QA-visible traces (code, this tree)

`ReopenSucceededReducerSetClause`
(`go/internal/storage/postgres/reducer_queue_replay.go`) never touches
`reason`/`payload`, so bare `ReopenSucceeded`/`ReplayDomain` reopens leave
no per-site stamp — only `reopened_at` + `status='pending'`. Per-site
attribution therefore rests on:

- Supply-chain fanout: `payload.fanout_origin`
  (`PayloadKeyFanOutOrigin`, `go/internal/reducer/supplychain/core/fanout.go`),
  written by `reopenSupplyChainImpactHolderQuery`
  (`go/internal/storage/postgres/supply_chain_impact_fanout_reopen.go`) on
  the succeeded→pending move (and merged on widenings, which do not move
  `reopened_at` — so `reopened_at`-in-window + stamp = genuine fanout reopen).
- Waiting wakes: keep `status='retrying'`
  (`waitingWakeReopenSetClause`,
  `go/internal/storage/postgres/reducer_waiting_store.go`; winners
  watermark variant sets `reopened_at` to the covering stamp). Never
  succeeded→pending, so they are disjoint from the pending-reopen buckets.
  Post-hoc trace is the `reducer_waiting_items` park row per `work_item_id`;
  the live rate lives in `eshu_dp_reducer_waiting_wake_total{site,outcome}`.
- Producer-activation settle (`ingestion_producer_activation.go:320`) and
  maintenance correlation reopen (`ingestion_reopen_correlation.go`,
  domains from `CrossScopeCorrelationReopenDomains()`): bare `ReopenSucceeded`,
  distinguishable only by domain + the changed-evidence side tables
  (`aws_drift_reopen_fingerprints`, `container_identity_reopen_epochs`,
  `deployable_unit_reopen_fingerprints`) joined per `work_item_id`.
- Completion wake (`scope/completion/fanout.go`, migration 093 ACK trigger):
  succeeded consumers get `reopened_at`; claimed/running get durable
  `cross_scope_replay_required` debt. The events table is deleted on
  processing, so post-hoc there is only the debt flag and the unattributed
  remainder.

## Commands and observed values

1. Window: 2026-11-25 13:11:23+00 to 14:11:23+00.
2. Reopen volume (q1, `reopened_at` in window, stage reducer): 1059 total —
   retrying 662, pending 360, succeeded 25, claimed 9, superseded 3. The 25
   still-succeeded rows completed a full reopen→rerun→succeed cycle inside
   the hour (drain works for a trickle).
3. By domain (q2): advisory_group_projection 752 (retrying 655, pending 90,
   claimed 7); supply_chain_impact 291 (pending 270, succeeded 15, retrying
   5, claimed 1); everything else ≤ 4.
4. Fanout stamp (q3): supply_chain_impact 250/291 stamped; all other domains 0.
5. Same-row reopens vs fresh inserts (q4): 1059 reopened_at-in-window vs 252
   fresh (`created_at` in window AND `reopened_at IS NULL`), overlap 16.
   Churn dominates intake ~4:1.
6. Fingerprint coverage (q5): only drift (2/2) and identity (1/1) reopened
   rows have side-table rows; both hot domains have none.
7. Changed-input (q6a/q6b): drift 2/2 recorded on a stale generation
   (changed); identity single recorded epoch (n=1). Wasted-reopen rate 0%.
8. Replay debt (q7, point-in-time): 8 claimed supply_chain_impact rows flagged.
9. Waiting-shaped (q8): 663 retrying + `reopened_at`-in-window, 344 still
   parked (first pass; follow-up q9: 819 retrying, 508 still parked, 0 stamped).
10. Follow-up split (q9): sci pending 264 (241 stamped, 0 parked);
    advisory pending 90 (0 stamped, 0 parked); sci succeeded 15 (0 stamped).

## Decision

Proceed to the suppression-count shim for the two convicted sites only:
(a) waiting-wake-shaped advisory_group_projection retrying churn,
(b) fanout-stamped supply_chain_impact pending reopens. No implementation
in this step. The pending-90 advisory remainder stays open as the
completion-wake residual until the shim accounts for it.
