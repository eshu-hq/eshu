# Installation

Install Eshu by choosing the local path you need.

Use [Local binaries](../run-locally/local-binaries.md) when you are developing
Eshu or want one workspace owner. That page has the current Go build commands,
the embedded local NornicDB path, and the `eshu graph start` workflow.

Use [Docker Compose](../run-locally/docker-compose.md) when you want the full
local service stack. Compose starts Postgres, the graph backend, API, MCP
server, ingester, reducer, and bootstrap indexer. Add the telemetry overlay
when you want a local OTEL collector and Jaeger for developer or DevOps
testing.

NornicDB is the default graph backend. Neo4j remains the explicit official
alternative.

After installing the binaries, `eshu doctor` checks the local CLI and helper
binary wiring. Graph checks depend on whether the selected backend is running.
