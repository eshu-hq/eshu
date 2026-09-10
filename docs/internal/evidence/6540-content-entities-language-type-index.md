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

Planner-relevant settings were left at Postgres defaults:
`random_page_cost = 4.0`, `seq_page_cost = 1.0`,
`effective_cache_size = 4GB`, `work_mem = 4MB`,
`default_statistics_target = 100`.

**These are Postgres defaults, not this repo's production setting**, and the
distinction matters here. `docs/public/reference/postgres-tuning.md` recommends
`random_page_cost = 1.1` on SSD and records that the B-7 golden-corpus gate runs
Compose Postgres at 1.1 — and it documents a 4.0 → 1.1 index-adoption flip on
this very table for an ordered-`LIMIT` read (#5490). The direction of risk is
favourable: a lower `random_page_cost` makes ordered index scans *more*
attractive, so A2's adoption is not at risk at 1.1 and the fix decision stands.
But the baseline numbers, and the **A1 rejection** in particular, are 4.0-specific,
and #5490 is the in-tree proof that an A1-style rejection can invert at 1.1.

Set deliberately and **not** planner-relevant:
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

**Root cause, as measured at checkout `392351ffd`.** No index in the tree
carried `language` at all — verified against the migration set at that
checkout: `content_entities` had 11 indexes and **zero** mentioning `language`.
**This is no longer true of the tree this migration lands in.** `origin/main`
now ships `104_content_entities_language_type_idx` on
`(language, entity_type)`, merged by #6540 via #6599 after these measurements
 were taken.

**Scope of that correction, stated precisely:** ONLY the "Re-measured after the
rebase" section and its subsections reflect the current tree. Every other
measurement in this note -- the ladder, `A1 rejected`, `A2 accepted`,
`Candidate (b)`, `Correctness before performance` and `Cost` -- was produced at
`392351ffd` against 2,000,000 rows with no migration 104 and no `EXISTS` gate.
They are kept as the record of how the index was chosen, not as a description of
the tree it lands in.

At that checkout, `language` could only ever be a `Filter`. The planner
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

## Re-measured after the rebase — this supersedes the ladder above

The ladder above was measured at checkout `392351ffd`, which predates
`origin/main`'s migration 104, and it profiles the statement the reader sees in
"The statement" above. **Neither still describes this change.** Two things moved
underneath it:

1. Migration 104 `(language, entity_type)` merged (#6540 via #6599), so the
   ladder's `baseline` column is not this branch's pre-change state. The `A1`
   column is.
2. `SearchEntitiesByLanguageAndTypeForAccess` now wraps the read in an
   uncorrelated `EXISTS` gate. Every `EXPLAIN` above profiles the pre-gate
   statement.

Re-measured against the statement the code actually sends:

```sql
SELECT ... FROM content_entities
WHERE EXISTS (SELECT 1 FROM content_entities WHERE <filters>) AND <filters>
ORDER BY relative_path, start_line, entity_name LIMIT $n
```

| arm | main only (104) | main + this index (107) |
| --- | ---: | ---: |
| zero-match `(hcl, Function)` | 0.113 ms, `Incremental Sort` | **0.078 ms, no Sort** |
| matching `(go, Function)` | 1.255 ms, `Incremental Sort` | **0.523 ms, no Sort** |

**The plan-shape claim holds.** With 107 the `Sort` node disappears and the scan
becomes `Index Scan using content_entities_language_type_path_idx`; with main's
104 alone both arms carry an `Incremental Sort` on exactly
`(relative_path, start_line, entity_name)`.

**The headline number does not.** This note's summary claims a 2,590 ms "before".
Against the branch's real pre-change state that arm is **0.113 ms**, because
main's 104 plus the `EXISTS` gate already short-circuit it. The honest gain this
index adds on top of main is **1.4x on the zero-match arm and 2.4x on the
matching arm** — real, and three orders of magnitude smaller than the ladder
implies.

### The `entity_name ILIKE` arm, previously unmeasured

| configuration | result |
| --- | ---: |
| 107 present, no trigram index | 29.873 ms — `Bitmap Index Scan` on **104**, ILIKE as a heap `Filter`, `Rows Removed by Filter: 34,994`, plus a full `Sort` |
| 107 present, migration 062's trigram index present | 0.571 ms — `Bitmap Index Scan` on `content_entities_entity_name_trgm_idx`, still a `Sort` |

**This index is not used for the ILIKE arm in either configuration.** It supplies
neither the access path nor the ordering there, so it neither improves nor
regresses that shape. What decides the ILIKE page is whether migration 062's
trigram index is present — a 52x difference.

### Environment for the re-measurement

Hand-built, and that bound is load-bearing: `content_entities` from migration
004 with 104 and 107 applied **by hand from the migration text**, not by the
branch's migration set.
**That gap is now closed separately:** the full committed migration set (129
files, 001 through this one) was applied in the byte order `BootstrapDefinitions`
uses, on a fresh database, with zero failures; applying the entire set a SECOND
time also gave zero failures and left every `content_entities` index oid
identical, so replay neither drops nor rebuilds this index. See "Migration
sequence and replay" below. PostgreSQL 16.15 **in a `postgres:16` container** -- not a host
binary, which is why this does not contradict the machine-profile note above
that the host's only Postgres *server* installs are 16.9. 300,000 rows,
16-core / 123 GB Linux x86_64 host.
Seed distribution derived from the 895-repo corpus, and it under-represents the
long tail: path p99 67 / max 121 against the corpus's 116 / 278, `entity_name`
max 93 against 105. Ordering conclusions are unaffected; **no key-size
conclusion is drawn from this seed, and none should be.** `entity_name` is
md5-derived, so the trigram figure is directional rather than exact.
`absolute_target_applicable = false`: these are plan-shape and same-machine
relative results.

### Migration sequence and replay

The re-measurement above applies the two indexes by hand, so it says nothing
about whether the migration itself applies. This section closes that separately,
against the real committed set.

Fresh PostgreSQL 16.15 database, all **129** committed migration files
(`001_ingestion_scopes.sql` through this one), applied in the order
`BootstrapDefinitions` uses — a **byte-wise** path sort
(`defs[i].Path < defs[j].Path`), reproduced with `LC_ALL=C sort`:

| pass | result |
| --- | --- |
| 1 — full set, fresh database | **0 failures** |
| 2 — the entire set applied AGAIN | **0 failures** |
| index oids, pass 1 vs pass 2 | **identical** |

The second pass is the one that matters. `ApplyDefinitions` runs every migration
on **every** bootstrap and there is no applied-ledger, so replay safety is a
correctness requirement rather than a nicety. Identical index oids across the
two passes is stronger than "it did not error": nothing was dropped and nothing
was rebuilt, so a bootstrap over an existing database does not pay to recreate a
34 MB index. `content_entities` ends with 13 indexes and this one reads:

```
CREATE INDEX content_entities_language_type_path_idx ON public.content_entities
    USING btree (language, entity_type, relative_path, start_line, entity_name)
```

**A harness note worth keeping, because it produced six false failures first.**
The first attempt applied the files in `ls | sort` order, which is locale
collated, and six migrations failed — `003a`–`003d` sorted *before*
`003_fact_records.sql`, so the SBOM-attestation indexes ran before the table
existed, and `097` then cascaded off the `092*` views. Go compares paths
byte-wise, where `_` (0x5F) precedes `a` (0x61). Under `LC_ALL=C` the same set
applies with zero failures. The failures were the harness, not the migrations.

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
Strict improvement on **every arm timed here**, regression on none; the matching
case gets ~10x faster too, because removing the sort helps a page that does have
rows. Two production shapes were not timed, so the claim is scoped rather than
universal: `entity_name ILIKE $n` — the canonical payload in
`language-query-dsl.md`, and the one shape with a real plan-flip mechanism
against migration 062's trigram index — and grant=1 MATCHING.

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

**THIS REJECTION NO LONGER HOLDS, AND THE REASON IS THAT MAIN SHIPPED THIS
CANDIDATE.** The paragraph above concludes the `EXISTS` pre-check "is only cheap
once an index like A2 exists, at which point the extra statement earns nothing".
Both halves are now false:

- Its premise, "with no index on `language`", is gone. `origin/main` ships
  migration 104 on `(language, entity_type)`.
- Its conclusion is contradicted by measurement. With 104 present and **no A2
  index at all**, the `EXISTS` pre-check returns the zero-match arm in
  **0.113 ms** — the initplan takes an `Index Only Scan` on
  `content_entities_language_type_idx`, returns 0 rows in 0.043 ms, the
  `One-Time Filter` fires and the ordered scan is `(never executed)`.

Candidate (b) is not a rejected alternative to this index. It is **already in
production**, merged as #6540 via #6599, and
`SearchEntitiesByLanguageAndTypeForAccess` at this branch's head issues it on
every call. The two are complementary rather than exclusive: the `EXISTS` gate
fixes the empty case, and this index removes the `Sort` from the non-empty one.

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
one more btree.

**Storage, measured on the 300,000-row re-measurement database and previously
unstated:** this index is **34 MB** against migration 104's **2,096 kB** — about
sixteen times larger, and larger than both the table's own `relative_path` index
(24 MB) and its primary key (16 MB), on a 48 MB table. Five columns including
two unbounded `TEXT` fields is what costs that. The figure that carries is the
**16x ratio against migration 104**, not the table-relative one: this seed puts
the index at 71% of a 48 MB heap while the 2,000,000-row seed in `Cost` below
puts it at 173 MB against a 934 MB heap (18.5%). Those two table-relative
ratios differ by 4x, so neither is a production estimate. `EXPLAIN (ANALYZE, BUFFERS, WAL)` over 200,000-row inserts, arms
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

Performance Evidence: this index removes the `Sort` from the ordered
language/entity-type page read. Re-measured against the tree it lands in
(migration 104 present, the #6540 `EXISTS` gate present) and the statement the
code actually sends: zero-match 0.113 ms -> 0.078 ms, matching 1.255 ms ->
0.523 ms, `Incremental Sort` -> no `Sort`, on 300,000 rows.
SUPERSEDED FIGURES, kept because they are what the original selection was made
on and they must not be quoted as current: 2,590 ms / 2,013,451 buffers before,
0.012 ms / 4 buffers after, on 2,000,000 rows across 600 repositories. That
"before" predates migration 104 and the `EXISTS` gate, so it is not this
branch's pre-change state -- see "Re-measured after the rebase".
Write cost +4.8%/+6.4% WAL records over two alternated rounds.

No-Observability-Change: this change adds one index through the existing
bootstrap replay path. It adds no metric, span, log, or status field, and
changes no existing one. The read it accelerates is already covered by the
`postgres.query` span `SearchEntitiesByLanguageAndTypeForAccess` starts, with
`db.operation = search_entities_by_language_and_type`.

## Documented assumption: the composite key stays far inside the btree tuple limit

This index puts two unbounded `TEXT` columns — `relative_path` and `entity_name` —
into one btree key. `entity_name` had only ever been GIN-indexed before, where no
tuple limit applies, so this is the first btree key to carry it. Review raised
that as a potential write-path failure: a row whose combined key exceeds
PostgreSQL's ~2704-byte btree limit would fail the `CONCURRENTLY` build at
bootstrap, or fail content projection on insert after a successful upgrade.

Measured against the 895-repository validation corpus (428,882 files) rather than
argued:

| component | measured |
| --- | --- |
| `relative_path` | mean 48.6 B, p50 46, p99 116, p999 147, max 278 |
| `entity_name` (longest real declaration name in the corpus) | 105 B |
| `language` + `entity_type` | well under 32 B combined |
| `start_line` | 4 B |
| worst-case composite key | **≈ 419 B against a ~2704 B limit** |

That 419 is already an over-count: the longest path and the longest declaration
name occur in different files, so no single row combines them. Zero declarations
anywhere in the corpus carry a name ≥ 200 characters — checked against
declaration syntax, not identifier-shaped tokens, because a naive token scan
returns a 574,719-character maximum that is entirely base64 blobs inside string
literals (inlined `data:image/svg+xml;base64,…`, an embedded certificate, a gzip
padding fixture), not one of them a declaration.

So the limit is not reachable on this corpus, and this is recorded as an
assumption rather than a redesign of the key. Two things that assumption does
**not** claim:

- **One corpus is not a universal bound.** These are 895 repositories of one
  estate. A different corpus — heavy protobuf or ORM codegen, or deeply nested
  monorepo paths — could sit higher.
- **Declaration tokens in source are not the parser's emitted `EntityName`.** If
  any language path builds a qualified or composite name, the stored value can
  exceed the raw token counted here.

Worth noting the exposure is not novel to this index: `content_file_references`
(migration 004) has a PRIMARY KEY over four unbounded `TEXT` columns, and
`content_files` keys on `(repo_id, relative_path)`. A PK is the more load-bearing
case, since an oversized key there fails the INSERT with no index to drop, while
this secondary index could be dropped and rebuilt in another shape.

`ContentWriter.Write` bounds neither column beyond non-emptiness, so nothing
upstream establishes that the concatenation fits. A length guard there would be
the cheap place to assert it for both tables, reusing `truncateUTF8ByBytes` in
`go/internal/content/shape/source_cache.go`, which already caps snippets at 4096
bytes and records `source_cache_truncated` / `source_cache_original_bytes`. That
is deliberately left out of this change: it is a write-path guard for the content
store generally, not a property of this index, and it belongs with the
`content_file_references` PK work rather than here.

## Replay safety

`BootstrapDefinitions` enumerates every file under `migrations/` and
`ApplyDefinitions` Execs all of them on **every** bootstrap; there is no applied
ledger. Migration 107 therefore only creates, under a new name, with
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
