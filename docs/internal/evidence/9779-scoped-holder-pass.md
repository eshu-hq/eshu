# #9779 scoped one-hop holder pass: theory proof and local evidence

## Decision

Build the scoped pass. The two blocking theories measured on 2026-11-08
against the QA read replica (PostgreSQL 18.6-eshu-7957, 5,805,740 dependency
rows) and the current main text:

- **Manifest read narrows now.** #9611 landed the per-name LATERAL probe
  (#10146). Re-ran the issue's shim with the shipped query text, 5 names
  against 400 names, alternating, on the replica:
- **One-hop rerun density at 1x is ordinary churn, but the fan-out volume is
  real.** 1,255 reducer `supply_chain_impact` item updates touched 1,237
  scopes in the last hour (median 1, max 6 passes for one scope; the max-6
  scope shows no stamp, i.e. generation churn, not a fan-out storm). At the
  same time 453 items carry a `fanout_origin` stamp, 338 of them pending
  rerun. Each of those reruns a full 12-second pass today. The cost the
  scoped pass removes is measured, not inferred.

| names | planner buffers | execution | shared buffers |
| --- | --- | --- | --- |
| 5 | 3,973 hit | 28.1 ms | ~13.1k |
| 400 | 3,973 hit | 2,867.2 ms | ~1.54M |

Five names cost 0.9% of the buffers of 400 names: the cost now follows the
requested names, so a scoped pass that asks for one CVE's packages pays
~28 ms of manifest read instead of ~11.8 s. The gate the issue set (under
10% of the buffers, under 1 s) passes.

The measured QA baseline for the after comparison: 115 supply_chain_impact
passes per hour, p50 12.1 s, p99 21.5 s, 2.9% over the 40 s evidence budget.
The post-rollout wave read (pass_total mode share, reopened+replay_flagged
vs executions, top-k scopes by passes per hour from the new scope_id log
field) repeats these numbers; the burst before/after on the same fixture is
a rollout-gated measurement and is recorded there, not here.

## What the change does

- The reopen stamps the sorted, de-duplicated, capped changed-CVE set on
  `fanout_origin` (`*` above 200, no `cves` key on enqueues and set-less
  reopens). The in-transaction go-modules fence path keeps its set-less
  stamp and still runs full.
- A stamped pass compares the CVEs it derived (every row it wrote plus
  retracted copies) with the stamp after its writer commit and before its
  acknowledgement; disjoint clears `cross_scope_replay_required` under a
  lease-and-stamp fence, so the ack succeeds instead of rerunning. A newer
  stamp, the sentinel, a set-less stamp, a truncated load, or continuation
  debt rides to the next claim. A lost lease stays an error.
- A one-hop pass whose stamp names a bounded set re-derives that set only:
  evidence filters derive from the stamped envelopes plus the finding-row
  seeds, the until-stable follow-up intersects every round with the scope,
  and the writer upserts and retracts inside the stamp. The sentinel, a
  set-less stamp, and an owed retry run full.

## Local proof (this tree)

- `TestReopenHolderStampsSortedCVEIDsLive`: reopen stamps the sorted
  de-duplicated set.
- `TestEnqueueMissingHolderStampsNoCVEIDsLive`: the establishing pass stamps
  no set and runs full.
- `TestClearReplayFlagLive`: the RED/GREEN pair — seen stamp clears, stale
  stamp is a no-op that keeps the flag, the ack returns the item to pending,
  and the rerun consumes the newer stamp and succeeds. A stolen lease is an
  error.
- `TestListScopedFindingPackagesLive`: the seed read returns distinct
  stamped-CVE package identities for the live generation only.
- `TestScopedFindingsMatchFullFindingsForStampedCVEs`: over a two-CVE
  holder fixture, scoped findings for the stamp are byte-identical to the
  full pass's, while cross-scope loads narrow (6 admitted envelopes full,
  3 scoped).
- `TestScopedWriteMatchesFullWriteLive`: over two identical generations, the
  scoped pass writes byte-identical rows for the stamp (pass identity
  redacted), retracts the stale in-stamp copy, and leaves the out-of-stamp
  stale copy byte-identical while the full pass retracts it.
- Unit pins: stamp cap/sentinel, disjoint/overlap/sentinel/partial clears,
  scope-decision legs (sentinel, set-less, retry, owed all run full),
  writer filter, derived-ids retraction scoping.

## Performance Evidence:

**Slow layer.** The holder rerun cost the issue measured: manifest
dependency load (43.9% of p50 worker time, ~11.8 s of the 40 s evidence
budget under a 400-name custom plan) plus cross-scope active evidence load
(42.5%). Both derive from filters over everything the scope holds; a
one-hop rerun for a 1-CVE change reloads all of it.

**Baseline.** QA reducer at image `sha-93b1af01538` (current main, carries
#9765 and #9611): 115 passes/hour, p50 12.1 s, 338 stamped items pending
rerun. Manifest shim numbers above (replica, current query text).

**After (local).** The fixture differential admits 3 cross-scope envelopes
scoped vs 6 full for the same stamped findings; the row differential shows
identical stamped rows. A wall-time burst before/after on the same fixture
is not meaningful locally (stub loaders have no I/O cost) and is owned by
the post-rollout wave read, which compares these QA baselines against
`pass_total{mode}` share, reopened+replay_flagged vs executions, and
duration percentiles.

**Why safe.** No new hot-path scans for full passes: the seed read runs only
for stamped one-hop passes, the conditional clear only for stamped passes,
both single indexed statements fenced like the acknowledgement; all other
passes skip both. No worker, batch, lease, retry, or serialization change.
The retraction statement gains one equality predicate on the existing
`cve_id` key. Rollback is a deploy revert: no schema migration, no new
payload keys the old code cannot read (an old reader ignores `cves`).

## Observability Evidence:

`eshu_dp_supply_chain_impact_pass_total{domain,mode}` (emitted once per pass
that completes its evidence load), sub-signals `scoped_cve_count`,
`scoped_evidence_envelopes`, and `fanout_replay_cleared` on the reducer
success line, plus a `scope_id` (`generation_id`, `source_system`) field on
that line so a bounded LogQL top-k aggregates passes per scope per hour.
Coverage row `projection (supply-chain)` in
`telemetry-coverage-reducer-domains.md` names the stages; `scripts/verify-telemetry-coverage.sh`
passes at this head. The rollout trigger the issue specified —
`reopened+replay_flagged` (holder fan-out outcomes) against
`eshu_dp_reducer_executions_total`, plus the `pass_total` mode share — reads
from these signals; no dashboard panel was added, matching the existing
fan-out signals.

## Suppression-branch bridge fix (follow-up to #10403)

The merged head predated the fix, so the contention gate caught a real
starvation: scoped holder passes ran one load round with zero suppression
rows and left copies active. The bridge (own id, upstream-key, or
finding-row deciding advisory admission; stamp keys seed the upstream set)
restores the arms the full pass follows.

## No-Regression Evidence:

**Baseline (pre-fix head).** Nine suppression live legs red with one
signature (single load round, suppression branch zero rows, empty
follow-up); the 260-holder mass leg converged 4 of 260 copies.

**After (this head, disposable PostgreSQL 18.6, local `pg-repro`).**
All nine legs green: the CVEID-only reopen, coalesced chain,
pre-activation defer, unchanged-set, diff-retry, rejected/restored
advisory, and identical-rescan legs, plus the 260-holder mass
continuation at 260 accepted_risk (first drain 54 s, concurrent 4-worker
drain 6 s, 256 reopened + 4 deferred). The scoped-vs-full differentials
stay byte-identical (3 admitted envelopes scoped vs 6 full, identical
stamped rows). Full `core` package suite plus `-race` green; `go vet`
and gofumpt clean.

**Input shape.** Single-holder suppression reopens plus the 260-holder
mass continuation with a newer-generation activation mid-drain.

**Why safe.** Scoped loads are strict subsets of what the full pass
loads: every follow-up arm intersects a fixed stamp-bounded seed set, so
the loop terminates and the distro id stays off the full CVE arm (pinned
by unit test). Full passes, workers, leases, queues, and retries are
untouched. Rollback is a deploy revert with no migration.

## Observability Evidence:

No new signals. The bridge is observed through the existing
`eshu_dp_supply_chain_impact_pass_total{domain,mode}`,
`scoped_cve_count`, `scoped_evidence_envelopes`, and
`fanout_reopened`/`fanout_deferred` sub-signals plus the per-branch
`active_evidence_branch_*_rows` tallies, all named in the row above and
the coverage row for this stage.
