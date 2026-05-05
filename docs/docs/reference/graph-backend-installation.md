# Graph Backend Installation

Eshu's `local_authoritative` profile and its Compose / production profiles
read the canonical graph through a **graph backend adapter**. The default
adapter is NornicDB. Neo4j remains available as an explicit compatibility
backend. Adoption history lives in
[ADR 2026-04-22 — NornicDB As Candidate Graph Backend](../adrs/2026-04-22-nornicdb-graph-backend-candidate.md).

This page documents how to install a graph backend locally so the Eshu
lightweight host can run the `local_authoritative` profile without Docker
Compose.

## Backend selection

Set the graph backend explicitly via environment variable:

- `ESHU_GRAPH_BACKEND=nornicdb` — default
- `ESHU_GRAPH_BACKEND=neo4j` — explicit Neo4j path

Invalid values are rejected at startup. There is no silent default.

The backend axis is also surfaced optionally in responses as `truth.backend`
and in telemetry span / metric labels as `graph_backend`.

## Profile requirements

| Profile | Graph backend required |
| --- | --- |
| `local_lightweight` | None |
| `local_authoritative` | Yes, embedded in the local `eshu` binary when built with `-tags nolocalllm`; external NornicDB process mode is optional |
| `local_full_stack` | Provided by Compose; NornicDB by default, Neo4j via `docker-compose.neo4j.yml` |
| `production` | Provided by Helm / Kubernetes as an external Bolt-compatible graph endpoint |

## Current local-authoritative runtime

`local_authoritative` defaults to the embedded NornicDB runtime when the `eshu`
binary is built with `-tags nolocalllm`. That is the normal local binary path:
users do not need to install `nornicdb-headless` just to run
`eshu graph start`.

Plain Eshu builds cannot link NornicDB's library-mode runtime today because the
current NornicDB import path still pulls in optional local-LLM linker inputs
unless `nolocalllm` is set. Those builds fail by default with rebuild guidance.
Process mode is available only when selected explicitly.

Process mode remains available for maintainers testing a specific latest-main
NornicDB binary:

```bash
ESHU_NORNICDB_RUNTIME=process \
ESHU_NORNICDB_BINARY=/absolute/path/to/nornicdb-headless \
eshu graph start --workspace-root /path/to/repo
```

Eshu chooses the local NornicDB runtime in this order:

1. `ESHU_NORNICDB_BINARY` set: run that binary as an external process
2. `ESHU_NORNICDB_RUNTIME=process`: run a discovered external process
3. embedded runtime available: run NornicDB inside the `eshu` owner process
4. embedded runtime unavailable: fail with guidance to rebuild with
   `-tags nolocalllm` or select process mode explicitly

When process mode is used, Eshu defaults to the laptop-friendly headless
artifact and discovers binaries in this order:

1. `ESHU_NORNICDB_BINARY`
2. `${ESHU_HOME}/bin/nornicdb-headless` installed by
   `eshu install nornicdb --from <source>`
3. `nornicdb-headless` on `PATH`
4. `nornicdb` on `PATH`

The environment variable stays first so maintainers can temporarily test a
different binary without mutating the managed install.

The full `nornicdb` binary is supported when users opt in with
`ESHU_NORNICDB_BINARY` or place it on `PATH`, but it is not the local-laptop
default because it can carry UI / local-LLM payloads and is materially
larger. Eshu still starts the process with `nornicdb serve --headless` and
`NORNICDB_HEADLESS=true`; those runtime flags disable UI endpoints but do
not make the full binary smaller. Hosted evaluation and future production
packaging may use the full deployment artifact when that profile is
promoted.

Eshu verifies process-mode candidate binaries by running `<binary> version` and
requiring a `NornicDB ...` version string. Both runtime modes generate a random
admin password once per workspace graph data root and store it in
`graph/nornicdb/eshu-credentials.json` with `0600` permissions. The live owner
copies it to `owner.json` so attach processes can connect without a hardcoded
shared secret.

## Managed install

Managed installs are only needed for process mode. Eshu currently tracks the
latest NornicDB `main` branch. For now, that means the truthful process-mode
install path is explicit: build or choose the NornicDB binary you want to
evaluate, then install it with `--from`.

Bare `eshu install nornicdb` is intentionally unavailable while the
process-mode release manifest has no accepted assets. It fails with guidance
to build from NornicDB `main` and install that binary explicitly. This keeps
explicit process-mode runs from silently using an older forked asset.

Explicit `--from` installs can consume any of these artefacts:

- a local NornicDB binary
- a local `.tar`, `.tar.gz`, or `.tgz` archive containing `nornicdb-headless`
  or `nornicdb`
- a local `.pkg` containing `/usr/local/bin/nornicdb` or `nornicdb-headless`
- an `http://`, `https://`, or `file://` URL to one of those artefacts

Remote URL downloads default to a `30s` client timeout and honor `Ctrl-C`.
Set `ESHU_NORNICDB_INSTALL_TIMEOUT=<duration>` when slower links need a larger
budget. The value uses Go duration syntax such as `45s`, `2m`, or `2m30s`.

```bash
eshu install nornicdb --from /absolute/path/to/nornicdb-headless

# Local archive.
eshu install nornicdb \
  --from /absolute/path/to/nornicdb-headless-darwin-arm64.tar.gz \
  --sha256 <expected-archive-sha256>

# Local or remote macOS package.
eshu install nornicdb \
  --from /absolute/path/to/NornicDB-main-arm64-lite.pkg \
  --sha256 <expected-package-sha256>

# Remote artefact URL.
eshu install nornicdb \
  --from https://example.com/releases/nornicdb-headless-darwin-arm64.tar.gz \
  --sha256 <expected-archive-sha256>

# Replace an existing managed binary.
eshu install nornicdb --from /absolute/path/to/nornicdb-headless --force
```

The command performs, in order:

1. Resolves `--from` to a local path or downloads the remote artefact to a
   temporary file. Remote downloads honor `cmd.Context()` cancellation and use
   `ESHU_NORNICDB_INSTALL_TIMEOUT` when set, otherwise `30s`.
2. If the source is a tar archive, extracts the first usable
   `nornicdb-headless` entry, or falls back to `nornicdb` when the archive
   only contains the full binary.
3. If the source is a macOS `.pkg`, expands the package and extracts the
   packaged `usr/local/bin/nornicdb` payload without installing the package
   system-wide.
4. Verifies the resulting binary by running `<binary> version` and requiring a
   `NornicDB ...` version string.
5. Computes the source artefact SHA-256 checksum and compares it with
   `--sha256` when provided.
6. Copies the verified binary to `${ESHU_HOME}/bin/nornicdb-headless` with
   executable permissions.
7. Writes
   `${ESHU_HOME}/graph-backends/nornicdb/manifest.json` with backend,
   version, installed-binary checksum, source checksum, source kind, source
   locator, install mode, and install timestamp.

After installation, `eshu graph status` should report `graph_installed: true`
and the managed binary path.

Because managed installs win over `PATH`, a previously installed binary can
shadow a freshly built `nornicdb-headless` on your shell path. Use one of these
paths when you want to evaluate a new NornicDB `main` build:

- reinstall it with `eshu install nornicdb --from <path> --force`
- set `ESHU_NORNICDB_BINARY=<path>` for a temporary override

## Manual source build options

A developer can build or provide the binary in one of these ways before
calling `eshu install nornicdb --from ...`:

```bash
# Preferred laptop path from a NornicDB checkout when plugin prerequisites
# are installed.
make build-headless
eshu install nornicdb --from /absolute/path/to/NornicDB/bin/nornicdb-headless

# Reliable local fallback when optional local-LLM/plugin prerequisites are
# absent on the laptop.
go build -tags 'noui nolocalllm' -o /tmp/nornicdb-headless ./cmd/nornicdb
eshu install nornicdb --from /tmp/nornicdb-headless

# Explicit opt-in to the larger full binary.
ESHU_NORNICDB_BINARY=/absolute/path/to/nornicdb eshu graph status

# Runtime-only headless mode for a full binary. This disables UI endpoints
# but does not reduce binary size.
NORNICDB_HEADLESS=true nornicdb serve
nornicdb serve --headless
```

Container builds are useful for hosted or Compose-style experiments:

```bash
docker build --build-arg HEADLESS=true -f docker/Dockerfile.arm64-metal .
```

## No-argument install status

```bash
eshu install nornicdb
```

This command is reserved for future release-backed process installs. Normal
local binary users do not need it because embedded NornicDB is part of the
`eshu` binary built with `-tags nolocalllm`. Today the no-argument installer
fails because Eshu tracks latest NornicDB `main` through explicit `--from`
binaries instead of shipping a release manifest asset. When no-argument
installs come back, they must be backed by an accepted manifest entry and
checksum policy.

## Planned release-install hardening

Still planned:

- accepted release or build manifest entries
- remote or release-synchronized manifest updates when needed
- Sigstore / Cosign signature verification
- explicit unsigned-artifact policy
- broader host coverage once those assets are genuinely published

### Upgrade

```bash
eshu graph upgrade --from /absolute/path/to/nornicdb-headless
```

Replaces the installed binary from a verified local executable after the
workspace graph has been stopped. Release-backed semver upgrades remain future
work.

### Rollback

Rollback is not wired yet. For now, keep the prior NornicDB binary somewhere
local and reinstall it explicitly with:

```bash
eshu install nornicdb --from <path> --force
```

### Offline archive or package install

```bash
eshu install nornicdb --from <path-to-tarball-or-pkg>
```

This is now supported for tar archives and macOS packages, but signature verification for offline
artefacts remains future work.

### Uninstall

Uninstall is not wired yet. Remove the managed binary and install manifest
manually only when the workspace owner is stopped and you no longer need that
managed backend entry. Workspace graph data directories are separate from the
binary install and should be preserved unless you are intentionally discarding
local graph state.

## Supply chain

The checked-in release manifest currently has no accepted NornicDB assets.
Latest-main evaluation is explicit-source only.

Still planned before promotion:

- pinned graph-backend version per Eshu release or an equivalent accepted-build
  policy
- artifact SHA-256 checksum
- upstream release URL
- signing policy (`required`, `preferred`, `disabled`)
- Sigstore / Cosign verification metadata
- externalized manifest publication / refresh strategy

The stricter operator override layer is not wired yet.

## Verification

After install, verify with:

```bash
eshu doctor
eshu graph status
```

Both should report the graph backend as present, the binary path, and the
installed version.

After managed install, verify with:

```bash
eshu graph status
```

When building directly from a local NornicDB checkout for laptop testing,
prefer the repository target when its optional plugin prerequisites are
available. If that target fails because local-LLM/plugin libraries are not
installed, use the direct no-UI/no-local-LLM fallback:

```bash
make build-headless
eshu install nornicdb --from /absolute/path/to/NornicDB/bin/nornicdb-headless
eshu graph status

go build -tags 'noui nolocalllm' -o /tmp/nornicdb-headless ./cmd/nornicdb
eshu install nornicdb --from /tmp/nornicdb-headless
eshu graph status
```

## Non-goals

- Installing Neo4j. Neo4j remains an explicit operator-managed compatibility
  path.
- Running process-mode NornicDB as a system service. Process mode is a
  maintainer/test path tied to the Eshu lightweight host lifecycle.
- Installing NornicDB for normal local binary mode. The default local path
  embeds NornicDB in the `eshu` owner process.
