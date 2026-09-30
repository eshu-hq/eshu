# Graph-Read Error Public Text Evidence (#7253)

#7065 redacted driver error text on the `neo4j.query` span but left
`Neo4jReader.Run` returning the raw driver error. A handler that fell through
`WriteGraphReadError` (which maps only the deadline and unavailable sentinels)
wrote `err.Error()` into the HTTP 500 body. A Neo4j statement error quotes the
offending statement, and ad-hoc routes send it with inline literals, so a
client could read back statement text and values. This is the #7065 review F3
finding, filed separately as an owner design question: an error taxonomy or
redaction at the HTTP boundary.

## Decision

Neither, exactly: the bound sits at the source. `Neo4jReader` now returns a
`graphReadError` whose `Error()` is the fixed public text `graph query failed`
(`ErrGraphQueryFailed`) and whose `Unwrap`/`Is` keep the driver cause for
`errors.As` classification (retry and connectivity checks still work). About
300 handler sites write an error into a 5xx body (inventory below); a
per-site taxonomy would need every one of them to stay correct forever, and a
boundary rewrite in `WriteError` cannot tell a driver string from a deliberate
message. Bounding the error value itself makes every present and future
`err.Error()`, `%w` wrap, `%v` format, or `errors.Join` of a reader error safe
without touching a handler. The operator detail moved to the two places the
issue names, both redacted with the scanner that already redacts the statement
head and fingerprint (#7035):

- the `neo4j.query` span status and exception event. `redactedSpanError` reads
  the cause only for the query-failed class, so the #7065 span contract holds
  for that class and an unavailable or deadline span keeps its fixed public
  text. The first cut unwrapped every class, which put the graph host from a
  connectivity error's dial text on the 503 span; review caught it and
  `TestNeo4jReaderSpanForUnavailableReadCarriesNoAddress` now pins it, and
- a new `query.graph_read.error` log with `graph_read.error` (redacted driver
  text), `graph_read.statement_fingerprint`, `graph_read.statement_head`, and
  `graph_query_name`. It is ERROR level, except WARN for a statement the
  backend rejects as malformed (`Neo.ClientError.Statement.*`), so a client
  cannot raise an ERROR stream with a bad statement. The first cut used the
  whole `Neo.ClientError.*` class, which also holds authentication,
  authorization, and missing-database errors: a rotated graph credential would
  have produced 500s with only WARN logs. Review caught it;
  `TestNeo4jReaderLogsServerSideClientClassFaultsAtError` pins the ERROR level
  for those codes. A rejected Eshu-built statement is WARN too, because the
  reader cannot tell who wrote the statement; the `outcome="error"` metric still
  counts it.
- The driver text is redacted from its first line only
  (`redactDriverText`). A Neo4j syntax error quotes the offending statement in
  double quotes after the message, and the redactor reads its input as Cypher,
  so a double-quoted literal inside that quoted line flipped the string
  boundaries and survived, in the log field, the span, and the 400 message.
  `TestDriverDetailIsRedactedFromTheFirstLineOnly` reproduces it with an inner
  `"alice-secret"`. The statement is not lost: `graph_read.statement_head`
  already records it redacted.

### User-authored Cypher routes

`POST /api/v0/code/cypher` and `POST /api/v0/code/visualize` run the caller's
own statement. Bounding the error to `graph query failed` there removed the
only feedback an author (or an MCP agent) had for a malformed query, and
reported a caller error as a 500. Both routes now ask
`querycontract.GraphStatementRejection`: a `Neo.ClientError.Statement.*` failure
answers `400 invalid_argument` with the first line of the backend message run
through the literal redaction, and any other failure stays `500 graph query
failed`. The
seam is an interface the reader's error implements, so the routes do not import
the reader. The trade-off is that the redaction reads the message as Cypher: it
replaces the offending token in `Invalid input 'x'` and the numbers in a
`line 1, column 24` position, and drops text after `//`. That is enough to name
the clause the graph expected, and it keeps the same guarantee as the log and
span. Not verified against a live Neo4j: the quoted-statement message shape is
Neo4j's documented syntax-error form and was reproduced with a hand-written
message, not captured from a server. `TestCypherRoutesAnswer400ForARejectedCallerStatement` covers both routes;
disabling the mapping turns it red.

The hot-Cypher source-coverage manifest
(`go/internal/queryplan/testdata/query-source-coverage.yaml`) pins a source
digest for each of these two handlers under the `operator_query` /
`validated_query_endpoint` classification. This change edits how each handler
maps a failed `Run` to a response, so the `hot-cypher-source-coverage` gate
failed on the stale digests and both were repinned. The statement, its bounds,
its validation, and the `Run` call are untouched, so the classification stands;
the digests moved because the function bodies did.

The existing 503 (`ErrGraphUnavailable`) and 504 (`ErrGraphReadDeadline`)
classes are unchanged and still win over the new sentinel. The response for the
new class stays the plain 500 the handlers already wrote, with a stable detail;
the OpenAPI status set does not change.

## Reproduction (before the fix)

`TestInfraSearchFallbackDoesNotEchoDriverText` drives the real
`InfraHandler` search route over a real `Neo4jReader` whose fake session
returns a Neo4j syntax error that quotes `MATCH (n:Secret {token:
's3cr3t-literal'}) RETURN n`. On the pre-fix reader:

```text
--- FAIL: TestInfraSearchFallbackDoesNotEchoDriverText
    accept="" 500 body exposed driver text: {"detail":"neo4j query: Neo4jError: Neo.ClientError.Statement.SyntaxError (Invalid input (line 1, column 24 (offset: 23))\n\"MATCH (n:Secret {token: 's3cr3t-literal'}) RETURN n\")","error":"Internal Server Error"}
--- FAIL: TestNeo4jReaderErrorTextIsStableAndRedacted
    Error() exposed driver text: "neo4j query: Neo4jError: Neo.ClientError.Statement.SyntaxError (...'s3cr3t-literal'...)"
```

After the fix the same tests pass, and the third test
(`TestNeo4jReaderKeepsAvailabilityMappings`) pins that a connectivity error
still answers `ErrGraphUnavailable` and not `ErrGraphQueryFailed`.

## No-Regression Evidence

No-Regression Evidence (#7253): the change is on the error path only. A
successful read returns before `graphReadResult` reaches the new branch, so
its cost, allocations, and query text are byte-for-byte unchanged. A failed
read pays one extra `graphReadError` allocation and, for the new
`query.graph_read.error` log, one more pass of `statement.Redact` over the
driver message (the same scanner and cost class #7035 measured for the
statement head); failures are rare and already cost a round trip and a
retry-classification pass. No Cypher, no SQL, no index, no transaction, and no
queue or lease path changes, so there is no query-plan or concurrency claim to
prove. The full `internal/query`, `internal/telemetry`, `internal/mcp`,
`cmd/api`, and `cmd/mcp-server` test trees pass with the change.

## Observability Evidence

Observability Evidence (#7253): a 500 with the bounded body stays diagnosable.
`query.graph_read.error` (ERROR, `pipeline_phase="query"`,
`failure_class="error"`) carries the redacted driver text in `graph_read.error`
and the `graph_read.statement_fingerprint`/`graph_read.statement_head` that
name the statement, so an operator matches a client's `graph query failed` to a
log line by fingerprint. The `neo4j.query` span keeps the redacted exception
event and the `eshu.graph_read.outcome=error` attribute
(`TestNeo4jReaderSpanErrorRedactsEchoedStatementLiterals`, unchanged).
`graph_read.error` is registered in the frozen log-key list
(`TestLogKeys`) and documented in `docs/public/reference/telemetry/logs.md`,
`graph-read-safety.md`, and `telemetry-coverage.md`.

## Remaining error-text writes to a 5xx response body

Method: a multi-line pattern scan of non-test Go under `go/internal` and
`go/cmd` at the commit that carries this change, for a 5xx status
(`StatusInternalServerError`, `StatusBadGateway`, `StatusServiceUnavailable`,
`StatusGatewayTimeout`) whose message argument is `err.Error()` or an
`fmt.Sprintf` that formats an `err` value, and a second pattern for
`http.Error`. The first counted 287 sites in 25 packages under
`go/internal/query`; the second found 3 more (`internal/status/http.go` twice,
`internal/runtime/metrics.go`), all fed by the Postgres status store. A second,
independent heuristic run in review found 291 sites in 26 packages, so treat
every count here as approximate. The scan's blind spot is a helper that takes
an error-code argument between the status and the message, such as
`writeSemanticSearchError(w, r, status, code, err.Error())`; that is how
`internal/query/semanticsearch` (`semantic_search.go:307` and `:328`, Postgres
scope-resolver and search-backend errors in 503 bodies) was missed by the
table below and is listed here. `go/cmd/api`, `go/cmd/mcp-server`, and
`go/internal/mcp` write none (the MCP layer relays the API body). The "reads" column is a coarse import/identifier check on the
package's non-test files, not a per-site data-flow proof: a package marked
`graph` may reach the error from a graph read at some sites and from a store at
others.

| Package | Sites | Reads | First site |
| --- | --- | --- | --- |
| `internal/query/codequery` | 52 | graph, store | `handler.go:156` |
| `internal/query` (root) | 43 | graph, store | `status_answer_narration.go:38` |
| `internal/query/repository` | 33 | graph, store | `handler.go:146` |
| `internal/query/impact` | 28 | graph, store | `trace_deployment.go:131` |
| `internal/query/entity` | 22 | graph, store | `handler.go:207` |
| `internal/query/iac` | 22 | graph, store | `handler.go:167` |
| `internal/query/supply/chain` | 19 | graph, store | `investigation_packet_api.go:148` |
| `internal/query/package/registry` | 16 | graph, store | `aggregates_handler.go:83` |
| `internal/query/admin` | 14 | store | `handler.go:371` |
| `internal/query/secrets` | 6 | graph, store | `handler.go:145` |
| `internal/query/codequery/deadcode` | 5 | graph, store | `results.go:82` |
| `internal/query/contentread` | 5 | store | `content_handler.go:91` |
| `internal/query/compare` | 4 | graph, store | `handler.go:107` |
| `internal/query/cicd` | 3 | store | `handler.go:130` |
| `internal/query/freshness` | 3 | store | `generations.go:124` |
| `internal/query/terraform/drift`, `codeowners` | 2 each | store (codeowners also graph) | `handler.go:286`, `ownership.go:152` |
| `internal/query/semanticsearch` | 2+ | store (scope resolver), search backend | `semantic_search.go:307` |
| `internal/status`, `internal/runtime` (`http.Error`) | 3 | store (status report) | `status/http.go:55`, `runtime/metrics.go:46` |
| `metrics`, `dependency`, `observability/coverage`, `querycontract`, `incident`, `service`, `kubernetes`, `workitem` | 1 each | mixed | see the scan |

What each class can put in a body:

- **Graph reads (`Neo4jReader`): fixed by this change.** Every one of those
  sites now receives `graph query failed`, whatever it does with the error,
  because the bound is on the error value. This covers the reported F3 sites
  (`infra.go` fallback) and the sites that never map through
  `WriteGraphReadError`, for example the k8s-resource read in
  `impact/trace_deployment.go`, which writes `query k8s resources: %v`.
- **Postgres store reads: listed, not fixed here.** pgx v5.9.2 formats a
  connection failure as ``failed to connect to `user=<user> database=<db>`:
  <dial error>`` (`pgconn/errors.go`, `ConnectError.Error`), and the dial error
  normally carries the target address. A server error formats as
  `<SEVERITY>: <message> (SQLSTATE <code>)` (`PgError.Error`), whose message can
  name a relation, column, or constraint and, for an input-syntax error, echo
  the offending bound value. Neither carries statement text: the query
  constants are static and pgx does not put them in `Error()`. The exposure is
  the connection target, schema object names, and a bound value, not the
  statement, so it is a smaller leak than the graph one.
- **Other internal errors (JSON encoding, config, in-process): listed.** They
  carry Eshu-authored text.

Why the Postgres and internal class is not fixed in this PR: a boundary rewrite
in `WriteError` cannot tell a driver string from a deliberate 5xx message such
as `content store unavailable`, and the safe fix is a per-site change (log the
detail, return a stable body) across the sites in the table above. That is a
separate, larger change than the graph-read leak this issue names. The follow-up needs the owner's agreement
before an issue is filed; it is recorded here as the disposition of every
listed site rather than left implicit.

An independent read-only scan classified the error source for a sample of the
sites and confirmed the classes above: Postgres store calls are the main
remaining surface (for example `query/repository/freshness.go:55` from
`storage/postgres/repository_freshness.go`, and the status handlers), one site
relays an upstream Prometheus dial error (`query/metrics/handler.go:109`), and
JSON decode errors reach `infra.go:227`. It found no production `fmt.Errorf`
that embeds a SQL or Cypher statement. It did not classify every site (its
cap left roughly a hundred unchecked), which is why the table reports
packages and coarse sources, not a per-site proof.

No follow-up issue is filed for the Postgres and internal class: the issue asks
to fix or list each site, this note lists them, and filing a new tracked issue
needs the owner's agreement, which this lane does not have. It is the natural
next change.
