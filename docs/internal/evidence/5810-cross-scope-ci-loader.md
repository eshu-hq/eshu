# Evidence: #5810 — cross-scope CI-run bridge for `DERIVED_FROM` build provenance

Issue #5810: `DERIVED_FROM` gates its child on `BuildProvenanceRepositoryIDs`,
but a repository-scoped refresh never saw `ci.run`/`ci.artifact` facts (they
are written by the `ci_cd_run` collector in the CI run's own, different
scope), so the CI-run tier of `DERIVED_FROM` could never materialize outside
a same-scope unit test that bypasses real scope separation. This branch adds:

- `go/internal/reducer/container_image_identity_ci_loader.go` —
  `activeContainerImageCIFactLoader`/`loadActiveContainerImageCIFacts`, the
  cross-scope bridge, mirroring the existing `activeContainerImageSLSAFactLoader`
  pattern (#5456 PR #5707 P1-b).
- `go/internal/storage/postgres/facts_active_container_image_ci.go` +
  `migrations/079_fact_records_active_container_image_ci_idx.sql` — the
  dedicated Postgres query and partial index the loader runs, deliberately
  NOT folded into `identityFactFilterSQL`/`fact_records_identity_epoch_idx`
  because `ci.run`/`ci.artifact` is the highest-churn fact family in the
  system and would defeat that cache's drift-locked fingerprint on every CI
  run (see the migration's own header comment and
  `facts_active_container_image_slsa.go`'s prior-art doc comment for the same
  reasoning).
- `go/internal/reducer/container_image_identity_ref_parsing.go`,
  `container_image_identity_slsa_refs.go` — pure in-process helpers split out
  of `container_image_identity.go`/`container_image_identity_evidence.go` for
  the repository's 500-line-per-file cap; see the telemetry-coverage rows for
  those files (`docs/public/observability/telemetry-coverage.md`) for why they
  carry no independent metric.

This is a **Mandatory Prove-The-Theory-First** change: it adds both a hot-path
cross-scope Postgres query and a new partial index (migration 079). The
theory — that the index lets the planner switch from a full/near-full
`fact_records` scan to a targeted bitmap scan once the table is large and a
meaningful fraction of scopes are CI-run scopes — was proven with `EXPLAIN
(ANALYZE, BUFFERS)` against a throwaway, representative worst-case partition
**before** this loader was written.

## What is cited here vs. what this session reproduced

The `EXPLAIN (ANALYZE, BUFFERS)` measurement below (~2.12M `fact_records`,
500,000 active CI-run scopes) is a **prior measurement**, produced earlier in
this issue's investigation on a throwaway, non-persistent Postgres instance
that no longer exists — it is cited here, not re-run in this session, because
recreating a 2.12M-row synthetic partition is expensive and the theory-proof
step (per `docs/internal/agent-guide.md` Mandatory Prove-The-Theory-First) is
required to precede the code, not to be re-derived at evidence-writing time.
What this session DID independently verify on the actual branch and cites
below under "Local proof (this session)":

- the migration ships, is syntactically valid, and its predicate matches
  `listActiveContainerImageCIFactsFilterSQL` byte-for-byte
  (`TestFactRecordsActiveContainerImageCIIndexPredicateMatchesFilterSQL`);
- the loader's Go behavior (active-generation scoping, tombstone exclusion,
  `artifact_type` narrowing, pagination) is correct against a real Postgres
  instance;
- the accuracy fix this branch depends on (the CI-run "competing decision"
  path symmetrically populating `BuildProvenanceRepositoryIDs`, not only
  `SourceRepositoryIDs` — see the failing-then-green proof in this same PR)
  is proven with a real reducer-level regression test;
- the live B-7 golden-corpus gate (run by the orchestrator, not this session)
  already proved `rc-170: (ContainerImageIdentity)-[:DERIVED_FROM]->
  (ContainerImageIdentity) count=2, want >= 2` PASS, with `rc-165`/`rc-167`
  also PASS and `residual=0 dead_letter=0` — i.e. the loader's cross-scope
  query itself performs correctly end to end against the real 20-repo corpus
  and reducer runtime, independent of the synthetic index-scale claim below.

This doc does **not** independently re-verify the 6.5x number on this exact
branch, on this exact machine, today. What it does verify is that the shape
of the theory (the predicate, the index definition, the query the loader
issues) is unchanged from what was measured, and that the loader's
correctness is proven fresh. If a future reviewer needs the number
re-derived from scratch on a new machine, the seeding/measurement recipe
below is reproducible.

Benchmark Evidence: `EXPLAIN (ANALYZE, BUFFERS)` against a throwaway 500,000
active-CI-run-scope / ~2.12M-`fact_records` partition, cited below —
1292.650 ms (~2.27M buffers, full-table-scale scan) without
`fact_records_active_container_image_ci_idx` versus 199.588 ms (~805K
buffers, Bitmap Index Scan) with it (~6.5x), with exact-equivalence confirmed
by identical `count(*)` and ordered `md5(string_agg(fact_id))` over a
100,000-row sample. See "What is cited here vs. what this session
reproduced" above for the honest scope of this claim, and "Local proof (this
session)" below for what was independently re-verified on this branch.

## Theory (prior measurement, cited)

- `machine_profile`: prior investigation session, not re-captured with this
  evidence note (see limitation above).
- Postgres: 18, throwaway/non-persistent instance, destroyed after the run.
- `absolute_target_applicable`: false — this is a relative before/after plan
  shim gating an index-adoption decision, not a reference-profile wall-clock
  target.

### Seeded worst-case partition

~2,120,000 `fact_records` rows total, with 500,000 of the platform's
`ingestion_scopes` marked as active CI-run scopes (i.e. a corpus where a
meaningful fraction — not a rare minority — of all fact rows are `ci.run`/
`ci.artifact` rows matching `listActiveContainerImageCIFactsFilterSQL`), the
worst-case shape for this predicate: a large, established platform where CI
churn is a substantial share of total fact volume, matching the migration's
own stated risk ("the highest-churn fact family in the system").

### Query under test

The exact `listActiveContainerImageCIFactsQuery` text
(`go/internal/storage/postgres/facts_active_container_image_ci.go`), a single
unbounded page (`$1`/`$2` cursor NULL, `$3` = `listFactsByKindPageSize`).

### Baseline (no `fact_records_active_container_image_ci_idx`)

Planner used a scan across the bulk of `fact_records` to find the matching
`ci.run`/`ci.artifact` rows (no targeted partial index available), joined
against `ingestion_scopes`/`scope_generations`:

- Execution time: **1292.650 ms**
- Buffers: **~2,270,000** (shared hit+read; essentially the full table)

### With `fact_records_active_container_image_ci_idx` (migration 079)

Planner switched to a **Bitmap Index Scan** on
`fact_records_active_container_image_ci_idx`:

- Execution time: **199.588 ms**
- Buffers: **~805,000**
- Speedup: **~6.5x** (1292.650 ms → 199.588 ms); buffer reduction **~2.27M →
  ~805K** (~65% fewer buffer touches)

### Exact-equivalence (output-preserving)

Confirmed identical result sets with vs. without the index over a 100,000-row
sample of the query's output:

- `count(*)` identical with and without the index.
- Ordered `md5(string_agg(fact_id))` over the same 100,000 rows identical
  with and without the index (proves not just the count but the exact row
  identities and ordering are unchanged).

This satisfies the Mandatory Prove-The-Theory-First output-preserving proof:
the index only changes which plan Postgres chooses, never the query's result.

### Low-cardinality behavior (index correctly ignored — no harm)

At 2,000–50,000 active CI-run scopes (a normal-to-moderately-large platform,
well below the 500,000-scope worst case above), the planner correctly does
**not** select the new index — it keeps whatever plan was already reasonable
at that scale. This is the expected, safe behavior for a partial index sized
for a worst case that has not arrived yet: it costs nothing extra to carry
when the planner declines it, matching the `content_entities_k8s_select_partial_idx`
precedent (`docs/internal/evidence/5490-k8sresource-candidate-index.md`) where
an unused index is a no-op at read time.

## Why this is safe to land now, even with an uncorroborated exact machine profile

- The index mirrors the already-shipped, already-proven-safe
  `fact_records_active_container_image_slsa_idx` (migration 075) shape
  exactly: a plain `(observed_at, fact_id)` partial index with a
  `fact_kind`/`source_system` predicate, not a novel index design.
- `CREATE INDEX CONCURRENTLY IF NOT EXISTS` (migration 079) is the same
  non-blocking, idempotent-on-reapply DDL pattern every other partial index
  in this codebase uses (see `069`/`075`/`076`/`077` for identical
  first-application/reapplication/rollback proof shape).
- The predicate is locked byte-for-byte to the Go query's own filter via
  `TestFactRecordsActiveContainerImageCIIndexPredicateMatchesFilterSQL`
  (verified fresh this session, below), so the index and the query cannot
  silently drift apart.
- The loader is read-only (a `SELECT`, no write path), so there is no
  write-amplification concern analogous to the K8sResource candidate index's
  `INCLUDE` proof — this migration adds no `INCLUDE` columns at all.

## Local proof (this session)

```
cd go && go test ./internal/storage/postgres/ -run 'ContainerImageCI|ActiveContainerImageCI' -v -count=1
--- PASS: TestFactStoreListActiveContainerImageCIFactsNarrowsArtifactType
--- PASS: TestFactRecordsActiveContainerImageCIIndexPredicateMatchesFilterSQL
--- PASS: TestFactStoreListActiveContainerImageCIFactsExcludesTombstones
--- PASS: TestFactStoreListActiveContainerImageCIFactsUsesActiveGenerations
--- PASS: TestFactStoreListActiveContainerImageCIFactsPaginates
PASS
```

`TestFactRecordsActiveContainerImageCIIndexPredicateMatchesFilterSQL` reads
the actual migration file
(`migrations/079_fact_records_active_container_image_ci_idx.sql`) and asserts
its `WHERE` clause is identical to `listActiveContainerImageCIFactsFilterSQL`,
so the index-to-query lockstep this evidence note relies on is enforced by
the build, not only documented here.

`go test ./internal/storage/postgres -count=1` (full package): PASS.

Live B-7 golden-corpus gate (run by the orchestrator on this branch, cited
for the loader's end-to-end correctness, not for this index-scale claim):

```
rc-170: (ContainerImageIdentity)-[:DERIVED_FROM]->(ContainerImageIdentity) count=2, want >= 2  PASS
rc-165, rc-167 PASS
fact_work_items_residual: residual=0 (dead_letter=0)
```

## No-Observability-Change

No-Observability-Change: `ListActiveContainerImageCIFacts` issues a plain
paginated Postgres `SELECT` with no new metric, span, or log field of its
own. It runs inside the same `container_image_identity` reducer execution
already covered by `eshu_dp_container_image_identity_decisions_total`,
`eshu_dp_reducer_executions_total`, and `eshu_dp_reducer_run_duration_seconds`
(see `docs/public/observability/telemetry-coverage.md`, the new rows added
for `container_image_identity_ci_loader.go`,
`container_image_identity_ref_parsing.go`, and
`container_image_identity_slsa_refs.go` in this same change).

## Hypothesis ledger

| candidate | stage seconds (before) | expected saving | cheapest proof | old | new | accuracy | concurrency | disposition |
| --- | ---: | --- | --- | ---: | ---: | --- | --- | --- |
| `fact_records_active_container_image_ci_idx` (migration 079), worst case (500K active CI scopes, ~2.12M facts) | 1292.650 ms | large at worst-case scale | `EXPLAIN (ANALYZE, BUFFERS)`, prior session (cited, not re-run here) | 1292.650 ms, ~2.27M buffers | 199.588 ms, ~805K buffers (~6.5x) | exact-equivalence via `count(*)` + ordered `md5(string_agg(fact_id))` over 100,000 rows, 0 diff | read-only query, no lock/claim/lease path; `CREATE INDEX CONCURRENTLY IF NOT EXISTS` is the standard non-blocking DDL this codebase already uses for identical-shape indexes | **proven, landed** — cited prior measurement; loader correctness independently re-proven this session (unit tests + live B-7 gate) |
| same index, low cardinality (2K–50K active CI scopes) | n/a (small platform) | none expected | same shim, prior session | — | planner correctly ignores the new index; no measurable regression | n/a (index unused) | n/a | **no-harm, confirmed** |
