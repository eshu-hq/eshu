# AppSync API key disclosure remediation

Code-scanning alert #20 identified AppSync API key bearer values in
`ApiKey.Id`. The earlier adapter copied those values into per-key resource IDs,
names, attributes, correlation anchors, source record IDs, and durable facts.

The scanner now drops IDs and descriptions at the SDK boundary, keeps only
API-level key count and lifecycle timestamps, and emits no per-key graph
resource. Legacy `aws_appsync_api_key` identities are filtered from cloud inventory
lists and other graph-backed identity reads. Fixed facet totals remain stored-node
counts and may include historical records, but expose no key identity fields. SQL and Cypher readers
apply the filter before ordering, pagination, candidate resolution, or result
hydration where those operations could otherwise expose a legacy key.

## Verification evidence

- RED regression tests failed before the read filters were added, including the
  Postgres identity page, graph list/count, infra search, and resource selector.
- Focused Go tests pass for the affected query, impact, deployment, compare,
  entity, and query-plan packages.
- A disposable PostgreSQL 18 fixture verifies that a seeded historical key is
  omitted from a full identity page while healthy resources remain. The same
  suite exercises 20,000 owner rows across 64 production SQL variants against a
  2-second per-query budget.
- An isolated Neo4j fixture verifies ten HTTP read paths: infra search, graph
  listing, entity context, infra relationships, resource investigation, change
  surface, entity map, deployment trace, environment comparison, and dependency-path tracing. A synthetic dependency
path whose only route crosses the hidden key is returned as no path. The
  synthetic key's stored name, resource ID, and ARN do not appear in responses;
  explicit selector requests return no match.
- Query source digests, emitted query hashes, and the 64-variant SQL family
  registry match the final production builders.

The historical-key filters change reads only. The scanner also stops emitting
per-key facts, so future graph projections no longer receive those key records.
No schema, queue behavior, or runtime instrumentation changed. Existing
`postgres.query` tracing and error recording remain in use.

Performance Evidence: on the isolated `neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f` fixture, an alternating warm Bolt comparison over 80 paired rounds used the exact pre-change query (SHA-256 `8e54e6fa6e5507f072062b6257802a748bde0cbfa5ca364d9dbe3fb73852d033`) and production candidate (SHA-256 `a9e94e4d1d12491e54404e511e17e36b2988356269973a77ea6a325cc386b27d`). The 1,000-edge graph included 20 historical keys; both paths returned the same 980 public relationships after `extractRelationships`. From Bolt `Session.Run` through result consumption and relationship filtering, baseline median/p95 was 4.651/7.718 ms and candidate was 5.184/7.224 ms. This synthetic result shows +0.534 ms median and -0.494 ms p95; it is not a production latency claim.

No-Observability-Change: existing entity-context resolution counters and span attributes continue to report resolution and statement counts; this read filter adds no new runtime signal.

Historical facts and graph nodes are **NOT_PURGED**. No deployed API scopes,
persisted production values, graph state, or key rotation were inspected or
changed. This PR does not purge records or rotate keys. The data owner must
assess deployed exposure and rotation separately; any targeted production
cleanup requires its own reviewed procedure.
