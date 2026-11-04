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
| Projector | `manifest_trigger.go`: when none of the twelve trigger kinds is present, the earliest git dependency row (content_entity Variable, `config_kind` dependency, source git, not a central version declaration, tombstones included) yields one intent keyed `supply_chain_impact:<scope>` with payload `trigger: manifest_dependency`. |
| Reducer | `impact.go` gets one early-return call site before the fencing token; `fanout.go` gets the `manifest_dependency` cause and the `FindPackageHolderCandidates` port method. The branch is in `manifest_trigger.go` and `manifest_change.go`. |
| Storage | `supply_chain_impact_package_holders.go` (holder read) and `supply_chain_impact_manifest_generation.go` (activation gate plus generation reads), both on `SupplyChainImpactFanOutStore`. No migration, no new index. |

The branch: (1) gates on activation with the shared generation freshness
check (active: fan out; pending: retry under the counting class
`supply_chain_impact_manifest_trigger_deferred`, bounded by the attempt budget;
superseded: succeed with no work); (2) derives changed packages as the
symmetric difference of dependency signatures against the newest activated
prior generation, comparing a delta on its touched paths only and reading a
prior delta as its base overlaid with its own rows; tombstoned rows always
count; (3) reads the active `vulnerability.affected_package` identities in
those ecosystems and keeps the scopes that share a consumption key with a
changed dependency, using the findings pass's own join; (4) reopens them with
the existing cap (256) and continuation list.

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

| Shape | 1x (5k active) | 5x (25k) | 10x (50k) |
| --- | --- | --- | --- |
| `SELECT DISTINCT ... ORDER BY`, no ecosystem filter | 21.6-22.0 ms | 94-102 ms | 181-202 ms |
| same, ecosystem filter | 9.6-10.9 ms | 37.9-40.0 ms | 73-90 ms |
| no sort, no filter | 8.0-8.6 ms | 22.6-30.1 ms | 44.1-58.1 ms, median 48.4 |
| **no sort, ecosystem filter (shipped)** | **6.7-7.4 ms, median 7.0** | **20.7-37.5 ms, median 21.4** | **38.8-57.9 ms, median 41.5** |

Seven runs per cell for the unsorted shapes, five for the sorted ones. The
sorted shapes spent most of their time in an external-merge text sort
(`Sort Method: external merge Disk: 3808kB` at 10x), so the shipped read has no
DISTINCT and no ORDER BY and dedupes in Go. The plan is a nested loop over
`ingestion_scopes` with an index scan on `fact_records_scope_generation_idx`
per scope's active generation, so superseded rows cost nothing. Cost is linear
at about 0.8 µs per active affected-package row. The shipped shape passes on
the median (41.5 ms); 2 of 7 samples at 10x exceeded 50 ms (57.3 and 57.9 ms),
both during host load spikes. It runs under the existing 10 s holder-read
statement timeout. No index anchors affected_package by (ecosystem, name);
migrations are out of this change's zone, so the residual filter stays.

## Generation reads

The dependency read anchors on the generation's repository fact `repo_id` so it
seeks `fact_records_content_entity_dependency_variable_repo_idx` (migration
121): 1.1 ms against 4.6 ms without the anchor, on a 3,000-entity generation
holding 428 dependency rows, with 1.5M content entities in the table. The
repository fact and repo id reads use the `(scope_id, generation_id,
fact_kind)` index; tombstones use `fact_records_changed_since_tombstone_keys_idx`.
A marker pass runs these reads for its own generation and its prior, plus the
prior's base when the prior is a delta.

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

Extra marker intents per day equal the git generations per day whose own facts
hold at least one dependency row (a delta that touched a manifest, or any full
generation of a repository with dependencies). A delta's #7797 union base rows
never count: the projector hands intent builders only the generation's own
facts (`generationOwnedFacts`), which
`TestBuildProjectionSkipsSupplyChainImpactForDeltaThatTouchedNoManifest` pins. That QA figure is unknown; lane 1
was asked for a seven-day readback. The fan-out bound is per generation, not
per pin: a 500-pin push is one intent
(`TestBuildProjectionQueuesOneSupplyChainImpactForGitManifestChange`) and one
holder read. Per marker pass, measured on the synthetic data above:

| Census scale | Holder read (median) | Generation reads | Reopens |
| --- | --- | --- | --- |
| 1x | 7.0 ms | about 1-5 ms per generation read | H x 0.12 ms p50 (#9695b) |
| 5x | 21.4 ms | same | same |
| 10x | 41.5 ms | same | same |

H is the number of advisory scopes holding a changed package. Each reopened
holder then runs one ordinary `supply_chain_impact` pass; that downstream cost
dominates and is the QA `supply_chain_impact` handler duration, which is not
measured here. A full re-index of a repository with no dependency change
compares equal to its prior and enqueues no reopen.

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
- Pending generation: retried under a counting class, so a generation that
  never activates dead-letters the marker after the attempt budget instead of
  retrying forever.

## Known gap (correctness)

When the prior generation is a delta whose base cannot be read (missing,
pruned, or a legacy delta without a baseline), the prior view is the delta's
own rows. A pin that only the base held then looks never-present, so its
removal is not fanned out and **the finding for the removed package stays
visible until that package's advisory scope reruns** (today's pre-#9874
behavior for that one case). The pass reports
`manifest_trigger_prior_base_missing=1`.
`TestManifestTriggerKnownLimitPriorDeltaWithMissingBase` pins the behavior and
fails if the gap closes. A prior whose facts retention pruned counts as no
prior, so every current pin fans out and a removal is likewise missed.

## Telemetry

Observability Evidence: `eshu_dp_supply_chain_impact_fanout_total{cause="manifest_dependency"}`
with the existing outcomes (reopened, replay_flagged, pending_noop, enqueued,
deferred, read_error, read_timeout, reopen_error, state_error); the
`supply_chain_impact.fanout` span with `cause`; the `supply chain impact holder
fan-out` log line; the deferral log `supply chain impact manifest trigger
deferred until generation activates` (key `manifest_trigger_deferral`, failure
class `supply_chain_impact_manifest_trigger_deferred`); result sub-signals
`manifest_trigger`, `manifest_trigger_changed_packages`,
`manifest_trigger_delta_paths`, `manifest_trigger_prior_base_missing`,
`manifest_trigger_superseded`. No new instrument.

Performance Evidence: holder-read and generation-read figures above (synthetic,
PG 18.6, scratch database); predicate microbenchmark above.

## Not checked

- QA convergence delay before and after (acceptance items 1 and 7).
- QA git-generation rate and `supply_chain_impact` handler duration, so the
  daily intent volume and downstream cost are stated as formulas only.
- Golden-corpus B-7 replay and the B-12 snapshot.
- NornicDB: no graph path is touched.
