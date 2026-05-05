# Quickstart

The local setup docs now split the two laptop paths.

If you want one workspace owner from your terminal, use
[Local binaries](../run-locally/local-binaries.md). That path builds the Go
binaries, embeds NornicDB in the local `eshu` owner, and starts
`eshu graph start`.

If you want the full local service stack, use
[Docker Compose](../run-locally/docker-compose.md). That path starts the API,
MCP server, ingester, reducer, bootstrap indexer, Postgres, graph backend,
and relational state. Add the telemetry overlay only when you want a local
OTEL collector and Jaeger.

For MCP client setup, use [Local MCP](../run-locally/mcp-local.md).

`eshu index` invokes `eshu-bootstrap-index`. `eshu list`, `eshu stats`, and
`eshu analyze` need an API process.
