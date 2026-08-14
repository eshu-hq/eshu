# CLI Docs Verification

## Purpose

Backs `eshu docs verify`. It inventories the Markdown under a path, extracts
the claims those documents make, and checks each one against a truth source:
the CLI's own command tree, the query API's OpenAPI spec plus the routes the
services mount outside it, the environment reference pages, the workspace's
files, its container image manifests, and its Terraform configuration. The
result is a set of findings and evidence packets that can be printed or
committed to Postgres as facts.

The package exists because `go/cmd/eshu` is `package main`. Nothing can import
it, so none of this logic was reachable from a test outside the binary.

## Ownership boundary

This package owns the verification: inventory, truth resolution, persistence,
and rendering to a writer.

It does not own process wiring. Flag parsing, resolving the `auto` image-truth
mode (that reads both flags and the environment), building the API client,
walking the live cobra tree, opening Postgres from the environment, and mapping
a result to an exit code all stay in `go/cmd/eshu/docs.go` and
`go/cmd/eshu/docs_image_api.go`.

## Exported surface

See [`doc.go`](doc.go) for the godoc contract.

- `Verify(ctx, VerifyOptions, Deps) (Result, error)` — the entry point.
- `VerifyOptions`, `Deps`, `Result` — resolved input, injected seams, output.
- `InventoryDocuments`, `Inventory` — the bounded document walk.
- `EnvironmentTruth`, `HTTPEndpointTruth` — the two truth sets built here
  rather than injected.
- `LocalPathResolver`, `LocalContainerImageResolver`,
  `APIContainerImageResolver`, `TerraformAddressResolver`, `TruthRoot` — the
  resolvers handed to the verifier.
- `Persistence`, `PersistenceFactory`, `PersistedGeneration`,
  `PersistenceSummary`, `PostgresPersistence`, `NewPostgresPersistence` — the
  storage seam and its Postgres implementation.
- `ScopeID`, `InventoryFreshnessHint` — scope identity and the freshness
  fingerprint. Exported because `go/cmd/eshu`'s persistence tests build the
  expected values with them.
- `Envelope`, `EnvelopeData`, `EnvelopeError`, `NewEnvelope`, `RenderText`,
  `WriteJSON` — the JSON and text output shapes.
- `EnvelopeGetter` — the one method this package needs from the CLI's API
  client.
- `NormalizeImageTruthMode` — trims and lowercases the mode, defaulting empty
  to `auto`. It does not reject an unknown mode; that is the CLI's flag
  contract.

## Dependencies

`internal/doctruth` (the verifier and claim normalizers), `internal/eshulocal`
(workspace root resolution), `internal/facts` and `internal/scope` (fact and
generation contracts), `internal/storage/postgres` (the ingestion and fact
stores), `internal/query` (the OpenAPI spec), and `hashicorp/hcl/v2` (Terraform
parsing).

The API client is a dependency by interface only: `EnvelopeGetter` is declared
here and satisfied by `go/cmd/eshu`'s `*APIClient`. Postgres arrives as an
already-open `*sql.DB` through `NewPostgresPersistence` — this package never
resolves a DSN.

## Telemetry

None. This package emits no metrics, spans, or logs. It returns findings and
errors; `eshu docs verify` is a foreground command whose output is its signal.

## Gotchas / invariants

- **Filesystem reads only.** No `os.Create`, `os.WriteFile`, `os.Mkdir`,
  `os.Remove`, `os.Rename`, or `os.OpenFile` in non-test code. The only writes
  are to Postgres, via `Persistence.CommitScopeGeneration`. `doc.go` lists
  every read call site; re-derive that list rather than amending it.
- **The image and Terraform scans walk the workspace root, not the scan
  path.** Verifying one README still scans that README's whole workspace. Both
  walks stop at 2000 files and 512 KiB per file.
- **An incomplete scan reports unsupported, never contradicted.** File limit
  reached, oversized file, unreadable file, unparsable HCL — each marks the
  scan incomplete, and an unmatched claim then reads as missing evidence. A
  bounded scan is not evidence of absence.
- **The freshness hint covers the scan bounds, not just the documents.**
  `--max-bytes`, `--limit`, and the effective image truth source are all in the
  fingerprint, because the same documents scanned with different bounds can
  produce different findings and must not be treated as a cache hit for one
  another.
- **Four error returns carry `//nolint:wrapcheck`.** `go/.golangci.yml` exempts
  `cmd/` from wrapcheck but not `internal/cli/`. These four propagate to the
  CLI and are printed verbatim, so wrapping them would silently change the text
  an operator sees.
- **`Deps.ContainerImageResolver` is resolved by the caller.** Choosing between
  the local scan and the API needs flags and the environment, so the wrapper
  picks and this package uses what it is handed.

## Related docs

- [`AGENTS.md`](AGENTS.md) — scoped guidance for agents editing this package.
- [`docs/internal/design/package-restructure.md`](../../../../docs/internal/design/package-restructure.md)
  — the `go/cmd/eshu` family extraction this package came from.
