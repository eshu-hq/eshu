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
continuation cap (0.15 x budget) before the tail runs, so it is slower here.
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
limits 2 and 1 under a small budget) returns the old statement's rows and its
look-ahead flag: the tail is asked for the look-ahead row, so a tail that
returns every row it was asked for means a further match exists. The unit
`TestSearchTailFilledPageReportsMore` pins the same for a tail that returns
exactly the rows it was asked for. Which phases run depends on host speed, so
the live tests try a series of budgets. At every budget a complete walk must
return the old statement's rows (and, where the test pins it, its look-ahead
flag) and a cut-short walk must return an ordered prefix of them; the wanted
phases (the tail, or two continuation steps and the tail) are required only at
the budget where the span shows them, and the test fails if no budget reaches
them. An answer with exactly the requested number of matches is complete and
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
