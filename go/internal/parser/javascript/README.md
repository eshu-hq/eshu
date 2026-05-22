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

See `doc.go` and exported comments in the package sources for the godoc
contract. Keep parser helper catalogs in source comments; this README should
describe ownership, determinism, and evidence limits.

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
then supported JavaScript/TypeScript declaration and runtime extensions, then
index files with the same extension order.

Package helpers use the closest package.json between the source file and
repoRoot. Workspace root manifests must not claim files owned by a nested
package manifest. A `types` target ending in `.d.ts` is treated as a declaration
artifact path, so `lib/index.d.ts` can map back to authored sources such as
`src/index.ts` when generated declaration files are not checked in.

Dead-code roots are evidence rows, not guesses. Package entrypoints, CommonJS
exports, framework routes, callbacks, module-contract exports, and public API
re-exports must remain grounded in syntax or bounded repository files.
CommonJS default-export class method roots apply only to the exported class
expression, not helper classes nested inside another exported expression.
Declaration public-surface walking stays repository-bounded, static, and
depth-capped.

## Verification

```bash
go test ./internal/parser/javascript -count=1
go run ./cmd/eshu docs verify ../go/internal/parser/javascript --limit 1000 \
  --fail-on contradicted,missing_evidence
```

## Related docs

- `docs/public/contributing-language-support.md`
