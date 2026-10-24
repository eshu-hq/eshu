# pythondep

`pythondep` parses Python package-manager manifests and lockfiles into Eshu's
`content_entity` dependency row contract so the supply-chain reducer can
correlate PyPI vulnerability advisories against repository dependency truth.

## Responsibility

- Parse pip `requirements.txt` (plus `requirements-*.txt` / `requirements_*.txt`).
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
  strings. `uv` writes neither.
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
  URL-like values may not contain whitespace and need a `://` scheme (a
  `registry` may instead be a plain path with no `@` or `:`, and scp-style git
  values are refused because userinfo cannot be told from a host), and a
  path-like value may not contain `://`. Userinfo is cut at the last `@` before
  the first `/`, so a raw `#` or `?` in a password cannot leave a prefix.
- **Unterminated value.** If an array or table is never closed, the next exact
  `[[package]]` line ends it, the entry is marked unresolved, and scanning
  resumes there. A value still open at end of file marks its entry unresolved.
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
`entries`, `resolved`, `unresolved`, `dropped_over_cap`, `redacted_urls`, and
`unsupported`. `resolved + unresolved + dropped_over_cap == entries`.

### Known limits

- No `dev_dependency`, `extras`, or `marker` on uv rows. A lock entry does not
  say whether a package is only a development dependency; deriving that needs
  a dependency-graph walk that this reader does not do. The edge lists and
  `resolution-markers` are skipped, not interpreted.
- Resolution forks that lock one package at several versions emit one row per
  entry; the reader does not choose between them.
- The workspace root and members appear as editable or virtual rows.
- Hand-written `uv.lock` files that use TOML features `uv` never writes
  (multi-line inline tables, dotted keys, quoted keys) come back partial.
- `constraints*.txt` files are not covered by this reader.

## Performance evidence

No-Regression Evidence: the reader is one forward pass per line with an
integer bracket counter, no recursion, and copies only validated name,
version, and URL fields. `BenchmarkParseUVLock` on 2,000 packages with
dependency and wheel arrays measured 3.19 ms, 2.94 MB, and 36,017 allocations
per parse, against 4.74 ms, 7.30 MB, and 60,012 allocations for
`BenchmarkParsePoetryLockBaseline` on the same 2,000 packages (arm64, 16 CPUs,
`-benchtime 200x`, three runs within 0.1% of each other). Tests assert, without
timing, that a 10 MB single line allocates under 1 MiB, that 100,000 entries
allocate as much as the 10,000-row cap, and that bytes examined stay within
three times the input.

No-Observability-Change: the reader adds no metric, span, or log key. The
`uv_lock_state` payload field and the existing collector parse timing and parse
errors diagnose it, and a partial file shows as `unresolved` and
`dropped_over_cap` counts.
