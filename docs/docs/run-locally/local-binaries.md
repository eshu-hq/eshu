# Local binaries

Use this path when you are developing Eshu, testing `eshu graph start`, or running
one workspace with a local owner.

This mode starts embedded Postgres, embedded NornicDB, the ingester, and the
reducer under one workspace owner. It does not start the full HTTP API unless
you run that separately.

## Full local end-to-end

Use this path from a checkout when you want the local owner to manage the graph,
Postgres, ingester, reducer, and MCP helper binaries for one workspace:

```bash
git clone https://github.com/eshu-hq/eshu.git
cd eshu

./scripts/install-local-binaries.sh
export PATH="$(go env GOPATH)/bin:$PATH"

eshu graph start --workspace-root "$PWD"
```

Leave `eshu graph start` running while you work. It owns the workspace, starts
embedded Postgres, starts embedded NornicDB inside the `eshu` process, launches
`eshu-ingester` and `eshu-reducer` from `PATH`, and prints progress in the
terminal. Stop the owner with `Ctrl-C`, or from another terminal:

```bash
eshu graph stop --workspace-root "$PWD"
```

No local NornicDB install is required for this default path. The script builds
the local owner `eshu` with embedded NornicDB and installs the service binaries
that the owner needs to supervise.

## Install the CLI

For the user-facing CLI, use modern Go install syntax:

```bash
go install -tags nolocalllm github.com/eshu-hq/eshu/go/cmd/eshu@latest
```

Use a pinned version instead of `latest` when you need repeatable installs.
Make sure `$(go env GOPATH)/bin` or `GOBIN` is on `PATH`. A binary installed
with `go install ...@vX.Y.Z` reports that module version through
`eshu --version`. Local source builds still report `dev` unless a version is
injected with `-ldflags`.

The `nolocalllm` tag is intentional. It links the local NornicDB runtime into
`eshu` without pulling in NornicDB's optional local-LLM pieces, so
`eshu graph start` can run the default local graph without a separate
`nornicdb-headless` install.

This installs only the `eshu` binary. For the full local owner workflow, use the
checkout installer above so `eshu-ingester`, `eshu-reducer`, `eshu-mcp-server`,
and the other helper binaries are present on `PATH`.

## Install the full local binary set

`go install` names binaries after the command directory, so `./cmd/api` becomes
`api`, not `eshu-api`. Local owner mode expects the ESHU-prefixed runtime names on
`PATH`, so use the repo installer when you are developing Eshu or running
`eshu graph start` from a checkout:

```bash
./scripts/install-local-binaries.sh
```

By default the script installs to `GOBIN`, or to `$(go env GOPATH)/bin` when
`GOBIN` is unset. It installs `eshu`, `eshu-api`, `eshu-mcp-server`,
`eshu-bootstrap-index`, `eshu-ingester`, `eshu-reducer`, and the supporting
runtime helpers.

Set `ESHU_VERSION=<version>` before running the script when you want the
installed binaries to report a specific build version. The default is `dev`.
Every installed Eshu binary supports a safe version probe:

```bash
eshu --version
eshu-api --version
eshu-ingester -v
eshu-reducer -v
```

The service binaries answer those probes before opening telemetry, Postgres,
the graph backend, queues, or HTTP listeners.

The script builds only the local owner `eshu` binary with
`ESHU_LOCAL_OWNER_BUILD_TAGS=nolocalllm` by default. The service binaries
(`eshu-api`, `eshu-ingester`, `eshu-reducer`, and friends) are plain deployment
style binaries that connect to an external graph endpoint. Set
`ESHU_LOCAL_OWNER_BUILD_TAGS=` only when you deliberately want a plain local
owner build for explicit process-mode testing.

`eshu graph start` discovers `eshu-ingester`, `eshu-reducer`, and
`eshu-mcp-server` through `PATH`, so keep that install directory on `PATH` for
the shell where you start Eshu.

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
future release-backed installs.

## Start a workspace owner

```bash
eshu graph start --workspace-root /path/to/repo
```

This runs in the foreground and prints local progress. Stop it with `Ctrl-C`
when you are done.

## Use MCP with the local owner

If the owner is already running, a stdio MCP process can attach to it:

```bash
eshu mcp start --workspace-root /path/to/repo
```

See [Local MCP](mcp-local.md) for client setup and the difference between local
owner MCP and the Compose MCP service.

## What still needs an API

Read-side CLI commands such as `eshu list`, `eshu stats`, and
`eshu analyze ...` call the HTTP API. Use Docker Compose or run `eshu-api`
separately when you need those API-backed commands.
