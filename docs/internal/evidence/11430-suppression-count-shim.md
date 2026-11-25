# #11430 suppression-count shim: exact-equality gate suppresses ~0/hr — STOP, flat

Date: 2026-11-25. Lane: drive/11430-drain-bottleneck (leaf child, read-only).
Tree: branch `drive/11430-drain-bottleneck`; no production code touched.
Prior: the [reopen census](11430-reopen-census-shim.md) convicted two wake sites
(waiting-wake `advisory_group_projection` ~750-820/hr, supply-chain fanout
`supply_chain_impact` ~240-290/hr) and left the waiting-wake bucket as
wake-or-retouch. This shim counts, per site, how many reopens an exact
fingerprint-equality gate would have suppressed with zero input change, reusing
only the existing machinery (reopen fingerprints, partition memo gate, emit
gating — read first, no new paths). All clocks UTC; QA reads via
`sh /tmp/qa-read.sh <file>` (15s statement timeout, read-only txn);
SQL files lived in /tmp only, never in the tree. Each query's window is
`reopened_at >= now() - interval '1 hour'` at its own execution (~14:25-14:45).

## Verdict

FLAT. Provably suppressible reopens: **~0/hr on both convicted sites**, against
the ~200/hr gate-design bar. STOP: no gate design in this lane. Gross volumes
are sustained (advisory ~720-910/hr, sci ~230-250/hr) but every counted reopen
is either genuine-change-implied by its writer's contract or a first delivery
with no recorded fingerprint — and the accuracy guard (missing fingerprint
reopens once and records, never suppresses) forbids suppressing either.
Repeat-zero-change deliveries (duplicate fanout, recovery-rerun retouch,
repeat spurious wake) are the only shape an exact-equality gate could take,
and post-hoc single-state reads cannot distinguish a repeat from a first:
`reopened_at` holds one value, there is no reopen history table, and the
events table is deleted on processing. So 0/hr is provable; the remainder is
genuine-or-unprovable, not suppressible-by-count.

## Machinery reviewed (code, this tree)

- Reopen fingerprints: `aws_drift_reopen_fingerprint.go` (ARN snapshot),
  `container_identity_reopen_gate.go` (fleet epoch),
  `relationship_store_completion_wake.go` (deployable_unit / workload /
  service-catalog fingerprint tables + missing-as-always-reopen treatment).
  Neither hot domain has a fingerprint table.
- Partition memo gate: `ingestion_reopen_partition_memo_gate.go` — same-pass
  skip-set only, nil skip-set reopens everything; covers
  deployment_mapping / code_import_repo_edge only.
- Emit gating (already suppresses the zero-change share before it becomes a
  reopen): unchanged rederivation fans out nothing, the queued-holder
  prefilter drops covered holders (`fanout_prefilter.go`), covered queued rows
  are widened without touching `reopened_at` (`supply_chain_impact_fanout_reopen.go:127`),
  and the winners-watermark wake stamps `reopened_at` with the covering
  watermark, not wall time (`reducer_waiting_winners_wake.go:53`), so a woken
  row passes the next claim recheck instead of reclaim-repark cycling.
- Retouch (`reducer_queue_retouch.go`, sole production caller
  `supply_chain_impact_advisory_group_enqueue.go:179`): preserves status and
  wait rows, advances `reopened_at`. The touch names groups from
  `TouchedGroupIDs`, which are groups of rows the pass CHANGED
  (`impact_write_contract.go:108-116`) — except a recovery/owed rerun, which
  re-enqueues every recovery group even with no change
  (`group_projection_enqueue.go:27-28`).
- Holder mapping is consumption-based (cve_id / advisory_id arms,
  `supply_chain_impact_fanout_key_range.go`): a stamped holder consumes the
  stamped CVE in its facts, so the stamped change is in the holder's input set.

## Advisory waiting-wake bucket: split settled, suppressible 0/hr

Distinct-row split of retrying reopens (s2f, n=722; the anchor discriminator
is exact here — every parked row in the window waits on `winners_watermark`
only, no mixed-kind rows; wake deletes the wait row, retouch preserves it):

| shape | stamp_age | n | claimed since reopen |
|---|---|---|---|
| retouch-while-parked | fresh | 338 | 0 |
| wake-draining-unparked | fresh | 285 | 0 |
| wake-then-reparked | stale (watermark) | 53 | 53* |
| unparked-scheduled-retry | fresh | 42 | 0 |
| wake-then-reparked | fresh | 4 | 3 |

\* weak signal: `reopened_at` is the old watermark, so any later claim counts.

No `generation_active` waits appear in this window — the bucket is
winners-watermark wakes plus retouch, not the generation-active wake. Zero of
the draining/retouch/scheduled rows were claimed since reopen (backlog depth,
consistent with the pick-order lane). Retouch implies a committed pass changed
the group (per the contract above); wakes are first notifications with no
recorded fingerprint, so the guard reopens. Recovery-rerun retouches are the
only zero-change retouch shape and are not isolable post-hoc. The 53 stale
reparked rows are wake→claim→repark cycles, but an intervening retouch cannot
be ruled out (claim also writes `updated_at`), so they are not provably
zero-change either.

## Supply-chain fanout: stamps are real changes, suppressible 0/hr

- s1: 230 pending reopens/hr, 214 stamped; 15 succeeded unstamped (success
  clears the stamp path); 1 claimed stamped. Stamp samples (s6) name a real
  source scope+generation+intent with a cves set (here the `["*"]` unbounded
  sentinel = full-pass delivery, fail-closed to full re-derivation).
- s4 (all-time current rows): pending 8293, 5778 stamped, 0 stamped-never-
  reopened; retrying 487, 413 stamped, 19 stamped-never-reopened (flag/widen
  path absorbed those without reopening). Every counted reopen is a genuine
  succeeded→pending move carrying a named source change.
- s5: 1150 hot-domain reopens in the window, 0 rows in any fingerprint
  side-table (drift / identity / deployable_unit). Coverage is exactly zero,
  so an equality gate built on existing machinery suppresses nothing today.

## Hermetic leg (focused unit tests, this tree)

`go test ./internal/reducer/supplychain/core/ -run
'TestUnchangedRederivationFansOutNothing|TestRetryFansOutEvenWhenTheRewriteChangedNothing|TestPrefilterDropsOnlyQueuedHoldersAndTheCapAppliesAfterIt'`
→ ok (1.0s). `go test ./internal/storage/postgres/ -run
'TestApplyReopenPartitionMemoGate'` → ok (4.7s). The zero-change fanout
suppression and the memo-gate reopen-all-on-nil behavior are pinned, not
assumed.

## Commands and observed values

1. s1 fresh volumes/hr: advisory 912 (retrying 720, pending 188, claimed 4,
   0 stamped); sci 246 (pending 230/214 stamped, succeeded 15/0, claimed 1/1).
2. s2f advisory retrying split (distinct): table above, total 722.
3. s3 advisory pairs (superseded by s2f): pending 188 all unparked; claimed 6.
4. s4 sci absorption: pending 8293/5778 stamped/0 never-reopened;
   retrying 487/413/19 never-reopened.
5. s5 fingerprint coverage: 1150 hot reopens, 0/0/0 side-table rows.
6. s6 stamp samples: `{scope_id, generation_id, intent_id, cves:["*"]}` —
   real deliveries, sentinel = full pass.
7. s7 workload_materialization (out of scope, see below): 255 pending +
   475 succeeded + 6 claimed/hr, 0 parked, 0 stamped.

## Decision

STOP after the count. Suppressible by exact fingerprint equality with zero
input change: ~0/hr provable on both sites (all counted reopens are
genuine-change-implied or missing-evidence first deliveries, which the
accuracy guard reopens). 0/hr << ~200/hr, so no gate design follows from this
lane. A future gate would need per-domain input fingerprints for
advisory_group_projection and supply_chain_impact — new paths, explicitly out
of scope here — plus reopen history to tell repeat zero-change deliveries
from first ones.

## Out-of-scope observation for the coordinator (NOT_CHECKED)

`s7`: `workload_materialization` churned ~730/hr in the same window (255
pending + 475 succeeded reopened, 0 parked, 0 stamped) — above the 200/hr
line, absent from the census 2.5hr earlier (≤4/hr then). Mechanism, stamps,
and whether its gated completion wake
(`workload_materialization_reopen_fingerprints`,
`relationship_store_completion_wake.go`) covers this churn are NOT_CHECKED
by this lane; the lane's scope was the two convicted sites only.

## Not covered

- Repeat-vs-first delivery distinction for any site (needs reopen history or
  per-domain fingerprints — new paths).
- Outcome of the 285 draining advisory rows (succeed vs re-park); with ~35hr
  queue depth most will still be draining for hours, so a short follow-up
  would not resolve them.
- workload_materialization churn (see above) and the advisory pending-188
  residual beyond noting it is unparked/unstamped.
