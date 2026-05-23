# AGENTS.md - internal/parser/javascript

The JavaScript adapter owns JavaScript, TypeScript, and TSX syntax evidence.
Use `README.md` and `doc.go` for the current package contract.

## Read First

1. `README.md` and `doc.go`.
2. `javascript_language.go`, `javascript_imports.go`,
   `javascript_exports.go`, `javascript_require.go`, and
   `javascript_metadata.go`.
3. `javascript_dead_code_roots.go` plus the focused
   `javascript_dead_code_*` helpers for package, framework, Hapi, CommonJS,
   TypeScript surface, and function-value roots.
4. `tsconfig.go`, `package_json.go`, `tsconfig_test.go`, and
   `package_json_test.go`.

## Mandatory Guardrails

- This package MUST NOT import `internal/parser`; callers pass `ParserFactory`
  and shared parser options instead of parent `Engine` values.
- Payload buckets, imports, re-exports, pre-scan results, and package/tsconfig
  candidates must be deterministic.
- TypeScript config parsing accepts JSONC. Do not parse raw `tsconfig.json`
  bytes with plain `encoding/json`.
- Path alias resolution stays repository-bounded. Absolute `baseUrl` values,
  absolute path targets, and candidates outside `repoRoot` resolve to no
  source.
- Package roots use nearest `package.json` ownership. Workspace-root manifests
  must not claim nested package files.
- Dead-code roots require syntax, package metadata, tsconfig evidence, route
  evidence, module-contract evidence, or bounded re-export evidence. Broad
  exported names are not roots by themselves.

## Change Scope

- Public `Engine.ParsePath` behavior starts with parent parser tests.
- `tsconfig` behavior starts in `tsconfig_test.go`; package ownership behavior
  starts in `package_json_test.go`.
- Keep parent wrapper edits limited to signature preservation, runtime lookup,
  and shared option conversion.
- Do not change `.js`, `.jsx`, `.ts`, `.tsx`, `.mts`, `.cts`, `.mjs`, or `.cjs`
  registry ownership without architecture-owner approval.
- Do not add collector, reducer, storage, query, or backend dependencies here.
