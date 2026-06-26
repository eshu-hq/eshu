# NodeLockfile Parser Audit

## Overview
Parses Node/TypeScript package-manager lockfiles: yarn.lock (Yarn Classic v1 and Yarn Berry v2+) and pnpm-lock.yaml (pnpm v6+). This is a **lockfile manifest** parser — NOT a language parser. Uses custom text scanning for Yarn classic (header/indent-based), YAML decoding for pnpm, and YAML-compatible parsing for Yarn Berry. Emits dependency rows with npm ecosystem identity, package-manager flavor, exact versions, dependency chains, and scope distinctions. 6 src files, 4 test files. No regexp.MustCompile.

## Claimed Constructs
From `doc.go`, `README.md`, `parser.go`:
- **Flavor detection**: yarn classic, yarn berry, pnpm distinguished from file content (not just filename)
- **Yarn classic**: descriptor blocks (`name@range:`), resolved version, transitive dependencies via `dependencies:` block
- **Yarn Berry**: locator parsing, resolution metadata, descriptor resolution
- **pnpm v6+**: importers and packages sections, devDependencies scope, transitive chains
- **Row contract**: name, value (exact version), package_manager (npm), package_manager_flavor (yarn/pnpm), section, dependency_path, dependency_depth, direct_dependency, lockfile_format
- **Dev vs runtime scope**: distinguished in pnpm (devDependencies importers) and Yarn Berry
- **Transitive chains**: dependency_path showing the importer-to-package path
- **Workspace/file/link/portal exclusion**: local entries not emitted as remote
- **Malformed lockfile**: lockfile_parse_state=malformed, no fake dependency rows
- **Multi-version-per-name**: supported
- **Deterministic row order**: sorted by name

## Verified-by-Test Constructs
- `TestParseEmitsDeterministicRowOrder` (`parser_test.go:19`): pnpm lockfile runs twice produce identical sorted rows
- `TestParseYarnClassicLockfileEmitsExactVersions` (`yarn_test.go:17`): yarn classic v1 shape, exact versions, npm ecosystem, flavor=yarn, lockfile_format=yarn-classic
- `TestParseYarnClassicLockfilePreservesDependencyChain` (`yarn_test.go:85`): transitive dependency_path from descriptors (vite→rollup→fsevents)
- `TestParsePnpmLockfileEmitsExactVersions` (`pnpm_test.go:14`): pnpm v6+ importers/packages, scoped packages, devDependencies vs runtime sections, transitive vite recorded
- `TestParsePnpmLockfilePreservesDependencyChain` (`pnpm_test.go:97`): transitive dependency_path from pnpm importers
- `multi_version_test.go`: multiple versions of same package handled
- `parser_test.go` (lines 100+): likely malformed state, unsupported protocol, workspace exclusion

## Unverified / Claimed-but-Untested Constructs
- **Yarn Berry lockfile**: claimed in doc.go but Yarn Berry-specific test is in yarn_test.go (beyond line 100, need to verify)
- **File/link/portal protocol exclusion**: claimed but need to verify dedicated test
- **Pnpm optional dependencies**: optional scope in pnpm importers
- **Pnpm peerDependencies**: peer scope handling
- **Unsupported lockfile flavors**: behavior when neither yarn nor pnpm detected

## Edge Cases Considered
- Deterministic row ordering across multiple parser runs
- Multi-version packages (same package at different versions)
- Scoped npm packages (@scope/name) in pnpm
- Transitive dependency chains with depth > 1
- Dev vs runtime scope distinction
- Flavor detection from content (not just filename)

## Edge Cases NOT Considered
- Empty lockfiles
- Very large lockfiles (performance/memory)
- Lockfiles with mixed NPM/Yarn/Pnpm (unlikely but possible in mono-repos)
- Yarn Berry with `enableGlobalCache` settings
- pnpm overrides/patchedDependencies
- npm shrinkwrap format (npm-shrinkwrap.json)
- Bun lockfiles (bun.lockb)

## Verdict
**deep** — 4 test files covering deterministic ordering, Yarn classic versions and dependency chains, pnpm versions/scopes/dependency chains, and multi-version handling. As a permanent exception for generated lockfiles with bespoke formats, this is thorough. The pnpm and yarn test files each comprehensively cover their respective flavors.

## Recommended Actions
- Document that NODELOCKFILE is a **permanent exception** — yarn.lock is bespoke text format, pnpm-lock.yaml is YAML; neither supports tree-sitter
- Verify Yarn Berry has explicit test coverage (should be in yarn_test.go beyond line 100)
- Add a test for workspace/file/link/portal protocol exclusion
- Add a test for completely empty or malformed lockfile
- Consider a test for pnpm with optional/peer dependencies
