# Scoped Content Search Signals

A file-content or entity-content substring search with a repository filter
(one repo or a set) runs one SQL statement per scope shape under the 30 s
dispatch deadline, with no `statement_timeout` and no work budget (#10606).
These are the signals an operator uses to see whether scoped searches are
timing out, on which operation, and in which pattern class — the evidence
that decides whether a query or timeout change is justified at all.

Every scoped call records one count and one duration sample at the chokepoint
both scope shapes pass through (`searchFileContentScoped` /
`searchEntityContentScoped`). No query or timeout behavior changes with these
signals.

## Metrics

| Metric | Type | Labels | Reading it |
| --- | --- | --- | --- |
| `eshu_dp_content_search_scoped_total` | counter | `operation`, `scope_class`, `pattern_class`, `outcome` | One increment per scoped search. A rising `context_deadline` share past the threshold below is the evidence that unlocks a query/timeout investigation. |
| `eshu_dp_content_search_scoped_duration_seconds` | histogram | `operation`, `scope_class`, `pattern_class`, `outcome` | SQL wall time of one scoped search. Buckets span 10 ms to 30 s so the approach to the 30 s dispatch deadline stays visible. |

`operation` is the fixed `db.operation` the statement ran under:
`search_file_content_page`, `search_file_content_in_repos`,
`search_entity_content_page`, or `search_entity_content_in_repos`.
`scope_class` is `single` (one repository) or `multi` (a set), derived from
the operation. `pattern_class` is `trigram`, `no_trigram`, `short`
(<3 runes), or `wildcard` (contains `%`, `_`, or `\` until fix 05b lands, when
the class empties by construction). `outcome` is `ok`, `error`,
`context_deadline` (the dispatch deadline fired), or `context_canceled` (the
caller went away first), so client aborts never inflate the deadline rate
the threshold reads.

## Span

`postgres.query` with the scoped `db.operation` above (file route:
`db.sql.table=content_files`; entity route: `db.sql.table=content_entities`)
carries `search.scope=scoped`, `search.pattern_class`, and
`search.rows_returned`. The file route also carries the same `ObservePG`
Postgres-phase clock the entity route already had.

## Log

One `scoped_search_context_deadline` line (WARN) per deadline outcome, with
`operation`, `pattern_class`, `elapsed_ms`, `limit`, and `offset` — the 3-AM
line. There is no per-request info logging.

No span attribute, metric label, or log line carries the search pattern, a
repo id, or any caller string.

## Reading it

Deadline rate per operation over 7 days, the numerator of the #10606
threshold (1% at >=1000 calls unlocks a query/timeout investigation):

```promql
sum by (operation) (rate(eshu_dp_content_search_scoped_total{outcome="context_deadline"}[7d]))
  /
sum by (operation) (rate(eshu_dp_content_search_scoped_total[7d]))
```

p95 duration per operation and pattern class, the other threshold (10 s in a
class with >=100 calls):

```promql
histogram_quantile(0.95,
  sum by (operation, pattern_class, le) (rate(eshu_dp_content_search_scoped_duration_seconds_bucket[7d]))
)
```

## Satellite evidence pair

The satellite (eshu-mcp) has no OTEL bootstrap yet (#9498), so production
timeout rates pair the core series above with the satellite's existing
outcome lines. The evidence pair for one window is the core deadline rate
above plus the satellite dispatch log for the same tool and window:

```logql
{service_name="mcp-server"} |= "mcp tool dispatch ended" | json | outcome="timeout"
```

with the `mcp tool dispatch context ended` warnings beside it. Below
threshold the intermittent scoped timeout stays recorded as load noise and
the claim closes as instrumented-and-accepted; above threshold the next step
is an EXPLAIN ANALYZE shim on the worst operation×pattern_class, never a
timeout raise, serialization, or deadline swallowing.

## Evidence

Observability Evidence: `TestRecordScopedSearchEmitsOneCountAndOneSamplePerOutcome`
proves one count and one histogram sample per call across ok/error/deadline/canceled,
including the closed label set, the scoped span attributes, and the WARN-only-on-deadline
log; `TestSearchFileContentScopedRecordsOKThroughChokepoint` and
`TestSearchEntityContentScopedRecordsErrorThroughChokepoint` prove the file and entity
chokepoints record through the production path. No-Regression Evidence: the existing
scoped suites run green untouched and one scoped response is byte-identical before/after;
recording shares one attribute set per call with the unscoped recorder beside it.
