# Scripts

This directory holds local verification and helper scripts for Eshu maintainers.
Most scripts assume they are run from a fresh checkout with Go, Docker,
Postgres client tools, and `rg` available.

Use `install-local-binaries.sh` when you need the full local binary set on
`PATH` with the same names Eshu expects at runtime: `eshu`, `eshu-api`,
`eshu-mcp-server`, `eshu-ingester`, `eshu-reducer`, and the supporting helper
binaries.

`install-local-binaries.sh` builds only the local owner `eshu` binary with
`ESHU_LOCAL_OWNER_BUILD_TAGS=nolocalllm` by default so local-authoritative mode
embeds NornicDB in the owner process. The service binaries are built plainly,
matching deployment mode. Set `ESHU_LOCAL_OWNER_BUILD_TAGS=` only when you
intentionally want a plain local owner for explicit process-mode testing.

Set `ESHU_VERSION=<version>` to embed a specific version string. The script
defaults to `dev`. Every installed Eshu binary accepts `--version` and `-v`;
service binaries answer before opening telemetry, Postgres, graph, queues, or
listeners, so the check is safe in local scripts and container probes.

The `verify_*_compose.sh` scripts are developer and DevOps proof lanes. They
start their own Compose project, choose ports, and tear the stack down unless
`ESHU_KEEP_COMPOSE_STACK=true` is set.
