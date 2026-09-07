# Evidence: #6555 — the relationship-story target read asks for the exact name

Follow-up to #6553 (batch 2b of #5167). `POST /api/v0/code/relationships/story`
resolved a target name through `ContentReader.SearchEntitiesByName`, an
`entity_name ILIKE '%' || $2 || '%'` read bounded by `LIMIT`, and then kept only
the rows whose name equalled the target (`exactEntityNameMatches`). #6553 gave
each granted repository its own budget, which fixed the case where an exact
symbol sat in a repository further down the grant. It could not fix the case
inside one repository: there the `LIMIT` page fills with near-misses before the
exact row is reached, and no way of dividing a budget across one repository
changes that.

The fix is the read, not the budget. `SearchEntitiesByExactName` and
`SearchEntitiesByExactNameAnyRepo` ask `entity_name = $1`.

This is a **Mandatory Prove-The-Theory-First** proof. The theory under test was
"an `entity_name = $n` read on `content_entities` is served by an index that
already exists, so this fix needs no new index." Every plan below was measured
with `EXPLAIN (ANALYZE, BUFFERS)` against a throwaway representative partition
**before** the index question was answered in the PR.

## Machine / backend profile (resource-qualified)

- `machine_profile`: Apple silicon (arm64) macOS host, shared with five sibling
  agents during the run; `/Volumes/Storage` is USB-attached and was saturated.
- Postgres: `postgres:16` in Docker, throwaway container `eshu-6555-pg`,
  database `eshu`, no persistent volume, destroyed after the run.
- `absolute_target_applicable`: false — these are relative before/after shim
  measurements gating an index-adoption decision, not a reference-profile
  wall-clock target. The host contention makes the absolute milliseconds
  unusable as a target; the plan shapes and buffer counts are what the decision
  rests on.

## Seeded partition

Real `content_entities` DDL plus every index the shipped migrations create
(004, 035, 062, 077): `content_entities_pkey`, `_repo_idx`, `_type_idx`,
`_path_idx`, `_repo_entity_idx`, `_source_trgm_idx`, `_name_trgm_idx`,
`_artifact_type_idx`, `_template_dialect_idx`, `_iac_relevant_idx`,
`_k8s_select_partial_idx`. `VACUUM ANALYZE` before measuring.

| slice | rows |
| --- | ---: |
| total | 500,001 |
| `entity_name ILIKE '%PaymentGateway%'`, corpus-wide | 100,001 |
| `entity_name ILIKE '%PaymentGateway%'`, repo `service-00` | 25,001 |
| `entity_name = 'PaymentGateway'` | 1 |

The one exact row lives in `service-00` on `zzz/gateway.go`, which sorts after
every near-miss under the read's `ORDER BY relative_path, start_line`. That is
the defect's worst case, and it is not contrived: the ordering is by path, so
whether the exact row lands inside the page is decided by where its file sorts,
not by anything about the name.

## Result: no new index. `content_entities_name_trgm_idx` serves the equality

The GIN `gin_trgm_ops` index migration 062 creates on `entity_name` answers
`entity_name = $n` as an **`Index Cond`**, not as a recheck filter:

```
->  Bitmap Index Scan on content_entities_name_trgm_idx
      Index Cond: (entity_name = 'PaymentGateway'::text)
      Buffers: shared hit=87
```

The predicate is also not a new shape for this table. `ContentReader.SearchSymbols`
(`content_reader_symbol_search.go`) already ships `entity_name = $1`, with and
without a `repo_id = $n` companion, against this same index set.

## Measurements

| # | read | answer | Execution Time | Buffers | plan |
| --- | --- | --- | ---: | ---: | --- |
| A | repo-bound substring, `LIMIT 3` (shipped) | **wrong** (`not_found`) | 0.194 ms | 25 | `content_entities_path_idx` scan + filter |
| B | repo-bound exact, `LIMIT 3` (new) | right | 0.591 ms | 88 | `content_entities_name_trgm_idx` equality |
| A2 | repo-bound substring widened until the page *could* contain the exact row (`LIMIT 25002`) | right | 80.296 ms | 10,524 + 413 temp | `BitmapAnd` + external merge sort (3,304 kB to disk) |
| C | corpus-wide substring, `LIMIT 3` (shipped) | **wrong** (`not_found`) | 85.758 ms | 50,727 | `content_entities_repo_idx` scan, 125,002 rows touched |
| D | corpus-wide exact, `LIMIT 3` (new) | right | 0.328 ms | 88 | `content_entities_name_trgm_idx` equality |

Read the table by answer, not by row:

- **Corpus-wide the new read is faster and correct**: 85.758 ms → 0.328 ms
  (~262x), 50,727 buffers → 88.
- **Repo-bound the new read costs 0.397 ms more than a read that returns the
  wrong answer.** A is fast because it stops after three near-misses; its speed
  is the defect. The only correct substring alternative is A2, and the new read
  is ~136x cheaper than that (0.591 ms vs 80.296 ms) with no external sort.

## Deferred-bootstrap worst case: no `entity_name` trigram index

The exact reads deliberately do **not** carry
`eshu_require_content_substring_indexes_ready()`. That guard `RAISE`s
(migration 057) while a deployment's deferred trigram bootstrap is unfinished,
and an equality read needs no trigram index to be correct — only to be fast.
Measured with the index dropped inside a rolled-back transaction:

| # | read | Execution Time | Buffers | plan |
| --- | --- | ---: | ---: | --- |
| F1 | repo-bound exact, no name index | 7.801 ms | 10,147 | `content_entities_repo_idx` bitmap + filter |
| F2 | corpus-wide exact, no name index | 16.092 ms | 10,123 | parallel seq scan |

Degraded and bounded, not failed. Routing this resolution through
`EntityNameSearcher.SearchEntityNames` — the existing exact-capable read — would
have raised an exception in this state instead, which is why that reuse was
rejected.

No-Regression Evidence: the corpus-wide read improves 85.758 ms → 0.328 ms
and the repo-bound read moves 0.194 ms → 0.591 ms, both sub-millisecond, against
the only correct substring alternative at 80.296 ms. No new index is added, so
there is no write-amplification or autovacuum cost to weigh.

No-Observability-Change: the new reads emit the same `postgres.query` span
shape as their substring twins, with `db.operation` values
`search_entities_by_exact_name` and `search_entities_by_exact_name_any_repo`.

## Reproduction

Throwaway container, destroyed afterwards:

```bash
docker run -d --name eshu-6555-pg -e POSTGRES_PASSWORD=eshu -p 55450:5432 postgres:16
docker exec eshu-6555-pg psql -U postgres -c 'CREATE DATABASE eshu;'
# schema.sql: the content_entities DDL from migration 004
# seed.sql:   500,000 generate_series rows + the one exact row + every shipped index + VACUUM ANALYZE
# explain.sql / explain2.sql: the statements above under EXPLAIN (ANALYZE, BUFFERS)
docker rm -f eshu-6555-pg
```
