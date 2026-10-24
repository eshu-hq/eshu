# AGENTS.md - internal/parser/gradle/wrapper

## Read first

1. `README.md` - status (not registered), grammar, and what reaches the payload.
2. `scan.go` - `Scan`, the row, and the archive-name grammar.
3. `lines.go` - physical and logical line handling and value assembly.
4. `parse.go` - `Parse`, which ignores `IndexSource`.

## Invariants this package enforces

- Keep the credential contract. Only the version and the distribution type
  leave the package. Never copy the URL, host, userinfo, query or any other
  property into a row, a count, an error message or the payload. `Parse` must
  keep ignoring `options.IndexSource`.
- Fail closed. Emit a row only for the documented archive-name grammar. Count
  everything else in `unsupported_count`.
- Keep every loop linear and the bounds in `types.go`. A value over its bound
  is dropped, never truncated.
- Do not register the file name without the content-store policy of #8812: a
  registry entry stores the raw file body.

## Common changes

- A new accepted form: add the negative and positive case to `scan_test.go`
  first, then the generator case in `bounds_test.go`.
- Registering the name: add `gradle-wrapper.properties` to the `gradle`
  definition's `ExactNames`, route it in `../../gradle_language.go`, add a
  fixture under `go/internal/collector/repo/git/testdata/content_version/`,
  re-pin it, regenerate `docs/public/languages/admitted-files.md`, and remove
  the `knownEcosystemGaps` entry.
