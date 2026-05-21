# Graph Backend Installation

Use this page when you need to install or replace a local NornicDB binary for
explicit process-mode testing.

Normal local Eshu does not need this page. The local installer builds the
owner `eshu` binary with `-tags nolocalllm`, so `local_authoritative` runs
embedded NornicDB inside the `eshu` process.

For day-to-day start, stop, status, logs, and upgrade commands, use
[Graph Backend Operations](graph-backend-operations.md).

## When Installation Is Needed

| Scenario | Install NornicDB separately? | What to do |
| --- | --- | --- |
| Normal local binary mode | No | Use `eshu graph start`. |
| Docker Compose | No | Compose provides the graph service. |
| Kubernetes or Helm | No | The deployment provides the Bolt-compatible graph endpoint. |
| Testing a specific NornicDB build | Yes | Use process mode and install or point at that binary. |
| Explicit Neo4j compatibility path | No | Operate Neo4j separately and set graph connection variables. |

Backend selection is controlled by `ESHU_GRAPH_BACKEND`:

- `nornicdb` is the default.
- `neo4j` is the explicit compatibility backend.

Invalid backend values fail at startup.

## Local Runtime Modes

Local-authoritative NornicDB has two runtime modes:

| Mode | How to select it | Binary source |
| --- | --- | --- |
| embedded | unset `ESHU_NORNICDB_RUNTIME`, or set it to `embedded` | linked into `eshu` when built with `-tags nolocalllm` |
| process | set `ESHU_NORNICDB_RUNTIME=process` | discovered from `ESHU_NORNICDB_BINARY`, managed install, or `PATH` |

If embedded mode is requested but the `eshu` binary was not built with
`-tags nolocalllm`, startup fails with rebuild guidance. It does not silently
fall back to an external process.

`ESHU_NORNICDB_BINARY` is not a runtime selector. Use it with process mode:

```bash
ESHU_NORNICDB_RUNTIME=process \
ESHU_NORNICDB_BINARY=/absolute/path/to/nornicdb-headless \
eshu graph start
```

Process-mode discovery order:

1. `ESHU_NORNICDB_BINARY`
2. `${ESHU_HOME}/bin/nornicdb-headless`
3. `nornicdb-headless` on `PATH`
4. `nornicdb` on `PATH`

Every candidate must pass `<binary> version` and print a `NornicDB ...`
version string.

## Managed Process-Mode Install

Managed installs copy a verified binary to:

```text
${ESHU_HOME}/bin/nornicdb-headless
```

They also write:

```text
${ESHU_HOME}/graph-backends/nornicdb/manifest.json
```

Install from an explicit source:

```bash
eshu install nornicdb --from /absolute/path/to/nornicdb-headless
```

Install from an archive, package, or URL:

```bash
eshu install nornicdb \
  --from /absolute/path/to/nornicdb-headless-darwin-arm64.tar.gz \
  --sha256 <expected-source-sha256>

eshu install nornicdb \
  --from /absolute/path/to/NornicDB-main-arm64-lite.pkg \
  --sha256 <expected-source-sha256>

eshu install nornicdb \
  --from https://example.com/releases/nornicdb-headless-darwin-arm64.tar.gz \
  --sha256 <expected-source-sha256>
```

Replace an existing managed binary:

```bash
eshu install nornicdb --from /absolute/path/to/nornicdb-headless --force
```

Remote downloads default to `30s` and honor cancellation. Raise the download
timeout only when the artifact source is slow:

```bash
ESHU_NORNICDB_INSTALL_TIMEOUT=2m \
eshu install nornicdb --from https://example.com/nornicdb-headless.tar.gz
```

## Accepted Source Types

`eshu install nornicdb` accepts:

- a local executable NornicDB binary
- a local `.tar`, `.tar.gz`, or `.tgz` archive containing `nornicdb-headless`
  or `nornicdb`
- a local macOS `.pkg` containing `/usr/local/bin/nornicdb-headless` or
  `/usr/local/bin/nornicdb`
- an `http://`, `https://`, or `file://` URL to one of those artifacts

The command verifies the resulting binary, computes source and binary
checksums, compares `--sha256` when provided, writes the managed binary, and
records the install manifest.

Managed install wins over `PATH`. If a managed binary is old, either reinstall
with `--force` or set `ESHU_NORNICDB_BINARY` for one process-mode run.

## No-Argument Install

```bash
eshu install nornicdb
```

This command is reserved for future release-backed installs. Today it fails
because Eshu has no accepted NornicDB release asset manifest checked in. Build
or choose the binary you want and pass it with `--from`.

The `--full` flag is also reserved for that future no-argument release flow.
To install a full NornicDB binary today, pass it explicitly with `--from`.

## Build A Binary For Process Mode

From a NornicDB checkout, prefer the project target when its prerequisites are
installed:

```bash
make build-headless
eshu install nornicdb --from /absolute/path/to/NornicDB/bin/nornicdb-headless
```

Fallback when optional UI or local-LLM prerequisites are missing:

```bash
go build -tags 'noui nolocalllm' -o /tmp/nornicdb-headless ./cmd/nornicdb
eshu install nornicdb --from /tmp/nornicdb-headless
```

The full `nornicdb` binary is supported when explicitly selected, but it is not
the laptop default because it can include larger UI or local-LLM payloads.
Process mode starts it with headless runtime flags.

## Upgrade And Rollback

Upgrade the managed process-mode binary only after stopping the workspace:

```bash
eshu graph stop
eshu graph upgrade --from /absolute/path/to/nornicdb-headless
```

Rollback is a reinstall of a previous binary:

```bash
eshu install nornicdb --from /absolute/path/to/previous-nornicdb-headless --force
```

Workspace graph data is separate from the managed binary. Preserve it unless
you are intentionally discarding local graph state.

## Verify

After install:

```bash
eshu graph status
```

For process mode, start the graph with:

```bash
ESHU_NORNICDB_RUNTIME=process eshu graph start
```

Then check status and logs:

```bash
eshu graph status
eshu graph logs
```

## Supply Chain Status

Current status:

- explicit-source installs are supported
- SHA-256 checking is supported when `--sha256` is supplied
- remote downloads are supported
- signature verification is future work
- release-backed no-argument installs are future work

Before no-argument installs are enabled, Eshu needs an accepted release or
build manifest, checksum policy, signature policy, and artifact publication
strategy.

## Non-Goals

- Installing Neo4j. Neo4j is an operator-managed compatibility backend.
- Running process-mode NornicDB as a system service. The local Eshu service
  owns the process lifecycle.
- Installing NornicDB for normal embedded local mode.
