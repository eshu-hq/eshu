# internal/contentrefs

## Read First

1. `go/internal/contentrefs/README.md`
2. `go/internal/contentrefs/doc.go`
3. `go/internal/contentrefs/hostnames.go`
4. `go/internal/contentrefs/service_names.go`
5. `go/internal/storage/postgres/content_writer_references.go`

## Package Rules

- Extraction MUST remain pure string logic: no file reads, Postgres calls,
  graph writes, telemetry emission, or returned errors.
- Run the line gate before the broad regex. `Hostnames` needs hostname context
  or a URL scheme; `ServiceNames` needs an approved service-context keyword.
- Outputs MUST stay lower-cased where applicable, deduplicated, and sorted so
  Postgres reference writes remain idempotent.
- False-positive filters are part of the contract. Extending extraction
  keywords MUST include tests that keep file extensions, code property chains,
  test matchers, and short hyphenated names out.
- Service-name candidates MUST keep the three-part minimum unless callers and
  lookup-table noise are re-evaluated with tests.

## Proof

- Run `cd go && go test ./internal/contentrefs -count=1` for package changes.
- Run `cd go && go vet ./internal/contentrefs` when exported surface or docs
  move.
- Run `go run ./cmd/eshu docs verify ../go/internal/contentrefs --limit 1400 --fail-on contradicted,missing_evidence`
  for docs changes in this package.
