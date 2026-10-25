# Gradle Wrapper Properties Parser

## Purpose

This package reads the Gradle version a project pins in
`gradle/wrapper/gradle-wrapper.properties`. The pin is the `distributionUrl`
property; its archive name carries the version, for example
`gradle-8.5-bin.zip`.

## Status

The package is registered (#8822). The default registry binds the exact name
`gradle-wrapper.properties` to its own `gradle_wrapper` definition, never to
the `gradle` build-script definition, and that definition uses the `metadata`
content-store policy of #8812. A `distributionUrl` can carry `user:password@`
userinfo, so the content store keeps an empty-body row with the path and the
source digest and never the file. `Engine.parseGradleWrapper` in
`go/internal/parser/gradle_language.go` routes the definition here. The policy
does not govern parser output, so this package, not the policy, keeps the
credential out of facts. The `TestGradleWrapperSeededCredentialReachesNoFact*`
test in `go/internal/collector/repo/git` scans every emitted fact for a seeded
credential under the production binding.

## Ownership boundary

The package owns the properties reading only. The parent parser package owns
registry dispatch and the engine hook. The row is not a package
dependency and the package reducer ignores it (`config_kind` is
`gradle_distribution`, not `dependency`). It is also not a
`runtime_declaration.version` fact: that contract (#8815) is not merged.

## Grammar

The parser reads one property, `distributionUrl`, as a Java properties file:
`key=value`, `key:value` or `key value`; `#` and `!` comment lines; a trailing
odd run of backslashes continues the line, and the next line's leading blanks
are dropped. Keys are case-sensitive. The escapes `\:`, `\=`, `\\`, `\#`, `\!`
and `\ ` are applied; any other escape, including `\uXXXX`, is outside the
grammar.

A row is emitted only when the unescaped value, with any `?query` or
`#fragment` cut off, ends in an archive name `gradle-<version>-bin.zip` or
`gradle-<version>-all.zip`. `<version>` starts with a digit and holds letters,
digits, `.`, `+`, `_`, `-`, up to 64 bytes. Everything before the archive name
is ignored and never stored.

Outside the grammar, the parser emits no row and adds one to
`unsupported_count`: a repeated `distributionUrl`, a value over 2048 bytes
after unescaping, an unknown escape, a dangling continuation, an archive name
that does not match, a version over 64 bytes, a trailing blank (Java keeps it,
and Gradle rejects the URL), or a key written with an escape. A file over
64 KiB is `oversize` and is not scanned.

## What reaches the payload

Only the version and the distribution type. The URL, its host, userinfo, path,
query, and every other property key and value are dropped. `Parse` ignores
`options.IndexSource` and never sets `source`. Tests plant a marker in the
userinfo and in every other property and fail if it appears anywhere in the
payload, with and without `IndexSource`.

## Exported surface

- `Parse(path, isDependency, options) (map[string]any, error)`.
- `Scan(source []byte) Result`.

The row carries `name` (`gradle`), `value` (the version), `line_number`,
`section` (`distribution`), `config_kind` (`gradle_distribution`), `lang`
(`gradle_wrapper`) and `distribution_type` (`bin` or `all`). The payload also carries
a `gradle_wrapper_state` envelope with `state` and `unsupported_count`.

## Telemetry

No-Observability-Change: the package emits no metrics, spans or logs. It runs
inside the parent engine's per-file parse. `gradle_wrapper_state` shows an
operator a file that held a `distributionUrl` the grammar could not read.

## Performance

No-Regression Evidence: registering the name adds one exact-name lookup entry
and no per-file work for other files (the default registry was already governed
by the `.npmrc` binding, so the policy lookup is not new). Work for a wrapper
file is one linear pass over at most 64 KiB; the tests assert that a 10 MB line
allocates nothing and that 60 KiB inputs of continuations, escapes and
backslash runs allocate under 16 KiB. `BenchmarkParseConfig/gradle_wrapper`
parses a 10,000-line file in about 0.66 ms (arm64).

## Verification

`go test -race -count=1 ./internal/parser/gradle/wrapper` runs the grammar
fixtures, the allocation ceilings, a random-byte no-panic pass, and a seeded
generator that checks the row and the absence of every planted marker.
