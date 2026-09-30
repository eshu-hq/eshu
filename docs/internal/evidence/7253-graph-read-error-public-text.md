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

- the `neo4j.query` span status and exception event (`redactedSpanError` now
  reads the cause, so the #7065 span contract is unchanged), and
- a new `query.graph_read.error` ERROR log with `graph_read.error` (redacted
  driver text), `graph_read.statement_fingerprint`, `graph_read.statement_head`,
  and `graph_query_name`.

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
`fmt.Sprintf` that formats an `err` value. It counted 287 sites in 25
packages, every one under `go/internal/query`; `go/cmd/api`,
`go/cmd/mcp-server`, and `go/internal/mcp` write none (the MCP layer relays the
API body). The "reads" column is a coarse import/identifier check on the
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

An independent read-only scan of the same population was started to classify
each site's error source with cited evidence; if it lands after this note it is
attached to the PR instead of changing these counts.
