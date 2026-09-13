# #6564 Tag-History Keyset Pagination

Companion to
[6564-tag-history-grant-binding.md](6564-tag-history-grant-binding.md), which
owns the grant binding itself. This file owns the pagination redesign that closed
the re-review's P1: `GET /api/v0/images/tag-history` (MCP
`list_container_image_tag_history`) now continues by ROW KEY, not by row offset.

## The Defect, Reproduced

The first implementation replaced `next_cursor: {"offset": n}` with an unsigned
base64url JSON token carrying the same integer. The re-review found that this
narrowed the disclosure without closing it, and the pre-fix regression test
reproduced both halves against the real handler:

```
$ go test ./internal/query/ -run TestTagHistoryCursorCarriesNoRowPosition -count=1 -v
    tag_history_keyset_test.go:38: next_cursor payload carries "o" = 2:
      map[string]interface {}{"l":2, "o":2, "ref":"ghcr.io/eshu-hq/demo:1.0.0", "v":1}
--- FAIL: TestTagHistoryCursorCarriesNoRowPosition (0.00s)
RED_EXIT=1
```

and, on a seed of four withheld rows followed by two granted ones, a token the
TEST minted rather than the server issued:

```
$ go test ./internal/query/ -run TestRedForgedOffsetCursorIsHonoured -count=1 -v
    forged-offset-4 status=200 body={... "count":2, "tag_history":[{"tag":"t04",...},{"tag":"t05",...}] ...}
    RED: a minted offset cursor was HONOURED (status 200); the caller addressed raw row 4 directly
RED_EXIT=1
```

`{"v":1,"ref":"<the caller's own image_ref>","l":2,"o":4}` passed every check in
`DecodeCursor` and returned exactly the rows at raw positions 4 and 5. The four
rows the grant filter withheld were addressable by index.

Why the first implementation did not simply sign it, for the record: no shared
server-side key exists for a query handler to sign with
(`ESHU_AUTH_SECRET_ENC_KEY` is `cmd/api`'s provider-config DEK), and a
process-local key breaks paging across a restart or a second replica. A signature
would also have left the honest cap-reached channel untouched. Keyset needs no
secret and removes the offset from existence, which is why it was chosen over a
MAC, an AEAD, or a server-side cursor table.

## The Design

### Cursor v2

```go
type Cursor struct {
    Version  int    `json:"v"`             // 2
    ImageRef string `json:"ref"`
    At       string `json:"at,omitempty"`  // "" is a real stored value
    NullAt   bool   `json:"nt,omitempty"`  // key row had NO first_observed_at
    UID      string `json:"uid"`
}
```

`DecodeCursor` refuses malformed base64 or JSON, a version other than 2 (which
includes every v1 offset token, so a client across the deploy boundary restarts
from page one), a foreign `image_ref`, an empty `uid`, and `nt: true` alongside a
non-empty `at`. None of those is a tamper detection, and no surface claims one:
the token carries no MAC. With a keyset payload that no longer matters, because a
forged key can only ask for the forger's OWN visible rows after it.

The `Limit` binding was DROPPED. It existed only to stop an offset token being
re-aimed at `limit=1`; a key has no position to re-aim, so a caller may change
`limit` mid-walk, matching `RepositoryRefPageCursor`, which also carries no
limit.

`NullAt` is load-bearing because the store holds three timestamp states with
three sort positions: a stored empty string (a zero `ObservedAt`, written by
`ociTagObservedAtValue`), a real fixed-width millisecond string, and NO
`first_observed_at` property at all on nodes created before #5459 shipped it. The
empty string needs no special case. "No timestamp" cannot be expressed as a
string value.

### The three statements

All are single-clause (`MATCH ... WHERE ... RETURN ... ORDER BY ... LIMIT`) on
the existing `container_image_tag_observation_ref` index, with the same
projection, and the grant join still runs in Go.

```cypher
-- (A) FirstPageCypher
MATCH (t:ContainerImageTagObservation {image_ref: $image_ref})
RETURN <projection> ORDER BY t.first_observed_at, t.uid LIMIT $limit

-- (B) AfterKeyCypher (the key is timestamped; $after_at may be "")
MATCH (t:ContainerImageTagObservation {image_ref: $image_ref})
WHERE t.first_observed_at > $after_at
   OR (t.first_observed_at = $after_at AND t.uid > $after_uid)
   OR t.first_observed_at IS NULL
RETURN <projection> ORDER BY t.first_observed_at, t.uid LIMIT $limit

-- (C) NullTailCypher (the key is inside the null tail)
MATCH (t:ContainerImageTagObservation {image_ref: $image_ref})
WHERE t.first_observed_at IS NULL AND t.uid > $after_uid
RETURN <projection> ORDER BY t.uid LIMIT $limit
```

They are three statements selected in Go, not one guarded statement. The single
statement they would collapse into needs an empty-string-guarded `OR` disjunct on
a parameter, which `docs/public/reference/nornicdb-query-pitfalls.md` records as
collapsing the whole predicate to zero rows. **That citation was re-measured on
the pinned build rather than taken on faith, and it is narrower than the page
states** — see "Re-measuring the cited defects" below. The case split is kept for
the reasons that survive the measurement: each case binds only the parameters it
needs, and a guarded single statement would make correctness depend on a backend
behaviour that varies by anchor shape on this build and is changing upstream.
Only the parameters the chosen statement names are bound.

`OffsetCypher`, the `SKIP` form, is retained for unscoped and all-scope callers
who page by the `offset` parameter. They still receive a v2 keyset
`next_cursor`, so there is ONE token format on the wire.

### Fixed refill windows

The refill window is `MaxLimit` (200) rows, NOT `limit` rows. This is the half of
the change that shrinks a disclosure rather than relocating one. `MaxRefillReads`
is 4, so the raw span one request scans was `4*limit`: at `limit=1`, a caller
receiving `count: 0, truncated: true` had learned that FOUR specific consecutive
observations belonged to someone else, and walking at `limit=1` mapped withheld
rows at granularity four. A fixed window makes that span a constant 800 raw rows
whatever `limit` is, so the same signal degrades to "at least 800 consecutive
rows here are not yours". It costs a `limit=1` scoped caller the 200-row read a
`limit=200` caller already pays: the already-measured worst case, not a new one.

### What the cursor names

- Page filled inside a window: the last row THE CALLER RECEIVED.
- Cap reached with at least one visible row: the last visible row. The next
  request re-scans the withheld rows after it, which returns no duplicate
  because no visible row sits in that span.
- Cap reached with ZERO visible rows: the last RAW row scanned. See Residue.
- History ended: no cursor, `truncated: false`.

Termination and no-duplicate: each window starts strictly after the last raw row
of the window before it, and (B)/(C) are strict `>` on the total order, so no
window is re-read inside a request and no visible row is returned twice across
requests. A key whose row was since retracted is harmless -- the predicate is on
values, not on that row still existing.

## Residue This Does NOT Close

Disclosed on the OpenAPI operation, the `cursor` parameter, the `next_cursor` and
`grant_filtered` properties, `docs/public/reference/http-api.md`, the MCP tool
description and input schema, the MCP contract matrix row, and
`tagHistoryScopedTruthReason` -- not only here.

1. **The fully-withheld capped page.** When 4 fixed windows (800 raw rows) yield
   ZERO visible rows, the cursor must name the last RAW row scanned or the caller
   re-reads the same 800 rows forever. That row may be withheld, so such a page
   carries one withheld observation's `first_observed_at` and `uid` per 800-row
   fully-withheld span. `uid` is
   `facts.StableID("OCIRegistryCanonicalNode", {kind:"tag_observation",
   repository_id, tag, resolved_digest})` -- a hash, not readable, but a caller
   already holding a candidate digest can confirm it by recomputing the hash: one
   guess per cap event. A forger can therefore still map withheld history at
   granularity 800 rows and one request per 800 withheld rows, versus granularity
   1 before this change.
2. **`count: 0` itself** still says "the next 800 raw rows hold nothing you may
   see". Fixed windows make it 800 regardless of `limit`; nothing short of not
   refilling removes it.
3. **Timing.** A refilled page costs up to 4 tag reads plus 4 `BUILT_FROM`
   lookups, so latency already tells an attentive caller that refill happened.
   Unchanged by this design, and deliberately not on the wire.
4. **Forward-only.** An observation inserted BEHIND the cursor's key between two
   requests is not returned on a later page. That is the
   `RepositoryRefPageCursor` contract and it is stated on the caller-facing
   surfaces. A specific case worth naming: once a walk has entered the null tail
   (which sorts last), a newly projected TIMESTAMPED observation sorts before
   that point and will not be seen by that walk. An offset cursor was worse here
   -- it returned a DUPLICATE under the same insert.
5. **`mutated: true` with a blanked `previous_digest`** -- unchanged, already
   disclosed.

## Theory Probe, Before The Loop Was Written

Per CLAUDE.md's prove-the-theory-first gate, the three statements were run by
hand over Bolt-HTTP against a disposable pinned NornicDB BEFORE any Go code was
written.

- Container: `eshu-nornicdb-pr290:3722b483c02c`, `NORNICDB_EMBEDDING_ENABLED=false`,
  `NORNICDB_NO_AUTH=true`, HTTP `http://127.0.0.1:17966/db/nornic/tx/commit`.
- `CALL dbms.components()` self-reports **NornicDB 1.2.1, community**. (The
  chart's own pin self-reports 1.2.2; this is the image the brief named.)
- Seed: 2 rows with a stored `""`, 4 timestamped rows of which TWO share one
  millisecond, and 3 rows with the property absent -- 9 rows, all three timestamp
  states plus the tiebreak case. A second pass added 400 more timestamped rows
  for the latency comparison.
- Script: `probe6564ks.py` (scratchpad; not committed -- its assertions are now
  the live Go test).

| Check | Result |
| --- | --- |
| Full order under (A) | PASS: `""` rows first, timestamps ascending, absent-property rows LAST |
| Absent property reads back as | `None` (a real null), while `""` reads back as `""` |
| (A) first page, limit 4 | PASS |
| (B) after `("", a01)` | PASS: keeps the sibling `""` row and the null tail |
| (B) across the shared millisecond, after `(…002, b02)` | PASS: returns `b03` and not `b02` |
| (B) after the last timestamped key | PASS: returns exactly the null tail |
| **(B) `OR ... IS NULL` does not collapse the predicate** | PASS: with the disjunct `[b02 b03 b04 c01 c02 c03]`, without it `[b02 b03 b04]` -- it ADDS exactly the tail |
| (C) after `c01` | PASS: `[c02 c03]`; after the last null row, empty |
| (B) at a key ahead of the timestamped history | PASS: only the null tail, no error |
| Keyset resumption vs `SKIP` at the same point | PASS: lands exactly one row past its anchor |

The IS NULL disjunct check is the one the design hinged on, because the
mechanically similar empty-string-guarded form is recorded as broken on this
backend. It was checked by CONTROL -- the same statement with the disjunct
removed -- not by eyeballing a row count, so a collapsed predicate could not have
read as a pass. The advice's mechanical fallback (drop the disjunct from B and
step into C on a short window) was therefore not needed.

One incidental finding, recorded because it cost a debugging cycle: on this build
a string LITERAL containing `//` inside a statement fails the whole statement with
`syntax error: unclosed quote`. The parser treats the double slash as a comment
start and eats the closing quote. Seeding an `oci-registry://...` value must bind
it as a parameter. The production statements are unaffected -- none carries a
string literal.

The container was removed after the work and `docker ps -a` confirmed none
remained.

## Re-measuring The Cited Defects On The Pinned Build

The paragraph above originally asserted that "the pinned build mis-evaluates
that shape". That was a CITATION, not a measurement: the pitfalls page measured
it on `eshu-nornicdb-pr261` (a v1.1.11 base) and the page's own header says none
of its entries has been re-measured on the image we ship, so an entry is "a
reason to check, not evidence that the behaviour is still there -- or that it is
gone". With the NornicDB author closing thirteen Cypher defects and cutting
v1.3.2/v1.3.3, an unverified citation in this direction is worth no more than an
unverified citation in the other. So it was measured, on
`eshu-nornicdb-pr290:3722b483c02c` (NornicDB 1.2.1), every check a CONTROL PAIR
or a control set against a seed whose contents were printed first.

| Shape | Anchor | Result on the pin |
| --- | --- | --- |
| Optional disjunct guarded by `$p <> ''`, `$p` empty | single-node | **correct** -- identical to the same predicate without the guard |
| Same, `$p` non-empty | single-node | **correct** |
| Parameter-comparison disjunct `$p = '' OR ...`, `$p` empty | single-node | **correct** -- returns all rows, which is the right answer |
| Optional disjunct guarded by `$p <> ''`, `$p` **empty** | relationship | **BROKEN -- 0 rows**, correct answer is 1 row |
| Optional disjunct guarded by `$p <> ''`, `$p` **non-empty** | relationship | **BROKEN -- 0 rows**, correct answer is 2 rows |
| Primary disjunct alone, no guarded `OR` | relationship | correct (so the anchor shape is not broken wholesale) |
| Unguarded `OR` disjunct | relationship | correct (so the `OR` is not the trigger) |
| Single-node predicate | relationship | correct |
| Node property vs **parameter**, inequality | relationship | correct |
| Node property vs **another node's** property, equality | relationship | **BROKEN -- 0 rows**, correct answer is 1 row |
| Node property vs **another node's** property, inequality | relationship | **BROKEN -- all rows**, correct answer is 1 row |

Three things follow, and the first two are sharper than what is currently
written down:

1. **The empty-string guard defect is anchor-shape dependent.** It collapses a
   relationship-anchored read and evaluates correctly in the single-node-anchored
   shape. The pitfalls page states it unconditionally.
2. **In the relationship-anchored form it is worse than recorded.** The page says
   it breaks when the guarded parameter is EMPTY. It returns zero rows for a
   non-empty parameter too, so the disjunct is dead regardless of the value.
3. **The cross-node comparison defect is not about `coalesce`.** The memory note
   frames it as "literal-default `coalesce` inequality returns zero rows". The
   trigger is comparing a property of one node to a property of another at all:
   plain `a.name <> b.name` over-returns and plain `a.name = b.name` returns
   nothing, with no `coalesce` anywhere. Comparing against a parameter is fine.

None of this changes a statement in this package, and that is the point of having
measured it. All four tag-history statements are single-node anchored, and every
predicate compares a node property to a parameter or tests `IS NULL`; the one
relationship-anchored statement, `BuiltFromCypher`, filters on
`i.digest IN $digests`, a node property against a parameter. Every shape this
route runs sits inside the region measured correct above, and the live proof
below exercises all of them end to end.

These results belong on `nornicdb-query-pitfalls.md` and in the upstream
defect sweep, not only here.

## Live Proof

`TestTagHistoryKeysetNornicDBLive`
(`go/internal/query/tag_history_nornicdb_live_test.go`), modelled on
`TestCloudResourceOwnerBackfillerNornicDBLive`. It refuses to run against a graph
that already holds observations on its `image_ref`, and it asserts its own
`BUILT_FROM` edge count before reading, so a dropped seed write fails the test
rather than producing a vacuous pass.

```
ESHU_TAG_HISTORY_KEYSET_NORNICDB_LIVE=1 ESHU_NEO4J_URI=bolt://127.0.0.1:17965 \
  go test ./internal/query -run TestTagHistoryKeysetNornicDBLive -count=1 -v
```

Seed: 457 observations on one `image_ref` -- 2 with a stored `""`, 450
timestamped, 2 sharing one millisecond, 3 with the property absent -- plus a
`ContainerImage` per digest and one `BUILT_FROM` edge each to a granted or an
ungranted `Repository`. 229 rows are granted.

| Subtest | Proves | Result |
| --- | --- | --- |
| statements order the three timestamp states | `""` first, timestamps ascending, absent-property rows last, over the whole 457-row order | PASS |
| the IS NULL disjunct does not collapse the predicate | (B) after the last timestamped key returns exactly the null tail | PASS |
| the null tail pages by uid alone | (C) after the first null row returns the rest | PASS |
| a shared millisecond is separated by the uid tiebreak | resuming at the first of the pair returns the second and never re-returns the first | PASS |
| a keyset walk reaches every row exactly once across pages | the concatenated walk equals the seeded order | PASS |
| RefillScopedPage returns exactly the granted rows | the whole route, `BuiltFromCypher` with its `DISTINCT` included | PASS: **229 granted rows over 3 pages of a 457-row history** |

Total 6.17 s including the seed. This also discharges the `DISTINCT` live
re-validation the previous commit recorded as owed.

## Mutation Proof

Each mutation was applied with `go test -overlay` against a copy OUTSIDE the
worktree, so no production file was edited and `git status` stayed clean.

| Mutation | Test that must catch it | Result |
| --- | --- | --- |
| M1: drop `OR t.first_observed_at IS NULL` from `AfterKeyCypher` | `TestTagHistoryKeysetPagesTheNullTail`; live IS NULL, walk and scoped subtests | **SURVIVED, then KILLED** -- see below |
| M2: use `limit` instead of `MaxLimit` as the refill window size | `TestTagHistoryRefillWindowIsFixedNotLimitSized` | KILLED |
| M3: drop the `lastRaw` fallback on a cap-reached empty page | `TestTagHistoryCapReachedEmptyPageAdvances` | KILLED |
| M4: always report `NullAt: false` in `windowRowFromGraph` | `TestTagHistoryKeysetPagesTheNullTail` | KILLED |

**M1 survived the first time, and that was a real coverage gap worth recording.**
`seededTagHistoryGraph` evaluates the statement's predicate, which is what makes
these tests sensitive to the keyset design instead of to a hard-coded index -- but
its after-key branch added the null tail UNCONDITIONALLY rather than reading the
disjunct off the statement text. So the double kept returning rows the mutated
production statement no longer asked for, and every unit test passed. The double
now decides from the text
(`strings.Contains(cypher, "OR t.first_observed_at IS NULL")`), the same way
`TestTagHistoryDistinctBoundsBuiltFromFanOut` reads `RETURN DISTINCT`, and M1 is
KILLED.

The LIVE test kills M1 independently of any double, and shows what the disjunct
is actually worth: `the IS NULL disjunct does not collapse the predicate` FAILS
with `got []` where the null tail was expected, the keyset walk reaches **454 of
457** rows, and `RefillScopedPage` returns **228 of 229** granted rows. Dropping
that disjunct is silent history loss, not a stylistic change.

Exact commands and outputs are in the handoff report.

## Performance

Stage: the `GET /api/v0/images/tag-history` read path. Metric boundary: one
statement's Bolt-HTTP round trip, start at request write and terminal at the last
row read, with the returned row count recorded so an empty result cannot read as
a fast one. Correctness invariant proven first (Live Proof above); this section
is a no-regression check on an unchanged contract, not an optimisation claim.

No-Regression Evidence: the three keyset statements were measured against the
`SKIP` form they replace on the pinned `eshu-nornicdb-pr290:3722b483c02c`
(self-reports NornicDB 1.2.1), same store, same seed of 409 observations on one
`image_ref`, same 201-row result set from each, best of 5 round trips: (A) first
page 1.27 ms, `SKIP 200 LIMIT 201` 1.26 ms, (B) keyset at that same point
0.71 ms, (C) null tail 0.5 ms. Same single anchoring `MATCH` on the same
`container_image_tag_observation_ref` index, same `limit+1` sentinel, same
registered `max_results` of 201, and the `BUILT_FROM` lookup and its enforced
bounds are unchanged. The one behaviour change that carries a cost is the FIXED
`MaxLimit` refill window: a `limit=1` scoped caller now pays the 200-row read plus
up-to-400-key lookup a `limit=200` caller already paid, which is the worst case
already measured in
[6564-tag-history-grant-binding.md](6564-tag-history-grant-binding.md) (1135-1175
ms cold for 400 missing keys at 5k images, 2247 ms at 10k, bounded at 4 windows),
not a new one.

Corpus-scale timing is PENDING, and is not claimed here. The owner rule is that
measurable performance proof runs on the remote instance, which was unreachable
for this change; the numbers above are a single-container probe on a contended
laptop and are a no-regression check at 409 rows only, not a repo-scale claim.
What to run on the remote, against a full-corpus store with the graph schema
already bootstrapped:

1. `ESHU_TAG_HISTORY_KEYSET_NORNICDB_LIVE=1 ESHU_NEO4J_URI=bolt://<remote>:7687
   go test ./internal/query -run TestTagHistoryKeysetNornicDBLive -count=1 -v`
   (correctness on real hardware).
2. The four statements timed by hand at `limit=201` on the largest real
   `image_ref` history, comparing (A)/(B)/(C) against `OffsetCypher` at an
   equivalent depth, best of 5 with the row count recorded for each.
3. `GET /api/v0/images/tag-history?...&limit=1` as a scoped token on that same
   `image_ref`, to confirm the fixed-window cost stays inside the handler
   histogram's 5 s top bucket.

Report exact seconds plus a human duration, and label the result non-comparable
if the corpus, profile, topology, or storage state differs from the baseline it
is compared against.

Observability Evidence: unchanged and deliberately so. The span
`query.container_image_tag_history` still carries
`eshu.query.tag_history.refill_reads` and
`eshu.query.tag_history.refill_read_cap_reached` -- the two an operator needs at
3 AM to tell "this caller's grant covers a thin slice of a busy tag" from "the
route is slow" -- alongside the four grant counts, and
`eshu_dp_query_container_image_tag_history_*` keeps its closed `disposition`
label set. `refill_reads` now means fixed `MaxLimit`-sized windows rather than
`limit`-sized ones, which is recorded on `annotateTagHistoryRefill`. No attribute
or label gained a digest, a repository id, a row position, or a withheld count,
so the keyset change discloses nothing new through telemetry.
