# Cloud Validation Runbook

Use this companion runbook when validating a hosted or Kubernetes deployment.
It is the cloud-side pair for [Local Testing Runbook](local-testing.md).

## What To Prove

Treat the runtime health checks and the completeness checks as different
questions:

- `health` or `healthz` proves the process is alive and initialized
- `index-status` proves the latest published checkpointed completeness state
- `/admin/status` proves the live runtime stage, backlog, and failure state
  when the runtime mounts the shared admin surface

Do not stop at pod health when the goal is operator confidence in freshness.

## Minimum Validation Order

1. Check the runtime health signal for the service you are validating.
2. Check `eshu index-status --profile <profile>` or the hosted
   `/api/v0/index-status` route for checkpointed completeness.
3. If you need repository-scoped completeness detail, inspect
   `/api/v0/repositories/{repo_id}/coverage`.
4. Run-scoped completeness routes are not part of the shipped public contract; do not
   treat `/api/v0/index-status` as a per-run view.
5. If you are debugging a recovery action, inspect the ingester
   `/admin/status` surface before and after the recovery call.

## Useful Hosted Checks

```bash
eshu index-status --profile qa
curl -fsS https://eshu.example.com/api/v0/index-status
curl -fsS https://eshu.example.com/api/v0/status/index
curl -fsS https://eshu.example.com/api/v0/repositories/repository:r_payments/coverage
curl -fsS https://eshu-ingester.example.com/admin/status
curl -fsS -X POST https://eshu-ingester.example.com/admin/refinalize \
  -H 'content-type: application/json' \
  -d '{"scope_ids":["scope-123"]}'
```

## Kubernetes Logs

When completeness and health diverge, inspect the live pod logs next:

```bash
kubectl logs -n eshu deployment/eshu-api --tail=50
kubectl logs -n eshu statefulset/eshu-ingester --tail=50
kubectl logs -n eshu deployment/eshu-resolution-engine --tail=50
```

Use the API logs for status lookups and admin calls, the ingester logs for
sync and checkpoint progress, and the resolution-engine logs for queue draining
and projection recovery.

## Recovery Boundary

Recovery is now owned by the Go ingester admin surface, not the hosted API.
Use `POST /admin/refinalize` to re-enqueue active scope generations and
`POST /admin/replay` to replay failed work items. There is no
`/api/v0/admin/refinalize/status` companion route anymore.

## When To Stop

You are done when:

- the health check is green
- `index-status` reports the expected checkpointed state
- the repository coverage rows you inspected match the expected remaining gaps
- the ingester `/admin/status` surface reflects the expected queue and stage
  state after the recovery flow you ran
