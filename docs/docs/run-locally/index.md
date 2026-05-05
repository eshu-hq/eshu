# Run locally

Use the local docs when you want Eshu running on your laptop. There are two
paths, and they solve different problems.

| Path | Use it when | What starts |
| --- | --- | --- |
| [Local binaries](local-binaries.md) | You are developing Eshu or want one local Eshu service | embedded Postgres, embedded NornicDB, ingester, reducer |
| [Docker Compose](docker-compose.md) | You want the full laptop service stack | Postgres, graph backend, API, MCP server, ingester, reducer, bootstrap indexer |

If you are not sure, start with Docker Compose. It gives you the same service
shape you will later deploy for a team.

Use local binaries when you need to test the `eshu graph start` workflow, debug
runtime behavior, or work on Eshu itself.

## Storage

The default graph backend is NornicDB. Neo4j is the explicit official
alternative. Postgres stores relational state, facts, queues, status, content,
and recovery data.

## Local commands and API commands

`eshu index` invokes the `eshu-bootstrap-index` binary and writes to the
configured graph and Postgres stores.

`eshu list`, `eshu stats`, and `eshu analyze ...` call the API. In Docker Compose,
that API is available at `http://localhost:8080`.

## Next steps

- [Run local binaries](local-binaries.md)
- [Run Docker Compose](docker-compose.md)
- [Connect MCP locally](mcp-local.md)
- [Index repositories](../use/index-repositories.md)
