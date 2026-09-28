# 7290: admin read surface for poisoned changed-since links

Issue #7290 gives the changed-since link writer's dark ledger
(`changed_since_scope_cursor`, migration 136, #7127 PR-3a) a bounded,
scoped, deterministic read: `POST /api/v0/admin/changed-since/poisoned-links/query`
and the `list_changed_since_poisoned_links` MCP tool. The writer is a runner
over its own ledger, not a `fact_work_items` queue domain (#7127 ruling 8.10
item 10), so a poisoned or retrying scope never appears in
`list_dead_letter_work_items`.

Source binding: branch `feat/7290-changed-since-poison-read`, rebased onto
`origin/main` `83cbf62fcb`. The PostgreSQL used throughout is
`postgres:18-alpine` in a disposable container, port 25432 (checked free with
`lsof` first, avoiding the reserved ports 17474, 17687, 27687, 15432, 18080,
58080-58081).

## Local proof

```
docker run -d --name eshu-7290-pg -p 25432:5432 \
  -e POSTGRES_USER=eshu -e POSTGRES_PASSWORD=change-me -e POSTGRES_DB=eshu \
  postgres:18-alpine
```

```
cd go && env -u GOROOT ESHU_POSTGRES_DSN="postgres://eshu:change-me@localhost:25432/eshu?sslmode=disable" \
  go test ./internal/query/admin/... -count=1
# ok  	github.com/eshu-hq/eshu/go/internal/query/admin           4.677s
# ok  	github.com/eshu-hq/eshu/go/internal/query/admin/store     3.796s
# (plus admin/audit, admin/identity, admin/provider/config — unaffected)
```

The same command was also run under `-race`, four repeated times for the
plan-shape test alone to rule out flake, and the container was removed
afterward (`docker rm -f eshu-7290-pg`).

### RED/GREEN

- **TDD RED (unit level, before implementation existed):** `go vet
  ./internal/query/admin/...` failed with `undefined:
  changedSincePoisonedLinkSchemaVersion`, `undefined:
  ChangedSincePoisonedLink`, and five more `undefined` errors, confirming the
  handler test was written before `admin.ChangedSincePoisonedLinkFilter`/
  `admin.ChangedSincePoisonedLink`/`Store.ListChangedSincePoisonedLinks`
  existed.
- **Live-test sensitivity RED:** with the store query's `status=poisoned`
  predicate mutated to `poisoned_activation_seq IS NULL` (inverted), the live
  filters/pagination test failed immediately: `unscoped page = 50 items,
  truncated=true, want 5 untruncated` — the seeded healthy/other-run rows
  leaked into the page because the predicate no longer excluded them. Revert
  restored the exact committed query text (`git diff --stat` empty) and the
  suite went green again.
- **GREEN:** every test below passes, repeatably, against the live
  container.

### Coverage against the checklist

Seeded fixture (`TestAdminHandler_ChangedSincePoisonedLinksQueryLiveFiltersAndPagination`):
five `changed_since_scope_cursor` rows across two `ingestion_scopes`
repositories (three poisoned, `poisoned_activation_seq` set; two retrying,
`attempt_count > 0` with it NULL), plus one healthy row (`attempt_count = 0`,
`poisoned_activation_seq` NULL) in the same repository as the first three.

- **Healthy rows excluded:** an unscoped page returns exactly the five
  poisoned/retrying rows; the healthy scope never appears.
- **Status filter:** `status=poisoned` returns exactly the three poisoned
  rows, each reporting its `poisoned_activation_seq` as `activation_seq`;
  `status=retrying` returns exactly the two retrying rows, each reporting its
  `attempt_activation_seq` and `attempt_count`.
- **scope_id filter:** returns exactly the one matching row.
- **Keyset pagination:** walking the five seeded rows at `limit=2` (three
  pages: 2, 2, 1) returns every row exactly once, in ascending `scope_id`
  order, with `next_cursor` present on every truncated page and absent on the
  final untruncated one.
- **Limit enforcement:** every page in the walk above returns at most the
  requested `limit`.
- **Scoped-token isolation**
  (`TestAdminHandler_ChangedSincePoisonedLinksQueryLiveRepositoryScopedGrant`):
  a token granted only repository A's identifier
  (`AllowedRepositoryIDs`, no `AllowedScopeIDs`) sees repository A's poisoned
  scope and none of repository B's retrying scope — proves the SQL join
  authorization (`scope.scope_kind = 'repository' AND scope.source_key =
  ANY($n)`) against real `ingestion_scopes` rows, mirroring
  `TestAdminHandler_InputInvalidFactsQueryLiveRepositoryScopedGrant`.
- **Timeout wired:** `TestListChangedSincePoisonedLinksFiltersAndTruncates`
  (unit, `go/internal/query/admin/changedsincepoisonedlinks_test.go`) asserts
  the exact `timeout_ms` value reaches `ChangedSincePoisonedLinkFilter.Timeout`
  (7500ms in, `7500*time.Millisecond` out), which the handler threads into
  `context.WithTimeout`. Every live test above passes `timeout_ms` on every
  request and completes well inside it. No test in this admin family
  (dead-letters, input-invalid-facts, or this one) forces an actual 504
  against a live backend — doing so reliably needs an artificial slow
  statement, which none of these tests inject, and would trade a real
  proof for a flaky one for no gain over the unit proof above.

## Query-plan shape at scale

`TestListChangedSincePoisonedLinksQueryPlanUsesPrimaryKeyAtScale`
(`go/internal/query/admin/store/changedsincepoisonedlinks_live_test.go`)
seeds 5,000 `ingestion_scopes` / `changed_since_scope_cursor` rows (about
1.7% poisoned or retrying, deterministically every 100th/137th row), runs
`ANALYZE` on both tables, then calls `buildListChangedSincePoisonedLinksQuery`
itself (not a hand copy, per `eshu-postgres-rigor`) wrapped in
`EXPLAIN (ANALYZE, BUFFERS)`.

**`changed_since_scope_cursor` holds one row per scope, not per generation or
fact** (unlike `changed_since_key_state`/`changed_since_link_deltas`, which
are per-key and sized in the millions on ops-qa per the #7127 shim). A "few
thousand" cursor rows is close to full production scale for this table, not
a small sample of something much larger.

Observed plans, across repeated runs with fresh `ANALYZE` each time:

```
-- Plan A (more common): Hash Join over two Seq Scans
Limit (actual time=1.172..1.183 rows=50 loops=1)
  ->  Sort (Sort Key: cursor.scope_id)
        ->  Hash Join (Hash Cond: scope.scope_id = cursor.scope_id)
              ->  Seq Scan on ingestion_scopes scope (rows=5000)
              ->  Hash
                    ->  Seq Scan on changed_since_scope_cursor cursor
                          Filter: (poisoned_activation_seq IS NOT NULL) OR (attempt_count > 0)
                          Rows Removed by Filter: 4914
Execution Time: 0.894-1.429 ms; ~200-2158 shared buffers total

-- Plan B (observed on an earlier run, before repeated ANALYZE sampling
-- shifted row-count/selectivity estimates): Index Scan + Nested Loop
Limit (actual time=0.060..1.084 rows=50 loops=1)
  ->  Merge Join
        ->  Index Scan using changed_since_scope_cursor_pkey on changed_since_scope_cursor cursor
              Filter: (poisoned_activation_seq IS NOT NULL) OR (attempt_count > 0)
        ->  Index Only Scan using ingestion_scopes_pkey on ingestion_scopes scope
Execution Time: 1.139 ms; 174 shared buffers
```

`changed_since_scope_cursor` carries no index on `poisoned_activation_seq` or
`attempt_count` (adding one is not justified without a measured hot path, and
none exists yet — the writer ships dark). At 1.7% match density spread
uniformly by `scope_id`, ANALYZE's row-count/selectivity sampling can push
the cost-based planner either way between these two plans across repeated
runs; both are legitimate and cheap. Neither is a "problem scan" by the
criterion that actually matters here: bounded cost, not a mandated physical
operator. The live test asserts that, not the operator name:

- **Execution Time <= 200 ms** (observed 0.894-1.429 ms — over 100x headroom).
- **Total shared buffers <= 3x the seeded row count (15,000)** (observed
  174-2,158 — a bound wide enough to tolerate the Plan A/Plan B flip without
  flaking, while still catching a genuinely pathological plan, e.g. a
  quadratic-cost join).

Both bounds were confirmed stable across `-race` reruns of the whole
`admin/store` package (4 consecutive green runs).

## Performance Evidence

No-Regression Evidence: this is a new read surface with no prior baseline to
regress. The bounded-cost proof above (sub-2ms, low-hundreds-to-low-thousands
of shared buffers at 5,000 rows, the realistic ceiling for this one-row-per-scope
table) stands in place of a before/after comparison.

## Observability Evidence

Observability Evidence: the `query.changed_since_poisoned_links` span (`db.system`, `db.sql.table`,
`changed_since.poisoned_links.status_filter`,
`changed_since.poisoned_links.count`) plus
`eshu_dp_query_changed_since_poisoned_links_duration_seconds` and
`eshu_dp_query_changed_since_poisoned_links_errors_total{reason}`, mirroring
the sibling `list_reducer_input_invalid_facts` read
(`eshu_dp_query_input_invalid_facts_*`). Registered in
`go/internal/telemetry/instruments.go`.

## Housekeeping

- `specs/live-tests.v1.yaml`: two new rows,
  `go/internal/query/admin/changedsincepoisonedlinks_live_test.go` and
  `go/internal/query/admin/store/changedsincepoisonedlinks_live_test.go`,
  both `class: scheduled` (untagged, env-gated, unmeasured cost — matching
  the sibling `admin/replay_fence_live_test.go` and
  `admin/replay_superseded_live_test.go` rows). `scripts/verify-live-tests-ledger.sh`
  passes (529 rows, 529 live files classified).
- **Plan-pin check** (the concern behind adding a `class: ci` row without a
  build tag): `scripts/lib/live_backend_test_targets.py` extracts targets
  from `class: ci` rows only (confirmed by inspecting
  `scripts/test-run-live-backend-tests.sh`'s own ci-row-count assertion). Both
  new rows are `class: scheduled`, so they are invisible to it: running the
  extractor against `specs/live-tests.v1.yaml` before and after this change
  returns the identical target count (28 targets, 29 `class: ci` rows, both
  unchanged) and the identical `class: ci` row count. Neither new
  Postgres-only test enters the graph-backend live-backend CI plan or its
  Docker/nornicdb/neo4j compose stacks.
- Two real bugs found and fixed while writing these tests: both new live
  tests' cleanup originally used a plain `defer db.Close()` ahead of a
  `t.Cleanup` delete callback (in the `admin/store` plan-scale test) — since
  a `defer` in a test function runs at function return, strictly before any
  `t.Cleanup` callback, the connection closed before the delete ever ran,
  silently (the error was discarded). Separately, every cleanup in both new
  files deleted only `ingestion_scopes`, leaving the seeded
  `changed_since_scope_cursor` rows behind — that table carries no foreign
  key to `ingestion_scopes` by design (#7127 ruling 7.3), so there is no
  cascade to rely on. Both were caught by inspecting the database directly
  after a run (`SELECT count(*) FROM changed_since_scope_cursor WHERE
  scope_id LIKE ...`), fixed (`t.Cleanup` for the close, ordered after the
  delete registration; delete from both tables in every cleanup), and
  reverified with a clean run showing 0 leaked rows in both tables.
