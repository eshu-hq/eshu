// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package pythondep parses Python package-manager manifests and lockfiles into
// the parser engine's content_entity dependency rows so the supply-chain
// reducer can match PyPI vulnerability advisories against repository
// dependency truth.
//
// The package owns four entrypoints:
//
//   - ParseRequirements parses pip-style requirements files (requirements.txt
//     and sibling requirements-*.txt / requirements_*.txt forms). It preserves
//     pinned/range specifiers, extras, environment markers, runtime vs dev
//     scope (derived from the filename), and distinguishes PEP 508 direct
//     references, VCS, path, URL, editable, and malformed entries from
//     registry-version evidence.
//   - ParsePyProject parses pyproject.toml for PEP 621 `[project]`
//     dependencies, `[project.optional-dependencies]`, Poetry's
//     `[tool.poetry.dependencies]` and group-style dev dependencies, and
//     Hatch's `[tool.hatch.envs.*.dependencies]` tables.
//   - ParsePipfile parses Pipenv's TOML Pipfile (`[packages]` and
//     `[dev-packages]`).
//   - ParsePoetryLock parses poetry.lock arrays of [[package]] tables,
//     preserving exact installed versions, dev-vs-runtime category, and any
//     `[package.source]` git/directory provenance.
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
