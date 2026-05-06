# Relationship Mapping Observability

Use this page as the operational companion to
[Relationship Mapping](relationship-mapping.md).

The platform no longer uses the older synthetic `relationships.*` log families
or `eshu.relationships.*` trace families as its primary contract. Relationship
mapping now rides on the Go reducer/materialization path plus the shared
storage traces and structured JSON logs.

## Where Relationship Work Happens Now

The important relationship stages on the Go path are:

- `reducer.cross_repo_resolution`
- `reducer.sql_relationship_materialization`
- `reducer.inheritance_materialization`
- `canonical.write`
- nested `postgres.exec`, `postgres.query`, and `neo4j.execute`

Those spans, together with reducer/projector metrics and structured logs, are
the current observability contract for relationship mapping.

## What To Watch First

Start with metrics:

- reducer queue depth and age from `eshu_dp_queue_depth` and
  `eshu_dp_queue_oldest_age_seconds`
- reducer execution latency from `eshu_dp_reducer_run_duration_seconds`
- canonical graph write latency from `eshu_dp_canonical_write_duration_seconds`

Then use traces:

- `reducer.cross_repo_resolution` when the problem looks like missing or slow
  cross-repo linkage
- `reducer.sql_relationship_materialization` when SQL-side relationship writes
  are slow or incomplete
- `reducer.inheritance_materialization` when inheritance-derived relationships
  lag
- `canonical.write` when the bottleneck is in graph persistence

Finally use logs:

- reducer logs with `domain`, `pipeline_phase`, `failure_class`, `scope_id`,
  and `generation_id`
- storage retry logs such as Neo4j transient retry warnings
- canonical write completion logs for batch-level confirmation

## Current Log Reality

Relationship debugging uses the shared Go JSON logger. The most useful fields
are:

- `message`
- `pipeline_phase`
- `domain`
- `scope_id`
- `generation_id`
- `failure_class`
- `trace_id`
- `span_id`

`event_name` is optional. Do not assume every relationship log line has one.

The useful current messages are the reducer/materialization messages emitted by
the Go code, for example:

- `cross-repo relationship resolution started`
- `cross-repo relationship resolution completed`
- `cross-repo relationship routing completed`
- `sql relationship materialization started`
- `sql relationship materialization completed`
- `inheritance materialization started`
- `inheritance materialization completed`
- `canonical atomic write completed`
- `canonical sequential write completed`
- `neo4j transient error, retrying`

## Required Proof For Relationship Changes

When relationship behavior changes, keep proof at three layers:

1. extractor or evidence tests
2. reducer/materialization tests
3. query/story/context proof

The important packages are typically:

- `go/internal/relationships`
- `go/internal/reducer`
- `go/internal/storage/postgres`
- `go/internal/storage/cypher`
- `go/internal/query`

## Investigation Recipes

### Missing cross-repo edge

1. Confirm the evidence family exists in tests or fixtures.
2. Check reducer queue depth and reducer latency metrics.
3. Open `reducer.cross_repo_resolution`.
4. Follow child `postgres.*` and `neo4j.execute` spans.
5. Use logs to extract the exact `scope_id`, `generation_id`, and
   `failure_class`.

### Relationship write is slow

1. Start with `eshu_dp_canonical_write_duration_seconds`.
2. Open the matching `canonical.write` span.
3. Check nested `neo4j.execute` spans for the slow statement.
4. Correlate with reducer logs for the owning domain and phase.

### Query answer disagrees with reducer truth

1. Confirm the canonical relationship exists in reducer/storage proof.
2. Check repository context/story or entity fallback tests in `go/internal/query`.
3. Treat it as a read-model/query-shaping gap, not as proof that reducer
   materialization failed.
