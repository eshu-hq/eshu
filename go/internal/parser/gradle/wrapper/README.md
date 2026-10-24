# Gradle Wrapper Properties Parser

## Purpose

This package reads the Gradle version a project pins in
`gradle/wrapper/gradle-wrapper.properties`. The pin is the `distributionUrl`
property; its archive name carries the version, for example
`gradle-8.5-bin.zip`.

## Status

The package is built and tested but **not registered**: no registry entry binds
`gradle-wrapper.properties` to it, and `go/internal/parser/gradle_language.go`
does not route to it. Admitting the file name stores the raw file body in the
content store, and a `distributionUrl` can carry `user:password@` userinfo.
The content store has no write-time redaction, so the admission waits on the
policy in #8812. Wiring it later is one registry name, one dispatch case, and
the re-pinned collector fixture.

## Ownership boundary

The package owns the properties reading only. The parent parser package would
own registry dispatch and the engine hook. The row is not a package
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
(`gradle`) and `distribution_type` (`bin` or `all`). The payload also carries
a `gradle_wrapper_state` envelope with `state` and `unsupported_count`.

## Telemetry

No-Observability-Change: the package emits no metrics, spans or logs. It runs
inside the parent engine's per-file parse. `gradle_wrapper_state` shows an
operator a file that held a `distributionUrl` the grammar could not read.

## Performance

No-Regression Evidence: the package is not registered, so no existing path
runs it. Work is one linear pass over at most 64 KiB; the tests assert that a
10 MB line allocates nothing and that 60 KiB inputs of continuations, escapes
and backslash runs allocate under 16 KiB.

## Verification

`go test -race -count=1 ./internal/parser/gradle/wrapper` runs the grammar
fixtures, the allocation ceilings, a random-byte no-panic pass, and a seeded
generator that checks the row and the absence of every planted marker.
