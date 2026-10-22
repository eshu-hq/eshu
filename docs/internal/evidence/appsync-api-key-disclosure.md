# AppSync API key disclosure remediation

Code-scanning alert #20 identified AppSync API key bearer values in
`ApiKey.Id`. The earlier adapter copied those values into per-key resource IDs,
names, attributes, correlation anchors, source record IDs, and durable facts.

The scanner now drops IDs and descriptions at the SDK boundary, keeps only
API-level key count and lifecycle timestamps, and emits no per-key graph
resource. Legacy `aws_appsync_api_key` identities are filtered from cloud
inventory and other supported graph-backed read paths. SQL and Cypher readers
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
- An isolated Neo4j fixture verifies nine HTTP read paths: infra search, graph
  listing, entity context, infra relationships, resource investigation, change
  surface, entity map, deployment trace, and environment comparison. The
  synthetic key's stored name, resource ID, and ARN do not appear in responses;
  explicit selector requests return no match.
- Query source digests, emitted query hashes, and the 64-variant SQL family
  registry match the final production builders.

The filters change reads only. No runtime instrumentation, fact writes, graph
writes, queue processing, or schema changed. The existing `postgres.query`
tracing and error recording remain in use.

Historical facts and graph nodes are **NOT_PURGED**. No deployed API scopes,
persisted production values, graph state, or key rotation were inspected or
changed. This PR does not purge records or rotate keys. The data owner must
assess deployed exposure and rotation separately; any targeted production
cleanup requires its own reviewed procedure.
