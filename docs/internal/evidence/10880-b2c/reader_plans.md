# #10880 PR-B2c reader plans: security-alert reconciliation and service-catalog vulnerability loaders

Refs #11198, #11199, #11200, #11201, #10880. Ruling:
[arbiter-10880-prb-ruling.md decision 5](../../../.claude/arbiter-10880-prb-ruling.md).

Two reducer loaders read supply-chain impact finding facts as input. They now
read `supply_chain_impact_finding_supports` (migration 338), whose payload is the
fact payload verbatim. This note records why both reads use the raw-first shape
and what they cost.

## The plan flip

The natural form, one `WHERE` over the supports joined to the active scopes and
generations, lets the planner drive the join from the active scopes: one
`(scope_id, generation_id)` index probe per active scope, in a nested loop, then
the filter. Its cost scales with the number of active scopes, not with the rows
the filter keeps.

The raw-first form selects the supports by their filter first, in a
`MATERIALIZED` CTE, then joins only those rows to the active-generation check
(`supplyChainImpactSupportRawFirstCTE`). Its cost scales with the rows kept.

## Measurements

Disposable PostgreSQL 18.6 (`postgres:18.6`), one session, warm cache, host load
average about 25 from other lanes (read the ratios, not the milliseconds). The
corpus is synthetic: 600,000 supports (table 419 MB with its indexes) over
6,000 active scopes, one active generation each, 60,000 distinct repositories,
20,000 packages, 5,000 CVEs and 7,000 advisories, with the migration-338 columns
and the primary key, `(scope_id, generation_id)`, `repository_id`, `finding_id`
and `generation_id` indexes (the foreign key is omitted).

| Read | Filter | Plan | Execution | Buffers touched |
| --- | --- | --- | --- | --- |
| Join form (shipped statement shape before this change) | repository, package, 3 CVEs, 2 advisories | nested loop, 6,000 probes of `(scope_id, generation_id)` | 389.5 ms | 618,084 |
| Join form | repository only | same | 349.8 ms | 618,084 |
| Join form | 5 repositories (#11199 shape) | same | 313.6 ms | not recorded |
| Raw-first, with new `package_id`, `cve_id`, `advisory_id` indexes | all four arms | `BitmapOr` over four index scans | 3.9 ms | 572 rows kept |
| Raw-first, with those indexes | repository only | bitmap scan of `repository_id` | 0.1 ms | 10 rows kept |
| Raw-first, no new indexes | all four arms | parallel sequential scan of the supports | 50.8 ms | one pass over the table |
| Raw-first | 5 repositories (#11199 shape) | bitmap scan of `repository_id` | 1.1 ms | 50 rows kept |

The `repository_id` index exists today, so #11199 and the repository arm of
#11198 run in the raw-first form at 0.1 to 1.1 ms with no new index. The
package, CVE and advisory arms of #11198 have no index: without one the OR scans
the table (51 ms at 600,000 supports, parallel, growing with the table). This PR
ships the raw-first form with no new index. Three btree indexes on those columns
would bring the read to 3.9 ms; they add write cost to every impact save and
need a migration, so they are a separate decision (see Open points).

## The legacy payload array arms

The legacy reconciliation statement also matched `payload->'cve_ids' ?| ...` and
`payload->'ghsa_ids' ?| ...`. The typed finding payload
(`reducerderivedv1.SupplyChainImpactFinding`) has no such field, so those arms
never matched a finding fact, and keeping them would make the OR unindexable.
`TestSupplyChainImpactFindingPayloadCarriesNoIDArrays` fails if either field is
ever added.

## Equality

`TestSecurityAlertReconciliationAndAdvisoryLoadersEqualFactReadsLive` writes a
multi-scope seed through the production impact writer (two repositories sharing
a package, a CVE-less advisory finding, a repository-less finding, a superseded
generation, a retracted finding) and compares each new read with the frozen
finding-fact read, for every filter shape: same fact ids, same kind, same
payload, same order. Deleting the CVE arm of the supports query fails its `cve`
and `combined` cases.

## No-Regression Evidence

No-Regression Evidence: baseline is the finding-fact loaders on `bccdb88982`;
after is the supports loaders on PostgreSQL 18.6 over the synthetic corpus above.
The join form measured 313 to 389 ms and the raw-first form 0.1 to 51 ms, so the
shipped form is the faster of the two on every shape measured; the
reconciliation read costs 51 ms without the three optional indexes where the
legacy keyed read (sidecar index probes) was not measured here. Worker, lease and
batch settings are unchanged: both loaders are single read statements per page.
The finding-fact write path gains no statement.

## Observability Evidence

Observability Evidence: `eshu_dp_supply_chain_impact_supports_reads_total`
(`source` = reader, `outcome` = `served`, `not_ready`, `failed`) shows each
loader's use of the supports table, and a refusal is a retryable error with
failure class `supply_chain_impact_supports_not_ready` in the durable queue.
`eshu_dp_supply_chain_impact_supports_backfill_ready` says when the refusals
stop. Both are documented in
[supply-chain-impact-supports.md](../../../public/reference/telemetry/supply-chain-impact-supports.md).

## Open points

- Three indexes (`package_id`, `cve_id`, `advisory_id`) on the supports table
  would cut the reconciliation read from 51 ms to 3.9 ms at this size. They need
  a migration and add write cost to every impact save; the arbiter's WAL bar for
  the supports table (impact-write WAL not above +25%) applies. Not built here.
- Rows the key sidecar built for impact finding facts before this change remain
  in `security_alert_reconciliation_keys`. Nothing reads them; a cleanup delete is
  a separate write on a large table and is not part of this change.
- The supports readiness check adds one marker read per load. Until the backfill
  is ready both loaders refuse and their intents retry; the not-ready counter is
  the signal.
