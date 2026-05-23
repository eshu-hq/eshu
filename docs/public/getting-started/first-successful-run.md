# First Successful Run

Use this guide when you are new to Eshu and want one working local run, one
indexed repository, one proof that indexing finished, and one useful answer.

## What You Will Start

The fastest full-product path is Docker Compose. It starts:

- Postgres for facts, queues, status, content, and recovery data.
- NornicDB as the default graph backend.
- schema bootstrap.
- bootstrap indexing.
- API on `http://localhost:8080`.
- MCP on `http://localhost:8081`.
- ingester and resolution engine.

Use [Local binaries](../run-locally/local-binaries.md) instead when you are
editing Eshu source code or testing `eshu graph start`.

## 1. Pick Repositories To Index

Point Eshu at a parent directory that contains Git repositories:

```bash
export ESHU_FILESYSTEM_HOST_ROOT="$HOME/src"
export ESHU_REPOSITORY_RULES_JSON='{"exact":["eshu"]}'
export ESHU_API_KEY="local-compose-token"
```

`ESHU_FILESYSTEM_HOST_ROOT` must be an absolute path visible to Docker. The
repository rule values are directory names under that root.
Setting `ESHU_API_KEY` makes host CLI and `curl` examples use the same bearer
token as the Compose API and MCP services.

## 2. Start Eshu

From the repository root:

```bash
docker compose up --build
```

Leave this terminal running. The first run builds images, creates schema,
starts Postgres and NornicDB, and runs bootstrap indexing.

## 3. Check Health And Completeness

Health tells you whether processes are alive. Completeness tells you whether
indexed data is ready for questions.

```bash
curl -fsS http://localhost:8080/healthz
curl -fsS http://localhost:8080/readyz
curl -fsS http://localhost:8080/admin/status
curl -fsS -H "Authorization: Bearer $ESHU_API_KEY" \
  http://localhost:8080/api/v0/index-status
```

Do not treat green health as "indexing is done." Wait until index status and
runtime status show no failed, retrying, or dead-letter work for the question
you want to ask.

## 4. Install The Host CLI

Install local CLI and helper binaries when you want to run `eshu` commands from
your host shell:

```bash
./scripts/install-local-binaries.sh
export PATH="$(go env GOPATH)/bin:$PATH"
```

The installer is required for local Eshu development because plain
`go install .../cmd/eshu@latest` installs only the `eshu` binary. It does not
install `eshu-api`, `eshu-ingester`, `eshu-reducer`, `eshu-mcp-server`, or the
other helper binaries local service mode expects.

## 5. Ask A First CLI Question

Compose defaults the API to `http://localhost:8080`.

```bash
export ESHU_API_KEY="${ESHU_API_KEY:-local-compose-token}"
eshu index-status
eshu list
eshu stats eshu
```

If `eshu stats eshu` does not match your repository selector, use the exact
repository name or path from `eshu list`.

## 6. Connect MCP

For the Compose MCP service, point your MCP client at:

```text
http://localhost:8081
```

For local stdio client configuration, print the client snippet:

```bash
eshu mcp setup
```

After editing your MCP client config, restart the client so it reloads the
server entry.

## 7. Ask A First Assistant Question

Use a narrow prompt:

```text
Use Eshu. List the indexed repositories, then explain what Eshu knows about the
eshu repository with file and symbol evidence.
```

Then try:

```text
Use Eshu. Find the HTTP API entry point and show the files that wire health,
status, and query routes.
```

For more examples, use [Starter Prompts](../guides/starter-prompts.md).

## If Something Fails

| Symptom | Next page |
| --- | --- |
| Docker cannot see repositories | [Index repositories](../use/index-repositories.md) |
| Health is green but answers are stale | [Health checks](../operate/health-checks.md) |
| Indexing is slow or noisy | [Tuning playbook](../operate/tuning-playbook.md) |
| MCP client does not connect | [Connect MCP](../mcp/index.md) |
| You want to develop Eshu from source | [Local binaries](../run-locally/local-binaries.md) |
