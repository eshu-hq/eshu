# pythondep agent rules

This directory parses Python package-manager manifests and lockfiles into
`content_entity` dependency rows. The full architecture and row contract
lives in [README.md](README.md); these are the in-tree rules agents MUST
follow when editing this package.

## MUST

- Keep `package_manager = "pypi"` on every emitted row so the supply-chain
  reducer's PEP 503 normalization (`packageidentity.EcosystemPyPI`) joins
  with PyPI registry identity.
- Use `config_kind = "dependency"` ONLY for registry-resolvable entries.
  VCS/path/url/editable/malformed rows MUST use one of the documented
  `*_dependency` sibling values; the reducer trusts that distinction to keep
  unproven provenance out of consumption decisions.
- Preserve `extras`, `marker`, `lockfile`, `source_kind`, `source_url`,
  `source_ref`, `malformed`, and `raw` fields as documented. Other
  consumers (security-intelligence, future reducers) depend on the shape.
- Update [`go/internal/parser/json/dependency_coverage.go`](../json/dependency_coverage.go)
  when graduating or regressing a file's coverage status. The matrix is the
  single source of truth for the documented contract.
- Add fixture coverage to the parser-level `*_test.go` file AND to the
  engine matrix test
  [`../dependency_coverage_engine_test.go`](../dependency_coverage_engine_test.go)
  when adding a new file the registry routes through this package.

## MUST NOT

- Invent a resolved PyPI version for a VCS, path, URL, or editable entry.
  The issue scope is explicit: do not treat unresolved provenance as
  registry-version evidence.
- Run `pip`, `poetry`, or `pipenv` to resolve a graph. The package only
  parses declared evidence.
- Emit a `python = "..."` row from `[tool.poetry.dependencies]` — that key
  is the Python interpreter constraint, not a PyPI package, and would
  mis-match PyPI advisories.
- Treat empty or whitespace-only files as "no dependencies." An empty file
  surfaces as a zero-row payload so the readiness envelope can report
  missing evidence rather than safe.

## uv.lock rules

- `uv_lock*.go` is a fail-closed reader. A row resolves only when the entry
  matches the grammar in [README.md](README.md#uvlock-grammar). Do not widen the
  grammar to accept a construct "that is probably fine"; count it as
  unresolved instead.
- Keep every loop linear in the input. Use the integer bracket counter in
  `scanValueBytes`; never recurse, never rescan accumulated text, never
  allocate per entry past the row cap (`uvParser.scratch` backs the open
  entry for that reason). `uv_lock_bounds_test.go` fails on an allocation or
  scan-length regression.
- Only `https://pypi.org/simple` may produce `config_kind=dependency`. Do not
  persist userinfo, a query, or a fragment from a URL, and never persist any
  text that contains an `@`: refuse the source instead.
- Count the project itself (`editable`/`virtual` `"."`) and trusted
  `[manifest] members` in `root_packages`/`workspace_members`; never emit them
  as rows, because readiness reads an editable or path row as an unsupported
  dependency gap. A genuine local path dependency stays a row. Keep
  `resolved + unresolved + dropped_over_cap + root_packages + workspace_members == entries`.
- A package form the reader does not read (quoted header key, `[package]`
  table, root `package` key) must make the state partial, never `complete`
  with zero entries. The same holds for a value left open outside an entry:
  count the swallowed lines as one unresolved unit (`countUnreadUnit`) at the
  resync and at end of file. Resync on any line `startsPackageHeader`
  accepts, not only the exact `[[package]]` text.
- Trust a `[manifest] members` list only when it follows the array grammar
  (single commas, one trailing comma) and lists at most `uvLockMaxMembers`
  names, counting duplicates. Never allocate per listed name: normalise into
  `uvWorkspace.scratch` and write the map only for a new distinct name.
  Omit a member only when its path is relative with no `..` segment.
- Any entry or unit the reader could not read exactly, whose source may be
  `"."`, makes a later `"."` entry stay a row.
- Do not set `dev_dependency`, `extras`, or `marker` on a uv row without
  proving the value from the lockfile; the entry itself does not carry it.
- A grammar change needs a failing case in `uv_lock_test.go`, a seeded-fuzz
  invariant that still holds, and a README grammar edit in the same commit.

## When changing parser behavior

1. Add or extend the failing fixture in the relevant `*_test.go` first.
2. Confirm the failure: `go test ./internal/parser/pythondep/ -run <name>`.
3. Implement the parser change.
4. Re-run the package test plus the engine matrix test:
   `go test ./internal/parser/pythondep/ ./internal/parser/ -count=1`.
5. If the change exposes new evidence to the supply-chain reducer, add or
   extend a fixture in
   [`../../reducer/package_consumption_python_test.go`](../../reducer/package_consumption_python_test.go).
6. Update `dependency_coverage.go` and the public docs page
   [`docs/public/reference/dependency-coverage.md`](../../../../docs/public/reference/dependency-coverage.md)
   in the same PR.
