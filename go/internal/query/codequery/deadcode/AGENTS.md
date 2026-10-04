# AGENTS.md — `go/internal/query/codequery/deadcode`

Scope: dead-code analysis for the code-family queries, split out of
`codequery` (#6060 lane A). Agents working here MUST read the package
[README](README.md) and [doc.go](doc.go) first, plus the parent
[codequery AGENTS.md](../AGENTS.md) for the family-wide digest and
export disciplines.

## Ownership

- This package owns dead-code candidate scanning, cross-repo consumer
  evidence, investigation packets, downgraded-root verdicts, and the
  default reachability policy behind `/api/v0/code/dead-code*`.
- `codequery` owns `*CodeHandler`, its `Mount` route table, and the
  two pinned row readers (`deadCodeCandidateRows`,
  `deadCodeResultsWithGraphIncomingEdges`); this package MUST NOT
  re-declare those -- call them through `Dependencies` func fields.
- `codemodel` owns read-model builders and response shapers;
  `querycontract` owns envelopes, capabilities, and the access
  filter; `auth` owns request auth bounds. Qualify to them.

## Import discipline (cardinal)

NEVER import package `codequery` or root package `query` -- not in
production code, not in tests. Both import this package; the reverse
is an import cycle. `go build ./...` is the tripwire. Allowed:
stdlib plus the leaves named in [doc.go](doc.go).

## Analyzer discipline

- `Analyzer` is stateless. `codequery` delegates build one per call
  from the handler's own fields; add no request state and no caching.
- A staying `codequery` dependency enters only as a `Dependencies`
  func field with a comment naming the helper. When that helper moves
  to a leaf, repoint the field and drop the `codequery` reference.
- New exports need a staying caller named in the doc comment
  (delegate, seam, grant proof, staying test). Unexported-migrated
  helpers stay unexported.

## Test discipline

- Tests that pin production Cypher bytes drive the `Analyzer`
  methods or stay in `codequery` driving the delegates over HTTP --
  never over a copied implementation.
- A test that needs both `deadcode` internals and the `codequery`
  handler cannot live in either package; keep it in `codequery`
  against the delegate surface and qualify `deadcode` exports.
- Fixture paths are package-depth sensitive: this package sits two
  levels below root, so `tests/fixtures/...` needs five `..`
  segments, not three.

## Cross-repo boundary evidence (#7129)

`bucketCrossRepoDeadCodeResults` keeps the repository-boundary items in the
`visible` slice that classification reads, and keeps them OUT of the row's
`consumer_evidence` (`setCrossRepoDeadCodeRowEvidence`); the handler returns
them once as `boundary_consumer_evidence` (nil when no row used the fallback:
the list is unbounded, so it ships only to explain a row). Never classify from the row map, and
never append the boundary list to a row again: 20 boundary items per row put a
default-args MCP reply at 824,864 bytes against a 262,144 budget. The
`TestFindCrossRepoDeadCodeBoundaryOnly...` test in `internal/mcp` and
`cross_repo_boundary_classification_test.go` pin both halves.

## Incoming-edge read (#7547)

`DeadCodeIncomingEntityIDs` must run the legacy one-hop read for every entity
the reachability snapshot did not answer. Never gate that read on
`CodeReachabilityCoverage` (`Available`/`Truncated`), a root count, a ratio, or a
language: a watermark does not prove the roots were adequate. The fallback is the
legacy read, not `ambiguous`.

## Cross-repo evidence detail (#7129)

`shapeCrossRepoDeadCodeEvidence` (`cross_repo_evidence_detail.go`) is output
projection that runs AFTER `bucketCrossRepoDeadCodeResults`. Never classify from
a shaped row, never move shaping before bucketing, and never mutate a bucket row
in place (project into new maps). Under `handles` a row ships at most
`crossRepoDeadCodeHandleGroupLimit` (5) groups and the boundary list at most
`crossRepoDeadCodeBoundaryHandleLimit` (25); every cut must keep its count,
marker and `truth.omissions` entry. The group order (highest confidence, then
`item_count`, then keys ascending) is what guarantees the group that decided
`live_by_consumer` is first and never cut; changing it breaks that. Lowering the
default `limit`, raising the MCP budget, or cutting items without a marker are
not alternatives. The byte bar is
`TestFindCrossRepoDeadCodeHandlesCalibratedBaseBars` in `internal/mcp`, which runs
on the #7168 calibrated base (`newDeadCodeBudgetStore`: 60-byte docstrings, full
suppressed bucket, default args): the pathological evidence may add at most
45,875 bytes (17.5% of the budget) and the reply must fit with
`structuredContent` delivered. A cap change must be measured against that test.
The thin-base fixtures in `dispatch_cross_repo_handles_budget_test.go` only
isolate the evidence term and are not a bar. The row base (each docstring
clipped to 512 bytes but echoed about six times) is outside this cap, so a long-
docstring repository can still be resource-only until the echo dedupe lands.

## Consumer coverage gates "dead" (#7547)

`dead` on the cross-repo route means "no consumer row AND no consumer repository
is a coverage gap". A consumer with no row proves nothing when its snapshot is
missing, its watermark is truncated, or its `verdict_schema_epoch` is below the
current `CodeReachabilityVerdictSchemaEpoch` (imported from the reachability
store, never copied, and bound as a parameter of both statements); those are the
only three gaps the check detects. It does not see a stale or partly drained snapshot whose watermark says
`truncated = false`, nor a zero-root snapshot until the writer stamps it
truncated.
`crossRepoDeadCodeConsumerCoverage` (`cross_repo_consumer_coverage.go`) runs one
statement per request, through the optional `crossRepoDeadCodeCoverageStore`
beside the evidence store, and `bucketCrossRepoDeadCodeResults` turns an
incomplete answer into `consumer_coverage_incomplete`. The rules:

- A strong live consumer outranks an incomplete one, exactly like the hidden
  count: used stays used. Every other path to `dead` needs complete coverage.
- The coverage universe is repositories that CAN be consumers: a missing,
  truncated or older-epoch watermark is a gap only when the scope's active generation has a
  `code_calls` or `inheritance_edges` intent (completed or pending). The intent
  only decides whether a missing, truncated or older-epoch watermark counts; a pending intent
  next to an existing `truncated = false` watermark is NOT a gap. No such
  intent means complete (docs, IaC). Zero-root repositories with edge intents are
  NOT excluded. On a full generation a refresh intent (`is_refresh_intent`,
  payload `action = 'refresh'`) is not an edge: it has no caller or child
  entity and the loader never reads it, so a repository whose only intents are
  refreshes is complete (#7591). On a delta generation any code intent counts,
  because no watermark is ever written for it; never replace the probe's
  `(is_delta OR NOT is_refresh_intent)` with a bare `NOT is_refresh_intent`.
  Both statements carry the same probe, pinned by
  `TestCrossRepoDeadCodeConsumerCoverageUniversePredicate`, and its meaning is
  proven on PostgreSQL by `TestCrossRepoDeadCodeConsumerCoverageRefreshIntentLive`
  (and `TestCrossRepoDeadCodeConsumerCoverageLive` for the rest of the universe).
- Keep the statements' shape: the intent probe is a scalar `LIMIT 1` subquery
  inside a `CASE` (as `EXISTS` Postgres hoists it into a hashed subplan over every
  such intent), and the named statement finds scopes with a hashed
  `source_key = ANY($1)` pass (a row-by-row join of the list to the scopes planned
  as a nested loop, 520 ms for 3,000 ids). The response carries no checked-count:
  it would force a full scan.
- Each gap says WHY and whether a snapshot is expected without action: `no_snapshot_yet` (no watermark),
  `truncated` (current-epoch watermark that cannot prove absence), `older_epoch`
  (a refresh is expected) or `no_active_scope` (a named repository nobody ingested), with
  the scope's active `generation_id` and a `retryable` HINT, not a promise (true only for the
  first and third; it can stay true for a long time on a delta generation or a
  full generation whose reducer work did not complete). The precedence inside one watermark is missing, then truncated,
  then older epoch. A repository with several gap scopes yields one entry, a
  truncated scope first and then the lowest generation id: the named statement
  does it with `DISTINCT ON`, the all-repositories statement has no `ORDER BY`
  (its `LIMIT` must stop the scan early) so
  `ContentReader.CrossRepoDeadCodeConsumerCoverage` applies the same rule in Go.
  The top-level `retryable` is false when the list was cut. Both are proven by
  `TestCrossRepoDeadCodeConsumerCoverageLive`. `ORDER BY` uses the database
  collation while `Outranks` and the final Go sort use byte order, so "lowest
  generation" and the sort order can differ under a non-C collation between the
  named and all-repositories paths; each is deterministic. Do not change the SQL
  for this.
- A store without the coverage method, or a coverage read that cannot answer,
  is `cross_repo_evidence_unavailable`, never covered. A coverage read error is
  a failed request.
- The check is per request, never per candidate, and is skipped when no candidate
  needs classifying. Do not move it into the candidate loop.
- The check does not cover an UNGRANTED consumer with a partial snapshot: the
  hidden-consumer probe that finds ungranted consumers needs rows to exist.
- `CrossRepoDeadCodeCoverageGapCap` and the two coverage query constants
  (`cross_repo_consumer_coverage_sql.go`) are exported for the one caller that
  runs them, `ContentReader.CrossRepoDeadCodeConsumerCoverage` in package query,
  whose receiver is declared in root.
- The test doubles that predate this carry a coverage method that reports
  complete; a new double that wants `dead` needs one too.

## Postgres reader failures (#7523)

Every store or scan error in the three handlers goes through
`Dependencies.WriteGraphReadError` before the 500 fallback, including the
cross-repo consumer-evidence read and the investigation coverage read, so a
stale guarded PostgreSQL reader, or one whose connection acquisition (pool wait
or dial) or identity check timed out inside the replay window (a reader failure
that is not a timeout stays a 500), answers a retryable 503
`backend_unavailable` with `Retry-After` instead of a 500 carrying Go error
text. A new store read in these handlers needs the same call.

## Verification (paste all)

```bash
cd go && go build ./internal/query/...
cd go && go vet -gcflags=-e ./internal/query/...
cd go && go test ./internal/query/ ./internal/query/codequery/ ./internal/query/codequery/deadcode/ -count=1
cd go && go test ./internal/queryplan/ -count=1
git diff --check
gofmt -l <touched files, repo-relative>
```
