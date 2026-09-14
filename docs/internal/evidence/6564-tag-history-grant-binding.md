# #6564 Tag-History Grant Binding Through BUILT_FROM

`GET /api/v0/images/tag-history` (MCP `list_container_image_tag_history`) left
the #5167 `pendingRowFilteringRoutes` ledger and is now a grant-bound
scoped-token route. `ContainerImageTagObservation` carries no source-repository
key, so a scoped caller is bound through the reducer's
`ContainerImage-[:BUILT_FROM]->Repository` edge.

## Design As Built

- Unscoped and all-scope callers: unchanged, one statement
  (`taghistory.OffsetCypher`, the `SKIP` form).
- Scoped caller with an empty grant: an empty page without a graph read.
- Scoped caller:
  1. It runs the unchanged tag read (`limit+1`).
  2. It then makes one single-clause lookup, `BuiltFromCypher`
     (`go/internal/query/taghistory/builtfrom.go`), keyed by that window's
     distinct,
     sorted, non-empty `resolved_digest` and `previous_digest` values. That is
     at most `2*limit` keys (400); the `limit+1` sentinel row is never looked
     up.
  3. A Go join keeps a row only when its `resolved_digest` image is
     `BUILT_FROM` at least one granted repository. It blanks `previous_digest`
     unless that image is also `BUILT_FROM` a granted repository. It withholds
     observations with no `BUILT_FROM` edge.
  4. `git-repository-scope:` grants resolve to their canonical repository
     (`WithCanonicalScopeRepositories`).
- A failure of either read fails closed with the same error handling; it never
  serves the unfiltered page.
- The BUILT_FROM lookup enforces both of its registered bounds in Go:
  `taghistory.BuiltFromMaxKeys` (400) and `taghistory.BuiltFromMaxRows` (1200).
  Overflow returns an error, so the read fails closed rather than serving a
  page built on a partially read edge set.
- `BuiltFromCypher` RETURNs `DISTINCT`, so a row is one distinct
  (digest, repository) pair, not one BUILT_FROM edge -- what makes 1200 hold,
  since edge identity is `{scope_id, evidence_source}` and parallel edges per
  pair are the designed model.

### Pagination (revised after review)

The first implementation took `truncated` and `next_cursor` from the raw
`limit+1` read BEFORE the grant filter, borrowing the change-surface route's
contract in `go/internal/query/impact/impact_change_surface_traversal.go`. That
is wrong for this route: `limit - count` then measured exactly how many rows of
another tenant's history the filter had withheld from that window, and a
`limit=1` walk over the raw offsets mapped where those rows sat in time. Both
halves were replaced.

**Refill.** A grant-filtered page now reads successive windows until it holds
`limit` VISIBLE rows, the history ends, or `taghistory.MaxRefillReads` (4)
windows have been read (`RefillScopedPage`,
`go/internal/query/taghistory/refill.go`). Consequences:

- A filled page has `count == limit`, so `limit - count` carries no
  withheld-row information.
- A short page means the history ended inside it, or the read cap stopped the
  scan; in the second case `truncated` is true.
- A window that keeps nothing still returns a usable page: `count: 0`,
  `truncated: true`, and a cursor that resumes past what was read. It is never a
  silent short page presented as complete, and never an endpoint the caller
  cannot advance past.
- The honest boundary, now stated on the caller-facing surfaces: on a
  CAP-REACHED page (`truncated: true`, `count < limit`) the shortfall DOES
  describe the scanned span, and `count: 0` says every raw row in it belonged to
  someone else. "Count below limit does not measure withheld rows" is true of a
  filled page, not of that one.
- Every window uses the SAME two single-clause statements. No multi-clause read
  was introduced: the pinned build returns 0 rows for those (see Theory Probe).
- Each window is a FIXED `taghistory.MaxLimit` (200) raw rows, not `limit` rows.
  With `limit`-sized windows the span one request scanned was `4*limit`, so at
  `limit=1` a `count: 0` page told the caller that four SPECIFIC consecutive
  observations were another tenant's. Fixing the window makes that span a
  constant 800 raw rows whatever `limit` is. See
  [keyset pagination](6564-tag-history-keyset-pagination.md).

The cap is 4 because each window costs one keyset read plus one
`taghistory.BuiltFromCypher` lookup, and the lookup's measured worst case is the
case refilling actually triggers. A page whose digests have no `ContainerImage`
node is exactly the fully-withheld page that refills, and 400 such missing keys
cost 1135-1175 ms cold at 5k images and 2247 ms at 10k (see Performance below).
Four windows bound that at roughly 4.7 s and 9 s: the first inside the handler
histogram's 5 s top bucket, the second honestly an outlier above it. A larger
cap multiplies it. Four windows also let a page step over up to `4*limit` (800
at the maximum limit) withheld rows before it must report truncation.

**Cursor token: KEYSET, not an offset.** `next_cursor` carries one row's
`(first_observed_at, uid)` position, so there is no raw row offset anywhere on
the wire or in the token (`taghistory.Cursor`,
`go/internal/query/taghistory/cursor.go`). The first implementation shipped an
unsigned base64url offset payload, which the re-review correctly called a leak
that was still open: decoding it read the pre-filter frontier, and re-encoding an
edited offset walked another tenant's history one row at a time. That design, why
it was replaced rather than signed, the statements the replacement runs, the
theory probe, the live proof and the residue it does NOT close are all in
[keyset pagination](6564-tag-history-keyset-pagination.md).

`offset` stays accepted for unscoped and all-scope callers (the existing
contract); a grant-filtered caller sending a non-zero `offset` gets a 400 naming
`cursor`, and its response OMITS the echoed `offset` field, because a raw row
position on a refilled page is the pre-filter frontier. `offset=0` remains legal
for everyone so the MCP route, which always sends an offset, keeps working on
page one. Every caller receives the same v2 keyset token, so there is one token
format on the wire.

The response still carries no per-page withheld count, which would describe
other tenants' history. Withheld counts go only to telemetry. The disclosure
lives in the truth reason, `grant_filtered: true`, the OpenAPI operation, the
MCP tool description and input schema, the MCP contract matrix, and
`docs/public/reference/http-api.md`.

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
- Statements: the tag read and the BUILT_FROM lookup (then
  `taghistory.Cypher` and `taghistory.BuiltFromCypher` in
  `go/internal/query/tag_history.go`; now `taghistory.Cypher` and
  `taghistory.BuiltFromCypher`, byte-identical), and the seed writer
  `canonicalProvenanceBuiltFromCypher`
  (`go/internal/storage/cypher/provenance_edge_writer.go`). All three were
  extracted from the branch source by constant name at run time, so they ran
  byte-for-byte.
- Resource profile: a local contributor Mac shared with other harness work.
  The timings are relative and local, so `absolute_target_applicable: false`.

## Live Proof (Correctness)

```text
seeded BUILT_FROM edges: d1->repo-granted, d2->repo-other, d4->repo-granted, d4->repo-other
taghistory.Cypher: t1(d1, prev d2), t2(d2), t3(d3), t4(d4)   -- 4 rows, first_observed_at order
digests param: [sha256:d1, sha256:d2, sha256:d3, sha256:d4]
taghistory.BuiltFromCypher: d1->repo-granted, d2->repo-other, d4->repo-granted, d4->repo-other
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
one statement, an empty page (no second read), an empty grant (no read), and a
second-read error. `TestTagHistoryScopedSpanCarriesWithheldCounts` proves the
span records kept=2, withheld_ungranted=1, withheld_unattributed=1, and
previous_digest_blanked=1, and
`TestTagHistoryScopedPageRecordsCompleteOutcome` proves the public counter
records the page outcome for the same request.

## Review Findings Closed

The review of the first implementation raised five findings. What each one is
and what closed it:

**P1 -- pagination disclosed withheld rows.** Closed by refill plus the opaque
cursor above. Proof (`go/internal/query/tag_history_refill_test.go`, all RED
before the change on a new offset-aware graph double that honors `$offset` and
`$limit`, unlike the pre-existing fixed-slice double):

- `TestTagHistoryScopedPageRefillsPastWithheldRows` -- four withheld rows ahead
  of the visible ones; the page still returns `limit` rows. RED: `tags = []`.
- `TestTagHistoryScopedShortPageMeansHistoryEnded` -- a short page carries
  `truncated: false` and no cursor, so `limit - count` measures nothing.
- `TestTagHistoryNextCursorIsOpaqueToken` -- `next_cursor` is a token string,
  is not a bare integer, and a grant-filtered page omits `offset`. RED:
  `next_cursor = map[...]{"offset": 2}`.
- `TestTagHistoryMalformedCursorIsRejected` (renamed from
  `...TamperedCursorIsRejected`; see re-review finding 1) -- foreign
  `image_ref`, non-base64, a flipped base64 character, `{}`, an unknown version,
  a negative offset, and a replay at a different `limit` each return 400.
  RED: 200.
- `TestTagHistoryScopedCallerCannotPageByRawOffset` -- a scoped `offset=1` is
  400 and reaches no graph read. RED: 200 serving a page.
- `TestTagHistoryUnscopedCallerKeepsOffsetPaging` -- the offset contract, the
  echoed `offset`, and the one-statement unscoped read are preserved.
- `TestTagHistoryCursorPagingReachesEveryVisibleRowExactlyOnce` -- a 16-row
  history mixing granted, ungranted and unattributed rows, paged at limits 1,
  2, 3, 5 and 50, returns every visible row exactly once in order, with a page
  budget that fails the test rather than looping.
- `TestTagHistoryFullyWithheldWindowStillPages` -- 40 withheld rows then one
  granted: the caller pages through and reaches it.
- `TestTagHistoryRefillHonoursReadCap` -- exactly `taghistory.MaxRefillReads`
  tag reads and the same number of BUILT_FROM lookups, `count: 0`,
  `truncated: true`, and a cursor at offset `cap*limit`.

**P2 -- `max_results: 400` was not a real bound.** `taghistory.BuiltFromCypher`
then returned one row per BUILT_FROM edge, so a multi-source image exceeded the
key count; the probe in Performance below returned 440 rows for 400 keys. (The
re-review found 1200 was not a real bound either -- see Re-Review Finding 2
below, which added the `DISTINCT` the derivation now rests on.) Closed by
capping in Go and failing closed on overflow, then registering the enforced
number: `max_keys: 400` and `max_results: 1200` in
`go/internal/queryplan/testdata/query-source-coverage.yaml`, enforced by
`taghistory.BuiltFromMaxKeys` and `taghistory.BuiltFromMaxRows` in
`LookupBuiltFromRepositories`. The 3x ratio mirrors the peer entry
`fetchOCIImagesByDigest` (250 keys / 750 results). No Cypher `LIMIT` was added:
truncating there would drop BUILT_FROM edges the caller is entitled to and could
turn a granted row into a withheld one, which is a wrong answer rather than a
bounded one. Proven by `TestTagHistoryBuiltFromFanOutOverflowFailsClosed`
(overflow is a 500 that leaks no repository id) and
`TestTagHistoryBuiltFromBoundsAreBelowTheRegisteredFanOut`.

**P2 -- a mutation survived.** Deleting `.WithCanonicalScopeRepositories()` at
the grant-resolution line left every test green, because no test held a
scope-only grant. `TestTagHistoryScopeOnlyGrantMatchesCanonicalRepository` now
grants only `AllowedScopeIDs: ["git-repository-scope:repository:r_payments"]`
against a BUILT_FROM edge on the canonical id `repository:r_payments`. It passes
on the real tree and fails under that exact deletion; the mutation run is
recorded in the Mutation Proof section below.

**P3 -- stale telemetry anchor.** `docs/public/observability/telemetry-coverage.md`
cited `tag_history.go:102`, which had slid into `Mount`. Repointed to the symbol
anchor ``tag_history.go (`TagHistoryHandler.listTagHistory`)``: a bare line
number is rejected by `scripts/verify-doc-citations.sh` as an unstable LINE
citation, and the symbol survives the next line shift rather than sliding
silently.

**P3 -- `mutated` survives a blanked `previous_digest`.** A scoped caller whose
`previous_digest` was blanked can still read `mutated: true` and conclude some
prior digest existed. The row is left truthful rather than lied about, and the
residue is now stated in the OpenAPI operation, the MCP tool description, the
truth reason, and `docs/public/reference/http-api.md`, alongside the pagination
disclosure.

## Re-Review Findings Closed

The re-review of the fix commit (`review-rev6564b`, NOT READY: P1=1,
P2-blocking=2, P3=2) raised five more. What each one is and what closed it:

**Re-review finding 2 (P2-blocking) -- the 1200-row bound could 500 an entitled
caller.** BUILT_FROM edge identity is `{scope_id, evidence_source}`
(`canonicalProvenanceBuiltFromCypher`,
`go/internal/storage/cypher/provenance_edge_writer.go`), and the retraction
statement beside it exists to drop one scope's support without disturbing
another's, so parallel edges per image/repository pair are the designed model.
Without `DISTINCT` the fan-out was the sum over digests of
`repository x scope_id x evidence_source`, not the distinct-repository count
both the 1.1x seed measurement and the borrowed 3x ratio modelled. A two-source,
two-scope deployment carries four edges per pair, so a `limit=200` page of 200
digests each built from two repositories produced 1600 rows -- over the bound, a
deterministic 500 for the scoped caller alone, on a page an unscoped caller read
successfully.

Closed by adding `DISTINCT` to the RETURN. The only consumer,
`anyRepositoryGranted`, needs set membership, so collapsing parallel edges is
semantically identical and drops no granted edge; `3 x BuiltFromMaxKeys` now
means what it says, up to three distinct source repositories per keyed digest.
The derivation is re-stated in the `BuiltFromMaxRows` and `BuiltFromCypher` doc
comments and in the queryplan manifest comment;
`LookupBuiltFromRepositories`'s `source_sha256` was re-recorded
(`a979c76a...` -> `3288f7cf...`) and `go test ./internal/queryplan/` verifies it.

Proven by `TestTagHistoryDistinctBoundsBuiltFromFanOut`
(`go/internal/query/tag_history_fanout_test.go`), whose graph double emits one
row per matching EDGE and collapses to one row per (digest, repository_id) only
when the statement it is handed asks for `DISTINCT` -- so the test is sensitive
to that word in the production constant, not to a hard-coded number. It also
asserts the seed holds 1600 distinct edge identities, so the fan-out is genuine
multiplicity and not a duplicated fixture. RED through `go test -overlay`
against a copy of `builtfrom.go` outside the worktree with `DISTINCT` removed:
`status = 500`, log line `rows=1600 keys=200 max_rows=1200`. GREEN on the real
tree: 200, `count: 200`, 400 lookup rows. Statement text changed, so a live
check is owed -- see Live Re-Validation Owed below.

**Re-review finding 3 (P2-blocking) -- a matrix row documented the removed
contract.** `docs/public/reference/mcp-tool-contract-matrix.md` was absent from
the fix commit's diff, so its `list_container_image_tag_history` row still said
"offset continuation" and "filtering is per fetched page, so a scoped page can
hold fewer than `limit` rows while `truncated` stays true". Both were false:
`offset` now 400s for exactly the scoped callers the row's body discusses, and
per-page filtering is what the refill removed. The pagination column now names
`cursor` (with `offset`'s unscoped-only status and `offset=0` still legal) and
the body carries the refill contract the other four surfaces state, plus the
reversible-token disclosure.

**Re-review finding 1 (P1) -- PARTIALLY closed; the mechanism half stays open.**
`DecodeCursor` has no MAC, so a caller can mint
`{"v":1,"ref":"<its own ref>","l":1,"o":N}` and read the exact position of every
withheld row -- at LOWER cost than the pre-fix walk, while the OpenAPI cursor
description and the MCP input schema both said an "edited" token is rejected.

This change closes the FALSE CLAIM only, and each correction holds under every
candidate cursor design: "edited" deleted from the OpenAPI cursor description,
the MCP input schema and this document's Pagination section; "count below limit
does not measure withheld rows" qualified as true of a filled page (on a
cap-reached page `count: 0` says every raw row in the scanned span was
withheld); and `TestTagHistoryTamperedCursorIsRejected` renamed to
`TestTagHistoryMalformedCursorIsRejected`, with a comment recording that a
well-formed forged payload passes every check.

It adds NO MAC and no caller-facing residual-disclosure wording. The leak is
still open and is to be FIXED, not disclosed and accepted; the choice across
signed, keyset, and server-side cursors is pending. A reviewer should read
finding 1 as OPEN.

**Re-review finding 4 (P3) -- the overflow 500 leaked cross-tenant counts.**
Both bound-breach messages named the edge and digest counts of the RAW
pre-filter window, which spans every tenant's images on the `image_ref`, and the
handler renders a read error into the 500 body verbatim. Both now log their
counts (`slog.ErrorContext` with `rows`/`keys`/`max_*`, no digest and no
repository id) and return fixed, count-free sentinels.
`TestTagHistoryBuiltFromFanOutOverflowFailsClosed` asserts the body carries
neither a repository id nor either count.

**Re-review finding 5 (P3) -- five dangling doc-comment references.**
`tagHistoryMaxRefillReads`, `tagHistoryBuiltFromCypher`,
`refillScopedTagHistoryPage in tag_history_refill.go` and `tagHistoryPageCursor
in tag_history_cursor.go` survived the package move and resolved to nothing.
Repointed in `tag_history.go` and `auth_scoped_routes_supply_chain_infra.go` to
`taghistory.MaxRefillReads`, `taghistory.BuiltFromCypher`,
`taghistory.RefillScopedPage` and `taghistory.Cursor`. The same sweep fixed
further stale names the re-review did not list, in this document
(`tagHistoryCypher`, `readTagHistoryWindow`) and in the test comments.

## Live Re-Validation Of `DISTINCT`: Done

Adding `DISTINCT` to `BuiltFromCypher` changed its text, so the live re-run it
owed was outstanding for one commit. It is now discharged, on the pinned
`eshu-nornicdb-pr290:3722b483c02c` (self-reports NornicDB `1.2.1`):
`TestTagHistoryKeysetNornicDBLive/RefillScopedPage_returns_exactly_the_granted_rows`
runs the real `LookupBuiltFromRepositories` -- `DISTINCT` and all -- against 457
seeded observations whose images carry real
`ContainerImage-[:BUILT_FROM]->Repository` edges, and returns exactly the 229
granted rows over 3 pages. The seed asserts its own edge count before reading, so
a dropped write fails the test instead of producing a vacuous pass. Details in
[keyset pagination](6564-tag-history-keyset-pagination.md).

## Mutation Proof

`TestTagHistoryScopeOnlyGrantMatchesCanonicalRepository` is the regression for
review finding 3, so its sensitivity was shown by mutation rather than by a
prior RED. The mutation was applied through `go test -overlay` against a copy of
`tag_history.go` OUTSIDE the worktree, so no production file was edited:
`.WithCanonicalScopeRepositories()` deleted from the `access` assignment in
`listTagHistory`. Both outputs are in the handoff report.

## Performance

No-Regression Evidence: the unscoped and all-scope path is unchanged. The tag
statement text is identical, it issues one statement
(`TestTagHistoryUnscopedCallerUnchanged` and
`TestTagHistoryUnscopedCallerKeepsOffsetPaging` each assert one graph call), and
it returns the same rows. The read moved into `taghistory.ReadWindow` without
changing the statement or the `limit+1` bound, which is why its queryplan entry
moved file but kept `class: keyed_support`, `key_bound: single_key` and
`max_results: 201`. The scoped path is new;
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
| `taghistory.Cypher`, limit 201, 201 rows | 9.8 ms | n/a |
| `taghistory.Cypher`, unused `image_ref`, 0 rows | 1.3 ms | 2.7 ms |
| `taghistory.BuiltFromCypher`, 400 keys all matching (440 / 400 rows) | 94-110 ms | 88 ms |
| `taghistory.BuiltFromCypher`, 200 match + 200 missing | 48 ms | n/a |
| `taghistory.BuiltFromCypher`, 400 keys all missing | 1135-1175 ms | 2247 ms |
| `taghistory.BuiltFromCypher`, 50 keys all missing | n/a | 350 ms |
| `taghistory.BuiltFromCypher`, 1 missing key | 12 ms | 76 ms |
| `taghistory.BuiltFromCypher`, 1 existing key | 0.7 ms | 0.9 ms |

Known hazard, open for an owner decision: the cold cost of a digest that has no
`ContainerImage` node grows with the size of the `ContainerImage` label, while
matching keys stay flat. The theory is that an index miss falls back to a scan;
the NornicDB source has not been read to confirm it. The `UNWIND $digests AS d
MATCH (i:ContainerImage {digest: d})-...` variant is no better: 1379 ms at 5k
and 2709 ms at 10k with all keys missing, returning the same 440 rows on
identical keys. So the cost sits in the backend lookup, not the statement shape.
It affects only scoped callers whose page references images not yet projected;
unscoped callers never run the lookup.

Refill multiplies that per-request cost by at most `taghistory.MaxRefillReads`,
which is why the cap is 4 rather than a round ten: 4 x 1135-1175 ms is roughly
4.7 s at 5k images, and 4 x 2247 ms is roughly 9 s at 10k. The worst case is
the SAME case -- a page whose digests have no `ContainerImage` node is
simultaneously the missing-key case above and the fully-withheld page that
triggers refill. These are the measured single-lookup numbers multiplied by the
cap, not a new measurement of the refill loop: no corpus-scale timing was run
locally for this change, because the owner requires measured performance proof
on the remote instance and the missing-digest probe is a separate open item.
The refill loop's own cost therefore remains unmeasured at corpus scale and is
bounded, not benchmarked. `eshu.query.tag_history.refill_reads` and
`eshu.query.tag_history.refill_read_cap_reached` are the signals that tell an
operator whether any real caller is paying more than one window.

Observability Evidence: the handler span `query.container_image_tag_history`
gains `eshu.query.tag_history.grant_filtered`, `grant_kept_count`,
`withheld_ungranted_count`, `withheld_unattributed_count`,
`previous_digest_blanked_count`, and -- for the refill -- `refill_reads` (how
many windows one scoped page consumed) and `refill_read_cap_reached` (the page
stopped on the cap rather than on a full page or the end of the history). Those
two are what separate "this caller's grant covers a thin slice of a busy tag"
from "the route is slow" at 3 AM, since a capped page is also the page that pays
the most BUILT_FROM lookups. All are bounded scalars, never digests or
repository ids. Those five counts live on the span and NOWHERE ELSE -- a
review finding this branch closed, not a design it started with. The first
implementation also published withheld_ungranted and withheld_unattributed as
`/metrics` increments labelled only by `disposition` and `service.namespace`.
`/metrics` bypasses authentication (a literal `publicHTTPPaths` entry in
`go/internal/query/auth.go`, honoured before any token handling) and is mounted
on the same admin mux as the API, so on a quiet deployment a scoped caller
could recover its own page's withheld count from a before/after scrape.
`TestTagHistoryMetricsDiscloseNoWithheldCounts` is the regression, asserting
over the WHOLE exported metric set rather than one name so the same numbers
cannot return under a different instrument. It was RED against the first
implementation, on `..._scoped_rows_total` carrying
`disposition="withheld_ungranted"`.

The public counter is now
`eshu_dp_query_container_image_tag_history_scoped_pages_total{outcome}`,
`outcome` bounded to complete and read_cap_reached: one datapoint per
grant-filtered page, carrying nothing the caller does not already hold.
`grant_filtered: true` is in its own body, and read_cap_reached is derivable
from it, because `RefillScopedPage` sets `CapReached` exactly when it returns a
truncated page holding fewer than `limit` rows -- a residue
`ScopedTruthReason` already discloses in full. It is also the signal an
operator is paged for, so the route stays diagnosable at 3 AM without running
the side channel. Accepted cost: a deployment with no
`OTEL_EXPORTER_OTLP_ENDPOINT` exports no traces and sees no withheld counts.
It records scoped callers only; an unscoped caller records nothing
(`TestTagHistoryUnscopedCallerRecordsNoScopedPages`). Scoped
latency, including the lookup above, shows in the existing
`eshu_dp_query_container_image_tag_history_duration_seconds`. A second-read
failure records `eshu_dp_query_container_image_tag_history_errors_total`, with
reason `query_error` or `backend_unavailable`.

## Package Placement

The refill loop and the cursor could not be new files in `go/internal/query`:
that directory is over the dirgate 40-file cap and pinned at 502 in
`scripts/lib/dirgate-grandfather.tsv`, whose header rules out absorbing a file
the change itself adds. The sanctioned exit the ledger names -- and the one
#6060 Lane B is already using for this package -- is a leaf package, so the two
statements, the bounds derived from them, the refill loop, the grant join and
the cursor now live in `go/internal/query/taghistory`. It imports only the
standard library and `querycontract` (`GraphQuery`, `RepositoryAccessFilter`,
`StringVal`/`BoolVal`), never the query root.

`go/internal/query` therefore still holds exactly 502 non-test `.go` files and
its ledger row is untouched, which also means this change cannot collide with a
concurrent extraction re-pinning the same row. `query.TagHistoryRow` is now an
alias for `taghistory.Row`, so the wire shape has one definition. The queryplan
manifest entries moved with the symbols: `taghistory/page.go:ReadWindow`,
`taghistory/page.go:ReadOffsetWindow` and
`taghistory/builtfrom.go:LookupBuiltFromRepositories`.
