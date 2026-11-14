# #10660 repository-scoped manifest dependency read: plan proof

Implementation-lane measure gate for `eshu-hq/eshu#10660` (part of epic
#10652). The existing `ListActivePackageManifestDependencyFacts` read probes
per (package manager, folded name) pairs across all scopes; the new
repository-scoped read needs a scope-first shape with no prior plan. This note
records the proof before any SQL-changing edit lands.

## Candidate shape

`scopeFirstManifestRead` (scratch
`TestScratch10660ScopeFirstMeasureLive`, since removed): the same manifest
predicates as `listActivePackageManifestDependencyFactsQuery`
(`fact_kind = 'content_entity'`, `source_system = 'git'`,
`entity_type = 'Variable'`, `config_kind = 'dependency'`,
`generation.status = 'active'`, `is_tombstone = FALSE`, the
`(observed_at, fact_id)` keyset cursor and ordering, `LIMIT $4`), with the
(pair, name) LATERAL probes replaced by one repository predicate
(`fact.scope_id = $1`) and the candidate base-generation ids at `$5`, keeping
the numbering dense for the pgx exec-mode first-arg convention.

Overlay compatibility (static, checked against
`deriveDeltaOverlayBaseQuery`): exactly one `fact_records AS fact` branch, one
`AND scope.active_generation_id = fact.generation_id`, one
`AND generation.status = 'active'`, no `$6` in the active text, no
`active_delta` alias. The derived base query therefore keeps the same
scope-first shape over the base generations.

## Corpus

Disposable PostgreSQL 18.6 (Homebrew, `localhost:5432`), full `ApplyBootstrap`
(322 migrations), `ANALYZE` after seeding. 400 git scopes, each with a
superseded and an active full generation of 120 npm dependency rows over a
skewed 4,000-name vocabulary (every 13th row tombstoned), plus 30 dev
dependencies in a second manifest file on scope `m-7`: **96,030
`fact_records` rows**.

## Plans (`EXPLAIN (ANALYZE, BUFFERS)`, read exec mode `CacheDescribe`)

Page 1, scope `m-7`, limit 200, no cursor — **Execution Time: 0.808 ms**,
940 shared buffers, no sequential scan:

- `Index Scan using fact_records_content_entity_dependency_variable_repo_idx`
  (#121 repo-scoped partial index), `Index Cond: (scope_id = 'm-7')`, 1
  search, 250 rows (110 superseded-generation rows removed by the
  active-generation join filter).
- `ingestion_scopes` by pkey (250 probes) and `scope_generations` by
  `scope_generations_active_scope_idx` (140 probes) for the active-generation
  joins; quicksort on `(observed_at, fact_id)` (96 kB), `LIMIT` on top.
- Planner estimates `rows=1` against 140 actual (the known per-scope
  under-estimate); the chosen shape is still the index probe, so the
  mis-estimate costs nothing here.

Cursor page 2 (edge of page 1): 0 rows, **0.173 ms**, same probe shape with
the keyset predicate as a residual filter. Absent scope (`m-404-absent`):
**0 rows** (empty list, not an error — matches the acceptance criterion).

## Row counts

- Scope `m-7`, limit 200: **140 rows** — 110 `pkg/package.json` runtime rows
  plus all 30 `pkg/dev/package.json` dev rows, both files in one
  `(observed_at, fact_id)` order.
- Absent scope: 0 rows.

## Conclusion

Scope FIRST (`scope_id` equality, the canonical scope key) over the #121
partial index serves the read in under 1 ms on a 96k-row fleet-shaped corpus
with no sequential scan and no new index: the brief's `247 folded-entity idx`
stays the pair-probe path, and the `003 scope-generation idx` family covers
the generation joins. No DDL is proposed. Read contract for the
implementation: scope one repository, limit 1..200 (default 50), 10 s
statement timeout, `(observed_at, fact_id)` keyset paging, delta-overlay
base pages through the same derived-text path with `WithRefusals` semantics.
