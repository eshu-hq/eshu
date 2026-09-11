# #6564 Tag-History Grant Binding Through BUILT_FROM

`GET /api/v0/images/tag-history` (MCP `list_container_image_tag_history`) left
the #5167 `pendingRowFilteringRoutes` ledger and is now a grant-bound
scoped-token route. `ContainerImageTagObservation` carries no source-repository
key, so a scoped caller is bound through the reducer's
`ContainerImage-[:BUILT_FROM]->Repository` edge.

## Design As Built

- Unscoped and all-scope callers: unchanged, one statement (`tagHistoryCypher`).
- Scoped caller with an empty grant: an empty page without a graph read.
- Scoped caller:
  1. It runs the unchanged tag read (`limit+1`).
  2. It then makes one single-clause lookup, `tagHistoryBuiltFromCypher`
     (`go/internal/query/tag_history.go`), keyed by the page's distinct,
     sorted, non-empty `resolved_digest` and `previous_digest` values. That is
     at most `2*limit` keys (400); the `limit+1` sentinel row is never looked
     up.
  3. A Go join keeps a row only when its `resolved_digest` image is
     `BUILT_FROM` at least one granted repository. It blanks `previous_digest`
     unless that image is also `BUILT_FROM` a granted repository. It withholds
     observations with no `BUILT_FROM` edge.
  4. `git-repository-scope:` grants resolve to their canonical repository
     (`WithCanonicalScopeRepositories`).
- A failure of the second read fails closed with the same error handling as
  the first; it never serves the unfiltered page.
- Pagination: `truncated` and `next_cursor` come from the raw `limit+1` read
  before the grant filter. This is the contract the change-surface route
  documents in `go/internal/query/impact/impact_change_surface_traversal.go`
  (`changeSurfaceFilterTraversalRows`: "a short page is flagged truncated rather
  than presented as complete"). A scoped page can hold fewer than `limit` rows,
  even zero, while `truncated` stays true. It is disclosed in the truth reason,
  `grant_filtered: true`, the OpenAPI operation, the MCP tool description, and
  `docs/public/reference/http-api.md`. The response carries no per-page withheld
  count, which would describe other tenants' history. Withheld counts go only to
  telemetry.

## Theory Probe (Orchestrator, Before Implementation)

The seed: repositories `granted` and `other`; images d1..d4; `BUILT_FROM` edges
d1->granted, d2->other, d4->granted and d4->other, d3 none; tags t1..t4 on one
`image_ref`; `t1.previous_digest=d2`.

The probe ran on the pinned NornicDB and on upstream v1.3.1:
- The naive two-MATCH grant join returned 0 rows on both. The correct answer is
  t1 and t4, so the join stays in Go.
- The single-clause tag read and the single-clause `BUILT_FROM` lookup both
  returned the seeded rows.

## Inputs

- Backend: the image `eshu-nornicdb-pr290:3722b483c02c`, image ID
  `sha256:f9b95ff1c059f5437278ba86de5c7725d2fe6527c8e84701294c0c42d9055e99`.
  It ran as a disposable container with `NORNICDB_EMBEDDING_ENABLED=false` and
  `NORNICDB_NO_AUTH=true`, through the HTTP endpoint `/db/nornic/tx/commit`, on
  a clean store each time.
- Schema: `container_image_digest` and `container_image_tag_observation_ref`
  indexes, applied before seeding. These are the production DDL strings from
  `go/internal/graph/schema_tables_indexes.go`.
- Statements: `tagHistoryCypher` (`tag_history.go`),
  `tagHistoryBuiltFromCypher` (`tag_history.go`), and the seed writer
  `canonicalProvenanceBuiltFromCypher`
  (`go/internal/storage/cypher/provenance_edge_writer.go`). All three were
  extracted from the branch source by constant name at run time, so they ran
  byte-for-byte.
- Resource profile: a local contributor Mac shared with other harness work.
  The timings are relative and local, so `absolute_target_applicable: false`.

## Live Proof (Correctness)

```text
seeded BUILT_FROM edges: d1->repo-granted, d2->repo-other, d4->repo-granted, d4->repo-other
tagHistoryCypher: t1(d1, prev d2), t2(d2), t3(d3), t4(d4)   -- 4 rows, first_observed_at order
digests param: [sha256:d1, sha256:d2, sha256:d3, sha256:d4]
tagHistoryBuiltFromCypher: d1->repo-granted, d2->repo-other, d4->repo-granted, d4->repo-other
join: t1 kept, previous_digest sha256:d2 blanked
join: t2 withheld_ungranted [repo-other]
join: t3 withheld_unattributed
join: t4 kept
scoped result for grant {repo-granted}: [t1, t4]
```

The container was removed with `docker rm -f eshu-6564b` after each run.
`docker ps -a --filter name=eshu-6564b` then listed nothing, and no listener
remained on 17985 or 17986. The image declares no `VOLUME`, so no anonymous
volume was created.

Two seed-side parser behaviours on this build were worked around by passing
values as parameters. Production passes every value as a parameter, so neither
touches these statements.
- A statement literal containing `//` (for example `RETURN 'a://b'`) fails with
  `syntax error: unclosed quote`. `RETURN 'a:/b'` and the parameter `$v = 'a://b'`
  both succeed.
- `UNWIND [...] AS d CREATE (:ContainerImage {digest: 'sha256:' + d})` stored
  the unevaluated text `sha256:' + 'd1`.

## Handler Proof

RED, commit 90dc1c15f, before the change:
`go test ./internal/query/ -run TestTagHistory`, exit 1.
- `TestTagHistoryScopedCallerKeepsOnlyGrantedBuiltFromRows`: scoped tags were
  `[t1 t2 t3 t4]`; want `[t1 t4]`.
- `TestTagHistoryScopedCallerBlanksUngrantedPreviousDigest`: `previous_digest`
  was `sha256:d2`; want it omitted.
- `TestTagHistoryScopedCallerTruncationFollowsUnfilteredWindow`: tags were
  `[t1 t2]`; want `[t1]`.
- `TestTagHistoryScopedCallerEmptyGrantShortCircuits`: count was 4; want 0.
- `TestTagHistoryScopedCallerBuiltFromErrorFailsClosed`: status was 200; want 500.
- `TestTagHistoryRouteIsGrantBoundAllowlisted`: not allowlisted.

GREEN after the change: `go test ./internal/query/ ./internal/mcp/
./internal/queryplan/ -count=1` exit 0 for each package, `go vet` on those
packages exit 0, and `gofmt -l` on the touched files printed nothing. The
handler tests cover these cases: positive, negative, no edge, multi-source,
`previous_digest` blanked and kept, unscoped (no auth and all-scope) staying on
one statement, an empty page (no second read), an empty grant (no read), a
second-read error, and truncation over the unfiltered window.
`TestTagHistoryScopedFilterRecordsDispositionCounts` proves the counter records
kept=2, withheld_ungranted=1, withheld_unattributed=1, and
previous_digest_blanked=1.

## Performance

No-Regression Evidence: the unscoped and all-scope path is unchanged. The tag
statement text is identical, it issues one statement
(`TestTagHistoryUnscopedCallerUnchanged` asserts one graph call), and it returns
the same rows. The queryplan source digest for `listTagHistory` changed only
because the handler body gained the scoped branch. The scoped path is new;
before this change it was a 403, so it has no prior baseline. Its cost was
measured with a 5,000-image seed (50 repositories, 5,500 `BUILT_FROM` edges
written by the production writer, every tenth image multi-source, 400
observations on one `image_ref`), then scaled to 10,000 images.

This NornicDB build caches results by parameter value. The same 400 missing
keys, reversed, cost 1145 ms cold again, and every repeat of an identical
parameter value returns in 1-5 ms. So only first runs measure the lookup, and
the table reports cold first runs. Warm repeats are cache hits and are not
reported as latency.

| Statement (cold first run) | 5k images | 10k images |
| --- | --- | --- |
| `tagHistoryCypher`, limit 201, 201 rows | 9.8 ms | n/a |
| `tagHistoryCypher`, unused `image_ref`, 0 rows | 1.3 ms | 2.7 ms |
| `tagHistoryBuiltFromCypher`, 400 keys all matching (440 / 400 rows) | 94-110 ms | 88 ms |
| `tagHistoryBuiltFromCypher`, 200 match + 200 missing | 48 ms | n/a |
| `tagHistoryBuiltFromCypher`, 400 keys all missing | 1135-1175 ms | 2247 ms |
| `tagHistoryBuiltFromCypher`, 50 keys all missing | n/a | 350 ms |
| `tagHistoryBuiltFromCypher`, 1 missing key | 12 ms | 76 ms |
| `tagHistoryBuiltFromCypher`, 1 existing key | 0.7 ms | 0.9 ms |

Known hazard, open for an owner decision: the cold cost of a digest that has no
`ContainerImage` node grows with the size of the `ContainerImage` label, while
matching keys stay flat. The theory is that an index miss falls back to a scan;
the NornicDB source has not been read to confirm it. The `UNWIND $digests AS d
MATCH (i:ContainerImage {digest: d})-...` variant is no better: 1379 ms at 5k
and 2709 ms at 10k with all keys missing, returning the same 440 rows on
identical keys. So the cost sits in the backend lookup, not the statement shape.
It affects only scoped callers whose page references images not yet projected;
unscoped callers never run the lookup.

Observability Evidence: the handler span `query.container_image_tag_history`
gains `eshu.query.tag_history.grant_filtered`, `grant_kept_count`,
`withheld_ungranted_count`, `withheld_unattributed_count`, and
`previous_digest_blanked_count`. The new counter is
`eshu_dp_query_container_image_tag_history_scoped_rows_total{disposition}`,
with `disposition` bounded to kept, withheld_ungranted, withheld_unattributed,
and previous_digest_blanked. It records scoped callers only; an unscoped caller
records nothing (`TestTagHistoryUnscopedCallerRecordsNoScopedRows`). Scoped
latency, including the lookup above, shows in the existing
`eshu_dp_query_container_image_tag_history_duration_seconds`. A second-read
failure records `eshu_dp_query_container_image_tag_history_errors_total`, with
reason `query_error` or `backend_unavailable`.
