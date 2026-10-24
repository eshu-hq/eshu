# declaration

## Purpose

`go/internal/collector/repo/runtime/declaration` turns the version-pin files a
repository commits into durable `runtime_declaration.version` facts (issue
#8815, epic #8807). One fact is one runtime version declared in one file. A
line that lists several versions is one fact per token.

## Where this fits

```
sync -> discover -> parse -> emit facts -> enqueue -> reducer -> projection -> query
                                    ^
                              this package
```

Discovery admits the pin files as plain text (#8809). The Git collector reads
their bodies during the content stream and calls `Emit` through
`go/internal/collector/repo/git/runtime/declaration`. The reducer projection
(#8816) and the query tool (#8817) are separate changes, so the kind is
disclosed unconsumed in `go/internal/facts/runtime_declaration.go` until they
land.

## Exported surface

- `Classify` — one version string to a `Spec` (`exact`, `partial`, `range`,
  `alias`, `unresolved`) with parsed parts and range bounds.
- `SourceKindForPath`, `ScopeDir` — the admitted pin file names and the
  declaring directory in the contract's form (`.` at the repository root).
- `Emit` — one pin file body to fact envelopes, built from the typed
  `runtimedeclarationv1.Version` through `factschema.EncodeRuntimeDeclarationVersion`.
- `CollectorKind`, `FixtureContext` — envelope provenance.

## Contract rules

| Input | Result |
| --- | --- |
| `20.11.1`, `v20.11.1` | `exact`, parts 20.11.1 (the `v` stays in `raw_spec`) |
| `20`, `3.2`, `20.x` | `partial`, only the components written |
| `>=18 <21` | `range`, lower 18 inclusive, upper 21 exclusive, `version_major` nil |
| `~=3.9` | `range`, lower 3 inclusive, upper 4 exclusive |
| `^14 \|\| >=16` | `range`, lower major 14 inclusive, no upper, `range_lower` is the whole spec |
| `lts/iron`, `node`, `system`, `stable`, `latest:^1.5` | `alias`, never resolved |
| `${X}`, `$X`, `${{ inputs.node-version }}` | `unresolved`, `raw_spec` verbatim |

An empty or comment-only file emits nothing. A tool name with no version emits
nothing. `.npmrc` and every other name off the list emit nothing.

`runtime` is normalised here (`nodejs` to `node`, `golang` to `go`); an unknown
tool keeps its lower-cased token, and `tool_name` carries the verbatim token
when it differed. `.sdkmanrc` `java=17.0.2-tem` is runtime `java`, exact
17.0.2, `distribution` `tem`.

Per-file line policy: `.python-version` reads every line (pyenv), the other
single-runtime pins read the first effective line only, `.tool-versions` and
`.sdkmanrc` read every line, and a `rust-toolchain` TOML body is read through
its `channel` key.

A token that matches none of the version, range or placeholder shapes (for
example `jruby-9.4.0`) is classified `alias`: a named reference is never
guessed into numbers.

## Telemetry

No-Observability-Change. The package is pure parsing. The fact stream counts
the emitted envelopes through the shared writer.

## Verification

```bash
cd go && go test -race ./internal/collector/repo/runtime/declaration/... ./internal/collector/repo/git/... -count=1
```

Changes to emitted facts also need the golden-corpus gate (B-7) and the B-12
snapshot; see `docs/public/reference/local-testing/golden-corpus-gate.md`.
