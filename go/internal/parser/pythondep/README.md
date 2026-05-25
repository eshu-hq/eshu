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
  filename derivation, VCS, path, URL, editable, and malformed entries plus
  the empty-file safety case.
- `pyproject_test.go` covers PEP 621, Poetry dependencies, Poetry group
  dependencies, Hatch envs, inline-table sources (git/path), and the
  malformed-array case.
- `pipfile_test.go` covers `[packages]` and `[dev-packages]` plus inline-table
  VCS/path entries.
- `poetry_lock_test.go` covers exact-version rows, dev category derivation,
  git source provenance, and directory source provenance.

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
