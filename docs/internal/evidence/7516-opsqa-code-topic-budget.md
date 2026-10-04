# #7516 ops-qa code-topic budget validation, 2026-10-04

## Claim boundary

This record validates the unscoped 16-term
`POST /api/v0/code/topics/investigate` endpoint against the deployed ops-qa
`<1 s` budget after the #7033 candidate merge (#7519). The accepted
exact-source fixed-corpus relative result is the current guarded-reader
comparison in
[7033-exact-code-topic-parallel.md](7033-exact-code-topic-parallel.md#current-guarded-reader-fixed-corpus-comparison-2026-10-02)
(baseline median 0.797406 s, candidate median 0.484409 s on the dedicated
test instance). That comparison remains the merge measurement; this note is
the deferred deployed check tracked in #7516. Refs #7033, #7516.

The endpoint reads the PostgreSQL content store
(`source_backend=postgres_content_store` on every response below). The API is
configured with Neo4j, but these timings do not establish Neo4j query
performance.

Outcome: the budget is missed on the deployed Service, and the candidate
parallel path never engaged: all measured requests served the legacy
`single_statement` path with fallback reason `snapshot_set_unavailable`.
No deployed speedup is claimed.

## Deployed identity

- API image `ghcr.io/eshu-hq/eshu:sha-791078e`
  (`sha256:3d4296993a4c7a50b12b2fb6528bfdaad5eca10b117bdf4cfd4d5c53b60a9350`),
  source `791078e81`. It contains the #7519 parallel-read candidate and
  predates #7572, so per-request reader stage sums are unavailable on this
  image (and #7572's log attribution covers the impact-findings read, not
  this endpoint). No instrumentation was added for this validation.
- API limits 1 CPU / 2 GiB; serving Pod 1/1 Ready with zero restarts.
- PostgreSQL 18.3 (one primary, one read replica); Neo4j
  2026.08.1-community.
- Migration readiness: `GET /healthz` 200 and `GET /readyz` 200 (`status=ok`)
  immediately before the run. Ledger `eshu_schema_migrations` holds 181
  applied rows, newest
  `159_fact_records_content_entity_dependency_legacy_gap_repo_idx.sql`
  (includes migration 155).
- Corpus at run time: 2,660,274 `content_entities`, 145,754
  `content_files`; counts identical before and after the timed window. This
  is a continuously replaying system, not a frozen dataset: the numbers
  below are an absolute current-state check, not a before/after speedup.

## Method

Time bound stated before the run: 20 minutes wall max, stop early on any
HTTP 5xx or writer-node CPU above 70%. Read-only throughout (three local
port-forwards, torn down after); no corpus rebuild, no routing, schema, or
Service change.

- Canonical request: the exact topic phrase from the accepted fixed-corpus
  runs (`config content deployment environment file function handler module
  package path repository resource service source system workspace`; limit
  25, offset 0, no `repo_id`), which derives the 16 terms `config content
  deployment environment file function handler module package path repo
  repository resource service source system` (`repository` contributes the
  `repo` synonym, reaching the 16-term cap before `workspace`; verified
  against `CodeTopicSearchTerms`). The live responses confirm
  `searched_terms` exactly this set.
- 2 discarded warmups, then 10 timed sequential full-response-body samples,
  concurrency 1.
- Readiness and headroom at run start: API Pod sampled at 2m CPU / 16 MiB
  against its 1 CPU / 2 GiB limit; read-replica/API node at ~1% CPU and 13%
  memory; writer node at 16-20% CPU (below the 70% stop gate) and 90%
  memory. No stop gate fired.

## Results

All 10 timed requests returned HTTP 200. Every response carried `count=25`,
`truncated=true`, `candidate_pool_truncated=true`, all 16 searched terms,
and an unscoped selector. All 10 raw response bodies were byte-identical
(one SHA-256), so the ranked page was stable across the window.

Full-body HTTP seconds in request order:

| # | 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8 | 9 | 10 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| s | 1.185468 | 1.183742 | 1.180814 | 1.190767 | 1.184173 | 1.183030 | 1.197328 | 1.176540 | 1.177910 | 1.184820 |

Median 1.184 s, nearest-rank p95 1.197 s. Every sample exceeds the `<1 s`
budget (minimum 1.177 s). No errors occurred.

## Operator signals

Performance Evidence: Tempo traces in the run window show 12 of 12
code-topic spans with `code_topic.execution_mode=single_statement` and 12
of 12 with `code_topic.parallel_fallback_reason=snapshot_set_unavailable`.
Trace span durations ran 1010-1263 ms, consistent with the HTTP wall times.
The #7519 parallel path was therefore dormant on the deployed route during
this window; the miss above measures the legacy single-statement path, on
which the deployed single-host reader DSN keeps snapshot sets unavailable
(the `sslmode=prefer` fallback mechanism diagnosed 2026-10-04 on #7033).

Observability Evidence: execution-mode and fallback-reason span attributes
shipped by #7519 supplied the per-request attribution above. No new
instrumentation was added. The durable production-safe reader route remains
the open #7033 follow-up; no DSN, Service, or code change was made here.

## Teardown

No Kubernetes objects were created: no canary Pod, Secret, role, or Service
change. The three local port-forwards (API, read replica, Tempo) were
stopped and verified absent (no listeners, no processes), and the pod list
was unchanged. No credentials or private infrastructure details are recorded
in this note.

## Disposition

Validation complete with a miss: deployed median 1.184 s against `<1 s`
with the candidate path dormant. Closing #7516 on this evidence hands the
remaining budget work to the open #7033 production-safe route item, which
already tracks a deployed acceptance measurement. The issue close should
link this note and #7033.
