# #7458: a superseded generation stops writing search documents

## Problem

The reducer runs its generation check once, immediately before
`EshuSearchDocumentHandler.Handle` (`go/internal/reducer/runtime.go`). The
handler then streams every source page, runs the retire `DELETE`s and writes the
index stats without looking at the generation again. The reducer heartbeat only
renews the lease, so a newer generation that activates while an older one
streams does not stop it. The #7450 audit
(`docs/internal/evidence/7389-remote-no-regression-audits.md`) attributes
+565.6 s of statement time to 4 redundant stale search-document items (78.8 s to
289.1 s each), `DELETE fact_records` retires of about 91 s per large cycle, and
`DELETE eshu_search_index_documents` calls of 90.74 s and 24.87 s.

Accuracy was never at risk: every search-document row is keyed by
`(scope_id, generation_id)`, every reader joins the active generation, and
`FinalizeReady` no-ops for a non-active generation. The cost is the waste, plus
freshness: the active generation's search item shares the scope conflict key and
waits behind the stale one.

Root-Cause Evidence: on the RED commit (the new tests on top of `725fa19425`, then origin/main, before the fix) the live interleave
`TestEshuSearchDocumentHandlerAbandonsSupersededGenerationLive` (PostgreSQL
18.6, real loader, writer, projection-state store and
`NewGenerationFreshnessCheck`; the projector's own activation statements commit
newer generation H after page 1) failed with every page and the stats row
written for the superseded generation:

```text
WARN search document projection ready skipped stale scope_id=scope-7458 generation_id=gen-7458-g projection_revision=1 build_fence=1 document_count=600
G after supersede: result=succeeded pages_streamed=3 page_docs=[256 256 88] facts=600 index_docs=600 stats=1 state="building" evidence="considered=600 included=600 skipped=0 written=600 retired=0"
G status = "succeeded", want "superseded"
G rows facts=600 index_docs=600, want exactly page 1 (256) of 600
G eshu_search_index_stats rows = 1, want 0 (Finalize must not run)
--- FAIL: TestEshuSearchDocumentHandlerAbandonsSupersededGenerationLive
```

The unit regressions in `eshu_search_document_supersede_test.go` failed on the
same commit for the same reason (`InsertPage calls = 3, want exactly 1`,
`finalize=1 cancel=0, want 0/0`, `checks = 0, want 4`).

## What changed

- `EshuSearchDocumentHandler.GenerationCheck` (a
  `reducercontract.GenerationFreshnessCheck`) is required; a nil check is a
  `Handle` construction error before any write, like a nil loader or writer.
- The check runs at the start of every page callback (before
  `ProjectSearchDocuments` and `InsertPage`, including the first page) and once
  after the stream, before `Finalize`. There is no time throttle.
- A superseded generation returns a package sentinel from the callback; `Handle`
  detects it with `errors.Is`, skips `Cancel` and `Finalize`, and returns
  `ResultStatusSuperseded` with `CanonicalWrites` set to the documents actually
  written. The queue acks it `succeeded`, as it does for every superseded
  reducer intent; only the `status` label on
  `eshu_dp_reducer_executions_total` becomes `superseded`.
- Rows already written are left in place. `Cancel` is the empty-keep-set retire,
  the same `DELETE` statements this change removes. The rows are invisible to
  every active-generation reader and retention prunes them by cascade from
  `scope_generations`, identical to a superseded generation that finished before
  the newer one activated. The projection-state row stays `building`;
  `MarkFailed` would no-op against a non-active generation.
- A check error fails closed through the existing stream-error path: `Cancel`,
  then the item fails. The queue retries `GenerationNotYetActiveError` without
  counting an attempt (it can occur when the item started while the scope had no
  active generation yet); any other lookup error, such as a Postgres error, is
  not retryable, so the item dead-letters on that failure.
- `cmd/reducer` wires `postgres.NewGenerationFreshnessCheck` through
  `SearchDocumentHandlers.EshuSearchDocumentGenerationCheck`.
  `TestSearchDocumentHandlersWireGenerationCheck` fails when it is unset, and
  `TestSearchDocumentDomainHandlerReceivesGenerationCheck` proves the registry
  hands it to the handler.

## Concurrency

- Conflict domain: the reducer scope key. `eshu_search_document` takes the
  default branch of `reducerConflictDomainKey`, so the claim defers a pending
  item on the same `(scope)` key to a running holder. The activating generation's
  item is therefore blocked until the stale item acks; abandoning early is what
  releases it.
- No worker count, batch size, lease duration, claim query, lock order or
  transaction scope changed. The check is one read statement of three
  primary-key probes with no lock (`generationFreshnessSQL`), run outside any
  transaction the writer holds.
- Bounded overlap: a check that passes just before the newer generation
  activates lets that one page finish, so at most one extra page is written for
  the superseded generation. Those rows are invisible and pruned.
- Replay and retry matrix: a superseded item is acked `succeeded` and the sweeper
  only enqueues the active generation with no ready projection state, so nothing
  re-drives G; replaying an already-superseded G fails the first-page check with
  zero rows written (`TestEshuSearchDocumentHandlerChecksBeforeFirstPage`); a
  check error takes the existing failure path (`Cancel`, then a retry for
  `GenerationNotYetActiveError`, where the next `BeginBuilding` bumps the
  revision and fence, or a dead letter for any other lookup error); the live test finishes H
  after G's abandon and asserts the active reader returns exactly H's documents
  and the pending lister returns nothing for the scope.

## Proof 3a: the check is three primary-key probes

Measured before implementation with a throwaway script (two tables with the
production primary keys, `ingestion_scopes(scope_id)` and
`scope_generations(generation_id)`, seeded with 100,000 scopes of 10
generations each, then `PREPARE`/`EXECUTE` of the statement under
`plan_cache_mode = force_custom_plan` and `force_generic_plan`):
`generationFreshnessSQL` on 100,000
scopes and 1,000,000 `scope_generations` rows is 3 primary-key index scans, 12
shared buffers, 0.033 ms execution with a custom plan and 0.102 ms with a
generic plan, about 0.10 to 0.15 ms per psql round trip, host load average about
9 to 12.

## Proofs 3b and 3c: measured

Harness: `TestEshuSearchDocumentSupersedeCostLive`
(`go/internal/storage/postgres/eshu_search_document_supersede_cost_live_test.go`,
opt-in with `ESHU_7458_COST_PROOF=1`). It uses a throwaway
`postgres:18-alpine` container (PostgreSQL 18.6, `shared_preload_libraries =
pg_stat_statements`, `track=all`, default 128 MB shared buffers), the bootstrap
schema, and a 4,000-file fixture (the burst size; about 0.5 KiB per file; the
256-row file page yields 16 pages). "Before" is the same binary with an
always-current check injected, which issues exactly the database statements the
pre-fix handler issued (no freshness statement); "after" uses the real
`NewGenerationFreshnessCheck`. Each of 5 rounds runs both arms with the first
mover alternating, and each arm starts from freshly recreated generations
(cascade delete), so storage state matches. `pg_stat_statements_reset()` runs
immediately before the handler (3b) or immediately after H's activation commits
(3c), so 3c counts only what runs after the supersede point.

Host: the shared development machine, load average 22 to 39 for the whole run
(`uptime` before 22.5/33.2/36.8, after 38.9/34.8/37.0). Wall times swing by up to
4x between identical arms because of that load, so wall deltas below are noise;
the counts and server-side statement times are the evidence.

### 3b: overhead when the generation is not superseded

| metric (median of 5) | before | after |
| --- | --- | --- |
| statements in the handler window | 96,108 | 96,125 (+17 = 16 pages + 1) |
| handler wall (noise-dominated) | 1,484.1 ms | 1,418.7 ms |

- Added statements are exactly pages + 1 in all five rounds (17). Bar met.
- Server-side freshness time per item (17 calls): 0.586, 0.851, 0.477, 0.520,
  1.001 ms across the five rounds (median 0.586 ms, 0.0395 % of the median
  before-arm wall of 1,484.1 ms). Worst single call in any round: 0.163 ms. Bar
  (added time at most 1 % of item wall) met.
- Per-check p99 with the paired control: 5,000 alternating iterations of the real
  check and a `SELECT 1` on the same pool, run twice (host load average 32.8 and
  31.5):

  | run | series | p50 | p90 | p99 | max |
  | --- | --- | --- | --- | --- | --- |
  | 1 | check | 241 us | 387 us | 744 us | 22.6 ms |
  | 1 | control `SELECT 1` | 197 us | 310 us | 703 us | 22.1 ms |
  | 2 | check | 244 us | 404 us | 716 us | 10.1 ms |
  | 2 | control `SELECT 1` | 189 us | 305 us | 520 us | 14.3 ms |

  p99 is 0.72 to 0.74 ms, under the 2 ms bar, and the max outliers appear in the
  control series too, so they are host scheduling, not the statement. An earlier
  unpaired 5,000-call loop under heavier load measured p99 2.24 ms and max
  17.9 ms; the paired runs above are the ones with a control, and the throttle
  fallback in the ruling is not needed.

### 3c: a superseded generation (H activates after page 1)

| metric (median of 5) | before (no fence) | after (fence) |
| --- | --- | --- |
| statements after the supersede point | 89,952 | 2 |
| `DELETE` statements after the supersede point | 2 | 0 |
| statement exec time after the supersede point | 1,613.6 ms | 0.7 ms |
| wall after the supersede point | 2,794.1 ms | 2.3 ms |
| documents written | 4,000 | 256 (page 1) |
| handler wall | 3,721.0 ms | 116.1 ms |
| result status | succeeded | superseded |

The two "after" statements are the loader's page-2 read and the page-2
freshness check that noticed the supersede; no `INSERT`, `DELETE` or stats
statement runs after it.
Scope-key wait (real `ReducerQueue`: G claimed and run, H enqueued on
activation, a second owner polling `Claim` every 20 ms):

| metric | before (fence off) | after (fence on) |
| --- | --- | --- |
| activation to H's claim, per round (ms) | 5023, 3462, 4340, 7900, 4116 (median 4340) | 29, 410, 55, 401, 166 (median 166) |
| refused claim polls before H claimed | 135 to 273 | 1 in four rounds, 14 in one |
| G's ack to H's claim (ms, median) | 17 | 49 (poll interval plus load) |

The 135 to 273 refused polls before the claim in the before arm are the scope
conflict key holding H behind the stale item. The fence turns that wait from
seconds into the time G needs to notice the supersede and ack.

## Reading the result

Performance Evidence: on a 4,000-file repository (16 file pages) with the
newer generation activating after page 1, the superseded search-document item
drops from 89,952 statements, 1,613.6 ms of statement time and 2,794.1 ms of wall
after the supersede point to 2 statements, 0.7 ms and 2.3 ms (median of 5
interleaved rounds, alternating first mover, PostgreSQL 18.6 with
pg_stat_statements, host load average 22 to 39 so wall deltas are noisy and the
counts and server times carry the claim). The stale item no longer holds the
scope conflict key: activation to the newer item's claim falls from a median of
4,340 ms to 166 ms. When the generation is not superseded the fence adds exactly
pages + 1 statements (17), 0.586 ms median server time per item (0.04 % of item
wall), and a per-check p99 of 0.72 to 0.74 ms against a `SELECT 1` control
(p99 0.52 to 0.70 ms). The per-check plan is 3 primary-key index scans, 12 shared
buffers, 0.033 ms (custom plan) and 0.102 ms (generic plan) on 100,000 scopes and
1,000,000 generations. Conflict domain: reducer scope key. Worker count,
lease, batch size, claim query and lock order are unchanged. Not measured: the
production 91 s and 289 s statement times from the #7450 audit are quoted, not
reproduced, and this fixture uses small files, so the production saving per
abandoned item is larger than the 2.8 s measured here; ops-qa was not used.

Observability Evidence: `eshu_dp_search_document_generation_superseded_total`
by closed `phase` (`page`, `finalize`), emitted once per abandoned item with
`context.WithoutCancel`; the unit tests assert one point with only the `phase`
attribute for each phase and none for a current generation or a check error. The
same event is `status=superseded` on
`eshu_dp_reducer_executions_total{domain="eshu_search_document"}` and an INFO log
`eshu search document projection abandoned: generation superseded` carrying
`scope_id`, `generation_id`, `domain`, `phase`, `pages_written`,
`documents_written`, `duration_seconds` and `pipeline_phase=reduction`; the
result's evidence summary repeats `phase`, `pages_written` and
`documents_written`. Abandoned pages are not counted in
`eshu_dp_canonical_writes_total`, because `recordCycle` only runs for a
finalized projection. A check error is not counted; it fails the item and shows
in the existing reducer failure metrics and dead-letter counter. A routine
supersede is INFO, not ERROR.

## Verification

The local proofs are the unit suite in `go/internal/reducer/eshusearch`, the wiring tests
in `go/cmd/reducer` and `go/internal/reducer`, the live interleave
(`ESHU_POSTGRES_TEST_DSN=... go test ./internal/storage/postgres -run
TestEshuSearchDocumentHandlerAbandonsSupersededGenerationLive`), and the cost
harness above.

GREEN at the branch head, live interleave (PostgreSQL 18.6):

```text
G after supersede: result=superseded pages_streamed=2 page_docs=[256 256] facts=256 index_docs=256 stats=0 state="building" evidence="eshu search document projection abandoned: generation superseded phase=page pages_written=1 documents_written=256"
--- PASS: TestEshuSearchDocumentHandlerAbandonsSupersededGenerationLive
```

H then finishes `ready` with 600 documents, the active-generation reader returns
exactly H's documents, and the pending lister returns nothing for the scope.
