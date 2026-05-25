# AGENTS.md - internal/parser/nodelockfile

## Read first

1. `README.md` - package purpose, ownership boundary, row contract.
2. `doc.go` - godoc contract for parent parser callers.
3. `parser.go` - `Parse`, lockfile flavor detection, shared row builder.
4. `yarn_classic.go` - Yarn 1.x block parsing and descriptor resolution.
5. `yarn_berry.go` - Yarn Berry (v2+) locator parsing and descriptor
   resolution.
6. `yarn_common.go` - shared block/descriptor helpers and the composite
   instance-key chain walker used by both Yarn flavors.
7. `pnpm.go` - pnpm-lock.yaml v6+ importer and package decoding.
8. `parser_test.go`, `yarn_test.go`, `pnpm_test.go`, and
   `multi_version_test.go` - fixture coverage for direct, transitive,
   scoped, workspace/local, malformed, unsupported-protocol, and
   multi-version-per-name cases.
9. Parent wrapper in `../node_lockfile_language.go`.

## Invariants this package enforces

- Do not import the parent `internal/parser` package. The parent wrapper
  depends on this package and supplies parent-owned helpers through the
  `Options` struct.
- Workspace, `file:`, `link:`, and `portal:` lockfile entries MUST NOT be
  emitted as remote-package rows. The lockfile does not prove a remote
  registry identity for those entries.
- Malformed or unsupported lockfile shapes MUST set
  `lockfile_parse_state` (or per-row `lockfile_unsupported_feature`)
  rather than emitting fake dependency rows.
- Always emit `package_manager: "npm"` (canonical ecosystem) and put the
  actual package manager tool in `package_manager_flavor`. SQL filters in
  `storage/postgres/owned_package_targets.go` join on
  `package_manager = "npm"` and would silently drop rows that label
  yarn/pnpm as a separate ecosystem.
- Keep deterministic row order so collector dedupe and reducer ordering
  stay stable across reparses.

## Common changes

- New lockfile flavors (Bun, npm shrinkwrap, etc.) belong in their own
  branch of `Parse` with a sibling fixture-driven test file.
- New row fields belong in `dependencyRow` so all flavors share the same
  shape; update the README row contract section in the same change.
- New protocols (e.g., a future Yarn Berry resolver) belong in
  `isLocalProtocol` or `isSupportedRemoteProtocol`; default to recording
  rather than silently dropping evidence.

## Failure modes

- Missing dependency rows usually mean the flavor detection in
  `detectFlavor` did not match the lockfile shape; sniff the file head
  and confirm the expected `version "x"` (classic), `resolution:`
  (berry), or `lockfileVersion:` (pnpm) markers are present.
- Missing transitive chain rows usually mean the `dependencies:` block
  inside a yarn classic entry was indented unexpectedly. Yarn classic
  parsing depends on column-zero header detection and two-space body
  indentation; the parser does not synthesize chains from package
  resolution paths.
- pnpm `(peer@version)` suffixes on package keys are stripped by
  `parsePnpmPackageKey`; if a future pnpm release changes that suffix
  shape, both `parsePnpmPackageKey` and `readPnpmDependencies` must be
  updated together.
