# declaration

## Purpose

`go/internal/collector/repo/runtime/declaration` turns the version-pin files a
repository commits, the `engines`, `volta` and `packageManager` sections of its
`package.json` files, the `go` and `toolchain` directives of its `go.mod`
files, the `FROM` base-image tags of its Dockerfiles, and the `setup-*`
`with:` version inputs of its GitHub Actions workflows into durable
`runtime_declaration.version` facts (issue #8815, epic #8807; Dockerfile and
workflow sources issue #8818). One fact is one runtime version declared in one
file. A line that lists several versions is one fact per token.

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
kinds whose tools define it (`package.json` engines, which the manifest
readers pass to `Classify` whole).

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

A `.php-version` file declares the PHP runtime using the `php_version`
source kind. Its value passes the manifest credential guard before emission.

## Manifest sources

`package.json`, `composer.json`, `composer.lock` and `go.mod` are matched by
exact, case-sensitive base name at any depth, so a monorepo emits one set of facts per package directory. A copy
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
| `composer_json_require_php` | `composer.json` `require.php` | `php` |
| `composer_json_platform_php` | `composer.json` `config.platform.php` | `php` |
| `composer_lock_platform_php` | `composer.lock` `platform.php` | `php` |
| `composer_lock_platform_overrides_php` | `composer.lock` `platform-overrides.php` | `php` |
| `pyproject_requires_python` | `pyproject.toml` `[project].requires-python` | `python` |
| `poetry_python` | `pyproject.toml` `[tool.poetry.dependencies].python` | `python` |
| `pipfile_python_version` | `Pipfile` `[requires].python_version` | `python` |
| `pipfile_python_full_version` | `Pipfile` `[requires].python_full_version` | `python` |
| `setup_cfg_python_requires` | `setup.cfg` `[options].python_requires` | `python` |
| `setup_py_python_requires` | `setup.py` bound `setuptools.setup` or `distutils.core.setup` literal keyword | `python` |
| `runtime_txt` | `runtime.txt` `python-<spec>` line | `python` |
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

Python TOML manifests are validated once with the bounded TOML decoder. The
same decoded values feed dependency rows and runtime declarations; source
positions come from TOML syntax nodes. Duplicate keys and malformed documents
emit no runtime fact. `setup.cfg` reads only `[options].python_requires`,
rejects duplicate sections or options, and never evaluates interpolation.
`setup.py` reads only literal `python_requires` keywords on calls proven bound
to setuptools or distutils imports; Python code is never executed. Dynamic
expressions, f-strings, `**kwargs` overrides, and shadowed calls emit nothing.
It counts bound calls past `MaxTokensPerFile` (1,024) without retaining more
values, then skips the whole file with the exact count instead of emitting a
prefix.
`runtime.txt` has a separate exact-name registry entry under metadata-only
content storage: its original body reaches this guarded reader, while content
and file facts keep only the path and source digest. It admits one effective
`python-<spec>` line and keeps that wrapper in `raw_spec`.

Rules that keep a manifest read bounded and fail closed:

- Only the named runtime sections above are read. Any other `engines` key
  (`vscode`, `electron`), a non-string value, `volta.extends`, non-PHP Composer
  dependencies, and every `require`, `replace`, `exclude`, `retract`,
  `godebug` and `tool` line or `( ... )` block member in `go.mod` never emit.
- A repeated key behaves as `JSON.parse` does: the last `engines`, `volta`,
  `packageManager` or member wins, and only that one emits. A repeated `go` or
  `toolchain` directive makes the file ambiguous (the go tool refuses it), so
  neither occurrence emits and every occurrence counts as rejected. A repeat
  that is itself malformed (`go 1.22 extra`) counts too.
- A `package.json` that is not readable JSON (truncated, a trailing comma,
  trailing text, an unquoted key, nested more than 64 levels) emits nothing,
  because the part before the error would pass for the file. `//` and `/* */`
  comments, a byte-order mark and CRLF are accepted.
- **Credential guard.** The content store keeps these manifests under the
  `store` policy, so a value that is not a version must not leave the file in a
  fact. A value is emitted only when it is at most 128 bytes of ASCII from the
  version and range grammar, holds no run of 16 or more letters, digits or
  underscores, and is not a URL. A failing value is dropped and counted, never
  truncated or masked; the file's other declarations still emit. "Failing"
  covers a string that declares nothing: an empty or whitespace-only value, a
  `packageManager` with no `@`, with an empty version after it, or with a name
  corepack does not permit. A value that is not a string (a number, `null`, an
  object) is absent, not rejected. The
  `packageManager` name must be one of the three package managers corepack
  permits (`npm`, `pnpm`, `yarn`), so the name cannot carry file text either;
  any other, `bun` and `deno` included, is rejected and counted.
- `line_number` is the line of the key (`go` and `toolchain` for `go.mod`).
  `order_index` counts declarations already placed on the same line, so a
  minified one-line `package.json` still gets distinct stable keys. Declarations
  come out in a fixed order: engines, volta, `packageManager`, each runtime as
  listed in the table.
- Runtime declaration extraction skips a manifest body over 1 MiB
  (`MaxManifestBytesPerFile`). JSON manifests such as `package.json` and
  Composer files may still be read and decoded by the generic JSON parser up
  to its shared 64 MiB per-file cap before this extraction guard runs.

### PHP collector cost and signals

Performance Evidence (#9308, 2026-10-28): a five-pair, sequential local
benchmark ran the same snapshot-to-fact-stream harness and the same 424-byte
three-file `php-app` fixture on the branch merge base `6d04851df8` and PHP
commit `7c3f9afff3`. The harness SHA-256 was
`68d9f32f4b19a4b9b49fea70a407c84c1d9984abb460e317b865aa0a2ed163a1`;
each built test binary used Go 1.26.10 on Linux arm64, 16 CPUs, no graph or
Postgres backend. Each round ran the base control then the changed binary with
`-test.benchtime=500ms -test.count=1 -test.benchmem`. Load1 stayed below 3.72,
under the 8.0 quiet-host ceiling. A pre-existing idle query-proof container
remained up, so these local timings are diagnostic rather than a production
throughput or no-regression claim.

| Per three-file snapshot and drained fact stream | Base | PHP commit |
| --- | ---: | ---: |
| Median time | 1,623,100 ns | 1,874,785 ns |
| Observed time range | 1,499,039-1,645,786 ns | 1,776,529-1,977,383 ns |
| Fact count | 21 | 28 |
| `runtime_declaration.version` facts | 0 | 5 |
| Allocated bytes per operation, observed range | 435,336-435,510 | 532,736-536,822 |

The five same-round time ratios averaged 1.172 (sample SD 0.040). The new
`.php-version` admission adds one `file` and one `content` fact; the other five
new facts are the PHP declarations. The baseline did not support these sources,
so the measured extra cost accompanies the intended output. The accepted
Composer scan caps each body at 1 MiB and retains guarded declarations rather
than the body; `TestNoteRuntimeDeclarationCandidateRetainsNoManifestBody`
checks the retention boundary. The corpus pin, deterministic digest, and
five-source snapshot stream tests check the changed fact counts and truth.

No-Regression Evidence (#9308 raw-byte identity fix, 2026-10-28): the actual
JSON `Parse` and legacy `ExtractComposerRuntimeEvidence` paths were measured on
the same 180-byte LF `php-app/composer.json` fixture (SHA-256
`374d6651c0573c42d14eb7352eeefd3f34ff715dc67e4da7b5f0a49536ecde10`).
The temporary benchmark harness SHA-256 was
`131a9bba5035b9922816bfebabb7653c0d5ba8789f2293591c3ef83e74b3376d`;
its setup required both PHP values. On Linux arm64, Go 1.26.6, 16 CPUs,
`GOMAXPROCS=1`, each of five rounds ran pre-fix `9f7bb0cceb` then fixed
`16f7b115f2` with `go test ./internal/parser/json -run '^$' -bench
'^BenchmarkComposer(Parse|Extract)LF$' -benchtime=300ms -count=1 -benchmem
-cpu=1` after a 100 ms warmup per binary. The first set was discarded because
an unrelated proof overlapped it. The repeat set ran from 22:26:12 to 22:26:33
UTC with no observed peer at its boundaries, no containers, and maximum sampled
load1 2.01 (the quiet-host ceiling was 8). `Parse` base/head ns/op by round:
32,220/31,943; 31,895/32,522; 31,953/31,812; 31,993/32,073;
31,816/32,028. Medians were 31,953/32,028 ns/op; same-round head/base ratios
averaged 1.0032 (sample SD 0.0110). `Extract` pairs were 13,755/13,607;
13,612/13,714; 13,665/13,529; 13,671/13,939; 13,526/13,772 ns/op;
medians 13,665/13,714, ratio mean 1.0049 (SD 0.0147). Allocations were
unchanged: `Parse` 25,289 B and 390 allocs/op, `Extract` 10,928 B and 200
allocs/op. The difference is within the paired spread. This measures the two
parser paths on an LF fixture; bare-CR correctness is proved by the native
snapshot-to-fact-stream regression, and end-to-end collector time was not
remeasured for this fix.

No-Observability-Change: no metric, span, or log key was added. Existing
`git_runtime_declaration_file_skipped` and
`git_runtime_declaration_values_rejected` WARN logs report bounded skips with
reason, relative path, byte size, and rejected count without raw values. A
malformed Composer body that remains byte-identical, or one that remains over
the 1 MiB cap, reaches the typed skip WARN without marking the generation
`stream_dropped`; an unchanged clean scope can reproject without reparsing.
After a native parser miss, the collector makes a failed-only bounded read to
classify that skip and rechecks it at emission. A transient read failure,
valid fallback body, or changed body marks `stream_dropped` with the affected
relative path so the next sweep recollects it. The existing pre-parse identity
drop metric and WARN log cover file read failures, not this Composer mismatch
branch. The accepted Composer path makes no additional read.
An under-cap parser miss adds at most two 1 MiB plus one byte reads and one
canonical JSON extraction in that generation; a proven over-cap file needs
only size checks. This cost is confined to failed Composer parses.
Queue and graph counts were not measured in this local parser/collector
benchmark; the live golden-corpus gate owns projection proof.

### PHP collector cost and signals

Performance Evidence (#9308, 2026-10-28): a five-pair, sequential local
benchmark ran the same snapshot-to-fact-stream harness and the same 424-byte
three-file `php-app` fixture on the branch merge base `6d04851df8` and PHP
commit `7c3f9afff3`. The harness SHA-256 was
`68d9f32f4b19a4b9b49fea70a407c84c1d9984abb460e317b865aa0a2ed163a1`;
each built test binary used Go 1.26.10 on Linux arm64, 16 CPUs, no graph or
Postgres backend. Each round ran the base control then the changed binary with
`-test.benchtime=500ms -test.count=1 -test.benchmem`. Load1 stayed below 3.72,
under the 8.0 quiet-host ceiling. A pre-existing idle query-proof container
remained up, so these local timings are diagnostic rather than a production
throughput or no-regression claim.

| Per three-file snapshot and drained fact stream | Base | PHP commit |
| --- | ---: | ---: |
| Median time | 1,623,100 ns | 1,874,785 ns |
| Observed time range | 1,499,039-1,645,786 ns | 1,776,529-1,977,383 ns |
| Fact count | 21 | 28 |
| `runtime_declaration.version` facts | 0 | 5 |
| Allocated bytes per operation, observed range | 435,336-435,510 | 532,736-536,822 |

The five same-round time ratios averaged 1.172 (sample SD 0.040). The new
`.php-version` admission adds one `file` and one `content` fact; the other five
new facts are the PHP declarations. The baseline did not support these sources,
so the measured extra cost accompanies the intended output. The accepted
Composer scan caps each body at 1 MiB and retains guarded declarations rather
than the body; `TestNoteRuntimeDeclarationCandidateRetainsNoManifestBody`
checks the retention boundary. The corpus pin, deterministic digest, and
five-source snapshot stream tests check the changed fact counts and truth.

No-Observability-Change: no metric, span, or log key was added. Existing
`git_runtime_declaration_file_skipped` and
`git_runtime_declaration_values_rejected` WARN logs report bounded skips with
reason, relative path, byte size, and rejected count without raw values. A
changed or unparsed Composer body marks the generation `stream_dropped` with
the affected relative path, so the next sweep recollects it; an unparsed file
also reaches the typed skip WARN. The existing pre-parse identity drop metric
and WARN log cover file read failures, not this Composer mismatch branch.
Queue and graph counts were not measured in this local parser/collector
benchmark; the live golden-corpus gate owns projection proof.

## Dockerfile and workflow sources

A Dockerfile `FROM` line declares the base image's tag, and a workflow
step declares its runtime through its `with:` version inputs (whatever the
step's `uses:` names), so both emit through the same contract (issue
#8818). `source_kind` names the section, as for the manifest sources.

| `source_kind` | Read from | `runtime` |
| --- | --- | --- |
| `dockerfile_from` | the tag of each `FROM` line | the image's last path segment (`golang` normalises to `go`) |
| `workflow_setup_version` | each `with:` key ending in `-version`, plus `node-version-file` | the key prefix (`node-version` to `node`) |

| Input | Result |
| --- | --- |
| `FROM node:20.11.1`, `ARG V=20.11.1` + `FROM node:${V}` | `exact` 20.11.1: a global `ARG` default resolves before the `FROM` line is read |
| `FROM python:3.11-slim`, `FROM golang:1.22` | `partial`, only the components written |
| `FROM node:latest`, `FROM debian:bookworm-slim` | `alias`, never resolved |
| `FROM node:${V}`, `FROM node:20-${VARIANT}` | `unresolved`, `raw_spec` the tag as written |
| `with: {node-version: 20}`, `{python-version: "3.11"}` | `partial` 20, `partial` 3.11 |
| `with: {go-version: "${{ inputs.go-version }}"}` | `unresolved`, `raw_spec` verbatim |
| `with: {node-version-file: .nvmrc}` | `alias` `.nvmrc`: the value is a file pointer, recorded as written, never resolved |

Rules that keep both readers bounded and fail closed:

- Admission mirrors the sources that already define the files. A Dockerfile
  is the exact name or a suffixed variant at any depth, case-insensitively,
  outside a dependency directory (the parser registry's `Dockerfile` exact
  and prefix names, which the registry itself matches case-insensitively).
  Suffixed variants read per that prefix rule, so a `Dockerfile.bak` with
  valid `FROM` lines reads too. A workflow is exactly
  `.github/workflows/<name>.yml|yaml`, through the shared
  `ghactionsref.IsWorkflowPath` gate, so composite action files, nested
  workflow directories and other YAML never emit.
- Only `FROM` tags and job-level and step-level `with:` maps are read. A
  `with:` key that does not end in `-version` (a checkout `repository`, a
  `distribution`), a trigger `inputs:` definition, `run:` text, `env:` and
  matrix values never emit. A step is read whatever its `uses:` names: a
  `*-version` input declares a version regardless of which action reads it.
- A `FROM` with no tag (`scratch`, a digest pin, a bare image, a local stage
  name) declares no version and emits nothing. A tag whose image is itself a
  placeholder (`FROM ${BASE}:20`) has no runtime to attribute it to and is
  dropped and counted, never guessed. A tagless reference that still holds a
  placeholder is counted too: a variable is never silently swallowed.
- An unresolved tag outside the version grammar (`${V:?missing}`,
  `$(...)`) is dropped and counted like any other guard rejection, never
  guessed. The same text in `package.json` is dropped the same way: the
  guard is the contract.
- A `with:` value is read only as a string or number scalar. A mapping, a
  sequence, a boolean and null declare nothing and are absent, not rejected,
  as a non-string `package.json` member is. An empty value declares nothing
  and is rejected and counted. A repeated key behaves as `JSON.parse` does:
  the last occurrence wins.
- A `node-version-file` pointer is one alias fact with the path as written.
  Any other `*-version-file` key is not read.
- Every tag and `with:` value passes the same credential guard as a manifest
  value (`safeSpec`): short ASCII version grammar, no opaque run, no URL. A
  failing value is dropped and counted, never truncated or masked; the
  file's other declarations still emit.
- A Dockerfile or workflow body over 1 MiB (`MaxManifestBytesPerFile`) is
  skipped whole and reported, never read as a prefix. A workflow that is not
  readable YAML emits nothing, because the part before the error would pass
  for the file.
- Workflow `line_number` is the value's own YAML line, so a block-style
  value on the next line reports that line. Declarations emit in file order:
  Dockerfile stages re-sorted from the parser's name order back to line
  order, workflow jobs and steps in document order. `order_index` counts
  declarations already placed on the same line.

### Source priority

The `with.*-version` inputs are the lower-priority source: a workflow usually
restates a version the repository pins elsewhere. The declaration contract
carries no priority field, and none was added: reads order by the graph key
(`repo_id`, `relative_path`, `line_number`, `runtime`, `order_index`), and
the reducer writes one row per fact with no dedup. A `workflow_setup_version`
fact is therefore an additional row a consumer deprioritises by filtering
`source_kind`, not a row the pipeline ranks. If a future reader needs ranked
sources, that is a contract change, not a reader default.

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

### Evidence for the Dockerfile and workflow readers (#8818)

No-Regression Evidence: the pin and manifest emission paths are unchanged
apart from additive switch arms for the two new manifest kinds. Benchmarks at
HEAD `30af13f0` against base `8545023c9f` (`go test -run XXX -bench
'BenchmarkEmitPackageJSON$|BenchmarkEmitGoMod$' -benchtime 1000x -count 3`,
linux/arm64): `BenchmarkEmitPackageJSON` 54.3µs to 54.1µs median with 620
allocs on both, `BenchmarkEmitGoMod` 19.7µs to 19.6µs median with 233 allocs
on both. Identical within noise with identical allocation counts.

Performance Evidence: the new readers run once per changed file per
generation under the 1 MiB manifest byte cap, in a single pass, keeping only
the few declarations (heap retention pinned by
`TestNoteRuntimeDeclarationCandidateRetainsNoManifestBody`, extended to both
readers). Near-cap (~1 MiB) worst case at HEAD `30af13f0` (same bench
harness, `-benchtime 100x`): Dockerfile 1.27ms/op with 166 allocs, workflow
8.8ms/op with 260 allocs (a `yaml.v3` decode, linear in lines). Realistic
fixtures (a multi-stage Dockerfile with `ARG`, a five-input workflow) emit in
about 90µs combined.

No-Observability-Change: the package logs nothing itself and adds no metric.
Dockerfile and workflow files flow through the existing skip reasons
(`byte_cap_exceeded`, `token_cap_exceeded`, `manifest_unparseable`,
`value_rejected`) on the existing Git collector WARN logs; no new reason
string, log line, or instrument was added.
