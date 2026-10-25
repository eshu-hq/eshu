# pythondep

`pythondep` parses Python package-manager manifests and lockfiles into Eshu's
`content_entity` dependency row contract so the supply-chain reducer can
correlate PyPI vulnerability advisories against repository dependency truth.

## Responsibility

- Parse pip `requirements.txt` (plus `requirements-*.txt` / `requirements_*.txt`).
- Parse pip `constraints.txt` (plus `constraints-*.txt` / `constraints_*.txt`) as
  version policy, not declarations; see
  [Constraints files](#constraints-files).
- Parse `pyproject.toml` PEP 621 `[project]` + `[project.optional-dependencies]`
  tables, Poetry `[tool.poetry.dependencies]`, Poetry group dependencies, and
  Hatch `[tool.hatch.envs.*.dependencies]`.
- Parse `Pipfile` `[packages]` and `[dev-packages]`.
- Parse `poetry.lock` `[[package]]` arrays with optional `[package.source]`
  subtables.
- Parse `uv.lock` `[[package]]` entries with the fail-closed version-1 grammar
  below.

`Pipfile.lock` is JSON; it is parsed by `go/internal/parser/json` and shares
the same row contract so the reducer sees Python lockfile evidence uniformly.

## Row contract

Every row mirrors the npm / composer rows emitted from `go/internal/parser/json`:

- `name` — declared package name (manifest spelling).
- `value` — declared version specifier for manifests; exact version for
  lockfiles; source URL or path for VCS/path/URL entries.
- `section` — the source table or filename (e.g. `requirements`,
  `project.dependencies`, `tool.poetry.group.dev.dependencies`, `packages`,
  `package`, `default`).
- `config_kind` — `dependency` for registry-resolvable entries.
  `vcs_dependency`, `path_dependency`, `url_dependency`, `editable_dependency`,
  and `malformed_dependency` mark provenance that the supply-chain reducer
  MUST NOT admit as registry consumption.
- `package_manager` — always `pypi`. The reducer normalizes through
  [`packageidentity.NormalizeEcosystem`](../../packageidentity).
- `dev_dependency` — `true` for dev/test scopes (Poetry groups named
  dev/test/lint/ci/qa, `[dev-packages]`, `requirements-dev.txt`,
  `develop` lockfile section, etc.).
- `extras`, `marker`, `lockfile`, `source_kind`, `source_url`, `source_ref`,
  `malformed`, `raw` — optional fields populated when applicable.

## Constraints files

`ParseRequirements` also reads a pip constraints file (`constraints.txt`,
`constraints-*.txt`, `constraints_*.txt`; `.txt` only, case-insensitive). pip
applies a constraint only when something else installs the package, so a
constraint never declares a dependency:

- Every row carries `central_version_declaration: true`, the flag a NuGet
  central `PackageVersion` uses. The consumption extractor, the Postgres
  consumption sidecar, and the readiness SQL all drop a flagged row, so a
  constraint produces no `package.consumption`, alert, or `DEPENDS_ON`
  evidence. A registry pin keeps `config_kind=dependency` so its content
  identity matches a requirements row. `direct_dependency` is never set.
- `section` and `dev_dependency` follow the file name the way `requirements-*`
  names do (`constraints-dev.txt` is `section=constraints-dev`, dev scope).
- `-c`, `-r`, and index options (`--index-url https://user:secret@host/...`)
  start with `-`, so they are skipped and never stored in a row.
- A row with a direct source (URL, VCS, path, editable) or an unparsed line
  keeps its `config_kind` and `source_kind` but loses `value`, `source_url`,
  `source_ref`, `marker`, `raw`, and any name that holds `@` or `://`. A
  password may hold a slash or an at sign, so source text cannot be cleaned
  reliably. A registry pin whose specifier holds `@` or `://` is demoted to
  `malformed_dependency` the same way.
- Limit: a non-registry constraint row still reaches the readiness
  unsupported-source gap check, which does not read the flag.
- `requirements*.txt` rows are unchanged. They still keep the source text of a
  direct reference, userinfo included.

## Safety rules

- VCS, path, URL, editable, and malformed entries always reach the payload
  with a non-`dependency` `config_kind` so
  [`package_consumption_correlation`](../../reducer/package_consumption_correlation.go)
  cannot mis-admit them as registry consumption. The reducer admits only
  `entity_metadata.config_kind == "dependency"`.
- The Python interpreter constraint (`python = "^3.10"` under
  `[tool.poetry.dependencies]`) is dropped on the floor instead of being
  emitted as a `python` dependency row, so it cannot mis-match an unrelated
  PyPI advisory.
- Pip resolution (running `pip install`, Poetry's `poetry lock`, or Pipenv's
  `pipenv lock`) is intentionally out of scope. Eshu reports the declared
  evidence as-is.

## Tests

- `constraints_test.go` covers the policy flag, filename scope, the
  requirements negative control, and credential-bearing source lines.
- `requirements_test.go` covers pinned, range, extras, markers, dev-scope
  filename derivation, PEP 508 direct references, VCS, path, URL, editable,
  and malformed entries plus the empty-file safety case.
- `pyproject_test.go` covers PEP 621, Poetry dependencies, Poetry group
  dependencies, Hatch envs, inline-table sources (git/path), and the
  malformed-array case.
- `pipfile_test.go` covers `[packages]` and `[dev-packages]` plus inline-table
  VCS/path entries.
- `poetry_lock_test.go` covers exact-version rows, dev category derivation,
  git source provenance, and directory source provenance.
- `uv_lock_test.go` covers the realistic lock shape, every source form, each
  construct outside the grammar, resynchronisation after an unterminated array,
  the lock-format gate, CRLF, empty input, fork duplicates, and the row cap.
  `uv_lock_bounds_test.go` asserts allocation and scan-length ceilings for a
  10 MB single line, 100,000 entries, deep nesting, and unterminated strings.
  `uv_lock_fuzz_test.go` runs a seeded generator and mutator for 4,000 cases
  against the invariants.

The parent
[`go/internal/parser/dependency_coverage_engine_test.go`](../dependency_coverage_engine_test.go)
binds every covered file in the matrix to the engine dispatch path so the
documented coverage stays provable end-to-end.

## Observability

This package adds no new metric, span, log key, queue, reducer lane, graph
write, or runtime worker. The reducer's existing
`reducer_supply_chain_impact_finding` evidence-source counts and the
supply-chain readiness envelope's `missing_evidence: ["owned_packages"]`
family continue to expose dependency coverage to operators.

## uv.lock grammar

`ParseUVLock` reads a `uv.lock` line by line and resolves a package only when
the entry matches this grammar. Everything else is partial and counted; the
reader never guesses a name, version, or source.

- **Lock format.** The root `version` key must be exactly the integer `1`
  (a trailing `#` comment is allowed). Any other value, a quoted or decimal
  value, a duplicate, or a missing key marks the whole file unsupported: no
  rows, every `[[package]]` counted unresolved. A file that contains a
  triple-quoted string (three double or three single quotes in a row) is
  unsupported the same way, because the line scanner cannot track multi-line
  strings. `uv` writes neither. A root `package` key, or a quoted or dotted
  root key, makes the file unsupported the same way, because it can define
  packages (`package = [{ ... }]`) that this reader does not read.
- **Headers.** A header must be bare keys joined by dots. A quoted header key
  (`[["package"]]`), a `[package]` table, or mismatched brackets counts as one
  unresolved entry, so the file is never `complete` with an unread package.
- **Entry.** `[[package]]` (exact header, optional `#` comment) opens an entry.
  Its keys run until the next header. Only the top-level keys `name`,
  `version`, and `source` are read; other keys (`dependencies`, `wheels`,
  `sdist`, `resolution-markers`, ...) are skipped by bracket depth. Keys inside
  `[package.optional-dependencies]`, `[package.dev-dependencies]`,
  `[package.metadata]` and any other sub-table are ignored, so a look-alike
  `name` or `version` there can never override the entry's own.
- **Keys.** Bare keys only (`A-Za-z0-9_-`). A quoted or dotted key in the
  entry's scope marks the entry unresolved.
- **name.** A basic string, no escapes, `[A-Za-z0-9][A-Za-z0-9._-]*`, at most
  214 bytes. An unreadable or repeated name emits no row and counts as
  unresolved.
- **version.** A basic string, no escapes, digit-led, `[A-Za-z0-9._+!-]*`, at
  most 128 bytes. A repeated or invalid version makes the row malformed. A
  missing version is malformed for an index package and allowed (empty
  `value`) for git, path, directory, editable, virtual, and url sources, which
  can have a dynamic version.
- **source.** A one-line inline table with exactly one of `registry`, `git`,
  `path`, `directory`, `editable`, `virtual`, `url`, plus an optional
  `subdirectory`. Values are basic strings without escapes, at most 2048 bytes;
  URL-like values may not contain whitespace and must start with a
  `scheme://` (a `registry` may instead be a plain path with no `@` or `:`;
  scp-style git values are refused because userinfo cannot be told from a
  host), and a path-like value may not contain `@` or `:` (so a scheme, an
  scp-style `user:secret@host:path`, and a Windows drive path are refused).
  Userinfo is cut at the last `@` before the first `/`. A `?` or `#` before
  that slash with an `@` after it is refused, because a standards parser and
  this reader would disagree about the host
  (`https://evil.example?x@pypi.org/simple`). Any `@` left after the cut, in
  the path, query, or fragment, is refused too: a raw `/` in a password
  (`https://u:S3/CR3T@host/simple`) moves the real userinfo past the cut, so
  a persisted URL never contains an `@`. A refused source makes the row
  malformed and stores none of its text.
- **Project and workspace members.** The first well-formed entry whose source
  is `editable = "."` or `virtual = "."` is the project itself. It is counted
  in `root_packages` and emits no row. A well-formed entry with an `editable`
  or `virtual` source whose PEP 503 normalized name is listed in
  `[manifest] members`, and whose path is relative with no `..` segment, is
  counted in `workspace_members` and emits no row. These are the repository's
  own code; as rows they would read as unsupported local dependencies in
  supply-chain readiness. The members list is trusted only when it is one
  `members` key in a single `[manifest]` table that comes before every
  `[[package]]`, its array closes, and it follows the array grammar:
  basic-string names separated by single commas, with one optional trailing
  comma.
  A missing or doubled comma, any non-string value, or more than 4,096 listed
  names (duplicates included) makes the list untrusted. Then no member is
  omitted and each stays an ordinary row. A `.` entry stays a row when an
  earlier `.` entry, an entry whose source is unreadable, or an unread unit
  came first. A member with a `path`, `directory`, or index source, a member
  path that is absolute or leaves the workspace (`../outside/lib`), and a
  local path outside the members list all stay ordinary rows.
- **Unterminated value.** If an array or table is never closed, any line that
  starts with `[` and the word `package` ends it (`[[package]]` with or
  without a comment, `[package]`, `[package.x]`), because such a line is never
  part of a valid value; scanning resumes at that header. Inside an entry,
  the entry is marked unresolved. Outside an entry (a root array, the
  `members` array, or a sub-table array), the swallowed lines count as one
  unresolved entry, so the state is `partial`. A value still open at end of
  file does the same: it marks its entry unresolved, or counts one unresolved
  entry when it opened outside an entry.
- **Row cap.** At most 10,000 rows. Entries past the cap are counted in
  `dropped_over_cap` and read no further.

### Row mapping

| Source | `config_kind` | `source_kind` | `source_url` | `source_ref` |
| --- | --- | --- | --- | --- |
| `registry` = `https://pypi.org/simple` (optional trailing `/`) | `dependency` | `registry` | the index | none |
| `registry` = any other index or path | `url_dependency` | `registry` | the index | none |
| `git` | `vcs_dependency` | `vcs` | clone URL | commit fragment if hex, else `rev`/`tag`/`branch` |
| `url` | `url_dependency` | `url` | the URL | none |
| `path`, `directory`, `virtual` | `path_dependency` | `path` | the path | none |
| `editable` | `editable_dependency` | `path` | the path | none |
| outside the grammar | `malformed_dependency` | none | none | none |
| the project (`.`) or a `[manifest]` member | no row; counted in `root_packages` or `workspace_members` | | | |

Every row carries `lockfile=true`, `section=package`, `package_manager=pypi`
(the reducer joins PyPI identity through that value), and `line_number` of the
`[[package]]` header. Registry rows set `value` to the exact locked version.
Persisted URLs never include userinfo, a query, or a fragment, because any can
carry a credential or a signed token; each removal is counted in
`redacted_urls`. Only the public PyPI index is `dependency`, because a private
index can serve a package that merely shares a PyPI name and must not match
PyPI advisories.

### Payload state

The payload adds `uv_lock_state` with `state` (`complete` or `partial`),
`entries`, `resolved`, `unresolved`, `dropped_over_cap`, `root_packages`,
`workspace_members`, `redacted_urls`, and `unsupported`.
`resolved + unresolved + dropped_over_cap + root_packages + workspace_members == entries`.

### Known limits

- No `dev_dependency`, `extras`, or `marker` on uv rows. A lock entry does not
  say whether a package is only a development dependency; deriving that needs
  a dependency-graph walk that this reader does not do. The edge lists and
  `resolution-markers` are skipped, not interpreted.
- Resolution forks that lock one package at several versions emit one row per
  entry; the reader does not choose between them.
- The project itself and the `[manifest]` members are counted, not emitted
  (see the grammar). A local path dependency that is not a listed member, or
  whose path is absolute or leaves the workspace, stays a `path_dependency` or
  `editable_dependency` row and reads as an unsupported dependency source in
  readiness.
- Hand-written `uv.lock` files that use TOML features `uv` never writes
  (multi-line inline tables, dotted keys, quoted keys, a `[package]` table, a
  root `package` array) come back partial.
- A source URL with an `@` outside its userinfo, and a local path with `@` or
  `:`, is a malformed row, even where the value is harmless.
- The parser keeps credentials out of rows, but the raw lock body still
  reaches the content store verbatim. `uv` persists credentials for direct
  URLs in `uv.lock`, so those credentials are stored as written until the
  content-store policy gates of #8812 land. `uv.lock` is in the #8812 scope,
  like the `poetry.lock`, `Pipfile.lock`, `yarn.lock`, and
  `package-lock.json` bodies already admitted.
- `constraints*.txt` files are not covered by this reader.

## Performance evidence

No-Regression Evidence: the reader is one forward pass per line with an
integer bracket counter, no recursion, and copies only validated name,
version, and URL fields. `BenchmarkParseUVLock` on 2,000 packages with
dependency and wheel arrays measured about 2.94 MB and 36,019 allocations
per parse, against about 7.30 MB and 60,012 allocations for
`BenchmarkParsePoetryLockBaseline` on the same 2,000 packages (arm64, 16 CPUs,
`-benchtime 200x`; byte and allocation counts repeat across runs, and the uv.lock
reader was faster than the baseline in every run, with wall time varying with
host load). Tests assert, without timing, that a 10 MB single line allocates
under 1 MiB (a `members` line included), that a one-million-line `members`
array stays under 1 MiB and 40,000 allocations, that 100,000 entries allocate
as much as the 10,000-row cap, and that bytes examined stay within three times
the input.

No-Observability-Change: the reader adds no metric, span, or log key. The
`uv_lock_state` payload field and the existing collector parse timing and parse
errors diagnose it, and a partial file shows as `unresolved` and
`dropped_over_cap` counts.
