# iamcantargets

Postgres implementation of `iamcan.CrossScopeTargetLoader` for the IAM
CAN_PERFORM projection (#6785).

## Why it exists

The awscloud collector writes roles and `aws_iam_permission` into the IAM
service scope (`aws:<account>:<claim-region>:iam`) and every CAN_PERFORM target
into its own service scope (`aws:<account>:<region>:s3`, `...:kms`, and so on).
A same-scope join resolves no production target, so the handler asks this store
for exact target ARNs in the sibling scopes of the same account.

## How it reads

1. `candidateScopesQuery` samples every candidate scope in one round trip. A
   candidate matches the account, a requested service kind, and a requested
   region (any region for S3, whose bucket ARNs carry none), and is not the IAM
   scope. Per scope it returns the active generation, whether that generation
   is `active`, whether `aws_resource_materialization` published
   `canonical_nodes_committed` on `cloud_resource_uid` for it, and whether any
   generation is still `pending`.
2. For each scope with an active generation, `FactLister` (the shared
   `postgres.FactStore`) reads only the requested ARNs of that scope's service
   through `ListFactsByKindAndPayloadValue`, pinned to the generation the
   sample saw. Readiness is sampled before the load, so a generation that
   activates mid-call is never judged against another generation's flag.

The handler, not this store, decides resolved, not-ready, or unresolved.

## Cost

Measured before landing on Postgres 16 with 105 000 scopes (85 000 AWS across
50 accounts x 17 regions x 100 services, plus 20 000 git) and 525 000
generations. Three runs took 10.9-13.1 ms. The `ingestion_scopes` filter is a
parallel seq scan (3 530 shared hits), because the default collation cannot
serve a `LIKE` prefix. The phase and pending probes are index-only scans on
`graph_projection_phase_state_lookup_idx` and `scope_generations_scope_idx`.
The fact reads use `fact_records_scope_generation_idx`.

## Telemetry

No-Observability-Change: the store registers no metric. The CAN_PERFORM
handler reports every lookup outcome through
`eshu_dp_iam_can_perform_cross_scope_targets_total{outcome}` and every defer
through `eshu_dp_reducer_readiness_waits_total{domain,outcome}`. A store error
fails the intent as an ordinary error, visible through
`eshu_dp_reducer_executions_total`, never as a readiness miss.

## Related

- `go/internal/reducer/iamcan` (the consumer)
- `docs/internal/design/6785-cross-scope-can-perform-and-uses-readiness.md`
