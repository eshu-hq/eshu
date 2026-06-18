# First Successful Run

Use this guide for your first five minutes with Eshu: one working runtime, one
indexed repository, one proof that indexing finished, and one useful answer.

`eshu first-run` automates that loop. It detects the runtime shape (a reachable
API, local binaries, or Docker Compose), verifies the runtime, indexes a target
repository (or reuses one already indexed), waits for indexing **completeness**
rather than process health, runs one bounded query, and prints next steps. It
reports success only when a bounded query returns an answer.

## Pick one path

There are three first-five-minutes paths. Each ends at the same place: one
useful answer. Pick the row that matches what you are doing.

| Path | Choose when | Runtime |
| --- | --- | --- |
| [Local Compose](#path-1-local-compose) | You are evaluating Eshu and want the full product stack with the HTTP API and MCP service running together. | Containers for Postgres, the graph backend, schema, bootstrap indexing, API, MCP, ingester, and reducer. |
| [Local binaries](#path-2-local-binaries) | You are developing Eshu from a checkout or testing `eshu graph start`. | The local `eshu` owner with embedded Postgres, embedded NornicDB, ingester, and reducer. |
| [Hosted service](#path-3-hosted-service) | A deployed Eshu service already exists and you want an assistant or CLI to connect to it. | A remote API and MCP endpoint you reach over HTTPS with a bearer token. |

The first two paths run Eshu on your machine for local development. The third
connects to a hosted, deployable Eshu that someone has already operated. Keep
that distinction in mind: local paths start a runtime; the hosted path only
connects to one.

## Readiness is not health

This is the single most important idea in onboarding.

- **Health** (`/healthz`) tells you whether processes are alive.
- **Readiness** (`/readyz`) and index completeness tell you whether indexed
  data is ready for questions.

Green health does **not** mean indexing is done. `eshu first-run` and
`eshu hosted-setup` both wait for indexing completeness, not for `/healthz`, and
they only report success when a bounded query actually returns. Never treat a
green health check as "ready to ask questions."

---

## Path 1: Local Compose

The fastest full-product path. Compose starts Postgres, NornicDB, schema
bootstrap, bootstrap indexing, the API on `http://localhost:8080`, the MCP
service on `http://localhost:8081`, the ingester, and the reducer.

### 1. Pick repositories to index

Point Eshu at a parent directory that contains Git repositories:

```bash
export ESHU_FILESYSTEM_HOST_ROOT="$HOME/src"
export ESHU_REPOSITORY_RULES_JSON='{"exact":["eshu"]}'
export ESHU_API_KEY="local-compose-token"
```

`ESHU_FILESYSTEM_HOST_ROOT` must be an absolute path visible to Docker. The
repository rule values are directory names under that root. Setting
`ESHU_API_KEY` makes host CLI and `curl` examples use the same bearer token as
the Compose API and MCP services.

### 2. Run first-run

From the repository root:

```bash
eshu first-run
```

`first-run` detects the Compose file, starts the stack if needed, waits for
indexing completeness, and runs one bounded query. Add `--report` to print a
redacted evidence summary, or `--no-start` to verify an already-running stack
without starting anything.

If you prefer to drive Compose yourself, run `docker compose up --build` in one
terminal and leave it running, then use `eshu first-run --no-start` to prove
readiness and ask the first question.

### 3. Ask a first CLI question

Compose defaults the API to `http://localhost:8080`.

```bash
eshu index-status
eshu list
eshu stats eshu
```

If `eshu stats eshu` does not match your selector, use the exact repository name
or path from `eshu list`.

### 4. Connect MCP and ask an assistant

Point your MCP client at the Compose MCP service or generate a client snippet.
See [Set up MCP for your client](#set-up-mcp-for-your-client). Then ask a narrow
first prompt:

```text
Use Eshu. List the indexed repositories, then explain what Eshu knows about the
eshu repository with file and symbol evidence.
```

---

## Path 2: Local binaries

Use this path when you are developing Eshu or testing `eshu graph start`. It
starts embedded Postgres, embedded NornicDB, the ingester, and the reducer under
one local Eshu owner.

### 1. Install the helper binaries

```bash
git clone https://github.com/eshu-hq/eshu.git
cd eshu
./scripts/install-local-binaries.sh
export PATH="$(go env GOPATH)/bin:$PATH"
```

The installer is required because `go install .../cmd/eshu@latest` installs only
the `eshu` binary, not the `eshu-api`, `eshu-ingester`, `eshu-reducer`,
`eshu-mcp-server`, and other helper binaries local service mode expects on
`PATH`. See [Local binaries](../run-locally/local-binaries.md) for the full list.

### 2. Run first-run

```bash
eshu first-run
```

From a checkout with no reachable API, `first-run` detects the local-binaries
shape, starts the local owner, waits for indexing completeness, and runs one
bounded query. Pass a path to index a different repository:
`eshu first-run /path/to/repo`.

If you want to manage the owner yourself, run
`eshu graph start --workspace-root "$PWD"` in one terminal, then
`eshu first-run --no-start` in another.

### 3. Ask a first CLI question

Read-side CLI commands such as `eshu list`, `eshu stats`, and `eshu analyze`
call the HTTP API. Run `eshu-api` separately, or use Compose, when you need those
API-backed commands. The bounded query that `first-run` runs uses the same API.

### 4. Connect MCP locally

```bash
eshu mcp start --workspace-root "$PWD"
```

This attaches MCP over stdio to the running local owner. Run on its own (without
`eshu first-run` or `eshu graph start` first), `eshu mcp start` boots its own
owner on the `local_authoritative` profile by default — embedded Postgres,
NornicDB, reducer, and ingester in one binary — so graph-backed questions like
"who calls this function?" work on a fresh install. Add
`--profile local_lightweight` for the faster Postgres-only owner. See
[Local MCP](../run-locally/mcp-local.md) and
[Set up MCP for your client](#set-up-mcp-for-your-client).

---

## Path 3: Hosted service

Use this path when a deployed Eshu service already exists and you only need to
connect to it. You connect over HTTPS with a bearer token; you do not start a
runtime.

### 1. Resolve the endpoint and token

Provide the deployed endpoint and a bearer token through flags or environment.
Use placeholders for your own values; never paste a real hostname or token into
shared notes.

```bash
export ESHU_SERVICE_URL="https://eshu.example.com"
export ESHU_API_KEY="${ESHU_API_KEY}"
```

### 2. Run hosted-setup

```bash
eshu hosted-setup
```

`hosted-setup` runs ordered, individually reported checks against the deployed
service: `/healthz`, `/readyz` (which also proves authentication), status and
index readiness, MCP tool visibility, and one bounded query. It reports
**connected** only when the bounded query actually returns; health alone is
never success. It names the specific reason a connection is not yet usable
(unavailable auth, an empty or stale index, a missing repository scope, partial
readiness, or an unavailable MCP surface). The raw bearer token is never
printed.

Useful flags:

- `--repository <name>` requires that repository to be present in the indexed
  scope before reporting success.
- `--platform <name>` also emits a hosted MCP setup snippet for an assistant
  client. The snippet references `${ESHU_API_KEY}` rather than embedding the
  secret.
- `--json` writes the canonical envelope for scripting.

```bash
eshu hosted-setup --platform claude --repository eshu
```

### 3. Ask a first CLI question against the hosted service

```bash
eshu list --service-url "$ESHU_SERVICE_URL"
eshu stats eshu --service-url "$ESHU_SERVICE_URL"
```

`--service-url` and `--api-key` override the resolved endpoint and token when
you are not using environment variables. Then connect an assistant with
[Set up MCP for your client](#set-up-mcp-for-your-client) and ask the same narrow
first prompt as the local paths.

---

## Set up MCP for your client

`eshu mcp setup` prints platform-specific MCP client configuration. By default it
prints a safe snippet and **writes nothing**. Use `--write` to merge the `eshu`
server entry into the platform config (preserving existing servers and keys), and
`--verify` to run staged checks.

```bash
eshu mcp setup --platform claude
```

| Client | Command | Where the snippet goes | `--write` |
| --- | --- | --- | --- |
| Codex CLI | `eshu mcp setup --platform codex` | `~/.codex/config.toml` under `[mcp_servers.eshu]` | Print only |
| Claude Code | `eshu mcp setup --platform claude` | `.mcp.json` (project) or `~/.claude.json` (user) | Supported |
| Cursor | `eshu mcp setup --platform cursor` | `.cursor/mcp.json` (project) or `~/.cursor/mcp.json` (global) | Supported |
| VS Code | `eshu mcp setup --platform vscode` | `.vscode/mcp.json` (`servers` key) | Supported |
| Generic | `eshu mcp setup --platform generic` | Your client's `mcpServers` configuration | Print only |

`--platform generic` is the default. For a hosted service, add `--hosted` with a
resolved `--service-url`:

```bash
eshu mcp setup --platform claude --hosted --service-url https://eshu.example.com
```

Hosted setup never embeds the raw bearer token. Clients that support env-var
references receive a `${ESHU_API_KEY}` reference, so export the variable before
launching the client. After editing a client config, fully restart the client so
it reloads the server list, then verify:

```bash
eshu mcp setup --platform claude --verify
```

`--verify` runs four independent stages so a reachable endpoint is never reported
as a successful query: config generated, client reachable, tools visible, first
query successful.

### Install project-scoped assistant guidance (optional)

`eshu assistant install` writes a clearly delimited managed block that tells AI
assistants to prefer Eshu's bounded MCP/API tools and respect Eshu truth labels.
It never disturbs other content in your instruction files.

```bash
eshu assistant install
eshu assistant status
eshu assistant uninstall
```

Restrict to one assistant with `--platform claude|codex|cursor`. See
[Assistant Guidance Install](../reference/assistant-guidance.md).

---

## Read the first-run report

Add evidence flags to capture what the run proved:

```bash
eshu first-run --report
eshu first-run --report --report-out first-run-evidence.md
eshu first-run --report-out first-run-evidence.json --report-format json
```

You can also regenerate the artifact offline from a saved envelope:

```bash
eshu first-run --json > first-run.json
eshu first-run report --from first-run.json --format md
```

The report states the indexing state as exactly one of `complete`, `partial`,
`stale`, or `failed`. This label is derived from the readiness verdict and the
completeness the run proved, never invented from process health; an unknown or
empty completeness collapses to `failed` so the packet never overstates truth.
The outcome is `succeeded` only when a bounded query returned, otherwise
`incomplete`.

Redaction is mandatory and applied before any value enters the report:
credentials embedded in endpoints become `redacted`, the selected repository is
reduced to its final path element, and tokens are never recorded. Artifacts are
written with owner-only (`0600`) permissions. See
[First-Run Evidence](../reference/first-run-evidence.md) for the full section
table.

To score a run against the onboarding success criteria, pass the envelope to the
benchmark, which rejects "first answers" that are health-only:

```bash
eshu first-run --json | eshu first-run-benchmark --path local_compose
```

See [First five minutes benchmark](../reference/local-testing/first-five-minutes-benchmark.md).

---

## Ask a first assistant question

Once a client is connected, start with a narrow prompt:

```text
Use Eshu. List the indexed repositories, then explain what Eshu knows about the
eshu repository with file and symbol evidence.
```

Then try:

```text
Use Eshu. Find the HTTP API entry point and show the files that wire health,
status, and query routes.
```

For more examples, see [Starter Prompts](../guides/starter-prompts.md).

---

## Troubleshooting

When `eshu first-run` or `eshu hosted-setup` cannot finish, it classifies the
failure and prints concrete recovery steps plus a docs link. The underlying
root-cause error is always preserved. Common classes:

| Symptom | Likely class | Recovery | More detail |
| --- | --- | --- | --- |
| Compose services are not running or unhealthy | Compose unhealthy | `docker compose up -d`, then `docker compose ps`; re-run `eshu first-run` once the API is healthy. | [Docker Compose](../run-locally/docker-compose.md) |
| Docker cannot see the repository paths | Docker repo paths | Mount the host path under `volumes` in the compose file; confirm the target resolves inside the container; re-run. | [Docker Compose](../run-locally/docker-compose.md) |
| `eshu-*` helper binaries missing from `PATH` | Binaries missing | Build with `cd go && make build`, add them to `PATH`, then re-run. | [Local Testing](../reference/local-testing.md) |
| API rejects the request as unauthorized (401/403) | Auth mismatch | Export a matching `ESHU_API_KEY`; confirm the server's configured key matches the client. | [HTTP API](../reference/http-api.md) |
| Health green but answers are stale or indexing still building | Indexing not ready | Wait for the queue to drain (`eshu index-status`), raise the budget with `eshu first-run --timeout 30m`, re-run once indexing completes. | [Health checks](../operate/health-checks.md) |
| Queue has failed, retrying, or dead-letter work | Queue failed work | Inspect with `eshu index-status`, resolve or retry the blocked items, re-run once the queue drains. | [Troubleshooting](../operate/troubleshooting.md) |
| No repositories match the selector | No repositories | Index one with `eshu scan <path>`, widen or correct the selector, confirm with `eshu list`. | [Index repositories](../use/index-repositories.md) |
| MCP endpoint points at the API instead of the MCP service | MCP endpoint is API | Point the client at the MCP path (e.g. `/mcp/message`) or use `eshu mcp start` for stdio; re-run `eshu mcp setup`. | [MCP Guide](../guides/mcp-guide.md) |
| Assistant config exists but the eshu tools are not visible | Assistant tools hidden | Fully restart the assistant, confirm the config is in the path the client reads, re-run `eshu mcp setup --verify`. | [MCP Guide](../guides/mcp-guide.md) |

For deeper local diagnostics, run `eshu doctor`. See
[CLI: System And Configuration](../reference/cli-system.md).
