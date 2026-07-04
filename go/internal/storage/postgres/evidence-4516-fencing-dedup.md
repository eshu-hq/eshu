# Fact Dedup Fencing Order (#4516)

No-Regression Evidence: `go test ./internal/storage/postgres -run
'TestDeduplicateEnvelopes|TestUpsertFactsDeduplicatesByFactID' -count=1`
failed before `deduplicateEnvelopes` compared duplicate fact envelopes by
`FencingToken`, then passed after the helper chose the highest token while
preserving last-position behavior when tokens tie. The changed code stays
inside the existing in-memory pre-batch dedup step that Postgres requires before
`INSERT ... ON CONFLICT DO UPDATE`; it adds no SQL, index, batch-size, worker,
lease, queue, graph-write, or transaction-boundary change.

No-Observability-Change: this changes only which duplicate envelope is retained
before the existing fact-record upsert. Operators still diagnose the path
through the existing `eshu_dp_postgres_query_duration_seconds` instrumentation
around fact writes and the durable `fact_records` row shape (`fact_id`,
`fencing_token`, `payload`, `scope_id`, `generation_id`). No new metric, span,
log field, status field, route, runtime knob, or backend dependency is added.
