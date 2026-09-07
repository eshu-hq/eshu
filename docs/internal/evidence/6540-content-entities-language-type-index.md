# Evidence: #6540 — `content_entities` language search walks the whole path index when its filter matches nothing

Scope: `SearchEntitiesByLanguageAndTypeForAccess`
(`go/internal/query/content_reader_entity_search.go`), the one SQL choke point
behind every content read `POST /api/v0/code/language-query` makes.

This is a **Mandatory Prove-The-Theory-First** proof. The defect was reproduced
and both candidate fixes named in #6540 were measured with
`EXPLAIN (ANALYZE, BUFFERS)` on representative data **before** any production
code changed.

## Machine / backend profile (resource-qualified)

- Remote validation host, 16 logical CPU, 123 GiB RAM, `/dev/root` on
  `nvme0n1p1` (`ROTA=0`, internal NVMe SSD, 295 GB free). **Not** the
  USB-attached volume — that volume stalls 4–6 s on first write after idle and
  would have landed inside these timings.
- PostgreSQL **16.9** (workspace build), own cluster under
  `/home/ubuntu/eshu-6540-measurement`, own port, torn down after the run.
  **Not 16.15**, which is what #6540 reports: the only Postgres *server*
  binaries on that host are 16.9 (the 16.15 install is client-only). PG16 minor
  releases carry no planner changes, so the reproduction holds, but the version
  is stated rather than claimed equal.
- The host is shared. Load average ran **7.6–11.5 on 16 cores** and
  `MemAvailable` **93–111 GiB** throughout; load is recorded before and after
  every arm in the run log. Wall times are therefore reported with that caveat,
  and buffer counts — which are load-independent — carry the claim.
- `absolute_target_applicable`: false. These are relative before/after
  measurements gating an index-adoption decision, not a reference-profile
  wall-clock target.

Planner-relevant settings were left at Postgres defaults so plan choice matches
production: `random_page_cost = 4.0`, `seq_page_cost = 1.0`,
`effective_cache_size = 4GB`, `work_mem = 4MB`,
`default_statistics_target = 100`. Set deliberately and **not** planner-relevant:
`shared_buffers = 2GB`, `maintenance_work_mem = 1GB`, and `fsync`,
`synchronous_commit`, `full_page_writes` off (load speed only; they do not
change read plans or warm read timings). Per session: `SET jit = off` and
`SET max_parallel_workers_per_gather = 0`, so plan choice is deterministic.

## Corpus

Schema applied by running all **128** files in
`go/internal/storage/postgres/migrations/` in sorted path order — the order
`BootstrapDefinitions` uses — against a checkout verified at
`392351ffd2c8a4366c82a54f20a5c8a322820b3a`. **128 applied, 0 failures**, so the
index set is the real one, not an approximation.

- 2,000,000 rows across 600 repositories, 3,333–3,334 rows each.
- Heap 119,499 pages, table 934 MB, existing indexes 1,329 MB.
- `VACUUM (ANALYZE)` before any plan was read.
- `relative_path` is md5-scattered, so an ordered walk of
  `content_entities_path_idx` is a random heap traversal.

Key denominators:

| population | rows |
| --- | ---: |
| `hcl` + `Function` (the zero-match filter) | **0** |
| `go` + `Function` (the matching control) | 132,000 |
| `hcl`, all entity types | 120,000 |
| `Function`, all languages | 486,000 |

The zero match is not contrived: HCL has no function declarations, so
`(hcl, Function)` is empty in any real corpus while both halves are populated.

## The statement

Not paraphrased. Captured from the production path with the repo's recording
driver, so it is exactly what `SearchEntitiesByLanguageAndTypeForAccess` sends:

```sql
		SELECT entity_id, repo_id, relative_path, entity_type, entity_name,
		       start_line, end_line, coalesce(language, ''), coalesce(source_cache, ''),
		       metadata
		FROM content_entities
		WHERE entity_type = $1 AND (language = $2)
		ORDER BY relative_path, start_line, entity_name
		LIMIT $3
```

and, for a scoped caller who named no repository,
`WHERE entity_type = $1 AND repo_id = ANY($2) AND (language = $3)`.

Executions replicate pgx v5's default exec mode: `PREPARE`/`EXECUTE`, five
executions first so `plan_cache_mode = auto` settles, then five sampled
`EXPLAIN (ANALYZE, BUFFERS)` runs. All five samples are recorded, not a bare
median, so a bad spread would be visible rather than averaged away.

## Reproduced

Arm 1, five warm samples: 2634 / 2574 / 2599 / 2580 / 2560 ms — under 3% spread.

```
Limit (cost=20.50..1026.08 rows=50 width=149) (actual time=2634.058..2634.060 rows=0 loops=1)
  Buffers: shared hit=2013451
  -> Incremental Sort (cost=20.50..573498.41 rows=28515 width=149) (actual rows=0)
       Sort Key: relative_path, start_line, entity_name
       -> Index Scan using content_entities_path_idx on content_entities (actual rows=0)
            Filter: ((entity_type = 'Function'::text) AND (language = 'hcl'::text))
            Rows Removed by Filter: 2000000
            Buffers: shared hit=2013451
Execution Time: 2634.085 ms
```

**Root cause.** No index in the tree carried `language` at all — verified
against the migration set: `content_entities` had 11 indexes and **zero**
mentioning `language`. So `language` could only ever be a `Filter`. The planner
estimates `entity_type = $1 AND language = $2` by multiplying the two
selectivities as though independent, giving **rows=28,515 for a combination that
has 0**. A non-zero estimate makes the `LIMIT` look like it fills early, so the
ordered walk costs out at 1026 and wins — then walks all 2,000,000 rows.

## Candidate comparison

Medians of five warm samples, same corpus, same warm state, same plan-cache
state.

| arm | baseline | A1 `(language, entity_type)` | A2 `(language, entity_type, relative_path, start_line, entity_name)` |
| --- | ---: | ---: | ---: |
| 1 unscoped, zero-match | 2590 ms | 2601 ms | **0.012 ms** |
| 2 grant = 500 repos, zero-match | 2591 ms | 0.022 ms | **0.012 ms** |
| 3 grant = 1 repo, zero-match | 22.4 ms | 0.458 ms | 0.470 ms |
| 4 unscoped, MATCHING | 0.41 ms | 0.313 ms | **0.040 ms** |
| 5 grant = 500, MATCHING | 0.45 ms | 0.418 ms | **0.058 ms** |
| 6 unscoped, multi-variant `typescript OR tsx` | 0.54 ms | 0.58 ms | 0.537 ms |

| candidate | build | size |
| --- | ---: | ---: |
| A1 | 1.9 s | 14 MB |
| A2 | 3.1 s | 173 MB |

### A1 rejected — the planner does not take it

#6540 left this open: "if the planner takes it under the `ORDER BY`". It does
not, for the unscoped caller. With A1 present the plan is **byte-identical to
the baseline** — still `Index Scan using content_entities_path_idx`, still
`Buffers: shared hit=2013451`, still `Rows Removed by Filter: 2000000`, still
~2601 ms. The new index is never read, because it does not serve the `ORDER BY`,
so any plan using it needs a sort that the (wrongly non-zero) row estimate makes
look expensive beside a walk the `LIMIT` is expected to cut short.

A1 *does* fix both grant arms (2591 ms → 0.022 ms), which is a real partial
result worth recording. But the unscoped caller is the one #6540 is about.

### A2 accepted

```
Limit (cost=0.55..180.68 rows=50 width=149) (actual time=0.009..0.009 rows=0 loops=1)
  Buffers: shared hit=4
  -> Index Scan using content_entities_language_type_path_idx on content_entities
       Index Cond: ((language = 'hcl'::text) AND (entity_type = 'Function'::text))
       Buffers: shared hit=4
```

The filter becomes an `Index Cond` and the `Sort` node disappears — the index's
trailing columns are exactly the `ORDER BY` key. **2,013,451 buffers → 4.**
Strict improvement on every arm, regression on none; the matching case gets ~10x
faster too, because removing the sort helps a page that does have rows.

**What it does not fix.** `normalizedLanguageVariants` returns two spellings for
`javascript`, `typescript` and `csharp`, which the builder emits as
`(language = $2 OR language = $3)`. An `OR` cannot drive one ordered index scan,
so those three keep today's plan — arm 6 is unchanged at 0.537 ms, neither
helped nor harmed. Their zero-match case is still bounded, because a `BitmapOr`
over an empty set is cheap, but this index is not what bounds it. Emitting
`language = ANY($n)` instead would be a query-shape change with its own proof
obligation and is deliberately not bundled here.

### Candidate (b), the EXISTS pre-check, rejected

Measured on the zero-match case **and** the matching case, because a fix that
repairs empty answers by taxing every non-empty one is not a fix.

| case | median | plan |
| --- | ---: | --- |
| zero-match, unscoped | **603.9 ms** | `Seq Scan`, `Buffers: shared hit=119499`, `Rows Removed by Filter: 2000000` |
| MATCHING, unscoped | 0.007 ms | short-circuits on the first row |
| MATCHING, grant = 500 | 0.018 ms | short-circuits on the first row |

On the baseline index set the `EXISTS` degrades to a sequential scan, because
with no index on `language` it must still check every `Function` row before it
can conclude "none". It would replace a 2,590 ms empty answer with a 604 ms one
— still three orders of magnitude off A2 — while adding a round trip and
0.007–0.018 ms to every non-empty call. It is only cheap once an index like A2
exists, at which point the extra statement earns nothing.

## Correctness before performance

Row sets captured position by position with and without A2 over six shapes —
single language, the multi-variant `OR`, `hcl/Resource`, a 500-repository grant,
an `entity_name ILIKE` query, and the zero-match case — 1,000 rows per arm.

- rows only in WITHOUT: **0**
- rows only in WITH: **0**
- position-by-position mismatches: **0**

The first run of this differential returned 0 rows from a malformed
`= ANY((SELECT array_agg(...)))`. It was caught because the harness prints
capture counts; a positive control was then added that aborts if a capture
returns fewer than 600 rows, so an empty result can never read as agreement.

## Cost

`content_entities` is hot and continuously ingested, so the write path pays for
one more btree. `EXPLAIN (ANALYZE, BUFFERS, WAL)` over 200,000-row inserts, arms
alternated over two rounds:

| round | without | with | delta |
| --- | ---: | ---: | ---: |
| 1 | 3,389,323 WAL records | 3,552,237 | +162,914 (+4.8%) |
| 2 | 3,311,186 WAL records | 3,522,142 | +210,956 (+6.4%) |

About one extra WAL record per row. **WAL bytes are not the claim**: full-page
image counts varied 11,906–30,360 between arms and moved the byte totals the
opposite way (1,168 MB → 1,098 MB, 1,099 MB → 1,073 MB). That is fpi noise, not
a write saving, and must not be read as one. Seconds are not the claim either —
the host was shared and under load 7–11 throughout.

Index size is 173 MB on a fresh `CONCURRENTLY` build (3.1 s) and grew to 193 MB
after the insert/delete churn of the write-amplification runs, against a 934 MB
heap and 1,329 MB of pre-existing indexes. Nothing is dropped to pay for it; no
existing index is superseded by it.

Performance Evidence: unscoped zero-match `content_entities` language search
2,590 ms / 2,013,451 buffers before, 0.012 ms / 4 buffers after, on 2,000,000
rows across 600 repositories; matching filters unchanged in row set and ~10x
faster; write cost +4.8%/+6.4% WAL records over two alternated rounds.

No-Observability-Change: this change adds one index through the existing
bootstrap replay path. It adds no metric, span, log, or status field, and
changes no existing one. The read it accelerates is already covered by the
`postgres.query` span `SearchEntitiesByLanguageAndTypeForAccess` starts, with
`db.operation = search_entities_by_language_and_type`.

## Replay safety

`BootstrapDefinitions` enumerates every file under `migrations/` and
`ApplyDefinitions` Execs all of them on **every** bootstrap; there is no applied
ledger. Migration 106 therefore only creates, under a new name, with
`IF NOT EXISTS`, and no file in the tree drops it — so a bootstrap over an
install that already has the index does no index work.
`TestContentEntitiesLanguageTypeIndexIsCreatedOnceAndNeverDropped` pins that
statically, and `TestContentEntitiesLanguageTypeIndexCarriesTheOrderByKey` pins
the column list, because a truncated index would keep the name and the replay
guarantee while silently restoring the defect.

## Teardown

The measurement cluster was stopped and its data directory removed; the
confirmation is in the run report. Only the aborted run's *binaries* were read;
the live corpus run's cluster, data directory and processes were never touched.
