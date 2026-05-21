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

Set `ESHU_REMOTE_E2E_REQUIRED_SERVICES`,
`ESHU_REMOTE_E2E_COLLECTOR_SERVICES`, or `ESHU_REMOTE_E2E_EXTRA_SERVICES` to
override the checked service lists for a narrower or profile-expanded run.
Set `ESHU_REMOTE_E2E_API_BASE_URL` and `ESHU_REMOTE_E2E_API_KEY` when the API
is not discoverable through the `eshu` Compose service port and generated
token.

## Evidence

No-Regression Evidence: `scripts/test-verify-remote-e2e-runtime-state.sh`
covers the runtime gate against mocked Docker and API responses. The test
proves that an ingester stuck in `Created`, an unhealthy collector, and a
non-zero queue all fail before a run can be accepted, while a healthy runtime
set with queue-zero passes. This changes only the verification gate; it does
not alter Compose service definitions, worker counts, graph writes, collector
scan shape, retry behavior, or NornicDB settings.

Observability Evidence: the verifier prints each checked service with Docker
runtime state and health state, then records the checkpointed `/index-status`
payload on queue failure. Operators can distinguish a missing source-local
owner from collector failure, API unavailability, and true projection backlog
without reading private machine-specific logs or paths.
