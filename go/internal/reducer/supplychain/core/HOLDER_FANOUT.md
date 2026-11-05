# Holder fan-out and convergence (#9695)

This is the detailed contract behind the "Convergence: the holder fan-out"
bullet in `README.md`. The cost of the in-flight read and the signals an
operator reads are stated in the README bullet "In-flight cost"; measurements
and the defects found while proving it are in
`docs/internal/evidence/9695b-holder-fanout.md`, and the 1.5a probes in
`docs/internal/evidence/9695-advisory-retraction.md`.

The winners table keeps one copy per
canonical key, ordered by `priority_score`, then whether the row carries a
payload finding id, then `fact_id`, and another scope may hold its own copy
(KEV or EPSS enrichment, another advisory source, the operator suppression
scope). A copy only changes when its scope reruns, so a retraction, or its
reversal, used to reach the other copies on that scope's own cadence. Now, after
the writer commit and before the queue acknowledgement, a pass that changed or
retracted rows reopens the `supply_chain_impact` item of every scope that holds
an active copy of those CVEs (`fanout.go`, `writer_changed.go`, and the store in
`storage/postgres/supply_chain_impact_fanout.go`). The reopened pass
re-derives its copy, so a retracted CVE reads `advisory_retracted` in a holder's
copy once that holder's next pass runs, and a restored one returns each copy to
its pre-retraction state: `active`, or hidden for an operator's decision when
one applies. There is no precedence rule in the winners query. A holder's lag
is queue latency plus one holder pass plus one winners resweep, and a CVE with
more than 256 holders drains across claims.

## Rules

- A row is changed when it is new, revived, or its `provenance.content_digest`
  differs, and so is a finding the baseline had that this pass lacks. The
  baseline is the generation's own rows on a rerun and the scope's previous
  generation, matched by stable key, on the first pass of a new generation, so
  a rescan that derived the same findings changes nothing. The digest hashes
  the typed payload without `intent_id`, `cause`, `generation_id`, and
  `evidence_fact_ids`, and includes `suppression_state`. Rows written before
  the digest existed count as changed once.
- Holders come from one indexed read per 200 changed CVEs
  (`fact_records_supply_chain_impact_lookup_idx`), the asking scope excluded,
  the operator scope included, plus the scopes whose item at their active
  generation is claimed or running (`fact_work_items_stage_domain_status_idx`),
  read first: a pass in flight may have loaded evidence before this pass's facts
  were visible, and a new generation with no finding rows is invisible to the
  row read. In this order a holder that acknowledges between the reads has
  committed its rows.
- One hop: only the attempt a fan-out created is one hop (attempt 1; the reopen
  and the replay both reset the count to 0). That pass re-derives its copy,
  fans out nothing, and consumes the stamp. A stamp that reaches a later attempt
  was written while a genuine pass ran and failed, so the retry is genuine; an
  owed marker also makes a pass genuine. A continuation is not a hop: a stamped
  drain claim drains the stored list.
- A claim reopens at most 256 holders; the rest is stored as `fanout_remaining`
  on the committing item and drained by its next claims.
- Owed marker: when a commit changed rows, the writer sets `fanout_owed` on the
  pass's own item inside the same transaction, fenced on the lease and the
  attempt, so a pass that lost its lease cannot commit. The fan-out clears it
  when the last holder is reopened. A retry that finds the marker knows an
  earlier attempt committed and fans out for every CVE the scope names; a retry
  after a failure before the commit fans out only its own changes.
- A failed holder read, in-flight read, reopen, or state update fails the pass
  with a retryable error of class `supply_chain_impact_fanout_failed`; the
  queue retries it, then dead-letters it.
- Known limit, not introduced here: a scope that loaded its evidence before
  the source's facts were visible, derived no finding row for the CVE, and
  acknowledged before the in-flight read is not a holder; nothing reruns it
  until its own next generation. Current collectors can do this (KEV, EPSS, and
  alert scopes derive their own copies); the source scope's pass derives the
  finding from the same evidence, so the missing copy does not hide the
  finding.

## Enrichment refresh (#9878)

KEV and EPSS changes reach existing findings through this same edge, with no
enrichment-specific code path. An unchanged KEV catalog or EPSS answer is
skipped upstream by the collector content-digest hint (#9694), so it creates
no generation and no re-trigger wave. A changed one commits a generation in
its enrichment scope; that scope's pass re-derives its copies from the new
vintage (EPSS selection is newest `score_date` first, never magnitude), the
digest flip names the CVE in `ChangedCVEIDs`, and the fan-out reopens every
other scope holding an active copy. Each holder re-derives only its own
copy; the CVE-to-scope mapping is used solely to enqueue re-runs, never to
write another scope's rows. Until the holders drain, the winners table can
serve an older copy whose stale vintage outscores the fresh one: that window
is the freshness contract, bounded by queue latency plus one holder pass per
holder scope plus one 30-second winners resweep tick. The fan-out counter
carries the committing generation's evidence family as `cause` (`epss`,
`kev`, `advisory`, `suppression`, `package_evidence`, `mixed`, `other`), so
an enrichment wave is separable from an advisory or suppression one;
`eshu_dp_supply_chain_impact_winners_dirty_log_oldest_age_seconds` and the
watermark age bound how far the read model lags the converged copies.

Two enrichment residuals follow from the rules above. A delisting (KEV
removal, EPSS drop) converges through the retraction side of the changed set,
so a partial-evidence pass, which retracts nothing and proves no removal,
does not propagate it until a later complete pass. And a newer EPSS
`score_date` with unchanged probability still flips the digest (the selected
date is in provenance), so holders re-derive once and land on identical
scores: correct, at the cost of one holder pass each.

## Manifest dependency changes (#9874)

A git generation that changes a manifest or lockfile holds none of the twelve
projector trigger kinds, so before #9874 a new or removed pin waited for an
advisory scope to rerun. The projector now enqueues one marker intent for every
git generation without one of those kinds (payload `trigger: manifest_dependency`,
entity key `supply_chain_impact:<scope>`, so a 500-pin push is one intent; a
generation that changed no dependency costs one marker pass that reopens
nothing). `Handle` sends
it to `handleManifestTrigger` before the fencing token and the evidence load
(`manifest_trigger.go`). The marker pass writes no findings. It:

1. Relies on the reducer runtime's generation guard first: under the default
   runtime `Runtime.GenerationCheck` defers a pending generation before Handle
   with the shared, non-counting `generation_activation_not_ready` class
   (retried until the generation activates or turns terminal, then skipped as
   superseded) and skips a superseded one, so the marker branch only sees
   active generations (`TestRuntimeDefersAManifestMarkerBeforeTheHandler`).
   The branch re-checks with the shared generation freshness check, read
   through `ManifestDependencyGenerationReader` on the fan-out port: active fans
   out; superseded, reachable only when a newer generation activates in the
   gap, succeeds without work; pending is a defensive arm for a runtime without
   the guard and returns the same `generation_activation_not_ready` error.
2. Re-derives the changed packages over the activated chain
   (`manifest_change.go`, arbiter ruling D2/D3). The baseline W is the newest
   activated prior whose marker item carries `manifest_trigger_completed`
   (status irrelevant), so a marker the runtime skipped or a claim sweep
   superseded is diffed through, not trusted. A package key is changed when its
   dependency-signature set is not identical in every generation of
   [W .. current]; every view is base-overlaid, current included. With no
   stamped W, every pin of the chain counts; when (W, current], the current
   generation counted, holds more than 32 generations, or with
   a pruned W, every current pin counts (`manifest_trigger_baseline_degraded`);
   a pruned intermediate is skipped (`manifest_trigger_chain_pruned`).
   Tombstoned rows always count.
3. Reads the active `vulnerability.affected_package` identities in those
   ecosystems (`FindPackageHolderCandidates`) and keeps the scopes whose
   identity shares a consumption key with a changed dependency, the findings
   pass's own join.
4. Reopens them through `ReopenHolder` with the same cap and continuation list
   as the CVE fan-out, under `cause="manifest_dependency"`. A reopened holder
   re-derives its findings from the current manifest rows, which adds,
   re-scores, or retracts the finding.
5. Stamps its own item with `manifest_trigger_completed` through a
   lease-fenced UPDATE (ruling D1) on the claim that leaves no holder behind, or
   on a no-change pass; never on a first chunk, a superseded pass, or a
   deferral. `ReopenHolder` keeps the stamp, because it rewrites only the
   origin key.

A marker item reopened by another pass's fan-out is not a one-hop no-op: its
re-derivation is the holder fan-out, so it re-reads the generation and reopens
the holders again. That terminates, because a holder pass that changes nothing
fans out nothing.

Not reopened: security alert scopes. They build findings from manifest
consumptions too (`appendSecurityAlertImpactFindings`), but the holder read
covers only `vulnerability.affected_package` facts, so an alert finding whose
matched pin changed waits for that alert scope's next generation. Tracked in
#10061.

Known limit: when a delta in the diffed chain has a base that cannot be read
(missing, pruned, or a legacy delta), its view is the delta's own rows, so a
pin that only the base held looks never-present and its removal is not fanned
out. Its finding stays until that package's advisory scope reruns. The pass
logs `sub_signal_manifest_trigger_prior_base_missing=1` on the reducer success
line, and `TestManifestTriggerKnownLimitPriorDeltaWithMissingBase` pins the
behavior. A degraded pass misses removals the same way. The repository-keyed
reopen that closes both is tracked in #10059.
Evidence: `docs/internal/evidence/9874-lockfile-retrigger.md`.
