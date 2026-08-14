<!-- docs-catalog
title: First Successful Run
description: Gets a new reader to one working runtime, one indexed repository, and one useful answer.
type: how-to
audience: new-user
time: 5 minutes
entrypoint: true
landing: false
-->

# Choose your first successful run

Use this guide to reach one useful answer from one indexed repository with the
runtime you already have or plan to start.
`eshu first-run` detects a reachable API, local binaries, or Docker Compose. It
verifies the runtime, indexes the target or reuses a drained index, waits for
indexing completeness, and runs a bounded query. It does not start a runtime.

## Before you begin

Choose the path that matches the service you will use:

| Path | Choose it when |
| --- | --- |
| [Local Compose](#run-with-local-compose) | You want the full API and MCP stack in containers. |
| [Local binaries](#run-with-local-binaries) | You are developing Eshu from a checkout. |
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

   Replace `$HOME/src` and `payments-api` with your own values. Wait until the
   API at `http://localhost:8080` is healthy.
2. Run the guided check inside the Compose API service. The container uses the
   canonical mounted repository path and the same stores as the running stack:

   ```bash
   docker compose exec eshu eshu first-run /fixtures/payments-api
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

Keep the following three processes in separate terminals so the foreground
services remain running while the guided check executes.

### Terminal 1: start the local owner

```bash
eshu graph start --workspace-root "$PWD"
```

### Terminal 2: start the HTTP API

```bash
eshu api start
```

### Terminal 3: run the guided check

```bash
eshu first-run .
```

Use `eshu first-run --no-start .` when you want the error message to record
that safe mode was requested. The command only verifies in either mode. Confirm
the result with `eshu list` and `eshu stats <repo>`.

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
   eshu list --service-url https://eshu.example.com
   eshu stats payments-api --service-url https://eshu.example.com
   ```

See [Hosted onboarding](../deployment/hosted-onboarding.md) for team repository
rules and operator handoff.

## Connect an assistant for the selected runtime

Follow only the instruction for the path you chose.

### Local Compose

Connect Codex to the Compose MCP service over HTTP. The `--hosted` flag selects
the remote HTTP transport even though this service runs on your machine.
`--auth shared-key` reads the local `ESHU_API_KEY` exported above.

```bash
eshu mcp setup --hosted --platform codex --service-url http://localhost:8081 --auth shared-key
```

### Local binaries

Generate a Codex configuration that launches the local stdio MCP process:

```bash
eshu mcp setup --platform codex
```

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
content evidence. If it does not, confirm the scope with `eshu list`, wait for
`eshu index-status` to report a drained index, and ask again.

## Troubleshoot a failed run

| Symptom | What to do |
| --- | --- |
| API is unreachable | Start the chosen runtime, check its health, then rerun `eshu first-run`. |
| Compose cannot see the repository | Check that `ESHU_FILESYSTEM_HOST_ROOT` mounts the parent directory at `/fixtures`, then rerun `docker compose exec eshu eshu first-run /fixtures/payments-api`. |
| Local commands are missing | Rerun `./scripts/install-local-binaries.sh` and update `PATH`. |
| API returns 401 or 403 | Make the client and server `ESHU_API_KEY` values match. |
| Health is green but the answer is stale | Wait for `eshu index-status` to drain; do not treat health as readiness. |
| No repository matches | Run `eshu scan <path>`, correct the selector, and check `eshu list`. |
| MCP tools are missing | Run `eshu mcp setup --verify`, then restart the client. |

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

If this guide had you start Compose, run `docker compose down`. If it started the
local owner, stop `eshu api start` with Ctrl-C and run
`eshu graph stop --workspace-root "$PWD"`. Leave a hosted service or a runtime
that was already running alone.

## Continue from here

- [Ask code questions](../use/code-questions.md)
- [Trace infrastructure](../use/trace-infrastructure.md)
- [Connect MCP](../mcp/index.md)
