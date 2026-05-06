# Troubleshooting

## `eshu` is not on PATH

**Cause:** The installation completed, but the scripts directory is not on your PATH.

**Fix:**

Build the CLI from source and add it to your PATH:

```bash
cd go && go build -o bin/ ./cmd/eshu
export PATH="$PWD/bin:$PATH"
```

## Neo4j connection refused

**Cause:** Neo4j is not running, the URI is wrong, or the service is unreachable.

**Fix:**

```bash
docker start eshu-neo4j
```

Then verify your `NEO4J_URI`, username, and password. Run `eshu doctor` to confirm.

## API is healthy but indexing is incomplete

**Cause:** The process health check only proves the API can serve. It does not
mean the latest repository or run reached a completed checkpoint.

**Fix:**

1. Check checkpointed completeness with `eshu index-status`
2. Inspect `GET /api/v0/status/index` or `GET /api/v0/index-status`
3. Inspect the ingester `/admin/status` surface if you are debugging a
   recovery or backlog issue
4. Use `/admin/status` on the long-running runtimes to see live stage and
   backlog state

## MCP client cannot find the server

**Cause:** The client config was not updated, the command points at the wrong interpreter, or database credentials are missing.

**Fix:**

1. Rerun `eshu mcp setup`
2. Inspect the generated config snippet
3. Run `eshu mcp start` manually and confirm it starts cleanly

For this repository's checked-in MCP example, copy `.mcp.json.example` to `.mcp.json`, replace `<REPO_ROOT>` with your checkout path. Note that a host-run `eshu mcp start` with only Neo4j credentials can answer graph queries, but content tools require the PostgreSQL DSN and container workspace mounts.

## HTTP API starts but queries fail

**Cause:** The service can start without a meaningful graph loaded — the repository was never indexed, or the entity ID is wrong.

**Fix:**

1. Confirm indexing completed: `eshu list`
2. Check repositories: `GET /api/v0/repositories`
3. Resolve fuzzy inputs: `POST /api/v0/entities/resolve`

## IaC cache directories polluting the graph

Eshu should skip `.git`, `.terraform`, `.terragrunt-cache`, and similar cache trees automatically.

If you suspect this is not happening:

1. Check your `.eshuignore` patterns and repository-source configuration
2. Re-index with `eshu index <path> --force`
3. Inspect file paths in the graph for hidden cache segments

## Docker Compose deployment is unhealthy

Check these in order:

```bash
# NornicDB connectivity
docker compose logs nornicdb | tail -20

# Bootstrap indexing completion
docker compose logs bootstrap-index | tail -20

# API service health
docker compose logs eshu | tail -20

# Repo sync status
docker compose logs ingester | tail -20

# Resolution engine status
docker compose logs resolution-engine | tail -20
```

Common causes:

- Neo4j not ready before API startup (check depends_on health checks)
- Workspace mount permissions
- Port conflicts — override with `NEO4J_HTTP_PORT`, `NEO4J_BOLT_PORT`, `ESHU_HTTP_PORT`

## Docker Compose cannot see mounted repositories

**Cause:** `ESHU_FILESYSTEM_HOST_ROOT` points at a symlinked path or an unsafe
temporary path.

**Fix:**

1. Use an absolute real directory for `ESHU_FILESYSTEM_HOST_ROOT`
2. Do not use `~` in Compose environment values
3. On macOS, do not use `/tmp`; use a real directory such as
   `$HOME/tmp/eshu-compose-repos`
4. If you copied repositories for Compose testing, copy them into that real
   directory instead of symlinking them there

## Kubernetes deployment is unhealthy

```bash
# Check pod status
kubectl get pods -n eshu

# API logs
kubectl logs -n eshu deployment/eshu-api --tail=50

# Ingester logs
kubectl logs -n eshu statefulset/eshu-ingester --tail=50

# Resolution Engine logs
kubectl logs -n eshu deployment/eshu-resolution-engine --tail=50

# Check events for scheduling or resource issues
kubectl get events -n eshu --sort-by=.lastTimestamp | tail -20
```

Common causes:

- Neo4j secret missing or incorrect credentials
- Postgres DSN not configured or `pg_trgm` extension not enabled
- PVC not bound for the ingester workspace
- Resource limits too low — the ingester and resolution engine need enough memory for large repo indexing and graph projection

## Getting help

If these don't solve the issue, open an issue on [GitHub](https://github.com/eshu-hq/eshu/issues) with the output of `eshu doctor`.
