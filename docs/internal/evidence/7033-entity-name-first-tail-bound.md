# #7033 entity name-first tail bound, fixture evidence

## Claim boundary

This record proves the retrieval contract of the #7033 entity-lateral change,
not a deployed endpoint speedup. The unscoped 16-term
`POST /api/v0/code/topics/investigate` read keeps its exact match semantics:
uncapped per-term pools return the identical result multiset as the old
name-OR-source lateral, capped pools hold exactly the per-term cap with the
capped marker preserved, and capped selection becomes name-priority under the
approved stable-selection policy. Refs #7033.

No deployed endpoint timing is claimed here. The deployed acceptance run
against the under-1s budget is follow-up work recorded in the PR body.

## Fixture identity

Disposable local PostgreSQL 18 (public `postgres:18` image), schema from the
production bootstrap at this PR head, both content trigram indexes present
(`content_entities_source_trgm_idx`, `content_entities_name_trgm_idx`).
Worst-case-shaped corpus, analyzed before timing:

- 2,000 filler rows with incompressible ~16 KiB `source_cache` values
  (out-of-line TOAST storage) occupying early heap; names match nothing.
- 300 early source-only rows for the capped term plus 5 deep name rows.
- 3 deep rows for the rare term (name-only, source-only, both).
- 16 terms per request, forcing the 250-row per-term cap.

## Method

The hermetic shape pins and the serial/parallel twin pin assert the emitted
SQL. The live test drives the production `ContentReader` against the fixture
and compares against an independently written OR-lateral oracle. Scratch
`EXPLAIN (ANALYZE, BUFFERS)` compared the old and new laterals on the same
fixture. Fixture plans used literal terms; production binds parameters, so
the buffer ratio below is mechanism evidence, not an endpoint speedup claim.

## Results

- Rare uncapped term: old and new laterals return the identical 3-row
  multiset (md5 `048c66d6bf3d434303481a7da17f1e4e` both sides).
- Capped term: the new pool holds exactly 250 rows and contains all 5 deep
  name rows; the old heap-order pool excluded them. Capped marker set.
- Name-dense term at cap 250: old lateral 200 ms with 10,039 shared-buffer
  hits (2,000 TOASTed rows detoasted); new lateral 1 ms with 39 shared-buffer
  hits, and the source_pool branch never executed (remaining budget zero).
  The name_pool predicate never references `source_cache`, so that branch
  cannot detoast under any plan.

## Performance Evidence:

Baseline is the shipped name-OR-source lateral; after is the name-first
split, measured on the disposable PostgreSQL 18 fixture above with the
16-term, cap-250 input shape. Terminal counts: uncapped rare pool 3 rows
identical both sides; capped pool exactly 250 rows both sides with the
capped marker set. Telemetry and span attributes are unchanged (see
No-Observability-Change below), so no new operator signal was needed. The
change is safe because uncapped multisets are proved identical, the per-term
row bound is structural (`LIMIT` on both pools with a non-negative
remainder), and the capped-membership change follows the approved
stable-selection policy.

## No-Observability-Change:

No span, metric, log, or status field was added, removed, or renamed. The
`code_topic.*` probe, assembly, partition, and fallback attributes record
identically on both shapes, and no operator runbook changes. The existing
per-partition spans continue to attribute the slowest-partition wait that
this change targets.
