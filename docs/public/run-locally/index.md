# Run locally

Use these docs when you want Eshu running on your laptop. Pick one path first;
the details live on the linked page.

| Path | Start here when | Runtime shape |
| --- | --- | --- |
| [Docker Compose](docker-compose.md) | You are evaluating Eshu, testing the full product stack, or need the HTTP API and MCP services running together. | Containers for Postgres, the graph backend, schema migration, bootstrap indexing, API, MCP server, ingester, and reducer. |
| [Local binaries](local-binaries.md) | You are developing Eshu, testing `eshu graph start`, or running one workspace-local service from a checkout. | The local `eshu` owner process starts embedded Postgres, embedded NornicDB, `eshu-ingester`, and `eshu-reducer`. |

## Quick choice

- Start with [Docker Compose](docker-compose.md) when you want the easiest
  full-stack run or need API-backed CLI commands such as `eshu list`,
  `eshu stats`, `eshu find ...`, `eshu analyze ...`, `eshu map`, or
  `eshu trace service ...`.
- Start with [Local binaries](local-binaries.md) when you want fast rebuilds
  from source or want to test the local owner service that `eshu graph start`
  manages.
- Use [Local MCP](mcp-local.md) after the local owner service is running and
  you want an assistant or editor client to attach to the workspace.

## Important setup difference

Docker Compose builds and starts the service containers. Local binaries use
`./scripts/install-local-binaries.sh` because `go install .../cmd/eshu@latest`
installs only the `eshu` binary, not the Eshu-prefixed helper binaries that
local service mode expects on `PATH`.

## Next Steps

- [Run Docker Compose](docker-compose.md)
- [Run local binaries](local-binaries.md)
- [Connect MCP locally](mcp-local.md)
- [Index repositories](../use/index-repositories.md)
