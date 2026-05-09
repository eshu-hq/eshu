# JavaScript Parser Helpers

## Purpose

This package owns JavaScript and TypeScript parser helpers that do not need
tree-sitter nodes or parent parser payload helpers. The first helper resolves
tsconfig.json `baseUrl` and `paths` aliases so import metadata can point at the
same repository source file the TypeScript compiler would look for. It also
maps nearest-package `package.json` main, bin, scripts, exports, and types
targets back to source files for dead-code root evidence.

## Ownership boundary

The package is responsible for typed JavaScript-family evidence that can be
computed from files and strings. The parent parser package still owns source
parsing, map payload assembly, framework roots, Hapi route roots, and bucket
sorting.

## Exported surface

The godoc contract is in doc.go. Current exports are TSConfigImportResolver,
NewTSConfigImportResolver, TSConfigImportResolver.ResolveSource,
TSConfigSourceCandidates, PackageFileRootKinds, NearestPackageRoot, and
PackagePublicSourcePaths.

## Dependencies

This package imports only the Go standard library. It must not import the
parent parser package, collector packages, graph storage, or reducer code.

## Telemetry

This package emits no telemetry. Parse timing remains owned by the parent
parser engine.

## Gotchas / invariants

TypeScript config files use JSONC, so comments and trailing commas are accepted
before unmarshalling.

Resolution is repository-bounded. Absolute `baseUrl` values, absolute path
targets, and candidates outside the repository root return no result.

TSConfigSourceCandidates returns candidates in a stable order: the base path,
then supported JavaScript/TypeScript extensions, then index files with the same
extension order.

Package helpers use the closest package.json between the source file and
repoRoot. Workspace root manifests must not claim files owned by a nested
package manifest.

## Related docs

- docs/plans/2026-05-09-parser-language-layout.md
