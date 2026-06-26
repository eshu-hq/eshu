# PythonDep Parser Audit

## Overview
Parses Python package-manager manifests and lockfiles: pip requirements.txt, pyproject.toml (PEP 621, Poetry, Hatch), Pipfile, and poetry.lock. This is a **dependency manifest** parser — NOT a language parser. Uses `bufio` line scanning for requirements, bounded TOML scanning for pyproject.toml/Pipfile, and TOML-structured parsing for poetry.lock. 7 src files, 4 test files. No regexp.MustCompile.

## Claimed Constructs
From `doc.go`, `README.md`, `requirements.go`:
- **requirements.txt**: pinned versions (==), range specifiers (>=, <=, ~=), extras ([security]), environment markers (; python_version >= '3.8'), runtime vs dev scope (filename-based), line continuations (\\), comments/hash flags (-e, -r, --hash)
- **VCS/path/URL/editable entries**: non-registry provenance (config_kind≠dependency): editable_dependency, vcs_dependency, url_dependency, path_dependency
- **Malformed entries**: surfacing as malformed rows rather than ignoring
- **pyproject.toml**: PEP 621 [project].dependencies, [project.optional-dependencies], Poetry [tool.poetry.dependencies], Poetry group dev ([tool.poetry.group.dev.dependencies]), Hatch [tool.hatch.envs.*.dependencies]
- **Pipfile**: [packages] and [dev-packages] with version specifiers, extras
- **poetry.lock**: [[package]] arrays with version, source metadata
- **Row fields**: name, value, section, config_kind, package_manager (pypi), extras, marker, dev_dependency, source_kind, source_url, source_ref, malformed, raw

## Verified-by-Test Constructs
- `TestParseRequirementsPreservesPinExtrasMarkersAndScopeReason` (`requirements_test.go:19`): pinned version (==), range (>=4.2,<5.0), compatible-release (~=1.26), extras ([security]), markers, editable (-e), VCS (git+), URL (file://), malformed (@@@), comments/hash ignored
- `TestParsePyProjectPEP621AndPoetryDependencyTables` (`pyproject_test.go:16`): PEP 621 dependencies, optional-dependencies.dev, Poetry dependencies (string form + dict form with extras/path/git), Poetry group.dev.dependencies, Hatch env dependencies, python constraint excluded
- `TestParsePipfileTracksDevPackagesAndExtras` (`pipfile_test.go:`): Pipfile [packages] and [dev-packages], version specifiers, extras
- `TestParsePoetryLockEmitsExactLockfileVersionsWithSourceMetadata` (`poetry_lock_test.go:`): poetry.lock [[package]] arrays, source metadata

## Unverified / Claimed-but-Untested Constructs
- **requirements.txt line continuations** (\\ at end of line): claimed in requirements.go implementation, may be tested in requirements_test.go beyond line 100
- **-r include directive**: requirements.txt referencing another file
- **PEP 508 direct references** in requirements.txt
- **Path dependencies** as distinct from editable dependencies
- **Raw field preservation** for malformed entries
- **Empty files**: zero-row payload handling
- **TOML parsing for inline tables** in pyproject.toml

## Edge Cases Considered
- Mixed requirement types in one file (pinned, ranged, VCS, editable, URL, malformed)
- Poetry's `python = "^3.10"` excluded (not a PyPI package)
- Poetry dict-form dependencies with extras, path, and git source
- Hatch env-specific dependencies in pyproject.toml
- Dev vs runtime scope for requirements files (filename-based)
- Environment markers preserved verbatim
- Comments and hash flags (-r, --hash) skipped

## Edge Cases NOT Considered
- requirements.txt with inline comments (`numpy==1.26  # this is numpy`)
- Very long requirement lines with multiple markers and extras
- Poëtry groups beyond dev (arbitrary group names)
- Flit/PEP 621 without Poetry
- Hatch environments with matrix/config
- Poetry source configuration ([tool.poetry.source])
- Pipenv Pipfile with [requires] section
- Unicode/emoji package names

## Verdict
**deep** — 4 test files covering requirements.txt (pinned, ranged, VCS, editable, URL, malformed, markers, extras, dev scope), pyproject.toml (PEP 621, Poetry, Hatch), Pipfile, and poetry.lock. As a permanent exception using bounded text scanning over dependency manifests, this is comprehensive.

## Recommended Actions
- Document that PYTHONDEP is a **permanent exception** — uses `bufio` line scanning and bounded TOML scanning, not tree-sitter
- Add a test for requirements.txt line continuations (\\ continuation)
- Add a test for completely empty files
- Add a test for pyproject.toml without any Python dependency tables
- Verify raw/malformed field preservation in requirements rows
