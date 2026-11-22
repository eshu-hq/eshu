# Reducer result saves that rewrite an unchanged fact row: measurement (#11221)

Refs #11221. Part of #10860. Measurement first: no production code changed. The
decision this feeds is gate item 3 of the #11202 close-out and the update-WAL share
of `fact_records` write load: is a write-on-change guard worth building on the
reducer result saves, and for which families. Time-box 60 minutes (started
2026-11-22 08:02Z).

Performance Evidence: per-row write-ahead log (WAL) of an identical re-save, a guarded
skip and an insert, measured on a fixture with per-backend counters (below); the rate of
re-saves per reducer family comes from the QA read replica's queue (below). No runtime
behaviour changed.

No-Observability-Change: documentation, evidence and fixture scripts only.

## What the code does today

Every `ON CONFLICT (fact_id)` upsert into `fact_records` in production code
(derive with `rg -n 'ON CONFLICT \(fact_id\)' go --glob '!*_test.go'`):

| Path | Guard on the DO UPDATE | Identical re-save |
| --- | --- | --- |
| Collector upsert, `upsertFactBatchSuffix` in the storage package | fencing token and a NULL-safe `IS DISTINCT FROM` over every content column (not `observed_at` or `ingested_at`) since #10873 and #10965 | skipped, no new row version |
| Reducer batch, `BatchInsertConflict`, and versioned batch, in the factwrite package | fencing token only (`fencing_token <= EXCLUDED.fencing_token`) | rewritten |
| Reducer single row, `SingleInsertQuery` (workload identity, cloud asset, secrets and IAM, platform materialization, Kubernetes correlation, observability coverage) | none | rewritten |
| Search document writer, `eshu_search_document_writer_queries.go` | none | rewritten |

The reducer writers read for this record set `observed_at` and `ingested_at` to the time of the save, so even a
byte-identical re-derivation changes two columns. A guard has to leave those two out of
its comparison, as the collector path does. The supply-chain impact retraction updates
are a separate path and were not read for this record.

## What was measured

**1. Cost of one identical re-save (fixture, exact).** 50,000 rows with a 1.33 KB payload
(the replica's average for the largest reducer kind is 1.30 KB), one scope, on the
production schema at two index profiles. Per-backend WAL (`pg_stat_get_backend_wal`),
`VACUUM` and a checkpoint before the cold arms, and a second pass with no checkpoint for
the steady arms. `scripts/dev/11221-identical-saves/` reproduces it; raw output is in
`docs/internal/evidence/11221-identical-saves/fixture-wal.txt`.

| WAL per row | Retained 5 indexes | 153 indexes |
| --- | --- | --- |
| identical re-save as shipped, steady | 2,025 B | 2,156 B |
| identical re-save, same timestamps, steady | 1,963 B | 2,093 B |
| identical re-save as shipped, right after a checkpoint | 3,531 B | 3,662 B |
| fresh insert, right after a checkpoint | 1,953 B | 2,085 B |
| guarded identical re-save (content guard, timestamps ignored), steady | 54 B | 54 B |
| guarded identical re-save, right after a checkpoint | 1,569 B | 1,569 B |

- An identical re-save costs about the same WAL as inserting the row (1.03 to 1.04 times,
  steady), and it is never a heap-only-tuple update: `n_tup_hot_upd` was 0 of 50,000 in
  every arm, so every index entry that matches the row is rewritten too.
- The guard cuts the steady cost to 54 B a row (2.5% to 2.7%): The skipped rows still
  write one WAL record each (50,126 records for 50,000 rows), consistent with the row lock
  that `ON CONFLICT DO UPDATE` takes. Right after a checkpoint
  the first touch of each page writes a full-page image, so a skipped row costs 1.57 KB
  until the pages have been touched once.

**2. How often a reducer save hits an existing id (replica queue, exact counts).** A
reducer fact id includes the generation, so a first derivation of a generation inserts.
An update needs the same intent to run again for the same generation: a retry
(`attempt_count > 1`) or a reopen (`reopened_at` set). `fact_work_items` for the reducer
stage, succeeded items; the last column is the last seven days
(`docs/internal/evidence/11221-identical-saves/queue-aggregate.txt`):

| Domain (fact kind) | Succeeded items | Re-saved items | Succeeded in last 7 days | Re-saved in last 7 days |
| --- | --- | --- | --- | --- |
| eshu_search_document | 12,529 | 4 | 1,471 | 0 |
| workload_identity | 14,002 | 2 | 969 | 0 |
| platform_infra_materialization | 9,770 | 0 | 579 | 0 |
| code_drifted | 8,052 | 16 | 796 | 11 |
| observability_coverage_correlation | 443 | 0 | 93 | 0 |
| aws_cloud_runtime_drift | 34 | 18 | 24 | 8 |
| service_catalog_correlation | 36 | 29 | 8 | 7 |
| container_image_identity | 1,345 | 1,331 | 172 | 163 |
| supply_chain_impact (excluded) | 928 | 874 | 328 | 304 |

Row volume per kind comes from a 1% system sample of `fact_records` on the replica
(954,192 rows; `replica-kind-sample.txt`): the search document kind is 30% of the sampled
rows (287,067), the supply-chain impact finding 13% (122,213), code-drifted findings 0.3%
(3,040); every other reducer kind is below 0.02%. Scaled by 100, search documents are
about 2,300 rows per succeeded item and code-drifted findings about 38.

## What this shows, and what it does not

- The identical-content share per fact kind was **not measured**. The replica holds only
  the current row version, so it cannot show whether a re-save changed content. What the
  queue gives is an upper bound: the share of items that ran again at all.
- Outside the supply-chain family the re-save population is tiny. The three reducer domains with the
  most items (workload identity, search documents, platform materialization) had no
  re-saved item in the last seven days (0 of 1,471, 969 and 579).
  Code-drifted findings had 11 of 796: at about 38 rows an item that is at most about 420
  rewritten rows a week against about 28,000 inserted, under 1.5%, or roughly 0.8 MB of
  WAL a week at 2 KB a row. Search documents show 4 re-saved items in 12,529 over the
  whole history.
- The families that re-save heavily are supply-chain impact (304 of 328 items in the
  last seven days; its rows carry a fencing token above 1 in every sampled row), container
  image identity (163 of 172), and three small domains. Supply-chain impact is the family
  #10880 moves off this table, so it is excluded here: at about 13,000 rows an item the
  upper bound is on the order of 4M rewritten rows a week, 8 GB of WAL at 2 KB a row, if
  every reopen rewrote every row and none changed. Whether any were identical is
  unknown. Container image identity writes through an atomic writer that deletes by fact
  id and inserts (not read in full here, NOT_CHECKED), and its kind does not appear in the
  1% sample.
- The table-wide 88,254,540 updates (`n_tup_upd`, read 2026-11-17, before #10873 merged)
  therefore cannot be mostly reducer saves of the families that stay on the table: those
  families re-save almost nothing. The queue evidence points to the collector path (since
  guarded) and to the supply-chain family.

## Recommendation: do not build a reducer write-on-change guard now

For the reducer families that stay on `fact_records` after #10880, the re-save population
is at or near zero for the three largest and at most 1.5% for the next one, and an
identical re-save costs about as much as an insert, so a guard would save little. This is a
no-build recommendation for that scope, not a finding that the guard is useless: for
supply-chain impact the upper bound is large, the family is leaving the table, and the
identical share is unmeasured.

What would change it (a bigger or different measurement, not more code):

1. A read of `n_tup_upd` and `n_tup_hot_upd` for `fact_records` by someone with primary
   access, twice at least 24 hours apart (gate item 2 of the #11202 close-out), with
   `pg_stat_statements` rows for the reducer upserts, to say how many updates are reducer
   saves today.
2. The identical-content share for supply-chain impact, by replaying one reopen on a
   fixture and comparing row versions, only if the #10880 write stop slips.
3. A look at the container image identity atomic writer, which re-saves in almost every
   item but writes few rows.

## NOT_CHECKED

- Identical-content share per kind (needs row history or a replay).
- Primary counters: `n_tup_upd`, `n_tup_hot_upd`, WAL volume, rows updated per day.
- The supply-chain impact retraction updates and the container image identity writer.
- Whether the five reopen-fingerprint tables already suppress some reopens (they exist
  for aws drift, deployable unit, service catalog, workload materialization and container
  identity); the queue counts above are after that suppression.
- Fixture payloads are random hex of the replica's average size; real payloads compress
  differently, which moves the per-row WAL.
- No guard was built, so no before and after on a corpus exists.
