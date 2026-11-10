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
  fans out nothing of its own rewrite, and consumes the stamp — except the
  suppression diff's ids, which pass through regardless of the stamp (#10019:
  a stamped first pass of an operator generation would otherwise admit without
  fanning out). A stamp that reaches a later attempt
  was written while a genuine pass ran and failed, so the retry is genuine; an
  owed marker also makes a pass genuine. A continuation is not a hop: a stamped
  drain claim drains the stored list.
- Stamp (#9740): the reopen stamps the changed CVE ids the committing pass
  derived, sorted, de-duplicated, and capped at 200 with the `*` sentinel above
  the cap, and a continuation drain keeps scoping the original change by
  unioning it in. An enqueued holder stamps no set, so a generation that never
  had an item runs its establishing pass full. After its writer commit and
  before its acknowledgement, a stamped pass compares the CVE ids it derived
  (every row it wrote plus retracted copies) with the stamp: disjoint clears
  `cross_scope_replay_required` under a lease-and-stamp fence, so the ack
  succeeds instead of rerunning; a newer stamp between the compare and the
  clear turns it into a no-op and the pass reruns once. The sentinel, a stamp
  without a set, and any continuation debt ride to the next claim; a failed
  pass never reaches the clear, so the retry still happens.
- Scoped one hop (#9779): a one-hop pass whose stamp names a bounded set
  re-derives that set only. Its evidence filters derive from the stamped
  envelopes plus the finding-row package seeds (the scope's own advisories and
  its active finding rows for the stamped CVEs); the until-stable follow-up
  intersects every round with the scope, so the loop converges on the stamped
  subgraph instead of re-widening. The writer upserts findings for stamped
  CVEs only and retracts inside the set only; rows outside it stay
  byte-identical. If a finding for a stamped CVE depended on evidence the
  scope leaves out, the scoped-vs-full differential goes red and scoping is
  the wrong fix. The sentinel, a set-less stamp, and an owed retry run full.
  Passes count `mode` (`full`, `scoped`, `scoped_fallback_sentinel`, `scoped_fallback_out_of_stamp`) in
  `eshu_dp_supply_chain_impact_pass_total`, and the success log carries
  `scope_id` plus the `scoped_cve_count`, `scoped_evidence_envelopes`, and
  `fanout_replay_cleared` sub-signals.
- A claim reopens at most 256 holders; the rest is stored as `fanout_remaining`
  on the committing item and drained by its next claims.
- A continuation survives its scope's next generation (#10060): the claim-time
  supersede sweep spares a non-marker `supply_chain_impact` row that still
  carries `fanout_remaining` or `fanout_owed`, and the reducer runtime's
  generation guard lets that stale intent execute (`IntentCarriesFanOutDebt`),
  so it drains the stored list under its own generation instead of dying
  `superseded` with the holders unreopened. The successor generation's pass is
  not a substitute: its changed-CVE diff is taken against this generation's
  stored rows, which already carry the change. Markers are exempt from the
  rescue because the completion-stamp baseline already diffs through them.
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

## Suppression-only changes (#10019)

A `cve_id`-scoped operator suppression reaches no distro record, so the
operator pass writes no finding row of its own and the changed-row set is
empty. The writer diffs the pass's suppression facts against the newest
admitted older generation whose fan-out debt is cleared
(`suppression_diff.go`, `suppression_walk.go`, wired in
`suppression_fanout_wire.go`), walking adjacent activated pairs up to the K=4
pair cap (`walk_truncated` counts the unwalked tail, which stays on each
holder's own cadence, and KEV/EPSS facts cannot join by construction). The
changed suppressions' CVE ids union into `ChangedCVEIDs` before the owed
marker is recorded, so a suppression-only change still sets the marker and
reopens the holders through the same edge, with `cause="suppression"`. A
pass whose own facts hold no suppression issues no new statement; a duplicate
or clean-retry delivery of an admitted generation names nothing and reopens
nobody; a pending generation fails closed with the existing
`generation_activation_not_ready` error. The successor's baseline is the
newest admitted generation whose owed marker is cleared, so a newer
generation that activates before the continuation drains covers the owed
range again instead of trusting it. Signals: `…_suppression_diff_total`
(`outcome`, `baseline`), `…_diff_duration_seconds`,
`…_walk_truncated_total`, `…_no_cve_scope_total`, the
`supply_chain_impact.suppression_diff` span, the result sub-signals
`suppression_diff_rows`, `suppression_diff_cve_ids`, and
`suppression_diff_no_cve_scope`, and the `supply chain impact suppression
diff` log line. Design: `docs/internal/design/10019-suppression-fanout.md`.

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
3. When a package changed, reads the in-flight `supply_chain_impact` scopes
   FIRST (`FindInFlightNonMarkerScopes`), then the active package-holder
   identities (`FindPackageHolderCandidates` with a `PackageHolderQuery`:
   `vulnerability.affected_package` rows in those ecosystems, plus
   `security_alert.repository_alert` rows for the repositories the changed
   keys belong to, #10061), and keeps
   the in-flight scopes plus the scopes whose identity shares a consumption key
   with a changed dependency, the findings pass's own join. The order is the
   CVE fan-out's: a pass still claimed or running at the first read is flagged
   for one rerun (it may have loaded evidence before this generation's manifest
   rows were visible, and with no committed affected_package rows yet the
   holder read cannot see it); one that acknowledged earlier committed its rows
   first, so the holder read sees it (#10105 review P2).
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
the holders again.

Termination. The marker's in-flight read leaves markers out
(`FindInFlightNonMarkerScopes`: a claimed or running item whose payload carries
`trigger: manifest_dependency`), and its holder read returns only scopes with
affected-package rows, which a git scope never holds. So a marker never reopens
or flags another marker. Without that exclusion two markers in flight at once
flagged each other on every round, forever
(`TestTwoConcurrentMarkersConvergeLive` failed with 8 passes over 4 rounds and
never drained). What a marker does reopen is advisory or other non-marker
scopes, stamped with its origin: a reopened succeeded item and a flagged
running item both rerun once as a one-hop pass, which re-derives its findings
and fans out nothing. A marker can be flagged by a CVE fan-out (that path's
in-flight read is unchanged and still includes markers), but the marker's
rerun only reopens non-markers one hop, so the chain is CVE pass, marker
rerun, one-hop holder reruns, and it stops. An in-flight non-marker holder
pass still gets exactly what the CVE path gives it: one replay of its pass
after it acknowledges.

Security alert scopes are reopened since #10061. They build findings from
manifest consumptions too (`appendSecurityAlertImpactFindings`), keyed by
repository, and hold no `vulnerability.affected_package` facts, so the
holder read's alert arm selects active `security_alert.repository_alert`
facts by the changed keys' repository id
(`fact_records_security_alert_repository_lookup_idx` leads with
`payload->>'repository_id'`; no migration) and the same consumption-key join
keeps only the alerts naming a changed package. Alerts a provider already
closed (`provider_state` `dismissed`, `auto_dismissed`, or `fixed` — only
the provider-state subset of what `securityAlertCanSeedImpact` refuses to
seed from) do not reopen their scope. Evidence: `docs/internal/evidence/10061-alert-scope-reopen.md`.

Gap passes: when a delta in the diffed chain has a base that cannot be read
(missing, pruned, or a legacy delta), its view is the delta's own rows, so a
pin that only the base held looks never-present and its removal is invisible
to the package-keyed diff. A degraded pass (chain over the cap, or a pruned
baseline) and a pass that skipped a pruned intermediate miss removals the same
way. Any of the three makes the pass reopen every scope holding an active
finding for the generation's repository (`FindRepositoryFindingHolders`,
#10059), so the stale finding is re-derived without waiting for the package's
advisory scope to rerun. The pass logs `sub_signal_manifest_trigger_prior_base_missing=1`
(or `baseline_degraded`, or `chain_pruned`) on the reducer success line, with
the reopened count on `sub_signal_manifest_trigger_repository_holders`.
`TestManifestTriggerPriorBaseMissingReopensRepositoryHolders` pins the
behavior. A clean pass never touches the repository-keyed read.
Evidence: `docs/internal/evidence/9874-lockfile-retrigger.md`,
`docs/internal/evidence/10059-repository-reopen.md`.

Re-key (L2, #10408): the genuine pass fans out
`union(ChangedCVEIDs, TouchedGroupIDs)` plus the owed-retry union with
`Recovery`, and the holder read is a 3-arm OR over `cve_id`, `advisory_id`,
and group `members` so late-alias advisory-keyed holders reopen. Migrations
288/289 add the advisory btree and member GIN partial indexes; 290 adds the
advisory extended-statistics object. Chunk 200 and the 256 holder cap are
unchanged: still one holder-lookup statement per chunk, no new locks, claims,
leases, or queue behavior.

Suppression carry (L3, #10408): a merge or split retires the dead record
while the survivor's aliases still name the dead id, so the suppression
load matches an extra alias-member arm over the in-pass alias ids
(`facts/impact/arms.go`, `aliasSuppressionArms`). The arms widen the
suppression-kind predicate only — `TestAliasListReachesOnlyTheSuppressionStatement`
pins that no non-suppression branch binds the alias list, so the load reads
zero extra NVD or product rows. The pass tally (`suppression_carry.go`)
counts coverage the wave carried through a non-key member anchor and
coverage a split orphaned; expired, scope-mismatched, and retracted
citations stay visible and out of both tallies by design, and the tallies
are pure counts over the already-loaded suppressions and findings (no new
reads). The tallies ride the result sub-signals
`suppression_carried_via_member` and `suppression_orphaned_by_split`.

Keyed-path convergence (L4, #10408): the winners keyed tick deletes the
dead winner key and drains the dirty log on both the tombstone and the
supersession routes, matching a fresh recompute with zero audit repairs
(`TestSupplyChainImpactWinnersResweepRekeyTombstoneDeadKeyConvergesLive`
and `...RekeySupersessionDeadKeyConvergesLive`, 0.10s each at HEAD against
disposable PostgreSQL 18.6). Two holder scopes re-keying the same component
in overlapping passes converge onto the survivor group with identical keys,
no uniqueness or fencing errors, and both replays converging with no manual
rerun (`TestRekeyTwoHolderScopesRekeySameComponentLive`, 1.32s at HEAD).

Re-key outcome (L7, #10408): the pass dispatcher counts one
`eshu_dp_supply_chain_impact_rekey_total` point per pass whose writer
commit changed a group's identity, with the closed outcome (`merge`,
`split`, `member_only`, `retire_only`; `rekey_signals.go`). A pass that
re-derives identical groups counts nothing — unless it dropped suppression
coverage, which reports `split` — so the steady state is silence.
Suppression carry and orphan
magnitudes ride
`eshu_dp_supply_chain_impact_suppression_carried_via_member_total` and
`eshu_dp_supply_chain_impact_suppression_orphaned_by_split_total` under the
same outcome label, and per-pass magnitudes ride the result sub-signals
`rekey_groups_touched`, `rekey_findings_rekeyed`, and
`rekey_rows_tombstoned`. Read the rate by outcome as the rollout's first
measurement of alias-churn: a `split` rate means dropped coverage the
operator must re-issue, a `merge` rate converges on its own.

No-Regression Evidence (#10482): the Go half of this change is comment-only
— every added and removed Go line is a `//` comment (`git diff
--unified=0 origin/main...HEAD -- '*.go'` shows no other lines), so no
counter, label, emission, control flow, lock, batch size, or query text
changes. `go test ./internal/reducer/supplychain/core/
./internal/telemetry/ -count=1` passes at the fix head, and `go vet` plus
`gofmt -l` on both packages are clean.

No-Observability-Change: the re-key path stays covered by the existing
`eshu_dp_supply_chain_impact_rekey_total` (plus carried/orphaned counters
and the `rekey_*` result sub-signals); this change adds no metric and
alters no emission.

Re-key freshness bound: queue latency plus one holder pass per holder scope
that holds a touched copy plus one 30-second winners resweep tick — the
same bound as the enrichment wave above. Until the drain finishes, a holder
can still serve the pre-merge copy and the winners can still rank it first.

No-Regression Evidence (#10408 L2): EXPLAIN ANALYZE on disposable PostgreSQL
18.6 with 90k finding rows (`TestRekeyHolderLookupUsesAllThreeArmsLive`):
single-id CVE and advisory probes both run a BitmapOr over all three arms
(`fact_records_supply_chain_impact_lookup_idx`,
`..._advisory_lookup_idx`, `..._group_members_idx`) in 2.8-2.9ms; the
200-id chunk-bound probe runs a Parallel Seq Scan in 44.1ms. These are
bounds on the 90k-row synthetic distribution in
`TestRekeyHolderLookupUsesAllThreeArmsLive`, not a production latency
claim: single-id and few-id advisory probes serve from the advisory
partial index, while a full 200-id chunk scans — by design, since the
chunk must touch every arm's rows. Baseline pre-L2 was the single
`cve_id = ANY(...)` arm plus the in-flight/owed union, which never
reopened advisory-keyed holders (the R-adv probe fails pre-L2 and passes
post-L2). The widened predicate adds rows only where the advisory or
member arm matches, which is the intended re-key coverage; the statement
shape, chunk bound, and cap are identical, so per-chunk cost is bounded
by the same single scan.

No-Observability-Change (#10408 L2): no new metrics, spans, logs, or status
fields; the reopened-scope count still surfaces on the existing reducer
success line.

No-Regression Evidence (#10408 L3): the suppression alias arms widen the
suppression-kind predicate only — `TestAliasListReachesOnlyTheSuppressionStatement`
fails if any non-suppression branch binds the alias list — so the wider load
reads zero extra NVD or product rows; the carry tallies count the
already-loaded suppressions and findings in memory (no new reads, no new
statements). `TestSuppressionStatementReadsTheAliasMembers` pins the arm
itself.

No-Regression Evidence (#10408 L4): `TestRekeyPassOverheadIsMeasured` (200
rounds, 4 rows, all changed, in-memory writer-core only, no Postgres round
trip) reports ordinary medians 118.0-119.2us vs re-key 119.3-121.0us
(delta +1.1% to +1.5% across repeated runs at HEAD: shared-host noise on
~120us medians),
against the fail-closed gate of twice the ordinary median: the member union
is a bounded in-memory cost. Overlap and keyed-tick behavior hold live
against disposable PostgreSQL 18.6: `TestRekeyTwoHolderScopesRekeySameComponentLive`
1.32s at HEAD, both keyed-path convergence tests 0.10s each
(`specs/live-tests.d/live-tests-07.yaml` reasons).

Observability Evidence (#10408 L7): the re-key signal is operator-visible
by design — `TestEmitRekeyCountsOutcome` proves one
`eshu_dp_supply_chain_impact_rekey_total` add per genuine identity-change
pass (closed outcome label) with zero adds on ordinary passes, plus
conditional carried/orphaned adds (zero on a pass with no carry or orphan);
no new spans, log lines, queue stages, locks, claims, leases, or status
fields. Magnitudes ride result sub-signals — log fields on the existing
reducer success line, following the `fanout_*` pattern — and the outcome
rides the new counter's closed label, never a group, CVE, or advisory id.
