# Supply-chain aggregate index scale evidence

Tracks the read-path index fix for the three supply-chain aggregate endpoints in
#3389 that timed out (>20-30s) at collector scale (~502,865 graph nodes,
`fact_records` grown to collector scale by the cloud/SaaS/package/vuln/sbom
collectors). The companion read-model/query-shape evidence lives in
`go/internal/query/evidence-notes.md` under "Supply-chain aggregation endpoints
(#3389)".

## Problem

Three handlers aggregate `fact_records` for a single `fact_kind`:

- `GET /api/v0/supply-chain/advisories` — `vulnerability.cve` (catalog spine) and
  `vulnerability.known_exploited` (KEV CTE), each enumerated with no `cve_id`
  anchor.
- `GET /api/v0/supply-chain/impact/findings/count` —
  `reducer_supply_chain_impact_finding`, enumerated then deduped with
  `ROW_NUMBER() OVER (PARTITION BY canonical_key ...)`.
- `GET /api/v0/supply-chain/sbom-attestations/attachments/count` —
  `reducer_sbom_attestation_attachment`, a global `COUNT(*)` plus two `GROUP BY`
  rollups.

In the common "count/list everything" case the per-payload filters are all
no-ops, so there is no payload anchor. The existing partial indexes for these
fact kinds all lead with a payload expression (`payload->>'cve_id'`,
`payload->>'subject_digest'`, `payload->>'impact_status'`, ...). With no
predicate on the leading payload column the planner cannot use them as a bounded
access path, so it falls back to a sequential scan of all of `fact_records`
(every `fact_kind` at collector scale) or a full scan of a wide payload-leading
index. That whole-table scan is the timeout.

## Change

Add one partial index per aggregated fact_kind whose `WHERE` clause is exactly
the query's fact-kind bound and whose key columns are the active-generation join
keys:

```sql
CREATE INDEX IF NOT EXISTS fact_records_<kind>_active_scan_idx
    ON fact_records (scope_id, generation_id, fact_id ASC)
    WHERE fact_kind = '<kind>' AND is_tombstone = FALSE;
```

| Index | fact_kind | File |
| --- | --- | --- |
| `fact_records_vulnerability_cve_active_scan_idx` | `vulnerability.cve` | `schema_fact_records_vulnerability_indexes.go` |
| `fact_records_vulnerability_known_exploited_active_scan_idx` | `vulnerability.known_exploited` | `schema_fact_records_vulnerability_indexes.go` |
| `fact_records_supply_chain_impact_active_scan_idx` | `reducer_supply_chain_impact_finding` | `schema_fact_records.go` |
| `fact_records_sbom_attestation_attachments_active_scan_idx` | `reducer_sbom_attestation_attachment` | `schema_fact_records_sbom.go` |

A *partial* index differs from a *covering* index here: the partial `WHERE`
clause is baked into the index contents, so the index physically holds only the
qualifying rows. Scanning it is therefore bounded to the single fact_kind's
active, non-tombstone tuples regardless of whether a leading-key predicate
exists. The earlier #3389 follow-up note rejected an index fix on the grounds
that "a covering index does not bound a global rollup/count" — true for a
payload-leading covering index, but it does not apply to a partial index that
restricts the scanned row set by predicate. The `(scope_id, generation_id)`
leading key columns are the join keys to `scope_generations` / `ingestion_scopes`,
so the planner can drive the active-generation join straight from the index. No
payload columns are covered because the count/group expressions are JSONB and
vary per aggregate; the load-bearing property is the partial predicate, not
coverage.

No GIN index was added for the SBOM `payload->'..._ids' ?| $n` source-scope
array-containment filters: those already have repository/workload/service GIN
anchor indexes, and an extra one would add write amplification (#3383 caveat) for
a path the new btree partial index already bounds.

## Performance Evidence

This environment has no provisioned ~500k Postgres stack, so a live
`EXPLAIN (ANALYZE)` wall-clock capture was not run (stated per
cypher-query-rigor; the no-live-backend posture matches #3380/#3384). The
load-bearing proof is the partial-predicate bounding argument plus the
query-shape and index-presence tests:

- Before: no bounded access path for the no-anchor case; the planner scans all of
  `fact_records` (every `fact_kind` at collector scale) or fully scans a wide
  payload-leading index.
- After: a partial index containing exactly the fact_kind's active tuples,
  ordered on the `(scope_id, generation_id)` join keys; the aggregate scan is
  bounded to that fact_kind's active row count. The scan is index-only when the
  heap pages are all-visible (vacuum-fresh); on a write-heavy table it may take
  heap fetches but stays bounded to the fact_kind.

Live finalization on a ~500k stack: `ANALYZE fact_records`, then run
`EXPLAIN (ANALYZE, BUFFERS)` on each aggregate with all payload params `''` and
confirm the plan node is an Index Scan / Index Only Scan on the new
`..._active_scan_idx`, not a Seq Scan on `fact_records`.

## No-Regression Evidence

The index is additive; no aggregate SQL changed, so every endpoint returns
byte-identical results (the fact-kind anchor, `is_tombstone = FALSE` filter,
active-generation join, dedupe ranking, grouping, ordering, and pagination are
untouched). Covered by `go test ./internal/storage/postgres -run
'TestBootstrapDefinitionsIncludeAdvisoryCatalogActiveScanIndexes|TestBootstrapDefinitionsIncludeSupplyChainImpactFactIndexes|TestBootstrapDefinitionsIncludeSBOMAttestationAttachmentFactIndexes'`
(index DDL presence and shape) and `go test ./internal/query -run
'TestAdvisoryCatalogQueryKeepsPerFactKindActiveScanAnchor|TestSupplyChainImpactAggregateQueriesKeepActiveScanAnchor|TestSBOMAttestationAttachmentAggregateQueriesKeepActiveScanAnchor'`
(the bounded query shape the indexes depend on).

## Write-Amplification

Each new index adds one btree entry per insert/update of its fact_kind's
non-tombstone rows. The keys are the existing `scope_id` / `generation_id` /
`fact_id` scalars (no JSONB extraction at write time), and the partial predicate
excludes tombstoned rows, so the per-row cost is small and the index stays
restricted to the one fact_kind's active population. This is the standard
read-index trade-off already used by the sibling `fact_records_*_lookup_idx`
partial indexes.

## No-Observability-Change

No metric, span, log, status row, graph write, or queue consumer is added or
altered. The fix only changes which rows the supply-chain aggregates scan; the
endpoint payloads, truth envelopes, and `postgres.query` spans/duration metrics
are unchanged.
