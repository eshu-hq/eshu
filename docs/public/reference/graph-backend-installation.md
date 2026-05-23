# Graph Backend Installation

Use this page only when you need a separate NornicDB binary for explicit
process-mode testing. Normal local Eshu does not need it: the checkout
installer builds the owner `eshu` binary with `-tags nolocalllm`, and
`eshu graph start` runs embedded NornicDB inside that process.

For start, stop, status, logs, and upgrades, use
[Graph Backend Operations](graph-backend-operations.md).

## When To Install

| Scenario | Separate NornicDB install? | Path |
| --- | --- | --- |
| Normal local binary mode | No | Run `./scripts/install-local-binaries.sh`, put the install directory on `PATH`, then run `eshu graph start`. |
| Docker Compose | No | Compose provides the graph service and pins the NornicDB image. |
| Kubernetes or Helm | No | The deployment provides the Bolt-compatible graph endpoint. |
| Specific NornicDB build testing | Yes | Select process mode and point at the binary. |
| Neo4j compatibility | No | Run Neo4j separately and set graph connection variables. |

`ESHU_GRAPH_BACKEND` selects the backend. `nornicdb` is the default; `neo4j` is
the explicit compatibility backend. Invalid values fail startup.

## Local NornicDB Modes

| Mode | Selector | Binary source |
| --- | --- | --- |
| embedded | unset `ESHU_NORNICDB_RUNTIME`, or set it to `embedded` | linked into the `eshu` binary when built with `-tags nolocalllm` |
| process | `ESHU_NORNICDB_RUNTIME=process` | `ESHU_NORNICDB_BINARY`, managed install, or `PATH` |

If embedded mode is requested from an `eshu` binary built without
`-tags nolocalllm`, startup fails with rebuild guidance. It does not fall back
to a process binary.

`ESHU_NORNICDB_BINARY` is not a mode selector. Use it with process mode:

```bash
ESHU_NORNICDB_RUNTIME=process \
ESHU_NORNICDB_BINARY=/absolute/path/to/nornicdb-headless \
eshu graph start
```

Process-mode discovery checks, in order:

1. `ESHU_NORNICDB_BINARY`
2. `${ESHU_HOME}/bin/nornicdb-headless`
3. `nornicdb-headless` on `PATH`
4. `nornicdb` on `PATH`

Each candidate must pass `<binary> version` and print a `NornicDB ...` version
string.

## Managed Process Install

Install or replace the managed process-mode binary:

```bash
eshu install nornicdb --from /absolute/path/to/nornicdb-headless
eshu install nornicdb --from /absolute/path/to/nornicdb-headless --force
```

The command copies the verified binary to:

```text
${ESHU_HOME}/bin/nornicdb-headless
```

and writes:

```text
${ESHU_HOME}/graph-backends/nornicdb/manifest.json
```

Accepted sources are a local executable, `.tar`, `.tar.gz`, `.tgz`, or macOS
`.pkg` containing `nornicdb-headless` or `nornicdb`, or an `http://`,
`https://`, or `file://` URL to one of those artifacts.

Use `--sha256` to verify the source artifact:

```bash
eshu install nornicdb \
  --from https://example.com/releases/nornicdb-headless-darwin-arm64.tar.gz \
  --sha256 <expected-source-sha256>
```

Remote downloads default to `30s`; override only for slow artifact hosts:

```bash
ESHU_NORNICDB_INSTALL_TIMEOUT=2m \
eshu install nornicdb --from https://example.com/nornicdb-headless.tar.gz
```

Managed install wins over `PATH`. To test another binary for one run, set both
`ESHU_NORNICDB_RUNTIME=process` and `ESHU_NORNICDB_BINARY`.

## No-Argument Install

```bash
eshu install nornicdb
```

This intentionally fails today. The embedded release manifest has no accepted
NornicDB assets while Eshu tracks the latest NornicDB main branch for
process-mode tests. The `--full` flag is reserved until release-backed
NornicDB assets exist. Pass an explicit `--from` source instead.

## Build For Process Mode

From a NornicDB checkout:

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

## Verify Process Mode

```bash
ESHU_NORNICDB_RUNTIME=process eshu graph start
eshu graph status
eshu graph logs
```

Use `eshu graph stop` before switching back to embedded mode or replacing the
managed process-mode binary.

## Supply Chain Status

Supported today:

- explicit-source local or remote installs
- optional source-artifact SHA-256 checking with `--sha256`

Future work:

- signature verification
- release-backed no-argument installs
- accepted release/build manifest and artifact publication policy

## Non-Goals

- installing Neo4j
- running NornicDB as a system service
- installing NornicDB for normal embedded local mode
