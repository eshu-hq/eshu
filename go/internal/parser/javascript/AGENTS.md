# AGENTS.md - internal/parser/javascript guidance

## Read first

1. README.md - package boundary, tsconfig resolver behavior, and invariants
2. doc.go - godoc contract for the JavaScript helper package
3. tsconfig.go - JSONC parsing, path alias resolution, and repository bounds
4. package_json.go - nearest package.json roots and public source targets
5. tsconfig_test.go - behavior coverage for JSONC, path aliases, and candidate
   ordering
6. package_json_test.go - behavior coverage for nearest package ownership and
   package public source mapping

## Invariants this package enforces

- Dependency direction stays one way: parent parser code may import this
  package, but this package must not import internal/parser.
- TSConfigImportResolver never resolves outside the repository root.
- PackageFileRootKinds and PackagePublicSourcePaths use nearest package.json
  ownership, so nested packages are not claimed by workspace-root manifests.
- JSONC syntax in tsconfig.json is valid input. Do not replace the JSONC
  cleanup with plain encoding/json on raw bytes.
- Candidate ordering is deterministic because import resolution affects fact
  output and reducer evidence.

## Common changes and how to scope them

- Add tsconfig behavior by writing a focused test in tsconfig_test.go first.
- Add package.json behavior by writing a focused test in package_json_test.go
  first.
- Keep map[string]any payload annotation in the parent parser package.
- Keep tree-sitter JavaScript parsing out of this package until the shared node
  helper boundary has a separate design.

## Failure modes and how to debug

- Missing resolved_source metadata usually means the nearest tsconfig.json was
  not found, baseUrl resolved outside the repo, or the candidate file does not
  exist.
- Incorrect resolution in workspaces usually points at nearest-config lookup.
  Add a fixture with nested tsconfig.json files before changing lookup order.
- Package roots leaking from a workspace root into a nested package usually
  means nearest package lookup changed. Reproduce it in package_json_test.go.
- Nondeterministic imports usually mean a map was iterated directly. Collect
  candidates, deduplicate explicitly, and preserve a stable order.

## Anti-patterns specific to this package

- Importing the parent parser package to reuse payload helpers.
- Returning payload maps from this package.
- Resolving absolute aliases or paths outside repoRoot.

## What NOT to change without an ADR

- Do not move the full JavaScript or TypeScript tree-sitter adapter here until
  shared tree helpers, payload helpers, and registry wiring have explicit
  package contracts.
