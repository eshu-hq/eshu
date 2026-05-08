# JavaScript And TypeScript Dead-Code Maturity Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Mature Eshu dead-code detection for Node, JavaScript, TypeScript, and TSX so the main local Node service family returns useful, bounded, evidence-explained findings without overclaiming cleanup-safe truth.

**Architecture:** Treat JavaScript, JSX, TypeScript, and TSX as one JavaScript-family dead-code capability with dialect-specific parser details. Keep `truth.level=derived` until package entrypoints, module resolution, framework roots, dynamic ambiguity, fixtures, API/MCP proof, and local dogfood gates agree. Add first-class Node/Hapi roots because the local validation corpus is dominated by Hapi services with `server/handlers`, `server/init/plugins/spec*`, and `package.json` start/dev scripts.

**Tech Stack:** Go parser/query code, Tree-sitter JavaScript/TypeScript grammars, Eshu local-authoritative NornicDB graph, API/MCP `code_quality.dead_code`, Node package metadata, `tsconfig.json`, JavaScript/TypeScript fixtures, and local dogfood repos selected outside the open-source tree.

---

## Context Snapshot

Current Eshu state:

- `go/internal/parser/javascript_dead_code_roots.go` emits parser-backed roots for Next.js route exports and Express registrations.
- `go/internal/query/code_dead_code_javascript_roots.go` excludes those parser-backed roots from dead-code results.
- `go/internal/query/code_dead_code_language_maturity.go` marks `javascript`, `typescript`, and `tsx` as `derived`.
- `tests/fixtures/deadcode/javascript`, `tests/fixtures/deadcode/typescript`, and `tests/fixtures/deadcode/tsx` exist, but they need stronger enforced parser/query/API proof.
- `docs/docs/reference/dead-code-reachability-spec.md` requires roots, fixture intent, ambiguity handling, API/MCP metadata, and backend proof before exactness can be claimed.

Local target evidence:

- The local validation corpus contains many Node service repos with `package.json`.
- Sample services use Node 20, `tsx`, `tsup`, `typescript`, Hapi test tooling, an internal Hapi service framework, and internal TypeScript build tooling.
- Runtime entrypoints usually appear in `package.json` as `start: node dist/<service>.js` and `dev: tsx <service>.ts`.
- Hapi route roots are mostly file-convention driven through `server/init/plugins/spec*` options:
  - `openapi.handlers: path.join(__dirname, '../../handlers')`
  - `openapi.handlers: path.resolve(__dirname, '../../handlers')`
- Route handler roots usually export HTTP methods, plus library-specific
  callback exports such as framework status payload builders:
  - CommonJS: `module.exports.get = async (...) => {}`
  - TypeScript/ESM: `export const get = async (...) => {}`
  - Supported method names should include `get`, `post`, `put`, `patch`, `delete`, `head`, and `options`.

Acceptance posture:

- Keep JavaScript-family maturity at `derived` for this plan.
- Promote only narrower subprofiles later, for example `derived_static_module_graph`, when static package/module proof exists.
- Do not claim exact cleanup safety while unresolved dynamic imports, computed property access, runtime plugin loading, Hapi convention wiring, or TypeScript path aliases remain ambiguous.

## File Map

Expected implementation touch points:

- Modify `go/internal/parser/javascript_dead_code_roots.go`
  - Add root-kind constants and parser-backed roots for Node package entrypoints, Hapi handler exports, and public module exports.
- Create `go/internal/parser/javascript_dead_code_package.go`
  - Parse and normalize `package.json` entrypoint evidence adjacent to a parsed source file.
- Create `go/internal/parser/javascript_dead_code_hapi.go`
  - Detect internal Hapi specs-plugin config and handler roots under configured handler directories.
- Create `go/internal/parser/javascript_dead_code_modules.go`
  - Model static import/export/require evidence and ambiguity markers without attempting full TypeScript resolution in one pass.
- Modify `go/internal/parser/javascript_language.go`
  - Attach new `dead_code_root_kinds` to functions, classes, variables, and components where parser evidence proves a root.
- Modify or create `go/internal/parser/javascript_imports.go`
  - Preserve import alias metadata needed by reducer call materialization,
    including TypeScript namespace imports such as `import * as jwt from ...`.
- Add parser tests:
  - `go/internal/parser/javascript_dead_code_node_roots_test.go`
  - `go/internal/parser/javascript_dead_code_hapi_roots_test.go`
  - `go/internal/parser/javascript_dead_code_module_roots_test.go`
- Modify `go/internal/query/code_dead_code_javascript_roots.go`
  - Exclude new JavaScript-family parser-backed roots.
- Modify `go/internal/query/code_dead_code_analysis.go`
  - Report new root categories and ambiguity counts.
- Add query tests:
  - `go/internal/query/code_dead_code_javascript_node_roots_test.go`
  - `go/internal/query/code_dead_code_javascript_hapi_roots_test.go`
  - `go/internal/query/code_dead_code_javascript_module_roots_test.go`
- Expand fixtures:
  - `tests/fixtures/deadcode/javascript`
  - `tests/fixtures/deadcode/typescript`
  - `tests/fixtures/deadcode/tsx`
  - Add a dedicated Hapi service fixture under `tests/fixtures/deadcode/typescript-hapi` or fold into `typescript` if the fixture stays small.
- Modify docs:
  - `docs/docs/reference/dead-code-reachability-spec.md`
  - `docs/docs/adrs/2026-05-07-dead-code-root-model-and-language-reachability.md`
  - `docs/superpowers/plans/2026-05-08-javascript-typescript-dead-code-maturity.md`

## Parallel Chunk Strategy

Chunks 1 through 4 can run in parallel after the shared root-kind names are agreed. Chunk 5 depends on the parser/query slices. Chunk 6 depends on all code slices.

Use these root-kind names unless a worker finds a better local convention:

- `javascript.node_package_entrypoint`
- `javascript.node_package_bin`
- `javascript.node_package_export`
- `javascript.hapi_handler_export`
- `javascript.hapi_plugin_registration`
- `javascript.static_import_reference`
- `javascript.static_require_reference`
- `javascript.reexport_public_api`
- `javascript.dynamic_import_ambiguous`
- `javascript.computed_property_ambiguous`

## Chunk 1: Node Package Entrypoints And Public Module Surface

**Owner:** Subagent A.

**Scope:** `package.json` entrypoints, package `exports`, `main`, `module`, `types`, and `bin`. Do not implement Hapi handler convention roots in this chunk.

**Files:**

- Create: `go/internal/parser/javascript_dead_code_package.go`
- Modify: `go/internal/parser/javascript_dead_code_roots.go`
- Modify: `go/internal/parser/javascript_language.go`
- Test: `go/internal/parser/javascript_dead_code_node_roots_test.go`
- Test fixture updates under `tests/fixtures/deadcode/javascript` and `tests/fixtures/deadcode/typescript`

- [x] **Step 1: Write failing parser tests for package entry roots**

Add a test repository with:

```text
package.json
service-entry.ts
server/public-api.ts
server/private-helper.ts
```

Assert:

- `service-entry.ts` root function or top-level entrypoint receives `javascript.node_package_entrypoint`.
- file named by `bin` receives `javascript.node_package_bin`.
- exported symbol reachable from package `exports` receives `javascript.node_package_export`.
- `private-helper.ts` unused local helper does not receive any root kind.

Run:

```bash
cd go
go test ./internal/parser -run TestDefaultEngineParsePathJavaScriptPackageDeadCodeRoots -count=1
```

Expected: FAIL because package metadata is not read yet.

- [x] **Step 2: Implement package metadata discovery**

Implementation rules:

- Walk upward from the parsed source file to the nearest `package.json`, stopping at repository root when available.
- Parse with Go `encoding/json`, not string matching.
- Normalize `main`, `module`, `types`, `exports`, and `bin` to relative file candidates.
- Handle extensionless candidates by checking `.js`, `.jsx`, `.ts`, `.tsx`, `.mjs`, `.cjs`, `.mts`, and `.cts`.
- Ignore `dist/`, `build/`, and generated output unless source maps back to an authored source file in the fixture. This first chunk should root authored source entrypoints, not compiled output.

- [x] **Step 3: Emit root metadata**

Attach root kinds to source entities only when evidence points at that source file or exported symbol. If the package points to `dist/service-entry.js` and source has `service-entry.ts`, root the source file entrypoint only when the basename relationship is direct and local tests prove it.

- [x] **Step 4: Re-run focused tests**

Run:

```bash
cd go
go test ./internal/parser -run 'TestDefaultEngineParsePathJavaScriptPackageDeadCodeRoots|TestDefaultEngineParsePathJavaScriptEmitsDeadCodeRootKinds' -count=1
```

Expected: PASS.

- [x] **Step 5: Add query exclusion test**

Add a failing query test that candidate rows with `javascript.node_package_entrypoint`, `javascript.node_package_bin`, and `javascript.node_package_export` are excluded and counted as parser metadata framework/public roots.

Run:

```bash
cd go
go test ./internal/query -run TestHandleDeadCodeExcludesJavaScriptPackageRoots -count=1
```

Expected before query change: FAIL.

- [x] **Step 6: Update query policy**

Modify `deadCodeIsJavaScriptFrameworkRoot` or split a clearer `deadCodeIsJavaScriptRoot` helper so package roots are excluded by root metadata.

- [x] **Step 7: Verify**

Run:

```bash
cd go
go test ./internal/parser ./internal/query -run 'JavaScriptPackage|DeadCode.*JavaScriptPackage' -count=1
```

Expected: PASS.

## Chunk 2: Hapi Handler Roots

**Owner:** Subagent B.

**Scope:** Model the route convention used by the local Node service family. This is the highest-value business slice.

**Files:**

- Create: `go/internal/parser/javascript_dead_code_hapi.go`
- Modify: `go/internal/parser/javascript_dead_code_roots.go`
- Modify: `go/internal/parser/javascript_language.go`
- Modify: `go/internal/query/code_dead_code_javascript_roots.go`
- Test: `go/internal/parser/javascript_dead_code_hapi_roots_test.go`
- Test: `go/internal/query/code_dead_code_javascript_hapi_roots_test.go`
- Fixture: `tests/fixtures/deadcode/typescript-hapi` or expanded `tests/fixtures/deadcode/typescript`

- [x] **Step 1: Write failing parser tests for handler exports**

Fixture shape:

```text
server/init/plugins/specs.ts
server/handlers/_status.ts
server/handlers/chat/response.ts
server/resources/chat-service.ts
server/resources/unused-service.ts
```

`specs.ts` should include:

```ts
import { plugin } from '@example/hapi-service/init/plugins/specs';
import path from 'path';

export const options = {
  openapi: {
    handlers: path.resolve(__dirname, '../../handlers'),
  },
};

export default { plugin, options };
```

Handlers should include:

```ts
export const get = () => ({ statusCode: 200 });
export const post = async (request: Request) => request.payload;
```

Assert `get` and `post` under the configured handlers directory receive `javascript.hapi_handler_export`.

- [x] **Step 2: Add CommonJS fixture**

Add:

```js
module.exports.get = async (request) => ({ ok: true });
module.exports.patch = async (request) => request.payload;
module.exports.payload = async () => ({ statusCode: 200 });
```

Assert all exported functions receive `javascript.hapi_handler_export`.

- [x] **Step 3: Implement Hapi spec-plugin detection**

Detection should require at least one strong signal:

- import or require of the internal Hapi service specs plugin
- `openapi.handlers` path configured in the plugin options

Resolve `path.join(__dirname, '../../handlers')` and `path.resolve(__dirname, '../../handlers')` conservatively. If resolution fails, do not root all handlers; emit ambiguity metadata later instead.

- [x] **Step 4: Implement handler export roots**

Root exported function symbols under configured handler directories. This
includes normal HTTP method exports and callback exports consumed by wrapper
handlers, such as `module.exports.payload` beside
the framework status handler. Do not root non-exported helper functions
inside handlers; they still need normal call/reference evidence.

- [x] **Step 5: Add query test and policy update**

Add a query test that `javascript.hapi_handler_export` excludes a candidate and appears in analysis root categories.

Run:

```bash
cd go
go test ./internal/parser ./internal/query -run 'Hapi|JavaScript.*Handler' -count=1
```

Expected: PASS after implementation.

## Later Chunks

Chunk 3 adds static ESM/CommonJS import, require, re-export, and barrel-file
reachability. It must prove relative static imports without emulating a full
bundler. TypeScript namespace member calls such as `jwt.encode()` now resolve
to `server/resources/jwt.ts::encode` from
`import * as jwt from "../resources/jwt"`. Parser-backed CommonJS `require()`
namespace calls and destructured require aliases are covered by reducer
regression tests. Static relative one-hop barrels such as
`export { encode } from "./jwt"` and `export * from "./jwt"` now resolve from a
caller import of the barrel to the original exported function. Tsconfig
`baseUrl` imports now emit bounded `resolved_source` metadata when an authored
file exists under the repo root. Constructor calls such as
`new SnapshotSync()` and local receiver calls such as `sync.invoke()` now
emit parser metadata that lets the reducer connect constructor, class method,
and static method edges through the same bounded import/re-export path.

Chunk 4 adds ambiguity metadata for dynamic imports, computed property
dispatch, DI containers, and event callback registrations. It should explain
uncertainty without hiding every candidate in the file.

Chunk 5 upgrades JS/TS/TSX fixtures into enforced parser, query, API, and MCP
proof. It must keep maturity at `derived` while exactness gates remain open.

Chunk 6 dogfoods against representative local Node services selected outside
the open-source tree. Each run must record discovery shape, query duration,
result count, truncation, and evidence-backed false positives without copying
private service names, paths, repository IDs, IP/domain-specific symbols, or
customer-specific package names into committed artifacts.

Live proof note from a representative local Node service: after rebuilding
local binaries, `eshu graph start` indexed 50 files and MCP `find_dead_code`
returned fresh derived results with 3 parser metadata framework roots. That
proof caught and fixed the Hapi `module.exports.payload` handler-root gap. A
follow-up namespace import fix increased reducer code call materialization from
19 to 24 calls, and MCP `execute_cypher_query` confirmed handler/resource edges
such as `post -> encode`, `getHealthChecks -> secretIsSafe`, `decode -> getSecret`,
`getSecret -> get/init`, and `payload -> getHealthChecks`. MCP `find_dead_code`
then returned only 6 fresh candidates, all under `scripts/`, with no resource
false positives. Local tsconfig evidence showed broad
`compilerOptions.baseUrl: "."` usage and no common `compilerOptions.paths`
usage in the sampled services, so this branch prioritized bounded baseUrl
imports before custom path aliases. The private service names, local paths,
repository IDs, package scopes, and customer-specific symbols are intentionally
kept out of committed docs.

## Final Evidence After PR #10

The final Node/TypeScript maturity slice keeps JavaScript-family dead-code
truth at `derived`. It is useful for triage, but it is not a cleanup-safe exact
claim while dynamic imports, computed dispatch, runtime plugin loading, and
unmodeled TypeScript path aliases remain open.

| Lane | Scope | Evidence | Result |
| --- | --- | --- | --- |
| Parser and query roots | Node, JavaScript, TypeScript, TSX | Package entrypoints, package `bin`, package exports, Next.js routes, Express registrations, configured Hapi/lib-api-hapi handler exports, TypeScript interface implementation methods, and bounded static module references | Parser-backed roots are excluded from dead-code candidates and reported as derived root evidence |
| MCP dogfood | Representative private Node/TypeScript service | Rebuilt local binaries, ran local-authoritative indexing, then called MCP `find_dead_code` | Fresh derived results, 3 parser metadata framework roots, 6 remaining candidates under `scripts/`, no resource-layer false positives |
| Graph drilldown | Same private service, sanitized in committed notes | MCP `execute_cypher_query` checked handler/resource edges after namespace-import materialization increased calls from 19 to 24 | Expected edges appeared, including handler-to-resource and payload-to-health-check paths |
| Privacy boundary | Local dogfood repositories | Evidence records only counts, result shape, tool names, and sanitized edge examples | Private repo names, local paths, repository IDs, package scopes, and customer-specific symbols are not committed |

## Final Verification Gate

Run before opening the implementation PR:

```bash
cd go
go test ./internal/parser ./internal/query ./cmd/eshu -count=1
go test ./internal/collector ./internal/storage/cypher ./cmd/ingester -count=1
cd ..
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml
git diff --check
```

If docs package files under `go/` are touched, also run the relevant `scripts/verify-doc-claims.sh go/internal/<pkg>` gates.

## Suggested PR Breakdown

PR 1:

- Chunk 1 package entrypoints/public module roots
- Chunk 2 Hapi handler roots
- Fixture parser/query tests for those two root categories

PR 2:

- Chunk 3 static import/require/re-export reachability
- Chunk 4 ambiguity metadata

PR 3:

- Chunk 5 fixture/API/MCP proof
- Chunk 6 local dogfood evidence against selected local Node service repos
- Docs/ADR updates and capability-matrix language notes

## Subagent Assignment Prompt Template

Use this prompt for each worker:

```text
You are working in the Eshu repository on the docs/js-ts-dead-code-plan branch or its implementation successor branch.

Read AGENTS.md, docs/docs/reference/dead-code-reachability-spec.md, docs/docs/adrs/2026-05-07-dead-code-root-model-and-language-reachability.md, and docs/superpowers/plans/2026-05-08-javascript-typescript-dead-code-maturity.md before editing.

Own only Chunk <N>. Do not edit files owned by other chunks unless the plan says they are shared and you coordinate the root-kind names. Use TDD: write the failing focused test first, run it, implement the smallest correct change, rerun the focused test, then report changed files and validation output. Do not claim exact dead-code support. Keep JavaScript/TypeScript/TSX maturity at derived unless the coordinator explicitly changes the acceptance target.
```

## Open Questions For The Coordinator

- Should Hapi roots live under the generic `javascript.*` root namespace or a more specific `node.hapi.*` namespace?
- Should package entrypoint rooting map `dist/<service>.js` back to `<service>.ts` by basename, or should that require `tsup` config evidence first?
- Should dynamic ambiguity metadata be stored as `dead_code_ambiguity_kinds`, or folded into `dead_code_root_kinds` with `*.ambiguous` suffixes?
- Which three local Node service repos become mandatory dogfood gates for this feature branch, tracked only in private session notes?
