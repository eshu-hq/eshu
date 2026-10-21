# AppSync API key disclosure assessment (#8713)

Alert #20 identified AppSync API key values flowing into durable resource facts.
AWS uses the `ApiKey.Id` value as the key clients send in `x-api-key`; it is a
bearer credential, despite the SDK field name. The earlier adapter copied that
value into each key resource's identity, name, `key_id` attribute, correlation
anchors, and source record ID.

The fix drops the SDK `Id` and free-text description before scanner-owned state.
The API resource retains only the observed key count and ordered expiry/deletion
timestamps. It emits no per-key graph resource. Tests cover the SDK snapshot,
serialized fact envelopes, the Postgres cloud-inventory allowlist, and the HTTP
readback projection.

The source fix does not erase historical records. The cloud-inventory HTTP
read model suppresses legacy `aws_appsync_api_key` identity rows so their
bearer-bearing canonical UIDs are not returned while an old generation remains
active. This filter does not erase historical facts or graph nodes. Fact
retention prunes eligible superseded generations under its configured age and
row limits; it is not an immediate targeted secret purge. A fresh successful AppSync scan may remove
predecessor-only graph resource nodes through the existing generation-aware
materialization path, subject to that path's runtime configuration. Neither
production fact records nor deployed graph state were inspected or changed here.

Performance Evidence: This change adds a selective predicate to the
cloud-inventory readback query. It does not change fact writes, graph writes, or
queue processing.

Benchmark Evidence: The before query is the production projection, joins,
filters, ordering, and limit with only the legacy-key predicate removed; the
after query adds `payload->>'resource_type' IS DISTINCT FROM
'aws_appsync_api_key'`. Five interleaved pairs ran `EXPLAIN (ANALYZE, BUFFERS,
FORMAT JSON)` against the local B-7 PostgreSQL 18.6 fixture (35 repositories,
76 active scopes, 121 active canonical identities). Median execution was 1.326
ms before and 1.247 ms after. Both plans used
`fact_records_collector_status_active_idx`, returned 51 rows, and recorded 232
shared buffer hits with zero reads. The filter preserves records with a missing
or JSON-null `resource_type`, as asserted by the seeded live PostgreSQL test.

No-Regression Evidence: The earlier B-7 run at runtime commit
`457b0ef5b95a4a3e7ac9673808656490f3b3b878` completed with 616 passes, 0
required failures, and 2 advisory timing warnings. B-7 ran again on final head
`9ab2dcb0146968561182e5ba0ead560ad2e58f36` inside `make pre-pr`: 615 passes,
0 required failures, 3 advisory timing warnings, and 376 seconds elapsed. The
final run ended with 0 residual fact work items, 0 nonterminal
shared-projection intents, and 0 unroutable intents. The matched local fixture
timings and plan shape show no regression; they are not production p95 evidence
and do not establish a speedup. The exact predicate was also validated against
the red pre-filter query in the live regression.

Observability Evidence: No runtime instrumentation changed. The query
continues to use the existing `postgres.query` span and error recording.

Before treating deployed exposure as resolved, rotate AppSync API keys for the
scanned API scopes that used the vulnerable collector, then have the data owner
assess historical fact and graph cleanup for those exact scopes. Any targeted
production deletion needs its own reviewed procedure; this PR does not execute
one.

**NOT_CHECKED:** deployed API scope coverage, historical persisted values,
current graph nodes, key rotation, and targeted cleanup.
