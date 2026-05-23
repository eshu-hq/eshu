# internal/repositoryidentity

## Read First

1. `go/internal/repositoryidentity/README.md`
2. `go/internal/repositoryidentity/doc.go`
3. `go/internal/repositoryidentity/identity.go`
4. `go/internal/collector/git_fact_builder.go`

## Package Rules

- Repository IDs MUST remain remote-first. Local path is a fallback only when no
  normalized remote URL exists.
- Empty remote URL plus empty local path MUST return an error. Do not invent a
  zero-value repository identity.
- The `repository:r_<8-hex>` prefix and hash length are canonical graph and fact
  contracts. Changing them requires a migration plan across fact payloads,
  graph constraints, query selectors, and tests.
- `NormalizeRemoteURL` is for git remotes, not arbitrary filesystem paths.
  Callers that need full metadata SHOULD use `MetadataFor` so slug, normalized
  remote, absolute path, and ID stay aligned.
- This package MUST stay pure value logic: no network, filesystem scanning,
  Postgres, graph, or collector scheduling behavior.

## Proof

- Add table-driven tests before changing URL normalization, slug extraction, or
  ID inputs.
- Run `cd go && go test ./internal/repositoryidentity -count=1` for package
  changes.
- Run `go run ./cmd/eshu docs verify ../go/internal/repositoryidentity --limit 1400 --fail-on contradicted,missing_evidence`
  for docs changes in this package.
