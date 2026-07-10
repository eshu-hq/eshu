# Tutorial: First Successful Run

Use this tutorial when you want one working Eshu runtime, one indexed
repository, and one answer you can trust.

## Outcome

By the end, Eshu has started or connected to a runtime, indexed a repository,
waited for readiness, and returned one bounded answer.

## Time

About 10 minutes for a local path after Docker or local binaries are available.

## Prerequisites

- A Git repository you can index.
- Either Docker Compose, local Eshu binaries, or access to a hosted Eshu
  service.
- For hosted Eshu, an API endpoint and bearer token from your operator.

## Steps

1. Pick the path that matches your runtime:
   - Local Compose for the full local stack.
   - Local binaries when developing from a checkout.
   - Hosted service when someone already operates Eshu for you.
2. For a local Compose run, export a host root and repository selector:

   ```bash
   export ESHU_FILESYSTEM_HOST_ROOT="$HOME/src"
   export ESHU_REPOSITORY_RULES_JSON='{"exact":["eshu"]}'
   export ESHU_API_KEY="local-compose-token"
   ```

3. Run the first-run command:

   ```bash
   eshu first-run
   ```

4. Wait for the command to finish. It should not call the run successful only
   because processes are healthy; it waits for indexing completeness and a
   bounded query answer.
5. Ask one narrow follow-up from the CLI or an assistant:

   ```text
   Use Eshu. List the indexed repositories, then explain what Eshu knows about
   the eshu repository with file and symbol evidence.
   ```

## Expected Result

`eshu first-run` reports success only after it reaches a usable index and a
bounded query returns. For local runs, `eshu list` and `eshu stats <repo>` should
show the indexed repository. For hosted runs, `eshu hosted-setup` should report
that health, readiness, status, MCP tool visibility, and a first query passed.

## Failure Hints

- If health is green but the query fails, wait for indexing readiness rather
  than restarting the API.
- If Compose cannot see repositories, check `ESHU_FILESYSTEM_HOST_ROOT` and
  Docker mount visibility.
- If the CLI reports auth failures, make sure the client token matches the
  runtime's configured API key.
- If local binaries are missing, run `./scripts/install-local-binaries.sh` and
  put `$(go env GOPATH)/bin` on `PATH`.

## Read Next

- [First Successful Run](../getting-started/first-successful-run.md) for the
  full path chooser and detailed commands.
- [Ask Eshu from an assistant](ask-from-assistant.md) when you are ready to use
  MCP.
- [Debug stale answers](debug-stale-answers.md) if the first answer is missing
  or stale.
