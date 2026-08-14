<!-- docs-catalog
title: Choose your first successful run
description: Gets a new reader to one working runtime, one indexed repository, and one useful answer.
type: how-to
audience: new-user
time: Varies with repository size
entrypoint: true
landing: false
-->

# Choose your first successful run

Use this guide to reach one useful answer from one indexed repository with the
runtime you already have or plan to start.
`eshu first-run` detects a reachable API, local binaries, or Docker Compose. It
verifies the runtime, indexes the target or reuses a drained index, waits for
indexing completeness, and runs a bounded query. It does not start a runtime.
`--no-start` does not change startup behavior: `first-run` never starts a
runtime. The flag only changes the failure hint so it records that verify-only
mode was requested.

## Before you begin

Choose the path that matches the service you will use:

| Path | Choose it when |
| --- | --- |
| [Local Compose](#run-with-local-compose) | You want the full API and MCP stack in containers. |
| [Local binaries](#run-with-local-binaries) | You are developing from a checkout and want to query its workspace-local owner through MCP. |
| [Hosted service](#connect-to-a-hosted-service) | An operator already runs Eshu for you. |

Health only proves that a process is alive. The selected path is complete when
the index is ready and a bounded query returns. See [Health checks](../operate/health-checks.md)
for the distinction between `/healthz`, `/readyz`, and indexing readiness.

## Run with local Compose

1. From the Eshu checkout, expose the host directory that contains the
   repository and select the repository by name:

   ```bash
   export ESHU_FILESYSTEM_HOST_ROOT="$HOME/src"
   export ESHU_REPOSITORY_RULES_JSON='{"exact":["payments-api"]}'
   export ESHU_API_KEY="local-compose-token"
   docker compose up -d
   ```

   Replace `$HOME/src` and `payments-api` with your own values. The API can
   become healthy while the one-shot bootstrap is still indexing. Verify that
   the completed index is reusable before running another indexing command:

   ```bash
   docker compose ps --all bootstrap-index
   docker compose exec eshu eshu workspace status
   docker compose exec eshu eshu list
   ```

   Continue when bootstrap shows `Exited (0)` and the API is healthy. `workspace
   status` must report healthy state, no outstanding queue work, a
   completed or active generation, and no failed or dead-letter stage or domain
   work. `list` must contain `payments-api` with `local_path` set to
   `/data/repos/payments-api`; these are the signals `first-run` uses for reuse.
2. Run the guided check inside the Compose API service with the managed `local_path` reported by `list`:

   ```bash
   docker compose exec eshu eshu first-run /data/repos/payments-api
   ```

3. Confirm the result from the same service container:

   ```bash
   docker compose exec eshu eshu index-status
   docker compose exec eshu eshu list
   docker compose exec eshu eshu stats payments-api
   ```

## Run with local binaries

Install the local commands and put them on `PATH`:

```bash
./scripts/install-local-binaries.sh
export PATH="$(go env GOPATH)/bin:$PATH"
```

Generate a Codex configuration for the local stdio MCP process:

```bash
eshu mcp setup --platform codex
```

Apply the printed configuration. Open the target repository as the Codex
workspace before you restart Codex; the generated `eshu mcp start` command uses
the client process's working directory to select the workspace. It starts or
attaches to that workspace's local owner. The owner provides embedded Postgres,
embedded NornicDB, ingestion, and reduction without a separate HTTP API
process.

Ask Codex to use Eshu and list the indexed repositories for the current
workspace. The API-backed `eshu first-run`, `eshu list`, and `eshu stats`
commands do not attach to this owner. Use the [Local Compose](#run-with-local-compose)
path when you need the exact `first-run` workflow or other HTTP API commands.

See [Local binaries](../run-locally/local-binaries.md) for runtime ownership,
ports, and recovery details.

## Connect to a hosted service

1. Get the HTTPS endpoint and bearer token from the service operator, then keep
   the token in the environment rather than a shell history or config file:

   ```bash
   export ESHU_SERVICE_URL="https://eshu.example.com"
   export ESHU_API_KEY="<token>"
   ```

2. Verify health, readiness, indexed scope, MCP visibility, and one bounded
   query:

   ```bash
   eshu hosted-setup --platform codex --repository payments-api
   ```

   Add `--json` for the canonical scripting envelope.
3. Query the hosted service through the same remote settings:

   ```bash
   eshu list
   eshu stats payments-api
   ```

See [Hosted onboarding](../deployment/hosted-onboarding.md) for team repository
rules and operator handoff.

## Connect an assistant for the selected runtime

Follow only the instruction for the path you chose.

### Local Compose

Generate the Local Compose HTTP configuration with the exported `ESHU_API_KEY`:

```bash
docker compose exec eshu eshu mcp setup --hosted --platform codex --service-url http://localhost:8081 --auth shared-key
```

Copy the printed `[mcp_servers.eshu]` TOML block into `~/.codex/config.toml`.
Restart Codex with `ESHU_API_KEY` in its environment before asking a question.

### Local binaries

Keep the Codex configuration from the local-binary steps. Do not use either HTTP command for the stdio owner.

### Hosted service

Hosted readers: use the MCP snippet emitted by `eshu hosted-setup` in the
hosted steps above. Do not rerun either local setup command.

After you apply the configuration for your path, ask a narrow question that
requests its evidence:

```text
Use Eshu. List the indexed repositories, then explain what Eshu knows about
payments-api. Include the files and symbols that support the answer.
```

The answer should name the indexed repository and cite concrete graph or
content evidence. Verify it with the surface for your selected runtime:

- For local binaries, ask Codex to call `get_index_status` with `{}`, then call
  `list_indexed_repositories` with `{"limit": 25, "offset": 0}`. Continue when
  the status is `healthy` and the bounded page includes the target repository.
- For Local Compose, rerun `docker compose exec eshu eshu index-status` and
  `docker compose exec eshu eshu list`.
- For a hosted service, rerun `eshu hosted-setup --platform codex --repository
  payments-api` and `eshu list`; the exported `ESHU_SERVICE_URL` and
  `ESHU_API_KEY` select that service.

If local status is `progressing`, follow its reasons and wait for it to drain.
If it is `degraded` or `stalled`, use the diagnostics below before asking
again.

## Troubleshoot a failed run

| Path | Symptom | What to do |
| --- | --- | --- |
| Local Compose | API is unreachable, or the repository is missing or stale | Fix unhealthy services and check the `/fixtures` mount and selector. Pass the managed `local_path` from `eshu list` to `first-run`. |
| Local binaries | Commands or MCP repository status are missing | Reinstall local binaries if needed. Reapply `eshu mcp setup --platform codex`, restart Codex in the target workspace, then call `get_index_status` and bounded `list_indexed_repositories` again. |
| Hosted | The API returns 401 or 403, or the repository is missing or stale | Make the client and server `ESHU_API_KEY` values match. Rerun `eshu hosted-setup --platform codex --repository payments-api`, then `eshu list`. |

For deeper recovery, use [Troubleshooting](../operate/troubleshooting.md), the
[MCP guide](../guides/mcp-guide.md), or [Index repositories](../use/index-repositories.md).

## Prove the stack is working with an evidence bundle

Export a live evidence bundle when you need stack-wide repository, queue, and
provider state beyond the first bounded answer:

```bash
eshu evidence bundle export --live --out evidence-bundle.json
eshu evidence bundle validate --from evidence-bundle.json
```

The live export reads status endpoints, so it cannot be combined with
`--scope`. Treat its redaction result as a screen for known private-data shapes,
not a guarantee that a bundle is safe to publish. Review the file before you
share it. See [Evidence bundles](../reference/evidence-bundle.md) for field and
truth-label details.

## Clean up

If this guide had you start Compose, run `docker compose down`. Closing Codex
ends the stdio MCP session created by the local-binary path. Leave a hosted
service or a runtime that was already running alone.

## Continue from here

- [Ask code questions](../use/code-questions.md)
- [Trace infrastructure](../use/trace-infrastructure.md)
- [Connect MCP](../mcp/index.md)
