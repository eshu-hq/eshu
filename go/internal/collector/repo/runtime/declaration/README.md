# declaration

## Purpose

`go/internal/collector/repo/runtime/declaration` turns the version-pin files a
repository commits, the `engines`, `volta` and `packageManager` sections of its
`package.json` files, and the `go` and `toolchain` directives of its `go.mod`
files into durable `runtime_declaration.version` facts (issue #8815, epic
#8807). One fact is one runtime version declared in one file. A line that lists
several versions is one fact per token.

## Where this fits

```
sync -> discover -> parse -> emit facts -> enqueue -> reducer -> projection -> query
                                    ^
                              this package
```

Discovery admits the pin files as plain text (#8809), and the parser registry
already admits `package.json` (json) and `go.mod` (gomod). The Git collector
reads their bodies during the content stream and calls into this package
through `go/internal/collector/repo/git/runtime/declaration`: a pin file keeps
its body until emission, a manifest is read when the walk meets it and keeps
only its declarations. The reducer projection
(#8816) and the query tool (#8817) are separate changes, so the kind is
disclosed unconsumed in `go/internal/facts/runtime_declaration.go` until they
land.

## Exported surface

- `Classify` — one version string to a `Spec` (`exact`, `partial`, `range`,
  `alias`, `unresolved`) with parsed parts and range bounds.
- `SourceKindForPath`, `ClassifyPath`, `ScopeDir` — the admitted pin file
  names, how a path is read (`FilePin`, `FileManifest`, `FileNotAdmitted`) and
  the declaring directory in the contract's form (`.` at the repository root).
- `Emit` — one admitted file body to fact envelopes, built from the typed
  `runtimedeclarationv1.Version` through `factschema.EncodeRuntimeDeclarationVersion`.
- `Extract`, `Extraction`, `EmitExtraction` — `EmitFile` split in two, so the
  Git collector can read a manifest when the walk meets it and keep only its
  few declarations until emission.
- `EmitFile` — `Emit` plus the skip signal: a pin file over `MaxBytesPerFile`
  (64 KiB) or `MaxTokensPerFile` (1,024 tokens), a manifest over
  `MaxManifestBytesPerFile` (1 MiB) or a `package.json` that is not readable
  JSON emits nothing and returns a `Skip` (reason, byte size, cap, token
  count). A manifest that lost values to the credential guard still emits its
  other declarations and returns a `Skip` with the rejected count. A prefix is never emitted, because it would pass for the whole file.
  The byte cap is checked before any parsing, and past the token cap the
  parser counts without storing, so memory is bounded by the caps and the
  longest line, not by the body. The byte cap also bounds one line and one
  `raw_spec`.
- `CollectorKind`, `FixtureContext` — envelope provenance.

## Contract rules

| Input | Result |
| --- | --- |
| `20.11.1`, `v20.11.1` | `exact`, parts 20.11.1 (the `v` stays in `raw_spec`) |
| `20`, `3.2`, `20.x` | `partial`, only the components written |
| `>=18 <21` | `range`, lower 18 inclusive, upper 21 exclusive, `version_major` nil |
| `~=3.9` | `range`, lower 3 inclusive, upper 4 exclusive |
| `^14 \|\| >=16` | `range`, lower major 14 inclusive, no upper, `range_lower` is the whole spec |
| `=20`, `= 20`, `== 20` | `partial` 20: a leading `=` or `==` pins one version with or without a space |
| `=v20.1.0`, `= v20.1.0` | `exact` 20.1.0 |
| `=>20`, `=<20`, `===20`, `= latest` | `alias`: not an operator before a version |
| `lts/iron`, `node`, `system`, `stable`, `latest:^1.5` | `alias`, never resolved |
| `${X}`, `$X`, `${{ inputs.node-version }}` | `unresolved`, `raw_spec` verbatim |

### Whitespace-split values

A pin file value is split on whitespace into a list of independent versions:
`.tool-versions` lists fallbacks, the other single-runtime pins hold one. Two
rules (`values.go`) keep one declared expression from being cut into
fragments. They apply to every single-runtime pin, every `.tool-versions` line
after the tool name, a legacy `rust-toolchain` line and a `.sdkmanrc` value.

| Value | Result |
| --- | --- |
| `nodejs 20.11.1 18.19.0` | two `exact` facts, order 0 and 1 |
| `>= 18 < 21` | two `range` facts, `raw_spec` `>= 18` and `< 21`, the same specs as `>=18 <21` |
| `= 20` | one `partial` 20, `raw_spec` `= 20` |
| `18 - 20`, `^14 \|\| >=16`, `20 -`, `>= latest 20` | one `alias` for the whole value at order 0 |
| `${LO} - ${HI}` | one `unresolved` for the whole value |

1. A token that is only a comparator (`=`, `==`, `<`, `<=`, `>`, `>=`, `~`,
   `~>`, `~=`, `^`) is joined to the version token after it.
2. An operator-only token left after that (a hyphen range's `-`, a `||`, a
   comparator with nothing or a name after it) means the value is not a list
   of versions. This package does not read hyphen-range or disjunction
   syntax in a pin file and fails closed: the whole value is one
   declaration, `alias` (or `unresolved` with a placeholder), never a
   guessed version.

`Classify` itself keeps its hyphen-range and disjunction grammar for source
kinds whose tools define it (`package.json` engines, a later change).

### `.swift-version`

The file holds one toolchain on its first effective line. Only a plain numeric
release gets version parts: `5.9` is `partial`, `6.0.1` is `exact`. A release
tag (`swift-5.9-RELEASE`), a development snapshot
(`5.10-DEVELOPMENT-SNAPSHOT-2024-01-01-a`), a channel snapshot
(`main-snapshot-2024-01-01`), a `v` prefix, a wildcard or a comparator stays an
`alias` as written. A snapshot is never reported as the release it precedes.

A placeholder span stays one token even when it holds spaces: `{{ v }}`,
`${{ inputs.node-version }}` and `${ NODE_VERSION }` are each one `unresolved`
fact with `raw_spec` verbatim. Nested `{{ }}` closes on the matching `}}`, and
an unterminated opener keeps the rest of the line as one token. A version then
a placeholder on one line (`20 {{ v }}`) are two tokens.

An empty or comment-only file emits nothing. A tool name with no version emits
nothing. `.npmrc` and every other name off the list emit nothing.

`runtime` is normalised here (`nodejs` to `node`, `golang` to `go`); an unknown
tool keeps its lower-cased token, and `tool_name` carries the verbatim token
when it differed. `.sdkmanrc` `java=17.0.2-tem` is runtime `java`, exact
17.0.2, `distribution` `tem`.

Per-file line policy: `.python-version` reads every line (pyenv), the other
single-runtime pins (`.swift-version` among them) read the first effective
line only, `.tool-versions` and
`.sdkmanrc` read every line, and a `rust-toolchain` TOML body is read through
its `channel` key.

A token that matches none of the version, range or placeholder shapes (for
example `jruby-9.4.0`) is classified `alias`: a named reference is never
guessed into numbers.

## Manifest sources

`package.json` and `go.mod` are matched by exact, case-sensitive base name at
any depth, so a monorepo emits one set of facts per package directory. A copy
inside `node_modules`, `vendor`, `bower_components` or `jspm_packages` never
emits: discovery already prunes those directories, and the emitter refuses them
as well, because a dependency's own `engines` is not this repository's
declaration. `source_kind` names the section, not the file, so a reader can tell
a constraint from a pin.

| `source_kind` | Read from | `runtime` |
| --- | --- | --- |
| `package_json_engines` | `engines.node`, `.npm`, `.yarn`, `.pnpm` | the key |
| `package_json_volta` | `volta.node`, `.npm`, `.yarn`, `.pnpm` | the key |
| `package_json_package_manager` | `packageManager` (`name@version`) | the name: `npm`, `pnpm` or `yarn` |
| `go_mod_go` | the `go` directive | `go` |
| `go_mod_toolchain` | the `toolchain` directive | `go` |

| Input | Result |
| --- | --- |
| `"engines": {"node": ">=18 <21"}`, `"^18 \|\| ^20"`, `"18 - 20"` | `range`, read by `Classify` |
| `"volta": {"node": "20.11.1"}` | `exact` 20.11.1 |
| `"packageManager": "pnpm@9.1.0+sha512.<hex>"` | runtime `pnpm`, `raw_spec` `9.1.0`, `exact`: the integrity suffix is cut and never stored |
| `"lts/*"`, `"latest"`, `"*"`, `"workspace:*"`, `"catalog:"`, `"npm:node@20"` | `alias`, never resolved offline |
| `"${NODE_VERSION}"` | `unresolved`, `raw_spec` verbatim |
| `go 1.22`, `go 1.22.1`, `go 1.21rc1` | `partial`, `exact`, `partial` |
| `toolchain go1.22.3` | `exact` 1.22.3; `raw_spec` keeps the `go` prefix |
| `toolchain default`, `toolchain local`, `toolchain go1.22.3+auto`, `go 1.22.3-custom` | `alias`: a selection rule or an unrecognised form is kept as written, never read as a version |
| `go ${GO_VERSION}` | `unresolved` |

Rules that keep a manifest read bounded and fail closed:

- Only the five sections above are read. Any other `engines` key (`vscode`,
  `electron`), a non-string value, a `volta.extends` and every `require`,
  `replace`, `exclude`, `retract`, `godebug` and `tool` line or `( ... )` block
  member in `go.mod` never emit.
- A repeated key behaves as `JSON.parse` does: the last `engines`, `volta`,
  `packageManager` or member wins, and only that one emits. A repeated `go` or
  `toolchain` directive makes the file ambiguous (the go tool refuses it), so
  neither occurrence emits and every occurrence counts as rejected. A repeat
  that is itself malformed (`go 1.22 extra`) counts too.
- A `package.json` that is not readable JSON (truncated, a trailing comma,
  trailing text, an unquoted key, nested more than 64 levels) emits nothing,
  because the part before the error would pass for the file. `//` and `/* */`
  comments, a byte-order mark and CRLF are accepted.
- **Credential guard.** The content store keeps these two files under the
  `store` policy, so a value that is not a version must not leave the file in a
  fact. A value is emitted only when it is at most 128 bytes of ASCII from the
  version and range grammar, holds no run of 16 or more letters, digits or
  underscores, and is not a URL. A failing value is dropped and counted, never
  truncated or masked; the file's other declarations still emit. The
  `packageManager` name must be one of the three package managers corepack
  permits (`npm`, `pnpm`, `yarn`), so the name cannot carry file text either;
  any other, `bun` and `deno` included, is rejected and counted.
- `line_number` is the line of the key (`go` and `toolchain` for `go.mod`).
  `order_index` counts declarations already placed on the same line, so a
  minified one-line `package.json` still gets distinct stable keys. Declarations
  come out in a fixed order: engines, volta, `packageManager`, each runtime as
  listed in the table.
- A body over 1 MiB (`MaxManifestBytesPerFile`) is skipped before any parsing.

## Known behaviours

These are deliberate and pinned by tests; change them as a contract decision,
not as a drive-by fix.

- `TestPinFileSourceKindsCoverEveryRegistryVersionPin` walks the parser
  registry's `raw_text` exact names. Each one is either in
  `pinFileSourceKinds` or in the test's `rawTextNonPinNames` list, so a pin
  file the registry admits later fails the test until it emits here.
- The parser registry matches the admitted names case-insensitively, while
  `SourceKindForPath` is case-sensitive. A file named `.NVMRC` is admitted as
  plain text by discovery but emits no fact.
- A `#` starts a comment only at the start of a line or after a space or tab.
  `.tool-versions` readers such as asdf cut at any `#`, so `20#x` is one
  `alias` token here with `raw_spec` `20#x`.
- `1.2.3.4.5` is `exact` 1.2.3: parts beyond the third are dropped from the
  parsed numbers and kept in `raw_spec`.
- `20.x.5` is `partial` with major 20, minor nil and patch 5: a wildcard
  component stays nil but the components after it are still read.
- A `#` after a space inside a placeholder span is cut as a comment before
  tokenising, so `{{ a # b }}` reads as the unterminated `{{ a`.
- `raw_spec` is verbatim only for valid UTF-8. A fact payload is JSON, which
  cannot carry an invalid byte, so each invalid UTF-8 byte in a pin file is
  decoded to U+FFFD in `raw_spec`.
- A pin file over 64 KiB is skipped whole even when it holds one token, and a
  file under it with more than 1,024 tokens is skipped whole too; neither
  emits a prefix.
- The credential guard is a heuristic, not a scanner. It rejects what cannot be
  a version, so a short word such as `hunter2`, or a UUID-shaped value such as
  `123e4567-e89b-12d3-a456-426614174000` (hyphens break the opaque-run bound),
  passes in an alias position: an alias is kept as written. It is a bound on what a fact can carry, and the
  body itself stays under the content-store policy.

## Telemetry

The package is pure parsing and logs nothing itself. The fact stream counts the
emitted envelopes through the shared writer. A file over a cap, or a
`package.json` that is not readable, emits nothing, and the Git collector logs
WARN `git_runtime_declaration_file_skipped`. Its fields are `reason`
(`byte_cap_exceeded`, `token_cap_exceeded` or `manifest_unparseable`),
`repository_id`, `scope_id`, `generation_id`, `relative_path` and `byte_size`,
plus `byte_cap` for a byte-cap skip (64 KiB for a pin file, 1 MiB for a
manifest) or `token_count` and `token_cap` for a token-cap skip. A byte-cap skip
has no `token_count`, because the file is never tokenized.

A manifest that lost values to the credential guard still emits the rest, and
the collector logs WARN `git_runtime_declaration_values_rejected` with
`reason=value_rejected`, the same identifiers and `rejected_count`. The log
never carries file content, the rejected value included. Both are log fields
only, with no metric.

## Verification

```bash
cd go && go test -race ./internal/collector/repo/runtime/declaration/... ./internal/collector/repo/git/... -count=1
```

Changes to emitted facts also need the golden-corpus gate (B-7) and the B-12
snapshot; see `docs/public/reference/local-testing/golden-corpus-gate.md`.
