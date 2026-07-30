# Cross-Scope Endpoint-Readiness Gate Evidence (#1380)

Moved from `go/internal/reducer/README.md` (issue #5786) to keep the
package README under the repository's 500-line cap. Content is
unchanged from the original section.

## Cross-scope endpoint-readiness gate (#1380)

The secrets/IAM graph projection writes `SECRETS_IAM_*` edges to endpoint nodes
(`KubernetesWorkload` for `USES_SERVICE_ACCOUNT`, `CloudResource` for
`ASSUMES_IAM_ROLE`) that are materialized in **different** reducer
scopes/generations. The same-scope/same-generation `graph_projection_phase_state`
gate cannot prove a specific cross-scope node committed, so this adds a
**uid-exact** presence primitive: `graph_endpoint_presence(keyspace, uid)`
(Postgres, migration `024`). The CloudResource and KubernetesWorkload node
materializers upsert one presence row per committed node uid, and
`SecretsIAMGraphProjectionHandler` confirms — before retract/write — that every
referenced endpoint uid is present (`EndpointPresenceLookup.MissingUIDs`, one
bounded `uid = ANY($2)` query, no N+1). If any are missing it returns a retryable
`secrets_iam_endpoint_not_ready` error so the durable queue re-enqueues the
intent instead of silently dropping edges.

No-Regression Evidence: the presence write and the gate are **flag-gated** — both
the materializers' `PresenceWriter` and the handler's `PresenceLookup` are nil
unless `ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED` is on, so the default
hot CloudResource/KubernetesWorkload node-commit paths carry **zero** extra write
and the projection keeps its current behavior. Proven by
`TestAWSResourceMaterializationNoPresenceWhenWriterNil`,
`TestPublishEndpointPresenceNilWriterIsNoOp`, and
`TestGraphProjectionGateDisabledWhenLookupNil`. The upsert is idempotent
(`ON CONFLICT (keyspace, uid)`) and safe under concurrent materializer workers —
no worker/batch reduction — verified by
`go test -race ./internal/reducer ./internal/storage/postgres`. Gate behavior is
proven by `TestGraphProjectionGateReEnqueuesWhenEndpointNotReady` (retryable, no
write before retract) and `TestGraphProjectionGateWritesWhenEndpointsReady`.

Observability Evidence: a readiness miss surfaces as the retryable error's
`FailureClass() = "secrets_iam_endpoint_not_ready"`, which the reducer service's
classified-execution log records (the same path as `aws_relationship_nodes_not_ready`),
so an operator can see projection intents waiting on cross-scope endpoints. The
error message names only the bounded keyspace and a missing-count — never a
redactable uid.

Cold-start liveness (#1391): when the flag is first enabled, already-committed
CloudResource and KubernetesWorkload nodes may lack presence rows until those
endpoint scopes re-materialize. The queue now treats
`secrets_iam_endpoint_not_ready` as a non-counting deferred retry: it preserves
the specific failure class, keeps `visible_at`/`next_attempt_at` backoff, and
does not increment `attempt_count` on later claims while that class is pending.
The projection intent therefore keeps re-driving instead of exhausting
`ESHU_REDUCER_MAX_ATTEMPTS` and terminally dropping the generation's edges.

No-Regression Evidence (#1391):

```bash
go test ./internal/storage/postgres -run 'TestReducerQueueFailDefersSecretsIAMEndpointReadinessPastAttemptBudget|TestReducerQueueClaimDoesNotCountSecretsIAMEndpointReadinessDefers|TestClaimBatchDoesNotCountSecretsIAMEndpointReadinessDefers' -count=1
```

This gate failed before the queue dead-lettered an over-budget readiness miss
and claim SQL always consumed `attempt_count`, then passed once deferred retries
became non-counting on both single and batch claim paths. The projection lane
still stays OFF by default until #2430 records the target deployment activation
proof and binds approval to that deployment.
