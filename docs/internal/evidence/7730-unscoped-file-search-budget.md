# Unscoped file search: budgeted key-ordered walk (#7730)

## Scope

`POST /api/v0/content/files/search` and the MCP tool `search_file_content`
with no repository filter. The entity twin, the scoped and multi-repository
pages, and the oversized-document policy are out of scope. This note records the
measurements behind the design and the proof of the change. The design and the
arbiter rulings are not in the repository. The V4c figures are in the V4c
comment on issue #7730 (issuecomment-6073885986); the transcripts behind them
are private. This change did not re-run them.

Performance Evidence: the old single statement, `content ILIKE '%x%' ORDER BY
repo_id, relative_path LIMIT/OFFSET`, costs the byte mass of the trigram
candidate set and flips plan with each `ANALYZE` (the planner prices the
pattern from a histogram of values of at most 1 KB, so one pattern is a 9 ms
ordered walk after one refresh and a 3 s trigram bitmap after the next). The
new walk bounds the work it issues and does not depend on the planner's choice.
It is not faster for every class: on the real corpus it is slower for the
selective and zero-match classes and much faster for the common-token classes.

V4c reader run (a deployed read replica, read-only, SQL `Execution Time` sums, one
run for old and new, figures rounded as in the issue comment; budget B = 800 ms;
old is the single statement in the planner's natural regime, warm with the
first run (cold) in parentheses):

| class | old warm (cold) | new | outcome | bar |
| --- | --- | --- | --- | --- |
| C2 selective literal, 3 matches | 334 ms (417 ms) | 411 ms | exact | 484 ms |
| C4 zero match | 0.5 ms (3.8 ms) | 72 ms | exact, 0 rows | 200 ms |
| C3p medium token, 348 small matching files | 2,640 ms (3,001 ms) | 40.5 ms | exact, pages 1 to 3 equal the old statement | 500 ms |
| C5b dense token, 5,809 matches | 5,603 ms | 32.8 ms | exact | 300 ms |
| C3 rare token, first match deep in key order | 1,921 ms (2,061 ms) | 528 ms | explicit partial, 0 rows, tail cancelled, overrun reported | partial within budget |
| C3pf medium token started 5,184 rows before its next match | 2,679 ms (2,765 ms) | 515 ms | explicit partial, tail cancelled, overrun reported | partial within budget |

Read the table plainly:

- C4: the old statement answered a zero-match pattern in 0.5 ms warm and 3.8 ms
  cold; the walk takes 72 ms, about 130 times slower warm. It stays inside
  its 200 ms bar. The same holds for C2 (334 to 411 ms, inside 484 ms).
- C3pf is the cap-edge class (the cursor sits 5,184 rows before the next
  match): the exact answer was not reachable inside B = 800 ms, so the walk
  answers partial with a cursor and reports the overrun of the cancelled tail
  (595.5 ms in the V4c run) instead of running on.
- C3 answers partial with no rows and a reported overrun of 539.1 ms in the
  V4c run: the cancelled tail ran past its timeout by one candidate's
  uninterruptible recheck. The call exceeds the budget by that reported amount.
- The 3 s and longer cost of the old statement for common tokens is the
  measured column above. Its worst case when the planner takes the ordered walk
  for a rare token is a full-table walk, computed (not measured) as 146,844 rows
  x 0.36 ms, about 53 s.
- Figures from the earlier S1 run (old C2 333.8 ms, old C3 2,613.6 ms) are a
  different run and are not mixed into this table.

Local fixture proof (disposable PostgreSQL 18.6, 12,000 synthetic files in 60
repositories, half under 1 KB, one in 20 about 26 KB; warm; two runs of
`TestSearchFilesUnscopedMatchesOldStatementLive`, page 1 of 10):

| class | old | new, run 1 | new, run 2 |
| --- | --- | --- | --- |
| dense | 49.4 ms | 1.7 ms | 21.1 ms |
| wildcard (`han_ler`) | 51.0 ms | 1.6 ms | 1.1 ms |
| medium | 3.0 ms | 4.8 ms | 5.6 ms |
| upper case of medium | 3.0 ms | 4.6 ms | 4.2 ms |
| selective | 1.0 ms | 91.4 ms | 77.5 ms |
| zero match | 0.4 ms | 113.7 ms | 102.7 ms |

Read the last two rows plainly: at this small scale the old bitmap answers a
selective or zero-match pattern in about a millisecond, and the walk spends its
continuation cap (0.15 x budget at the time; 0.55 since 2026-10-11, see the
last section) before the tail runs, so it is slower here.
On the measured corpus the same two classes stayed inside their bars (table
above). The walk trades a bounded constant for removing the multi-second tail
of the old statement; it does not make every class faster. Pages 1, 2 and 3 of
the walk equal the old statement's `OFFSET 0 / 10 / 20` pages for every class,
including ILIKE wildcards and case folding, and `truncated` agrees with the old
look-ahead row (`TestSearchFilesUnscopedMatchesOldStatementLive`, which runs
under a large budget so the probe and the continuation answer it). The oracle is
the shipped tail statement plus only the old OFFSET; the hermetic
`TestOracleStatementIsDerivedFromShippedTailText` pins that derivation.

A page the trigram tail fills (`TestSearchFilesUnscopedTailFilledPageKeepsMoreLive`,
limits 2 and 1 under a virtual clock) returns the old statement's rows and its
look-ahead flag: the tail is asked for the look-ahead row, so a tail that
returns every row it was asked for means a further match exists. The unit
`TestSearchTailFilledPageReportsMore` pins the same for a tail that returns
exactly the rows it was asked for. The two file proofs that need a chosen phase
(`TestSearchFilesUnscopedTailFilledPageKeepsMoreLive` and
`TestSearchFilesUnscopedEdgeRowsAreExactLive`) inject a virtual clock through
`Searcher.Now` and charge a fixed virtual cost to each window and tail
statement (#8141), so the phases that run depend on the fixture and a short
list of virtual costs, never on host speed; the SQL, the rows and the span
attributes stay real. At every cost a complete walk must return the old
statement's rows (and, where the test pins it, its look-ahead flag) and a
cut-short walk must return an ordered prefix of them; the wanted phases (the
tail, or two continuation steps and the tail) are required only at the cost
where the span shows them, and the test fails if no cost reaches them. An answer with exactly the requested number of matches is complete and
not truncated, and one more match makes it truncated
(`TestSearchFilesUnscopedMatchesOldStatementLive` pages of 3 and 2 for the
three-match selective token, and `TestSearchExactlyFullAnswerIsComplete`).

Plan shape (`TestUnscopedSearchPlanShapesLive`): the key-ordered window plans
as an index walk on `content_files_pkey` with no trigram node; the tail under
`SET LOCAL enable_indexscan = off` plans as `Bitmap Index Scan on
content_files_content_trgm_idx` with no sequential scan and no primary-key walk.
The test first proves the planner's natural plan for the medium token is the
primary-key walk (the plan lottery the setting removes) and fails when the
setting is dropped.

Cancel and resume (`TestSearchFilesUnscopedCancelledTailResumesToExactAnswerLive`):
a token with no trigram makes the tail recheck every row; at the smallest budget
the server cancels it, the savepoint keeps the transaction usable, and resuming
from each returned cursor gathers exactly the old statement's rows, no gap and
no duplicate.

Unit proof (`go/internal/query/search/unscoped`, fake clock and a fake that
aborts the transaction on a cancel until `ROLLBACK TO`): phase order and
timeouts, tail floor, exhaustion at the window boundary, offset served by
`offset + limit + 1`, cursor resume with and without an offset, overrun and the
large-document reason, scaling with the budget, exec mode on every bounded
statement. Mutation checks: removing the cursor advance fails two tests; removing the
tail-cancel counter fails the metrics test. The fake reads the keyset operator
and the edge OFFSET from the statement text, and hermetic tests pin the
shipped predicates (`TestShippedStatementsCarryStrictKeysetAndLastRowEdge`).
Three SQL mutants each fail a named unit test and a named live test: an
inclusive window keyset (`>=` in both window predicates), an inclusive tail
keyset, and an edge OFFSET past the window (`OFFSET ($2::bigint)`). The live
fixture plants the edge token on every window edge row and the row after it,
and `TestSearchFilesUnscopedEdgeRowsAreExactLive` needs the probe, at least two
continuation steps and a completed tail to return the old statement's rows.

Observability Evidence: span `postgres.query` (`db.operation=
search_file_content_any_repo_page`) carries `search.scope`, `search.budget_ms`,
`search.elapsed_ms`, `search.overrun_ms`, `search.probe_rows_visited`,
`search.continuation_steps`, `search.continuation_rows`, `search.tail_ran`,
`search.tail_cancelled`, `search.outcome` and `search.cursor_present`. Metrics:
`eshu_dp_content_search_unscoped_total{outcome}`,
`eshu_dp_content_search_tail_cancel_total`,
`eshu_dp_content_search_unscoped_duration_seconds{outcome}` and
`eshu_dp_content_search_unscoped_overrun_seconds`. One
`content_search.unscoped_partial` log line per partial result with the reason,
elapsed, overrun, rows scanned and matched. Tests assert that no span attribute
and no log line carries the search pattern. At 3 AM an operator reads the
`partial` share of the counter, the overrun histogram for the large-document
effect, and the `reason` on the log line.

## Concurrency and cost shape

One call is one read-only snapshot transaction on one pooled connection, so the
read pool sees the same connection use as before for a short call and at most
the budget plus one reported overrun for a long one. The walk takes no lock
beyond the snapshot, writes nothing, and never retries a statement. The
standby cancels a long conflicting transaction at about 30 s; the maximum
accepted budget is 10 s.

## NOT_CHECKED

- The V4c reader run is cited, not repeated here. A deployed re-sweep on a
  pinned image with two planner-estimate regimes recorded is the next proof.
- Candidate-size distribution per statement and the largest document's
  candidacy (arbiter rulings 4 and 5).
- Corpus-scale plan shape of every class on the tail (the fixture proves the
  selective and medium classes; the reader run proved the rest).
- The deployed end-to-end envelope overhead against the one-second bar.

## Limits recorded, not fixed here

- The hand-written live fixture schema has the primary key and the trigram
  index only; the production schema also has the repo-path indexes. The plan
  shape on the production schema is covered by the V4c reader run.
- The console maps an unknown `truth.level` (now including `partial`) to its
  exact chip. The console does not call file search today; the truth
  vocabulary is no longer closed, so a console label for `partial` is a
  follow-up.
- `search.continuation_rows` adds the full step size for each completed step,
  including a short final step; and the control statements (SAVEPOINT, RELEASE,
  set_config, RESET) go through the same transaction, so the fenced reader
  counts them as queries. Neither changes a decision.

## 2026-10-11 re-read and the 0.55 cap

A read of the QA environment on 2026-10-11 (pinned image, MCP tool, `limit`
10, no repository filter) found 9 of 17 tokens partial. Common tokens such as
`kubernetes`, `grafana` and `helm` returned 0 rows at 1.08-1.14 s after
scanning only 1,700-3,200 rows in key order: the 0.15 cap (120 ms) stopped the
key-ordered walk, and the trigram tail then met one 54.9 MB document whose
recheck cannot be cancelled for about 0.7 s. The read-only continuation shim
of the same day (`7730-continuation-shim-20261011.md`, kept with the arbiter
rulings outside the repository) measured each token's 11th match at key
positions 5,675-7,177, one key-ordered window to it at 289-409 ms, and
replayed the shipped walk methods against those costs (a model validated
against the observed A walk within about 5 %).

The change: the continuation before the tail is capped at 0.55 x budget
(440 ms at 800 ms). The tail cap (0.5 x budget), the tail floor and the phase
order are unchanged. The arbiter rejected "skip the tail once a match was
seen" (faster for kafka-like tokens) because it turns a token with an early
match, few matches and a cheap tail from exact to partial.

Per-class figures. A is the shipped 0.15 cap, B is the 0.55 cap. Each column
has one source: "deployed read" is the 10-11 tool read; "model" is the
QA-calibrated model of the shim (elapsed is the walk's own, min-max over 8
scenarios). X = exact or exhausted, P = partial, the number is rows.

| class | A, deployed read (10-11) | A, model | B 0.55, model | change |
| --- | --- | --- | --- | --- |
| baseline pattern | X10, about 11 ms server | X10, 10-14 ms | X10, 10-14 ms | none |
| kubernetes, grafana, helm | P0, 1.08-1.14 s client | P0, 982-1,075 ms | X10, 312-348 ms | partial to exact |
| prometheus | P0 | P0, 982-1,075 ms | X10, 320-352 ms | partial to exact |
| postgres | P5 | P4-5, 982-1,075 ms | X10, 421-459 ms | partial to exact |
| dockerfile | P5 | P4-5, 982-1,075 ms | X10, 322-355 ms | partial to exact |
| argocd | X10, 0.823 s client | X10, 719-781 ms | X10, 312-341 ms | faster |
| redis | X10, 0.636 s client | X10, 545-596 ms | X10, 138-156 ms | faster |
| kafka | P1 | P1, 936-1,069 ms | P2, 1,232-1,386 ms | **one more row, slower, over the 1 s bar** |
| xyzzy | P0, 766-773 ms server | P0, 776-782 ms | P0, 804-808 ms | same answer, about 25 ms slower |
| zero-match literal | X0, 0.22 s (the 10-11 literal) | X0, 217-416 ms | X0, 513-733 ms | **slower, still exact** |
| selective (10-08 class, tail 300 ms) | not read on 10-11 | X3, 427-448 ms | X3, 723-765 ms | **slower, still exact** |
| synthetic late cluster | not read | P0, 982-1,075 ms | P0, 1,278-1,392 ms | **same rows, slower** |
| synthetic early-match selective | not read | X3, 427-448 ms | X3, 723-765 ms | **slower, still exact** |

The classes that get slower are in bold. B 0.55 is not deployed yet; its
figures are the model, not a run. Closure of #7730 rests on the deployed
re-read after the image roll.

Unit proof (`TestPolicyClasses`, fake clock, synthetic sizes shaped like the
measured profile, no QA data): one row per class above plus the kafka-like
class without the large document (rule 4: the tail must still run and finish)
and the xyzzy and zero-match tails. At the shipped 0.15 cap the late-cluster
and spread rows fail (partial 0 and partial 5 rows), as do the kafka-like rows
(1 row instead of 2). Two seeded violations each fail the test and were
reverted: a 0.65 cap turns the selective and early-match-selective classes
partial, and a one-line "skip the tail when a match was seen" turns the
early-match-selective class partial and stops the kafka-like rows running the
tail.

Telemetry added: span attributes `search.phase_one_cap_ms`, `search.ended_in`,
`search.steps_cancelled`, `search.progressed`; counters
`eshu_dp_content_search_step_cancel_total` and
`eshu_dp_content_search_no_progress_total`; and `progressed`,
`steps_cancelled`, `ended_in` on the partial log line. No signal carries the
pattern (`TestSearchRecordsMetricsAndPartialLogWithoutPattern`,
`TestSearchCountsCancelledStepsAndNoProgress`).

Limit recorded, not fixed here: the 54.9 MB document keeps kafka-like tokens
over the bar under every continuation policy. Issue #7975 tracks it. A size
predicate cannot fix it inside the search statements, because the bitmap heap
scan rechecks the trigram condition before any other condition.

### Window timeouts from the phase room (decision 3.2)

Before this change the probe and each continuation step ran under a flat
300 ms statement timeout (0.375 x budget). A window that holds one large
document costs more than that, and the recheck cannot be interrupted, so the
cancel landed only after the window was paid for and then discarded it. A
resume chain for a token whose tail cannot finish then reached a dead end: the
next call's probe met the same document and was cancelled, no step ran, the
tail was cancelled by the same document, and the answer was
`progressed: false`.

Read-only probe of the QA environment's read replica, 2026-10-11
19:23:31-19:23:34Z, approved as six sequential statements, each in BEGIN READ
ONLY / ROLLBACK: two key-only cursor lookups, then the shipped step statement
(500-row window, zero-match 12-character literal so every row is rechecked,
values bound as literals) over the window after each cursor. Cursor literals
stayed in psql variables and were never printed. One sample each.

| statement | window (key ordinals) | timeout | wall | result |
| --- | --- | --- | --- | --- |
| cursor lookup A | ordinal 31,700 | 15 s | 98.0 ms | key read |
| cursor lookup B | ordinal 146,700 | 15 s | 103.2 ms | key read |
| window A | 31,701-32,200, holds the 33.2 MB document (31,838) | 300 ms | 524.0 ms | cancelled, 224 ms past the timeout |
| window A | same | 800 ms | 468.5 ms | completed |
| window B | 146,701-147,200, holds the 54.9 MB document (146,842) | 300 ms | 781.8 ms | cancelled, 482 ms past the timeout |
| window B | same | 800 ms | 723.4 ms | completed |

Read the table plainly: both windows exceed the old 300 ms timeout and both
complete inside 800 ms. The 33.2 MB window costs 0.47-0.52 s, more than the
0.27-0.37 s the ruling estimated from the per-document recheck; each window
ran at 300 ms first, so the 800 ms run may be the warmer one. The ordinals are
those of the 10-11 census; ingest may have moved them by a few rows, and the
walls show the large document is in each window.

The change: the probe's timeout is the remaining budget and a step's is the
room left in its phase (`min(phase allowance - used, remaining)`). No
statement text and no cursor rule changes (only a completed statement advances
the cursor). The rows one call returns can change for one corpus shape: a probe
window whose cost is spread over its rows and exceeds budget minus the tail's
cost now completes and advances the cursor 200 rows, and the tail runs with
less room; where the flat 300 ms cancel was cheap and the tail then completed,
the call is now partial and resumable (fake clock: 600 ms window, 300 ms tail:
exhausted 3 rows at 603 ms before, partial 0 rows progressed at 803 ms now). A
region of such windows costs one call per 200 rows (2,000 rows: 11 calls
instead of 1). The same shape with a tail above the tail cap was a dead end
before (progressed=false) and is resumable now. The census of 2026-10-11 holds
no such window: the heaviest 200-row window in key rows 1-25,000 is 11.4 MB,
about 92 ms. `TestSpreadProbeWindowTrade` pins the three rows.

At the default budget a window that holds the 33.2 MB
document completes in a probe (800 ms of room) but not in a continuation step
(at most 440 ms of room): the step is cancelled after the recheck, each call's
probe still advances the cursor 200 rows, and the first probe whose window
holds the document pays for it. The dead end now needs a window that costs
more than the whole budget; the 2026-10-11 census holds none (the costliest,
0.72-0.78 s, is under 800 ms with about 50 ms to spare, so a slower replica
could still reach it). For a window whose cost is spread over its rows this is
a new dead end when its tail would have completed under the flat timeout (fake
clock: 900 ms window, 300 ms tail: exhausted before, progressed=false now); no
measured window is near it.

Unit proof (fake clock): `TestResumeCrossesWindowWithLargeRow` resumes from a
cursor just before a 500-row window with one 370 ms row (the ruling's figure)
and with one 470 ms row (the measured window A); with the flat timeout both
chains stop at call 2 with `progressed: false`, and with the room timeouts the
first completes in one call and the second in two.
`TestProbeWindowWithLargeRowProgresses` puts one 700 ms row in the probe
window: with the flat timeout the probe is cancelled and the call does not
progress; with the room timeout the probe completes. Live proof
(`TestResumeChainCrossesLargeDocumentLive`, local PostgreSQL 18.6, a planted
16 MB document whose window costs about 47-52 ms warm, budget twice that): a
resume chain from the first row crosses the document in 3-4 calls with every
call progressed and returns the old statement's rows; the same test against
the flat-timeout code stops at call 3 with `progressed: false`. The budget is
derived from the measured window cost so the ratios hold on any host; the
ruling's 400 ms figure assumed the QA environment's recheck cost (about
13 ms/MB), and the laptop rechecks about 3 ms/MB.

After this change the after-tail continuation issues no statement at the
default ratios, at any budget. Let B be the budget, P the probe wall, U the
continuation spend before the tail, e the predicted next step, c = phase-one
cap (0.55 B) and t = tail cap (0.5 B). The continuation stops on one of two
conditions. (i) `e > min(c - U, B - P - U)`: the tail then runs with timeout
`min(B - P - U, t)`; if `B - P - U <= t` a cancelled tail leaves nothing, and
otherwise the room after it is `B - P - U - t < min(c - U, B - P - U) < e`
(because c >= t and P < B - t), so no step fits. (ii) A step is cancelled at
its room, so `U >= c - 1 ms` (the timeout is set in whole milliseconds); then
`B - P - U <= B - c + 1 ms`, under t by the margin `c - t = 0.05 B` (5 ms at
the 100 ms minimum budget), and the tail takes all that is left. The phase
stays in the order for other ratios.

### Evidence markers for the 0.55 cap and the window timeouts

Performance Evidence: baseline is the shipped walk (0.15 cap, flat 300 ms
window timeouts) as read on the QA environment on 2026-10-11 (PostgreSQL 18.3
read replica, 147,069 content rows, MCP tool, `limit` 10): 9 of 17 tokens
partial, common tokens 0 rows at 1.08-1.14 s. After: the QA-calibrated model of
the 0.55 cap (validated against the observed baseline within about 5 %) gives
the per-class table above; it is a model, not a deployed run, and the deployed
re-read after the image roll is the closure evidence. The window timeouts are
backed by the six-statement replica probe above (both large-document windows
cancelled at 300 ms, completed at 800 ms) and by the fake-clock and local
PostgreSQL 18.6 RED/GREEN tests. The change adds no statement text and changes
no cursor rule; it changes which rows one call returns for the spread-window
shape above. It moves where the budget is spent, and a call still ends within
its budget plus one reported overrun. One call stays one read-only snapshot on
one pooled connection, so the pool sees the same load shape.

Observability Evidence: `search.ended_in`, `search.steps_cancelled`,
`search.progressed` and `search.phase_one_cap_ms` on the search span;
`eshu_dp_content_search_step_cancel_total` and
`eshu_dp_content_search_no_progress_total`; `progressed`, `steps_cancelled`
and `ended_in` on the partial log line. The spread-window trade shows as
`search.ended_in=tail` with `search.tail_cancelled` and `search.progressed=true`,
or `ended_in=budget`, and its dead end raises
`eshu_dp_content_search_no_progress_total`, the 3 AM signal. After the roll, a
rising `no_progress_total` means a dead end is back, and `step_cancel_total` beside
the tail-cancel counter shows whether large windows or the tail stop walks.
