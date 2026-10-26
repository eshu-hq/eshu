# AGENTS.md - internal/parser/gradle/wrapper

## Read first

1. `README.md` - status (registered under `metadata`), grammar, and what reaches the payload.
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
- Keep the file name on its own `gradle_wrapper` definition with the
  `metadata` content-store policy. Binding it to the `gradle` definition, or
  setting its policy to `store`, stores the raw file body.
  `TestDefaultRegistryAdmitsGradleWrapperUnderTheMetadataPolicy` and
  `TestGradleWrapperNameStaysOffTheGradleDefinition` pin this.

## Common changes

- A new accepted form: add the negative and positive case to `scan_test.go`
  first, then the generator case in `bounds_test.go`.
- A change to the emitted row or the grammar: update the README and the
  scanner tests, and re-pin the collector fixture digest (`go test
  ./internal/collector/repo/git/ -run TestGenerationContentVersionPinsCollectorOutput
  -update-content-version-pin`). The re-pin advances the content epoch on its
  own when facts change for unchanged fixture input (#9097). A change that edits
  the fixture file and the output together needs `-advance-content-epoch` on
  the re-pin: the registry fingerprint hashes bindings only, so a logic change
  with unchanged bindings does not move it. A binding change moves the
  fingerprint on its own.
  Keep the fixture under
  `go/internal/collector/repo/git/testdata/content_version/jvm/gradle/wrapper/`.
