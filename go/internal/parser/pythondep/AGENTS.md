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
