# Lockfile and manifest changes re-trigger impact matching (#9874)

**A git generation whose only trigger fact is a manifest or lockfile dependency
row now enqueues one `supply_chain_impact` marker intent. The reducer branch
for it writes no findings: it reopens the scopes that hold advisory evidence
for the packages the generation changed, and their next passes add, re-score,
or retract the findings. This note records the cost shim, the holder-read
probe, the predicate cost, and the known gap.**

Design: arbiter ruling Option B, issue #9874 comment of 2026-11-04T14:19:59Z,
and the zone ruling recorded later the same day. The QA convergence-delay
measurement (acceptance item 1) and the post-deploy QA check are not part of
this change; see "Not checked".

## What changed

| Layer | Change |
| --- | --- |
| Projector | `manifest_trigger.go`: when none of the twelve trigger kinds is present, every git generation (one holding a git repository fact, delta or full; ruling F2) yields one intent keyed `supply_chain_impact:<scope>` with payload `trigger: manifest_dependency`, anchored on its earliest git dependency row (content_entity Variable, `config_kind` dependency, source git, not a central version declaration, tombstones included) or else on its repository fact. |
| Reducer | `impact.go` gets one early-return call site before the fencing token; `fanout.go` gets the `manifest_dependency` cause and the `FindPackageHolderCandidates` port method. The branch is in `manifest_trigger.go` and `manifest_change.go`. |
| Storage | `supply_chain_impact_package_holders.go` (holder read), `supply_chain_impact_manifest_generation.go` (activation gate, watermark, chain and view reads) and `supply_chain_impact_manifest_stamp.go` (the lease-fenced completion stamp), all on `SupplyChainImpactFanOutStore`. No migration, no new index. |

The branch: (1) runs only for an active generation: under the default runtime
`Runtime.GenerationCheck` defers a pending generation before Handle with the
shared non-counting `generation_activation_not_ready` class and skips a
superseded one, and the branch re-checks with the same freshness check
(superseded, reachable only in the activation race, succeeds with no work;
pending is a defensive arm for a runtime without the guard and returns the
same shared error); (2) derives changed packages over the activated chain
(arbiter ruling D2/D3): the baseline W is the newest activated prior whose
marker item carries the completion stamp `manifest_trigger_completed`
(whatever the item's status), and a package key is changed when its set of
dependency signatures is not identical in every generation of [W .. current].
Every view is base-overlaid (`facts.OverlayDeltaBase`), current included, so a
delta that reverts a path to base content is caught. The chain is capped at 32
generations over (W, current], the current one counted, so at most 31
intermediates (`TestManifestChainDepthCapBoundaryLive` pins both sides); over the cap or with a pruned W every current pin counts
(`manifest_trigger_baseline_degraded`), and a pruned intermediate is skipped
(`manifest_trigger_chain_pruned`). With no stamped W, every pin in any view of
the chain counts. A dependency's signature is every field it carries except
`FactID` and `ObservedAt`, so a scope or direct/transitive flip at the same
version counts; tombstoned rows always count; (3) reads the active `vulnerability.affected_package` identities whose
stored ecosystem spelling is any alias of those ecosystems
(`packageidentity.EcosystemAliases`; GitLab Gemnasium stores raw slugs such as
`go` and `gem`) and keeps the scopes that share a consumption key with a
changed dependency, using the findings pass's own join; (4) reopens them with
the existing cap (256) and continuation list; (5) stamps its own item with
`manifest_trigger_completed {at, baseline_generation_id}` through a
lease-fenced UPDATE (ruling D1), only on the claim that leaves no holder behind
or on a no-change pass, never on a first chunk, a superseded pass or a
deferral. A failed stamp fails the pass retryably.

## Decision, threshold, deadline (stated before the probe)

- Holder read: proceed if the scan stays under 50 ms at 10x of the 5k-row
  affected-package census. The lane lead read the bound as a median.
- Projector predicate: under 1 ms per 10k content entities in the worst case.
- Deadline: two hours from the start of the probe, then report raw numbers.

## Holder-read probe (EXPLAIN ANALYZE before the SQL was written)

Method: PostgreSQL 18.6 in a scratch container, database `lane3_9874`, schema
from `ApplyBootstrapWithoutContentSearchIndexes`. Synthetic data: 600 advisory
scopes, each with an active and a superseded generation; affected-package rows
across 8 ecosystems (npm is 25%); noise of 300,000 git content entities and
100,000 finding rows. Every affected-package census in the evidence directory
is under 5,000 rows (239 in the hosted census cited by
`9209-3-2-advisory-group-cost.md` from #8880; 1,115 retired-instance OSV facts
under the #9431 lane predicate in `9431-qa-retire-report.md`), so 1x is 5,000
active rows. All volumes are synthetic. Host load average 6 to 22 during the runs.

First probe, before the SQL was written (1,100 scopes, 50% superseded copies):

| Shape | 1x (5k active) | 5x (25k) | 10x (50k) |
| --- | --- | --- | --- |
| `SELECT DISTINCT ... ORDER BY`, no ecosystem filter | 21.6-22.0 ms | 94-102 ms | 181-202 ms |
| same, ecosystem filter | 9.6-10.9 ms | 37.9-40.0 ms | 73-90 ms |
| no sort, no filter | 8.0-8.6 ms | 22.6-30.1 ms | 44.1-58.1 ms, median 48.4 |
| no sort, canonical ecosystem filter | 6.7-7.4 ms, median 7.0 | 20.7-37.5 ms, median 21.4 | 38.8-57.9 ms, median 41.5 |

The sorted shapes spent most of their time in an external-merge text sort
(`Sort Method: external merge Disk: 3808kB` at 10x), so the read has no
DISTINCT and no ORDER BY and dedupes in Go.

**Correction after review (F6).** The first probe's claim that superseded rows
cost nothing does not hold in general. Re-measured with the shipped query (the
alias-expanded, C-folded ecosystem filter from F1) on two seeds with the same
50,000 active and 50,000 superseded affected-package rows, seven runs each,
host load 3 to 12:

| Seed | Plan the planner picks | Execution |
| --- | --- | --- |
| 1,101 scopes | nested loop over `ingestion_scopes`, index scan on `fact_records_vulnerability_affected_package_active_scan_idx` per scope's active generation (53,455 buffers) | 45.9-47.6 ms, median 46.6 |
| 20,601 scopes (600 advisory, the reviewer's shape) | hash join; bitmap scan of the whole partial index (100,000 entries, active and superseded), heap fetch of every row whose ecosystem matches, then the active-generation join (6,405 buffers) | 59.5-73.8 ms, median 65.3 |

On the large seed the read reads every retained affected-package row of the
requested ecosystems, superseded ones included, so cost scales with total
retained rows; on the small seed it scales with scopes times active rows per
scope. Alternatives tried on the large seed, six runs each: an explicit
`LATERAL` per scope, an `EXISTS` semi-join, and a forced nested loop all chose
or kept the same bitmap plan at 69-73 ms; a scope-driven `LATERAL ... OFFSET 0`
that probes each active generation took 118-122 ms (20,600 index probes); no
ecosystem filter took 56 ms server-side but 137 ms wall against 98 ms with the
filter, because it ships four times the rows. The shipped shape stays. On the
reviewer's seed it misses the 50 ms bound (median 65.3 ms); an index that
anchors affected_package by folded ecosystem, or a retention policy that keeps
fewer superseded generations, would be the fix, and both are outside this
change (migrations are out of zone). The read runs under the existing 10 s
holder-read statement timeout.

## Generation reads

The dependency read anchors on the generation's repository fact `repo_id` so it
seeks `fact_records_content_entity_dependency_variable_repo_idx` (migration
121): 1.1 ms against 4.6 ms without the anchor, on a 3,000-entity generation
holding 428 dependency rows, with 1.5M content entities in the table. The
repository fact and repo id reads use the `(scope_id, generation_id,
fact_kind)` index; tombstones use `fact_records_changed_since_tombstone_keys_idx`.
A marker pass runs these reads for the current generation, the stamped
baseline W, and every activated generation between them, plus the base full
generation of each delta among them. A base shared by several views is read
once per pass.

## Projector predicate cost

`BenchmarkManifestDependencyTriggerNoDependency` (worst case: every entity is a
git Variable whose `config_kind` is not dependency, so the predicate visits
all of them) and `BenchmarkFactLookupBuildScale` (the projector's existing
per-generation index build over the same entities, for scale). Eight runs
each, i7-8700K, host load 17 to 39.

| Entities | Predicate, first order | Predicate, reordered (shipped) | Fact lookup build |
| --- | --- | --- | --- |
| 10,000 | 0.82-2.91 ms | 0.57-0.89 ms, median 0.69 | 0.38-1.14 ms |
| 100,000 | 8.6-9.7 ms | 7.3-8.8 ms | 4.8-6.8 ms |

The reorder checks `config_kind` first (two map reads refuse almost every
entity), then entity type, then source; payloads are decoded maps, so no check
decodes JSON. At 10k entities the shipped predicate meets 1 ms per 10k on every
sample; at 100k it costs about 1.3x the fact lookup build the projector already
does once per generation. Per-row cost is about 70 to 90 ns. The QA count of
content entities per git generation is unknown, so whether 10x of QA sits above
the 1 ms line is not known; the lane lead takes that threshold question to the
arbiter.

## Intent volume at 1x, 5x, 10x

Ruling F2 raises one marker for every git generation that holds none of the
twelve trigger kinds, so extra marker intents per day equal the git
generations per day (QA figure unknown; lane 1 was asked for a seven-day
readback). A delta's #7797 union base rows never anchor the marker: the
projector hands intent builders only the generation's own facts
(`generationOwnedFacts`), and a code-only delta anchors on its own repository
fact (`TestBuildProjectionQueuesOneMarkerForDeltaThatTouchedNoManifest`). The
fan-out bound is per generation, not per pin: a 500-pin push is one intent
(`TestBuildProjectionQueuesOneSupplyChainImpactForGitManifestChange`) and one
holder read.

Most markers change nothing. No-change marker passes, measured on the probe
database (PG 18.6, synthetic, 1.7M+ fact rows, host load 3 to 9, 15 runs each,
including payload decode and the diff in Go; statements exclude the one stamp
UPDATE). A base full shared by several views is read once per pass (review
B2, `TestManifestPassReadsASharedBaseOnceLive`); before that cache the two
delta shapes read it twice:

| Shape (3,000-entity full, 428 dependency rows) | Read statements before / after the cache | Median, after (min-max) |
| --- | --- | --- |
| full after a stamped full | 15 / 15 | 23.6 ms (19.4-116.0) |
| delta after its stamped base full | 22 / 18 | 17.3 ms (13.6-47.6) |
| delta after a stamped delta, both on one base full | 29 / 25 | 19.4 ms (13.8-57.9) |

No holder read runs when nothing changed. Daily no-change cost is therefore
about G x 20-25 ms of reducer time and 16 to 26 statements, where G is git
generations per day.

**First marker per scope (review F9).** No stamp exists before deploy, so each
scope's first marker has no baseline: it reads every activated generation, up
to the cap, and every pin it sees counts as changed. Measured on synthetic
repositories of 32 full generations each (31 intermediates plus the current
one, the most the cap diffs; no stamps; dependency names drawn from the 20,000
synthetic advisory package names so they match holders):

| Repository | Statements | Changed packages | Holders reopened | Median (min-max) |
| --- | --- | --- | --- | --- |
| 3,000 entities per generation (428 dependency rows) | 169 | 428 | 150 | 338 ms (328-382), 7 runs |
| 30,000 entities per generation (4,285 dependency rows) | 169 | 4,285 | 150 | 2.50 s (2.43-2.64), 5 runs |

- Reopens are bounded by the distinct advisory scopes that hold a matched
  package, not by the package count: ten times the packages reopened the same
  150 scopes (`matchPackageHolders` dedupes by scope), and one claim reopens at
  most 256 before the continuation list carries the rest.
- The 10 s holder-read timeout is a per-statement `statement_timeout`, not a
  bound on the pass. The largest single statement here, one generation's
  4,285-row dependency read, took 3.5 to 6.9 ms; the 2.5 s is decode and diff
  in Go across 169 statements. A statement would need roughly a thousand times
  that row count to reach 10 s, so the timeout is not reachable at these sizes.
  The pass as a whole runs under the reducer lease (one minute, renewed by the
  heartbeat every 30 s).
- If a statement did time out, the pass fails retryably under
  `supply_chain_impact_fanout_failed` and counts
  `eshu_dp_supply_chain_impact_fanout_total{cause="manifest_dependency",outcome="read_timeout"}`
  (the generation read now classifies timeouts like the holder read,
  `TestManifestTriggerGenerationReadTimeoutIsCountedAsTimeout`); after the
  attempt budget the marker dead-letters, which leaves the pre-#9874 behavior
  for that generation.
- Decision: no change to the no-baseline walk. The proposed shortcut (no
  stamped baseline: fan out current pins without reading the chain) is not
  needed for cost, and it is not equivalent: walking also fans out pins that
  intermediates added and the current generation removed.

A marker pass that changes packages adds the holder read and the reopens,
measured on the synthetic data above (holder read: first-probe medians at 1x
and 5x with 1,100 scopes; the 10x row is the re-measured shipped query on both
seeds):

| Census scale | Holder read (median) | Generation reads | Reopens |
| --- | --- | --- | --- |
| 1x | 7.0 ms | about 1-5 ms per generation read | H x 0.12 ms p50 (#9695b) |
| 5x | 21.4 ms | same | same |
| 10x | 46.6 ms (1,101 scopes), 65.3 ms (20,601 scopes) | same | same |

H is the number of advisory scopes holding a changed package. Each reopened
holder then runs one ordinary `supply_chain_impact` pass; that downstream cost
dominates and is the QA `supply_chain_impact` handler duration, which is not
measured here. The lane lead accepted the holder read (F6): the 46.6 ms median
passes the 50 ms bound set for the QA census of 1.1k to 5k affected rows, the
65.3 ms median at 20,601 scopes misses it, and it runs once per changed-key
marker pass under the 10 s timeout, not per request. If QA volumes land near
the larger seed, the escape hatch is a lane 3 migration (278 or later) that
anchors affected_package by folded ecosystem; no migration now.

## Concurrency and replay

- In-flight OSV pass on the same package: `ReopenHolder` sets the durable
  replay flag on a claimed or running holder, so a pass that read the old pin
  reruns once (`TestManifestTriggerReopensAdvisoryHolderLive`).
- Redelivery is idempotent: a second marker pass sees `pending_noop` and a
  flagged holder stays flagged (same live test, and the replay matrix in
  `manifest_trigger_replay_test.go`).
- A fan-out reopen of a marker item re-derives (it re-reads the generation and
  reopens holders) rather than being treated as a one-hop no-op; it terminates
  because a holder pass that changes nothing fans out nothing.
- Completion stamp: lease-fenced (`TestManifestStampIsLeaseFencedLive`),
  kept by `ReopenHolder` (`TestManifestStampSurvivesReopenHolderLive`), and
  re-derived and re-written on redelivery
  (`TestManifestChainRedeliveryAfterAStampRederives`). A marker skipped by the
  runtime guard or superseded by a claim sweep is never stamped, so the next
  pass diffs through it (`TestManifestWatermarkSkipsUnstampedPriorsLive`,
  `TestManifestChainEndToEndRuntimeSkipLive`).
- Pending generation: deferred by the runtime's generation guard before the
  handler, under the shared non-counting `generation_activation_not_ready`
  class, until the generation activates or turns terminal (then the runtime
  skips it as superseded). That is the bound: the generation's own lifecycle,
  not the attempt budget (`TestRuntimeDefersAManifestMarkerBeforeTheHandler`).

## Known gap (correctness)

When a delta in the diffed chain has a base that cannot be read (missing,
pruned, or a legacy delta without a baseline), its view is the delta's own
rows. A pin that only the base held then looks never-present, so its removal
is not fanned out and **the finding for the removed package stays visible
until that package's advisory scope reruns** (today's pre-#9874 behavior for
that one case). The pass logs it as the result sub-signal
`sub_signal_manifest_trigger_prior_base_missing=1` on the reducer's success
log line (a log field, not a metric).
`TestManifestTriggerKnownLimitPriorDeltaWithMissingBase` pins the behavior and
fails if the gap closes. A degraded pass (chain over the cap, or a baseline
whose facts retention pruned) fans out every current pin, so a removal in that
window is likewise missed (`sub_signal_manifest_trigger_baseline_degraded=1`).
The repository-keyed reopen that closes both is tracked in the follow-up issue
#10059.

## Holders this change does not reopen (F7)

The holder read looks only at `vulnerability.affected_package` facts. Security
alert scopes also build findings from manifest consumptions
(`appendSecurityAlertImpactFindings` matches each provider alert to the
repository's manifest rows), and they are not reopened by a manifest change:
an alert finding whose matched pin changed keeps its old version or status
until that alert scope's own next generation. Provider alert scopes usually
re-collect after a lockfile push, but nothing in this change guarantees it.
Tracked in #10061.

## Related hazards

- #10060: a CVE-keyed fan-out continuation list can be stranded when its item's
  generation is superseded (lane 1's fan-out files, not changed here). The
  marker's own continuation drain has the same shape and the same exposure.

## Telemetry

Observability Evidence: `eshu_dp_supply_chain_impact_fanout_total{cause="manifest_dependency"}`
with the existing outcomes (reopened, replay_flagged, pending_noop, enqueued,
deferred, read_error, read_timeout, reopen_error, state_error); the
`supply_chain_impact.fanout` span with `cause`; the `supply chain impact holder
fan-out` log line; a not-yet-active marker is the runtime's
`generation_activation_not_ready` retry (counted by
`eshu_dp_reducer_retry_surge_total{failure_class}`); result sub-signals
`manifest_trigger`, `manifest_trigger_changed_packages`,
`manifest_trigger_delta_paths`, `manifest_trigger_prior_base_missing`,
`manifest_trigger_baseline_degraded`, `manifest_trigger_chain_pruned`,
`manifest_trigger_views`, `manifest_trigger_completed`,
`manifest_trigger_superseded`, logged as `sub_signal_<name>` fields on the
reducer success log. No new instrument.

Performance Evidence: holder-read and generation-read figures above (synthetic,
PG 18.6, scratch database); predicate microbenchmark above.

## Not checked

- QA convergence delay before and after (acceptance items 1 and 7).
- QA git-generation rate and `supply_chain_impact` handler duration, so the
  daily intent volume and downstream cost are stated as formulas only.
- Golden-corpus B-7 replay and the B-12 snapshot.
- NornicDB: no graph path is touched.
