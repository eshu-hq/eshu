# #8954 status blockage names the domain that holds the fence

Acceptance criterion 3 of #8827 asked the status tools to show a blocked
reconciliation and its holder. The blockage view named the blocked domain and
its conflict key but not the holder, so an operator had to find the holder by
hand.

## The change

Each `queue_blockages[]` row carries `holder_domain`: the `domain` of the one
live `claimed` or `running` reducer row whose `(conflict_domain,
COALESCE(conflict_key, scope_id))` equals the row's own. A row blocked only by a
missing readiness requirement has no lease and reports an empty string. The
field is present on every row.

`fact_work_items_reducer_live_lease_uniq` allows one such row per key, and the
blockage CTEs already select exactly those rows as `inflight_leases` (#6794), so
the holder is read from the lease set the blocked filter already uses. No new
table read, index, migration, or query round trip.

## Why a one-row map and not a join

The first version joined `blockage_groups` to `inflight_leases`. With no
statistics (a bulk load, before the first ANALYZE) the planner chose a nested
loop and scanned the lease CTE once per group: `an inflight_leases scan ran 100
times, want 1 (loops [1 100])` on 200 scopes and 100 live leases. That is the
#6794 failure mode. `holder_map` aggregates the lease set into one jsonb object
keyed by `jsonb_build_array(conflict_domain, conflict_key)::text`, so no key can
collide, and `blockage_rows` cross-joins that single row. There is no join
method left to choose.

## Proof

Differential Evidence (disposable PostgreSQL 18.6, all migrations;
`TestReducerConflictBlockageNamesHolderDomain`): a pending
`security_alert_reconciliation` and a claimed `supply_chain_impact` row, both
with `conflict_domain = 'scope'` and `conflict_key` NULL on one scope, report
`HolderDomain = supply_chain_impact`; a readiness-gated row reports empty; the
holder is not itself listed as blocked. The test fails without the change
(`HolderDomain = ""`).

Live Endpoint Evidence: a throwaway harness (not committed) bootstrapped the
migrated schema, seeded the same three rows relative to the current time, and
served the real `status.NewHTTPHandler` over a Postgres-backed
`NewStatusStore`. `GET /admin/status?format=json` returned HTTP 200 with
`queue_blockages` holding `security_alert_reconciliation` /
`conflict_domain scope` / `holder_domain "supply_chain_impact"` and
`aws_relationship_materialization` / `conflict_domain readiness` /
`holder_domain ""`. `?format=text` printed
`... conflict_key=scope-fence holder_domain=supply_chain_impact blocked=1 ...`
for the first and no `holder_domain` token for the second.

Performance Evidence: `EXPLAIN (ANALYZE, BUFFERS)` of `activeWorkSummaryQuery`
on the same disposable instance at 350,000 eligible pending reducer rows (5,000
scopes, 70 each) and 5,000 claimed lease rows of which 200 are live, analyzed,
three runs each, old statement then new:

| Statement | Execution ms (3 runs) | Planning ms (3 runs) |
| --- | --- | --- |
| before | 1619.99, 1407.35, 1336.85 | 14.44, 3.15, 3.13 |
| after | 1362.83, 1359.65, 1354.52 | 3.22, 3.37, 3.24 |

The lease set still comes from a bitmap AND of `fact_work_items_claim_until_idx`
and `fact_work_items_status_idx`, both existing. `CTE Scan on inflight_leases`
runs once for the hashed blocked filter and once for `holder_map`
(`loops=1`, 200 rows each); `CTE Scan on holder_map` is one row, `loops=1`. The
run was on a shared host and is a no-regression check, not a throughput claim.
`TestActiveWorkSummaryHolderLookupScansLeasesOnce` pins the loops and fails on
the join form, and `TestRequireLeaseScansRunOnceRejectsRescan` seeds the
violation the guard must refuse.

No-Observability-Change: the status payload is the operator signal; the field
adds the holder to the existing `queue_blockages` diagnostics. No metric, span,
log, label, or env var changed, and the holder's work item id and scope id stay
out of the payload so no unbounded identifier is exposed.

## Pinned renders re-derived

The blockage fragment is inside text that tests pin by SHA-256. The history
oracle (`activeWorkSummaryPreHistoryGroupsOracle`) was re-derived with the same
three fragment edits and re-pinned, as its lifecycle note allows;
`TestActiveWorkSummaryDiffersFromOracleOnlyInHistoryGroups` still reproduces it
byte for byte from the shipped query. `activeWorkSummaryQuerySHA256` and the six
S5 shim hashes in `status_active_work_gate_test.go` were re-rendered. The shim
hashes no longer equal the historical shim files byte for byte; the only
difference is the blockage fragment. This needs reviewer sign-off.

## Compatibility

A stored status summary row written before this change has no `holder_domain`.
`activeWorkRow.optionalText` decodes the missing key as empty, so a reader
never rejects rows an older writer stored (`TestActiveWorkSummaryDecodesBlockageHolderDomain`).
A present key of the wrong type still fails the summary.

## Not covered

- The MCP index-status tool passes `queue_blockages` through as generic
  objects, so no MCP schema or pin changed; the tool contract was not exercised
  against a live MCP server.
- A deployed run against a real reconciliation blocked behind a long
  `supply_chain_impact` run.
- Scoped callers still receive no `queue_blockages` at all (withheld, #5167).
