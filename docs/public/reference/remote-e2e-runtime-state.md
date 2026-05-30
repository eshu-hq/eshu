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
- `collector-sbom-attestation`
- `collector-security-alerts`
- `collector-vulnerability-intelligence`
- `collector-aws-cloud`
- `scanner-worker`

Each service must have a container, be `running`, and either have no Docker
healthcheck or report `healthy`. In smoke and full-corpus modes, the verifier
then calls `/api/v0/index-status` and requires `status=healthy` with
`outstanding`, `in_flight`, `pending`, `retrying`, `failed`, and `dead_letter`
all at zero. It also rejects workflow coordinator state with failed or
`reducer_converging` runs, blocked completeness rows, or pending completeness
rows. This keeps queue-zero from masking collectors whose reducer phase
contract never converged.

Representative mode keeps scheduled collectors enabled, so it uses a scoped
terminal contract instead of queue-zero. `/api/v0/index-status` must report
`healthy` or `progressing`, the queue must have zero `retrying`, `failed`, and
`dead_letter` work, and workflow coordinator `failed` or blocked-completeness
counts must be zero. The verifier still requires the representative aggregate
proof counters before accepting the run, while printing outstanding, in-flight,
pending, `reducer_converging`, and pending-completeness counts as active
follow-up work. A representative aggregate minimum explicitly set to `0` is not
required evidence, so the verifier skips that probe. Each API probe is bounded
by `ESHU_REMOTE_E2E_API_TIMEOUT_SECONDS`, which defaults to `30`.

Set `ESHU_REMOTE_E2E_REQUIRED_SERVICES`,
`ESHU_REMOTE_E2E_COLLECTOR_SERVICES`, or `ESHU_REMOTE_E2E_EXTRA_SERVICES` to
override the checked service lists for a narrower or profile-expanded run.
Set `ESHU_REMOTE_E2E_API_BASE_URL` and `ESHU_REMOTE_E2E_API_KEY` when the API
is not discoverable through the `eshu` Compose service port and generated
token.

Set `ESHU_REMOTE_E2E_PACKAGE_REGISTRY_GAP_PACKAGE_ID` to a bounded package ID
when a representative corpus intentionally includes package metadata that
exceeds the configured package-registry byte cap. The verifier calls the
supply-chain impact API for that package and requires
`unsupported_targets[]` to contain `target_kind=package_registry_metadata` and
`reason=metadata_too_large`, distinguishing an expected coverage gap from a
collector crash, transient provider outage, or retry storm.

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
healthy runtime set with queue-zero and workflow completion passes. It also
proves representative mode can accept scheduled follow-up work only when
required aggregate evidence has landed and `retrying`, `failed`, `dead_letter`,
failed workflow, and blocked-completeness counts are zero. It also proves an
explicit package-registry too-large metadata gap is accepted only when the
impact-readiness envelope reports
`package_registry_metadata/metadata_too_large`. Focused status and Postgres
status-reader coverage also proves `/api/v0/index-status`
health does not report `healthy` while workflow coordinator runs are still
`reducer_converging`, workflow completeness rows are pending or blocked,
workflow runs have failed, or status-age fields briefly go negative because the
database timestamp is newer than the status read clock. This changes only the
verification gate, operator status projection, and read-side age math; it does
not alter Compose service definitions, worker counts, graph writes, collector
scan shape, retry behavior, or NornicDB settings.

Observability Evidence: the verifier prints each checked service with Docker
runtime state and health state, keeps API bearer tokens out of process
arguments, bounds API probes with a max-time, and records the checkpointed
`/index-status` payload on queue, workflow-completion, or representative
runtime-safety failure. Representative scoped terminal output includes queue
counts, `reducer_converging`, pending completeness, and blocked completeness so
operators can distinguish active scheduled work from retry storms, terminal
failures, and blocked evidence. The existing `/api/v0/index-status`,
`/api/v0/status/index`, and admin status report now carry workflow coordinator
`run_status_counts`, `work_item_status_counts`, `completeness_counts`, active
and overdue claim counts, queue/domain ages, and health reasons that
distinguish fact-queue backlog, shared projection backlog, workflow
convergence, blocked completeness, failed workflow runs, and stale pending
workflow work.
When `ESHU_REMOTE_E2E_PACKAGE_REGISTRY_GAP_PACKAGE_ID` is set, the verifier
also prints `package_registry_metadata_too_large_gaps` from the bounded
readiness response without printing package names, metadata URLs, or feed
credentials.
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
