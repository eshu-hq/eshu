# #5261 — Changed Since repository ownership proof

## Correctness theory and failing-first reproduction

The defect was split ownership between the URL-backed applied request and an
editable repository draft. Selecting another repository could leave the old
result visible, retain the old `scope_id`, and later submit both that scope and
the new repository. The backend intersected both predicates, so the displayed
identity and the evidence owner could disagree.

Failing-first proof on this branch established three independent gaps before
the implementation:

- the component repro retained repository A evidence after selecting B and
  could construct a request containing both selectors;
- the query handler called its reader for a dual-selector request instead of
  rejecting the ambiguity with HTTP 400;
- a scope-selected Postgres response did not carry the resolved repository
  identity. Extending the expected scope row first failed with a scan mismatch,
  proving the storage contract had not selected that source key.

The finished flow has one owner:

1. The selector only contains repositories from the caller-authorized catalog;
   option values are canonical repository IDs.
2. A selection immediately invalidates result, lifecycle, baseline, error, and
   in-flight request ownership before writing a repository-only URL.
3. One bounded `limit=3` generation-lifecycle read selects a retained baseline.
   No valid prior generation produces an explicit no-baseline state and no
   changed-since read.
4. The baseline URL rewrite starts one changed-since read. Request sequence IDs
   prevent a slow B lifecycle or delta response from overwriting a later C
   selection.
5. The API and direct store both reject simultaneous `scope_id` and
   `repository` selectors. A successful repository-scope response reports the
   repository source key resolved from the selected scope; non-repository
   scopes are never mislabeled as repositories.

File evidence removes only the exact `file:<canonical repository id>:` prefix
to show the human path. The complete stable key remains available in diagnostic
details. Non-file keys and file keys without that exact prefix remain unchanged;
there is no delimiter-based heuristic parsing.

## Authorization and bounded-work proof

Repository search filters the already loaded catalog in memory. The focused
component test records the client call count before and after a search edit and
proves it does not change. Selection performs one bounded lifecycle read and one
changed-since read after baseline resolution; it does not scan the corpus per
keystroke.

The catalog's existing authorization boundary applies before the page receives
options. `TestRepositoryListGraphAppliesScopedAuthBeforePagination` and
`TestRepositoryListContentAppliesScopedAuthBeforeMetadata` prove out-of-grant
names are removed before pagination or metadata projection.
`TestResolveRepositorySelectorDeniesOutOfScopeCanonicalID` proves a direct
out-of-grant canonical ID fails closed. Changed Since submits canonical option
values, not ambiguous display names.

## Same-data performance evidence

The storage change adds only `scope.source_key` to the existing single-row scope
resolution projection. Five same-container runs executed the old and new query
shapes 5,000 times each against the same retained Postgres data:

| Run | Old projection | New projection |
| --- | ---: | ---: |
| 1 | 82.588 ms | 78.195 ms |
| 2 | 89.587 ms | 89.915 ms |
| 3 | 83.474 ms | 86.552 ms |
| 4 | 84.281 ms | 95.255 ms |
| 5 | 85.761 ms | 85.873 ms |

Median old was 84.281 ms and median new was 86.552 ms for 5,000 executions,
a 2.7% same-machine delta within the 10% no-regression band. A row differential
proved every pre-existing field identical and the new resolved repository field
non-empty.

After rebuilding the retained API and MCP containers from this branch, eight
authenticated HTTP trials of the changed-since endpoint on the same retained
scope and generation took 0.938755, 0.812632, 0.799725, 0.816045, 0.958990,
1.098280, 1.080361, and 0.965658 seconds. Median latency was 0.9489 seconds and
maximum latency was 1.0983 seconds, below the route's few-seconds contract.
After the preliminary-review fixes and a rebuild on the newly migrated retained
volume, the final eight-trial rerun returned eight HTTP 200 responses with a
0.738387-second median and 0.773836-second maximum.

Runtime contract probes produced:

- both selectors: HTTP 400;
- scope selector only: HTTP 200 with non-empty `scope_id` and `repository`;
- repository selector only: HTTP 200 with non-empty `scope_id` and
  `repository`.

The probe compared response keys and presence only; it did not persist API
credentials, repository IDs, scope IDs, or generation IDs.

The retained-stack probe selected one repository scope and prior generation
inside shell variables, read the running container's bearer credential without
printing it, and invoked the production route with URL-encoded query values:

```text
pg_user=$(docker exec eshu-postgres-1 printenv POSTGRES_USER)
pg_db=$(docker exec eshu-postgres-1 printenv POSTGRES_DB)
row=$(docker exec eshu-postgres-1 psql -v ON_ERROR_STOP=1 -At -F $'\t' \
  -U "$pg_user" -d "$pg_db" -c "SELECT s.scope_id, s.source_key, \
  g.generation_id FROM ingestion_scopes s JOIN LATERAL \
  (SELECT generation_id FROM scope_generations WHERE scope_id=s.scope_id \
  AND generation_id<>COALESCE(s.active_generation_id, '') \
  ORDER BY observed_at DESC LIMIT 1) g ON TRUE \
  WHERE s.scope_kind='repository' AND s.active_generation_id IS NOT NULL \
  AND s.source_key<>'' ORDER BY s.observed_at DESC LIMIT 1")
api_key=$(docker exec eshu-eshu-1 printenv ESHU_API_KEY)
curl -sS -H "Authorization: Bearer $api_key" --get \
  http://127.0.0.1:8080/api/v0/freshness/changed-since \
  --data-urlencode "repository=$repository" \
  --data-urlencode "since_generation_id=$prior_generation"
```

The SQL measurement harness used the same selected `scope_id` for both shapes,
ran each projection 5,000 times in the same retained Postgres container, and
compared the old five-field row with the new six-field row. The old shape was
the shipped `resolveChangedSinceScopeQuery` projection from `origin/main`; the
new shape differed only by the repository-kind `source_key` projection. The
row differential compared the five shared fields and separately asserted that
the repository-kind source key was non-empty.

## Observability

The route keeps its existing changed-since span and attributes. A selector
conflict records an error event on that span before returning HTTP 400. No
repository name, canonical ID, scope ID, or other high-cardinality attribute was
added.

## Verification

Passed on the branch after rebasing onto `origin/main` at `04aa756610`:

```text
cd go && GOCACHE=/tmp/eshu-gocache-5261 go test \
  ./internal/status ./internal/query ./internal/storage/postgres ./internal/mcp -count=1

cd apps/console && npx vitest run \
  src/pages/ChangedSincePage.test.tsx \
  src/pages/ChangedSincePage.repository.test.tsx \
  src/pages/ChangedSincePage.lifecycle.test.tsx \
  src/pages/changedSinceDefault.test.ts \
  src/api/changedSince.test.ts

cd apps/console && npx tsc -b --pretty false

uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml

git diff --check
```

The post-review focused console set passed 27 tests across five files.
The authenticated retained-stack browser workflow is a pre-PR blocking gate.
No PR is created until that run is complete and its result is added here.
