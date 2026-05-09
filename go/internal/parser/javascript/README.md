# JavaScript Parser

## Purpose

This package owns the JavaScript-family parser adapter for JavaScript,
TypeScript, and TSX. It reads source files through a caller-provided
`ParserFactory`, builds the legacy parser payload buckets, annotates imports
with tsconfig `resolved_source` evidence, and marks parser-proven dead-code
roots from package, framework, module-contract, route, and public API evidence.

## Ownership boundary

The package is responsible for JavaScript-family tree-sitter traversal,
payload assembly, import and re-export extraction, call metadata, component
evidence, TypeScript declaration rows, package.json roots, tsconfig alias
resolution, Hapi route evidence, framework callback roots, and deterministic
bucket sorting.

The parent `internal/parser` package owns registry dispatch, runtime grammar
caching, Engine.ParsePath, Engine.PreScanRepositoryPathsWithWorkers, and the
thin JavaScript wrapper that converts parent options into shared parser
options. This package must not import the parent parser package.

## Exported surface

The godoc contract is in `doc.go`. Current exports are `ParserFactory`,
`Parse`, `PreScan`, `TSConfigImportResolver`,
`NewTSConfigImportResolver`, `TSConfigImportResolver.ResolveSource`,
`TSConfigSourceCandidates`, `PackageFileRootKinds`, `NearestPackageRoot`, and
`PackagePublicSourcePaths`, and `ExpressServerSymbols`.

## Dependencies

This package imports tree-sitter, the Go standard library, and
`internal/parser/shared` for payload, source, tree, path, and option helpers.
The local alias file only exposes helper names with package-local callers. It
must not import the parent parser package, collector packages, graph storage,
or reducer code.

## Telemetry

This package emits no telemetry. Parse timing remains owned by the parent
parser engine and runtime instrumentation.

## Gotchas / invariants

`Parse` accepts a `ParserFactory` instead of a parent Engine so the child
package cannot depend on `internal/parser`.

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

Dead-code roots are evidence rows, not guesses. Package entrypoints, CommonJS
exports, Hapi handlers, Next.js route exports, framework callbacks,
TypeScript interface implementation methods, module-contract exports, and
public API re-exports must remain grounded in syntax or bounded repository
files.

## Related docs

- docs/plans/2026-05-09-parser-language-layout.md
