# Evidence: Neo4j parser interval race retry (#8190)

Refs #8190.

## What changed

`RetryingExecutor` retries one more failure class, reason
`neo4j_parser_interval_race`, in both `classifyRetryableGraphWriteError` and
`classifyRetryableGraphWriteGroupError`. The code is in
`retrying_executor_parser_race.go`. The `whereClausePattern` helpers moved
unchanged to `retrying_executor_where_bounds.go` to keep
`retrying_executor.go` under the file cap.

This is a vendor-bug workaround. Remove it once the pinned Neo4j image bundles
an ANTLR runtime that contains antlr/antlr4#4902.

## Why

The pinned Neo4j image (2026.08.1) bundles ANTLR 4.13.2. `Interval.of` keeps an
unsynchronized static cache for one-character intervals at character index
<= 1000 (antlr/antlr4#4901). On a weakly ordered CPU (aarch64), two parser
threads that first parse the same cold statement can read a half-built
`Interval`. The token text then comes back as character 0 of the query or a
prefix of it, and the server reports a false ``Variable `U` not defined`` at the
correct position of a one-character variable such as `f`. The Eshu statement
text is correct. The diagnosis (stock image 87 failures in 345,600 cold
same-text statements, a patched-jar control 0 in 230,400) is in the #8190
report. Upstream, antlr/antlr4#4902 merged 2025-11-19 and #4903 merged
2025-12-05; no ANTLR release has shipped since 4.13.2.

## Contract

Retry only when all hold:

- `Neo4jError` code is `Neo.ClientError.Statement.SyntaxError`.
- The message header is ``Variable `X` not defined (line L, column C (offset: O))``.
- `O` is at most 1000, the cache bound.
- One statement has a one-character identifier at `O` on a token boundary, and
  `L` and `C` agree with `O`.
- `X` is a prefix of that statement's text (the corrupted read, including the
  empty name) or equals the identifier (the defining-site read).

Statement shape is not checked. The error is raised while a statement is
parsed, before it runs, and every group that reaches `RetryingExecutor` runs as
one managed transaction (`session.ExecuteWrite`). The driver rolls the whole
transaction back, so nothing was applied and a replay cannot double-apply.
Replay safety rests on that atomicity, not on `AllStatementsAreReplaySafe`. The
group that failed in #8190 is the non-first-generation `CanonicalNodeWriter`
group: it is the only path that emits the two CreateMissing File templates, and
it carries `generation_id <> $generation_id` retracts that the shape check
rejects. A shape gate would leave that group terminal. The class stays bound to
the exact header and code above, never to a commit-time error shape.

The four Neo4j wirings run each group as one `ExecuteWrite`: bootstrap-index
(`cmd/bootstrap-index/wiring.go`), projector (`cmd/projector/neo4j_executor.go`),
ingester (`cmd/ingester/wiring_neo4j_executor.go`), and reducer
(`cmd/reducer/neo4j_wiring.go` through `executor_adapters.go`). A single
`Execute` is one autocommit statement, which is atomic too.

A real undefined one-character variable cannot be told apart from the
defining-site read. It costs at most `MaxRetries` retries (default 3, each
counted and WARN-logged) and then surfaces as itself. After the budget,
`runWithRetry` returns the original SyntaxError wrapped with `%w`
(`neo4j syntax error persisted after N retries: ...`). That error has no
`Retryable()` and no `FailureClass()`, so it is terminal: it is not recorded as
`graph_write_timeout` and feeds no producer write-timeout backpressure. Every
other non-matching shape (a multi-character name, an offset not on a
one-character identifier, an unrelated name, another code, a non-`Neo4jError`)
stays terminal on the first attempt.

## Where it applies

`RetryingExecutor` wraps the Neo4j write path in the projector
(`cmd/projector`), bootstrap-index (`cmd/bootstrap-index`), the ingester
(`cmd/ingester`), and the reducer (`cmd/reducer`, default `MaxRetries` of 3).

## No-Regression

No-Regression Evidence: the change adds no query, schema, index, batch size, or
worker setting. The only added hot-path work is on the error path of a write
that already failed: one regexp match on the Neo4j error message, and only when
its code is `Statement.SyntaxError`. The success path is unchanged.

Concurrency proof: the retry replays a whole `ExecuteWrite` transaction from
scratch, as the existing transient retry does. The failed transaction rolls
back, so the replay cannot double-apply. The live run below checks that with
graph counts after every round.

Live proof on the pinned image
`neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f`
(Neo4j 2026.08.1, image architecture arm64, host aarch64), `NEO4J_AUTH=none`, a
fresh container for every round so the JVM is cold.

- Subject: the real non-first-generation main group captured from
  `CanonicalNodeWriter.Write` with a recording executor: 119 statements, with
  the CreateMissing File templates and the `<>` retracts, so not replay-safe by
  shape. Each of 96 goroutines gets its own repository (2 directories, 6 files,
  3 functions), so the statement text is identical and the rows are disjoint.
- Executor: `RetryingExecutor{MaxRetries: 3}` over a raw Bolt executor that runs
  the group in one `session.ExecuteWrite`, like the production executors.
- Load: 96 pre-opened sessions start together, and on the first attempt each
  statement waits on a same-text barrier, so all 96 parse every cold statement
  text at the same moment.
- Counts after each round: File, Directory, Repository, Function nodes,
  `REPO_CONTAINS` edges to File, and duplicate File paths.

| Round | Statements | Raw race errors | Retries (`neo4j_parser_interval_race`) | WARN | Surfaced | Graph counts |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | 11,424 | 1 | 1 | 1 | 0 | equal to input |
| 2 | 11,424 | 1 | 1 | 1 | 0 | equal to input |
| 3 | 11,424 | 2 | 2 | 2 | 0 | equal to input |
| 4 | 11,424 | 2 | 2 | 2 | 0 | equal to input |
| 5 | 11,424 | 2 | 2 | 2 | 0 | equal to input |
| 6 | 11,424 | 3 | 3 | 3 | 0 | equal to input |
| 7 | 11,424 | 1 | 1 | 1 | 0 | equal to input |
| 8 | 11,424 | 2 | 2 | 2 | 0 | equal to input |
| 9 | 11,424 | 0 | 0 | 0 | 0 | equal to input |
| 10 | 11,424 | 0 | 0 | 0 | 0 | equal to input |
| Total | 114,240 | 14 | 14 | 14 | 0 | 576 File, 192 Directory, 96 Repository, 288 Function, 576 `REPO_CONTAINS`, 0 duplicate paths per round |

RED side: the same harness and the same subject, built with the replay-safe
gate put back into the classifier (the round-1 shape). The classifier then
returns no reason for the real group, so every race error surfaces.

| Round | Raw race errors | Retries | Surfaced | File nodes |
| --- | --- | --- | --- | --- |
| 1 | 3 | 0 | 3 | 558 (93 committed groups) |
| 2 | 0 | 0 | 0 | 576 |
| 3 | 1 | 0 | 1 | 570 (95) |
| 4 | 4 | 0 | 4 | 552 (92) |
| 5 | 0 | 0 | 0 | 576 |
| 6 | 1 | 0 | 1 | 570 (95) |
| Total | 9 | 0 | 9 | |

The surfaced errors were ``Variable `M` not defined`` at offsets 23, 24, 29,
63, and 159: character 0 of `MATCH` statements in the group, not only the File
templates. In the RED rounds the counts equal the committed groups exactly, so
a failed group left no partial writes. The harness is a throwaway test and is
not committed; it needs a live Neo4j and is not part of any gate.

NOT_CHECKED: x86_64 hosts and CI (`ubuntu-latest` x86_64), where TSO hardware
makes the race unlikely.

## Observability

Observability Evidence: the retry reuses
`eshu_dp_neo4j_deadlock_retries_total{write_phase,reason}` with the new bounded
`reason=neo4j_parser_interval_race`, so the reason set is now
`connectivity_error`, `transient_error`, `write_conflict`,
`commit_unique_conflict`, and `neo4j_parser_interval_race`. No metric name or
label key is added. Each retry also logs a WARN, `neo4j parser interval race
(antlr4#4901) reported a false undefined variable, retrying`, with
`write_phase`, `attempt`, `offset`, `reported_name` (cut to 32 characters),
`reported_name_length`, and `statement_summary`. The log carries no row values.
The reported name is the corrupted token text, which can be a prefix of the
fixed statement template, so up to 32 characters of that template can appear.
An operator sees the counter rise with this reason and finds the paired WARN.
Retry exhaustion surfaces the original SyntaxError as a terminal error.

The upstream fix is a Neo4j image that uses `new Interval(...)` in
`ThinCypherToken.getText` or an ANTLR release with antlr/antlr4#4902. Bump the
pinned image digest then and remove this retry class.
