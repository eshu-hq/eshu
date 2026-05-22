# Remote E2E Runtime State

Use this gate after starting the hosted remote collector E2E Compose stack.
The gate proves the long-lived runtimes are actually running and that
checkpointed completeness has reached queue-zero before the run is accepted as
deployment evidence.

This catches a specific failure mode: collectors can emit scope generations
while `projector/source_local` work stays pending if the ingester never starts.
In hosted mode, the ingester owns source-local projection, so a stack with
healthy collectors but a `Created`, stopped, or unhealthy ingester is not ready.

## Command

Run from the Eshu checkout that owns the Compose stack:

```bash
export COMPOSE_PROJECT_NAME="${COMPOSE_PROJECT_NAME:-eshu-remote-e2e}"
export ESHU_REMOTE_E2E_ENV_FILE="${ESHU_REMOTE_E2E_ENV_FILE:-.env.remote-e2e}"
export ESHU_REMOTE_E2E_COMPOSE_FILES="docker-compose.remote-e2e.yaml"

scripts/verify_remote_e2e_runtime_state.sh
```

If the run uses an additional local override file, append it with a colon:

```bash
export ESHU_REMOTE_E2E_COMPOSE_FILES="docker-compose.remote-e2e.yaml:/tmp/eshu-remote-e2e.override.yaml"
scripts/verify_remote_e2e_runtime_state.sh
```

## What It Checks

By default, the verifier requires these core runtimes:

- `eshu`
- `mcp-server`
- `ingester`
- `resolution-engine`
- `workflow-coordinator`

It also requires these hosted collector services:

- `collector-terraform-state`
- `collector-oci-registry`
- `collector-package-registry`
- `collector-aws-cloud`

Each service must have a container, be `running`, and either have no Docker
healthcheck or report `healthy`. The verifier then calls
`/api/v0/index-status` and requires `status=healthy` with `outstanding`,
`in_flight`, `pending`, `retrying`, `failed`, and `dead_letter` all at zero.
It also rejects workflow coordinator state with failed or
`reducer_converging` runs, blocked completeness rows, or pending completeness
rows. This keeps queue-zero from masking collectors whose reducer phase
contract never converged.

Set `ESHU_REMOTE_E2E_REQUIRED_SERVICES`,
`ESHU_REMOTE_E2E_COLLECTOR_SERVICES`, or `ESHU_REMOTE_E2E_EXTRA_SERVICES` to
override the checked service lists for a narrower or profile-expanded run.
Set `ESHU_REMOTE_E2E_API_BASE_URL` and `ESHU_REMOTE_E2E_API_KEY` when the API
is not discoverable through the `eshu` Compose service port and generated
token.

Remote E2E Compose supports either an explicit `ESHU_API_KEY` in the env file
or an auto-generated local token. When `ESHU_API_KEY` is blank, the API writes
the generated token under the shared `/data/.eshu/.env` volume, and the MCP
runtime reads the same token from that mounted Eshu home. That keeps
authenticated API and MCP `/api/*` validation on one bearer-token contract
instead of generating container-local tokens per service.

## Evidence

No-Regression Evidence: `scripts/test-verify-remote-e2e-runtime-state.sh`
covers the runtime gate against mocked Docker and API responses. The test
proves that an ingester stuck in `Created`, an unhealthy collector, a non-zero
fact queue, and queue-zero plus stale workflow `reducer_converging` /
pending-completeness state all fail before a run can be accepted, while a
healthy runtime set with queue-zero and workflow completion passes. Focused
status and Postgres status-reader coverage also proves `/api/v0/index-status`
health does not report `healthy` while workflow coordinator runs are still
`reducer_converging`, workflow completeness rows are pending or blocked,
workflow runs have failed, or status-age fields briefly go negative because the
database timestamp is newer than the status read clock. This changes only the
verification gate, operator status projection, and read-side age math; it does
not alter Compose service definitions, worker counts, graph writes, collector
scan shape, retry behavior, or NornicDB settings.

No-Regression Evidence: after the merged-main remote proof briefly classified
the run as `stalled` while `bootstrap-index` was still processing late large
repositories, focused status coverage now keeps the same shape `progressing`
when the queue has no in-flight work but scope-generation producer activity is
recent. The regression also proves recent producer activity does not hide
dead-letter or failed work; those states still report `degraded`. The storage
reader coverage proves the producer age is read from active or pending
`scope_generations`, clamped to a non-negative duration, and fed into the pure
status projection. This changes only `/api/v0/index-status` health
classification during producer-active idle gaps; it does not change queue
claims, workflow planning, collector behavior, graph writes, worker counts, or
the terminal verifier requirement that queue/workflow state reach zero.

Observability Evidence: the verifier prints each checked service with Docker
runtime state and health state, then records the checkpointed `/index-status`
payload on queue or workflow-completion failure. Operators can distinguish a
missing source-local owner from collector failure, API unavailability,
projection backlog, and stale workflow phase convergence without reading
private machine-specific logs or paths. The existing `/api/v0/index-status`,
`/api/v0/status/index`, and admin status report now carry workflow coordinator
`run_status_counts`, `work_item_status_counts`, `completeness_counts`, active
and overdue claim counts, queue/domain ages, and health reasons that distinguish
fact-queue backlog, shared projection backlog, workflow convergence, blocked
completeness, failed workflow runs, and stale pending workflow work.

Additional Observability Evidence: the existing `/index-status` health reason now names
recent producer activity when it is the reason an old idle fact queue remains
`progressing` instead of `stalled`. Operators can correlate that reason with
the existing scope/generation counts, queue counts, workflow coordinator
counts, and bootstrap or collector structured logs. No new metric label was
added because the signal is a bounded status projection over
`scope_generations` timestamps, and high-cardinality repository or path details
remain in logs rather than status metrics.

No-Regression Evidence: remote E2E Compose now overrides API and MCP
`ESHU_HOME` to `/data/.eshu` while preserving `ESHU_API_KEY=${ESHU_API_KEY:-}`
and `ESHU_AUTO_GENERATE_API_KEY=true`; focused coverage is
`go test ./internal/runtime -run 'TestRemoteE2EComposeSharesGeneratedAPIKeyState|TestRemoteE2EExampleEnvRequestsFullCorpusPreflight' -count=1`.
The change only moves remote read-surface auth state for API/MCP onto the
existing shared Eshu data volume; it does not change collector scheduling,
worker counts, graph writes, NornicDB settings, or fact/reducer queue behavior.

No-Observability-Change: authenticated validation still uses API and MCP
`/healthz`, mounted `/api/*` routes, Docker health state, and the verifier's
status payload. The token location is an operator contract, not a new runtime
signal, so no metric label or span attribute was added.
