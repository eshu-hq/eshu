# Security Intelligence API And MCP Contract

## API And MCP Contract

Security reads must be bounded, explainable, and scoped:

- require `limit`, timeout, deterministic ordering, and `truncated` signals for
  list responses;
- require at least one anchor such as repository, package, image digest,
  advisory id, service, workload, environment, or status;
- keep findings separate from readiness and source facts;
- keep provider alert state separate from Eshu impact state;
- return evidence handles and missing-evidence reasons instead of raw full
  source payloads;
- expose exact, derived, possible, known-fixed, unknown impact statuses and
  unsupported readiness states without collapsing them into one severity bucket.

The current vulnerability impact route is documented in
[HTTP Vulnerability Impact Routes](http-api/vulnerability-impact.md).
