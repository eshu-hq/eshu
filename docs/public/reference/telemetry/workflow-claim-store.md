# Workflow Claim Store Signals

Every claim-aware hosted collector picks its workflow claim store once, at
startup (#8875). With `ESHU_WORKFLOW_CLAIM_PLANE_URL` unset or blank, it
claims from the core Postgres workflow store, as every deployment did before.
With a valid `http` or `https` base URL, it claims through the `eshu-workflow`
claim plane over the `workflow.v1` protocol. It never falls back from the
claim plane to Postgres. See
[Environment Variable Registry](../env-registry/collectors-platform.md) for
the variable.

## Which store a pod runs

- Log `collector.workflow_control_store.selected` (INFO, once per start).
  Fields: `service`, `workflow_control_store` (`postgres` or `claim_plane`),
  and, for the claim plane, `claim_plane_endpoint` (scheme and host only).
- `eshu_dp_workflow_control_store_selections_total` — counter, one increment
  per collector start. Label: `backend_kind` (`postgres`, `claim_plane`). A pod
  that should claim from the claim plane but shows `postgres` is missing the
  variable.

An invalid URL never reaches these signals. Startup stops with an error that
names the variable and never echoes a credential. A collector that reads run
rows only core Postgres holds (`collector-vulnerability-intelligence`,
`collector-component-extension`) also stops startup when the variable is set.

## Claim-plane request health

Both metrics are recorded once per HTTP request, at the claim-plane client's
single request chokepoint (`go/internal/workflow/claim_plane_client.go`).

- `eshu_dp_workflow_claim_plane_requests_total` — counter.
- `eshu_dp_workflow_claim_plane_request_duration_seconds` — histogram,
  boundaries 5 ms to 10 s. 10 s is the client timeout, so a timed-out request
  lands in the last finite bucket.

Labels on both: `operation`, the protocol operation (`ClaimNext`,
`HeartbeatClaim`, `CompleteClaim`, `ReleaseClaim`, `FailClaimRetryable`,
`FailClaimTerminal`, `CheckClaimPacing`, `ValidateAndPinClaimForCommit`,
`ListCollectorInstances`, and the coordinator control routes), and `outcome`,
from this closed set:

| `outcome` | Meaning |
| --- | --- |
| `ok` | 200 response that decoded cleanly. |
| `rejected` | Fenced-away epoch (`stale_fence`, `lease_expired`, `claim_conflict`). The runner drops the stale item. |
| `not_found` | `not_found` envelope. On `ClaimNext` this is the idle queue, not a failure. |
| `paced` | `paced` envelope. |
| `invalid` | `invalid` or `version_mismatch` envelope. Check the protocol version on both sides. |
| `server_error` | Any other non-200 response, including a proxy page that is not a protocol envelope. |
| `timeout` | The 10 s client timeout or the caller's deadline expired. |
| `canceled` | The caller's context was canceled first, for example by SIGTERM during a rollout. Not a claim-plane fault. |
| `transport_error` | Connection refused, reset, or DNS failure. |
| `decode_error` | 200 response whose body did not match the operation's shape. |
| `request_error` | The client could not build the request. Nothing reached the network. |

## Reading an outage

A rising `timeout` or `transport_error` rate on `ClaimNext` means the claim
plane is slow or unreachable. A rollout shows up as `canceled`, not
`transport_error`. The collector exits on a failed `ClaimNext`, and
the orchestrator restarts it with backoff, the same path a core Postgres
outage takes. A rising `timeout` on `HeartbeatClaim` with no restarts means
leases are at risk: the claim plane's reaper reclaims an item whose lease
expires, and the next mutation from the old owner reports `rejected`. The
claim plane's own `/metrics` (`workflow_claim_mutations_total{code}`,
`workflow_claim_reaps_total`) gives the server side of the same calls.

Until the claim-plane commit fence lands (#8875 P2), an ingestion commit on a
claim-plane claim fails closed with
`verify active workflow claim before ingestion commit`. Expect
`eshu_dp_workflow_claim_retries_total` to climb for any instance pointed at the
claim plane before then.
