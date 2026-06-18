# Local binaries

Use this path when you are developing Eshu, testing `eshu graph start`, or
running one workspace with a local Eshu service.

This mode starts embedded Postgres, embedded NornicDB, the ingester, the
reducer, and the local MCP helper under one local Eshu owner. It does not start
the full HTTP API unless you run that separately.

## Full local end-to-end

```bash
git clone https://github.com/eshu-hq/eshu.git
cd eshu

./scripts/install-local-binaries.sh
export PATH="$(go env GOPATH)/bin:$PATH"

eshu graph start --workspace-root "$PWD"
```

Leave `eshu graph start` running while you work. It manages the workspace, starts
embedded Postgres, starts embedded NornicDB inside the `eshu` process, launches
`eshu-ingester`, `eshu-reducer`, and `eshu-mcp-server` from `PATH`, and prints
progress in the terminal. Stop the local Eshu service with `Ctrl-C`, or from
another terminal:

```bash
eshu graph stop --workspace-root "$PWD"
```

No local NornicDB install is required for this default path. The script builds
the local `eshu` binary with embedded NornicDB and installs the service binaries
that the local Eshu service needs to supervise.

## Use the checkout installer

Do not use `go install .../cmd/eshu@latest` as your first local setup path. That
command installs only the `eshu` binary. It does not install
`eshu-ingester`, `eshu-reducer`, `eshu-mcp-server`, `eshu-bootstrap-index`, or
the other helper binaries that local service mode expects on `PATH`.

Plain `go install` names binaries after the command directory, so `./cmd/api`
becomes `api`, not `eshu-api`. Local Eshu service mode expects the
ESHU-prefixed runtime names on `PATH`, so use the repo installer when you are
developing Eshu or running `eshu graph start` from a checkout:

```bash
./scripts/install-local-binaries.sh
```

By default the script installs to `GOBIN`, or to `$(go env GOPATH)/bin` when
`GOBIN` is unset.

The script installs:

- owner and API binaries: `eshu`, `eshu-api`, `eshu-mcp-server`
- indexing and runtime binaries: `eshu-bootstrap-index`, `eshu-ingester`,
  `eshu-reducer`, `eshu-projector`
- collector and control-plane binaries: `eshu-workflow-coordinator`,
  `eshu-collector-git`, `eshu-collector-confluence`,
  `eshu-collector-terraform-state`, `eshu-collector-package-registry`,
  `eshu-collector-sbom-attestation`, `eshu-collector-pagerduty`,
  `eshu-collector-jira`, `eshu-collector-aws-cloud`,
  `eshu-collector-gcp-cloud`, `eshu-webhook-listener`
- operator helpers: `eshu-bootstrap-data-plane`, `eshu-admin-status`

Set `ESHU_VERSION=<version>` before running the script when you want the
installed binaries to report a specific build version. The default is `dev`.
Every installed Eshu binary supports a safe version probe before opening
telemetry, Postgres, the graph backend, queues, or HTTP listeners:

```bash
eshu --version
eshu-api --version
eshu-ingester -v
eshu-reducer -v
```

The script builds only the local `eshu` binary with
`ESHU_LOCAL_OWNER_BUILD_TAGS=nolocalllm` by default. The service binaries
(`eshu-api`, `eshu-ingester`, `eshu-reducer`, and friends) are plain deployment
style binaries that connect to an external graph endpoint. Set
`ESHU_LOCAL_OWNER_BUILD_TAGS=` only when you deliberately want a plain local
service build for explicit process-mode testing.

`eshu graph start` discovers `eshu-ingester` and `eshu-reducer` through
`PATH`, so keep that install directory on `PATH` for the shell where you start
Eshu. `eshu mcp start` discovers `eshu-mcp-server` through `PATH` when you
start the local MCP surface.

## NornicDB runtime mode

NornicDB is the default local graph backend. For normal local binary installs,
there is nothing else to install: `eshu graph start` uses the embedded
library-mode runtime in the `eshu` process.

Use an external process only when you are testing a specific NornicDB build:

```bash
ESHU_NORNICDB_RUNTIME=process \
ESHU_NORNICDB_BINARY=/absolute/path/to/nornicdb-headless \
eshu graph start --workspace-root /path/to/repo
```

`eshu install nornicdb --from <source>` is still available for process-mode
testing and upgrade workflows. Bare `eshu install nornicdb` remains reserved for
release-backed installs.

## Use MCP with the local Eshu service

If the local Eshu service is already running, MCP can attach to it over stdio:

```bash
eshu mcp start --workspace-root /path/to/repo
```

For a local HTTP MCP endpoint backed by the same owner, pass an HTTP transport
and bind address:

```bash
eshu mcp start --workspace-root /path/to/repo --transport http --host 127.0.0.1 --port 8081
```

See [Local MCP](mcp-local.md) for client setup and the difference between local
Eshu service MCP and the Compose MCP service.

## What still needs an API

Read-side CLI commands such as `eshu list`, `eshu stats`, and
`eshu analyze ...` call the HTTP API. Use Docker Compose or run `eshu-api`
separately when you need those API-backed commands.
