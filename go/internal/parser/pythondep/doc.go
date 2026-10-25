// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package pythondep parses Python package-manager manifests and lockfiles into
// the parser engine's content_entity dependency rows so the supply-chain
// reducer can match PyPI vulnerability advisories against repository
// dependency truth.
//
// The package owns five entrypoints:
//
//   - ParseRequirements parses pip-style requirements files (requirements.txt
//     and sibling requirements-*.txt / requirements_*.txt forms). It preserves
//     pinned/range specifiers, extras, environment markers, runtime vs dev
//     scope (derived from the filename), and distinguishes PEP 508 direct
//     references, VCS, path, URL, editable, and malformed entries from
//     registry-version evidence. It also reads pip constraints files
//     (constraints.txt, constraints-*.txt, constraints_*.txt) as version
//     policy: each row is flagged central_version_declaration and a row with a
//     direct source drops its value, URL, and raw text.
//   - ParsePyProject parses pyproject.toml for PEP 621 `[project]`
//     dependencies, `[project.optional-dependencies]`, Poetry's
//     `[tool.poetry.dependencies]` and group-style dev dependencies, and
//     Hatch's `[tool.hatch.envs.*.dependencies]` tables.
//   - ParsePipfile parses Pipenv's TOML Pipfile (`[packages]` and
//     `[dev-packages]`).
//   - ParsePoetryLock parses poetry.lock arrays of [[package]] tables,
//     preserving exact installed versions, dev-vs-runtime category, and any
//     `[package.source]` git/directory provenance.
//   - ParseUVLock parses uv.lock with a fail-closed version-1 grammar: only
//     entries that match it resolve, only the public PyPI index yields a
//     registry-admissible row, and everything else is a malformed row counted
//     in the payload's uv_lock_state. The project itself and its workspace
//     members are counted there instead of emitted. See README.md for the
//     grammar.
//
// Pipfile.lock is JSON and is parsed by go/internal/parser/json instead of
// this package; it shares the same content_entity row contract so the
// reducer treats Python lockfile evidence consistently across managers.
//
// VCS, path, URL, editable, and malformed dependencies always reach the
// payload with a non-`dependency` config_kind so the supply-chain reducer
// cannot mis-admit them as PyPI registry consumption. Out of scope by
// design: invoking pip, Poetry, or Pipenv to resolve a graph.
package pythondep
