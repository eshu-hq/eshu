# JavaScript And TypeScript Dead-Code Maturity Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Mature Eshu dead-code detection for Node, JavaScript, TypeScript, and TSX so the main `api-node*` service family returns useful, bounded, evidence-explained findings without overclaiming cleanup-safe truth.

**Architecture:** Treat JavaScript, JSX, TypeScript, and TSX as one JavaScript-family dead-code capability with dialect-specific parser details. Keep `truth.level=derived` until package entrypoints, module resolution, framework roots, dynamic ambiguity, fixtures, API/MCP proof, and local dogfood gates agree. Add first-class Node/Hapi roots because the local validation corpus is dominated by `@dmm/lib-api-hapi` services with `server/handlers`, `server/init/plugins/spec*`, and `package.json` start/dev scripts.

**Tech Stack:** Go parser/query code, Tree-sitter JavaScript/TypeScript grammars, Eshu local-authoritative NornicDB graph, API/MCP `code_quality.dead_code`, Node package metadata, `tsconfig.json`, JavaScript/TypeScript fixtures, and local dogfood repos under `/Users/allen/repos/services/api-node*`.

---

## Context Snapshot

Current Eshu state:

- `go/internal/parser/javascript_dead_code_roots.go` emits parser-backed roots for Next.js route exports and Express registrations.
- `go/internal/query/code_dead_code_javascript_roots.go` excludes those parser-backed roots from dead-code results.
- `go/internal/query/code_dead_code_language_maturity.go` marks `javascript`, `typescript`, and `tsx` as `derived`.
- `tests/fixtures/deadcode/javascript`, `tests/fixtures/deadcode/typescript`, and `tests/fixtures/deadcode/tsx` exist, but they need stronger enforced parser/query/API proof.
- `docs/docs/reference/dead-code-reachability-spec.md` requires roots, fixture intent, ambiguity handling, API/MCP metadata, and backend proof before exactness can be claimed.

Local target evidence:

- `/Users/allen/repos/services` contains about 106 `api-node*` service repos with `package.json`.
- Sample services use Node 20, `tsx`, `tsup`, `typescript`, `@hapi/lab`, `@dmm/lib-api-hapi`, and `@dmm/lib-typescript-build-tools`.
- Runtime entrypoints usually appear in `package.json` as `start: node dist/api-node-*.js` and `dev: tsx api-node-*.ts`.
- Hapi route roots are mostly file-convention driven through `server/init/plugins/spec*` options:
  - `openapi.handlers: path.join(__dirname, '../../handlers')`
  - `openapi.handlers: path.resolve(__dirname, '../../handlers')`
- Route handler roots usually export HTTP methods:
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
  - Add root-kind constants and parser-backed roots for Node package entrypoints, Hapi/lib-api-hapi handler exports, and public module exports.
- Create `go/internal/parser/javascript_dead_code_package.go`
  - Parse and normalize `package.json` entrypoint evidence adjacent to a parsed source file.
- Create `go/internal/parser/javascript_dead_code_hapi.go`
  - Detect `@dmm/lib-api-hapi/init/plugins/specs` config and handler roots under configured handler directories.
- Create `go/internal/parser/javascript_dead_code_modules.go`
  - Model static import/export/require evidence and ambiguity markers without attempting full TypeScript resolution in one pass.
- Modify `go/internal/parser/javascript_language.go`
  - Attach new `dead_code_root_kinds` to functions, classes, variables, and components where parser evidence proves a root.
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

- [ ] **Step 1: Write failing parser tests for package entry roots**

Add a test repository with:

```text
package.json
api-node-sample.ts
server/public-api.ts
server/private-helper.ts
```

Assert:

- `api-node-sample.ts` root function or top-level entrypoint receives `javascript.node_package_entrypoint`.
- file named by `bin` receives `javascript.node_package_bin`.
- exported symbol reachable from package `exports` receives `javascript.node_package_export`.
- `private-helper.ts` unused local helper does not receive any root kind.

Run:

```bash
cd go
go test ./internal/parser -run TestDefaultEngineParsePathJavaScriptPackageDeadCodeRoots -count=1
```

Expected: FAIL because package metadata is not read yet.

- [ ] **Step 2: Implement package metadata discovery**

Implementation rules:

- Walk upward from the parsed source file to the nearest `package.json`, stopping at repository root when available.
- Parse with Go `encoding/json`, not string matching.
- Normalize `main`, `module`, `types`, `exports`, and `bin` to relative file candidates.
- Handle extensionless candidates by checking `.js`, `.jsx`, `.ts`, `.tsx`, `.mjs`, `.cjs`, `.mts`, and `.cts`.
- Ignore `dist/`, `build/`, and generated output unless source maps back to an authored source file in the fixture. This first chunk should root authored source entrypoints, not compiled output.

- [ ] **Step 3: Emit root metadata**

Attach root kinds to source entities only when evidence points at that source file or exported symbol. If the package points to `dist/api-node-jwt.js` and source has `api-node-jwt.ts`, root the source file entrypoint only when the basename relationship is direct and local tests prove it.

- [ ] **Step 4: Re-run focused tests**

Run:

```bash
cd go
go test ./internal/parser -run 'TestDefaultEngineParsePathJavaScriptPackageDeadCodeRoots|TestDefaultEngineParsePathJavaScriptEmitsDeadCodeRootKinds' -count=1
```

Expected: PASS.

- [ ] **Step 5: Add query exclusion test**

Add a failing query test that candidate rows with `javascript.node_package_entrypoint`, `javascript.node_package_bin`, and `javascript.node_package_export` are excluded and counted as parser metadata framework/public roots.

Run:

```bash
cd go
go test ./internal/query -run TestHandleDeadCodeExcludesJavaScriptPackageRoots -count=1
```

Expected before query change: FAIL.

- [ ] **Step 6: Update query policy**

Modify `deadCodeIsJavaScriptFrameworkRoot` or split a clearer `deadCodeIsJavaScriptRoot` helper so package roots are excluded by root metadata.

- [ ] **Step 7: Verify**

Run:

```bash
cd go
go test ./internal/parser ./internal/query -run 'JavaScriptPackage|DeadCode.*JavaScriptPackage' -count=1
```

Expected: PASS.

## Chunk 2: Hapi / lib-api-hapi Handler Roots

**Owner:** Subagent B.

**Scope:** Model the route convention used by the local `api-node*` service family. This is the highest-value business slice.

**Files:**

- Create: `go/internal/parser/javascript_dead_code_hapi.go`
- Modify: `go/internal/parser/javascript_dead_code_roots.go`
- Modify: `go/internal/parser/javascript_language.go`
- Modify: `go/internal/query/code_dead_code_javascript_roots.go`
- Test: `go/internal/parser/javascript_dead_code_hapi_roots_test.go`
- Test: `go/internal/query/code_dead_code_javascript_hapi_roots_test.go`
- Fixture: `tests/fixtures/deadcode/typescript-hapi` or expanded `tests/fixtures/deadcode/typescript`

- [ ] **Step 1: Write failing parser tests for handler exports**

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
import { plugin } from '@dmm/lib-api-hapi/init/plugins/specs';
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

- [ ] **Step 2: Add CommonJS fixture**

Add:

```js
module.exports.get = async (request) => ({ ok: true });
module.exports.patch = async (request) => request.payload;
```

Assert both exports receive `javascript.hapi_handler_export`.

- [ ] **Step 3: Implement Hapi spec-plugin detection**

Detection should require at least one strong signal:

- import or require of `@dmm/lib-api-hapi/init/plugins/specs`
- `openapi.handlers` path configured in the plugin options

Resolve `path.join(__dirname, '../../handlers')` and `path.resolve(__dirname, '../../handlers')` conservatively. If resolution fails, do not root all handlers; emit ambiguity metadata later instead.

- [ ] **Step 4: Implement handler export roots**

Root only exported HTTP method symbols under configured handler directories:

```text
get, post, put, patch, delete, head, options
```

Do not root every function in handler files. Helper functions inside handlers still need normal call/reference evidence.

- [ ] **Step 5: Add query test and policy update**

Add a query test that `javascript.hapi_handler_export` excludes a candidate and appears in analysis root categories.

Run:

```bash
cd go
go test ./internal/parser ./internal/query -run 'Hapi|JavaScript.*Handler' -count=1
```

Expected: PASS after implementation.

## Chunk 3: Static Import, Require, Re-export, And Barrel Reachability

**Owner:** Subagent C.

**Scope:** Improve static references so Eshu stops treating imported/exported symbols as dead when JS/TS code reaches them through module syntax. This chunk must stay conservative and avoid full bundler emulation.

**Files:**

- Create: `go/internal/parser/javascript_dead_code_modules.go`
- Modify: `go/internal/parser/javascript_language.go`
- Modify: `go/internal/parser/javascript_require.go`
- Modify: `go/internal/query/code_dead_code_javascript_roots.go`
- Test: `go/internal/parser/javascript_dead_code_module_roots_test.go`
- Test: `go/internal/query/code_dead_code_javascript_module_roots_test.go`

- [ ] **Step 1: Write failing parser tests for ESM imports**

Fixture:

```ts
import { formatUser } from './format';
import DefaultClient from './client';
export { PublicThing } from './public-thing';
export * from './barrel';

formatUser('a');
new DefaultClient();
```

Assert imported/re-exported symbols produce reference or root metadata sufficient to suppress false positives.

- [ ] **Step 2: Write failing parser tests for CommonJS require**

Fixture:

```js
const forex = require('../resources/forex');
const { formatRate } = require('../resources/format');

module.exports.get = async () => forex.getRates(formatRate());
```

Assert required members are referenced.

- [ ] **Step 3: Implement static module evidence**

Model:

- `import x from './x'`
- `import { x } from './x'`
- `import * as x from './x'`
- `require('./x')`
- destructured `require`
- `export { x } from './x'`
- `export * from './x'`

Rules:

- Static relative imports can produce references.
- Package imports should not root local source unless workspace/package metadata maps them to a local package.
- Dynamic imports with non-literal arguments should emit ambiguity metadata, not roots.

- [ ] **Step 4: Add query classification**

If a candidate is excluded because parser metadata says it is statically imported or re-exported, report the root category in `analysis.modeled_framework_roots` or a better JavaScript-family root list.

- [ ] **Step 5: Verify**

Run:

```bash
cd go
go test ./internal/parser ./internal/query -run 'JavaScript.*Module|TypeScript.*Module|Require|Reexport' -count=1
```

Expected: PASS.

## Chunk 4: Dynamic Ambiguity And Framework Callback Markers

**Owner:** Subagent D.

**Scope:** Make uncertain JS/TS patterns explicit so results are useful without pretending to be exact.

**Files:**

- Modify: `go/internal/parser/javascript_dead_code_roots.go`
- Create or modify: `go/internal/parser/javascript_dead_code_modules.go`
- Modify: `go/internal/query/code_dead_code_classification.go`
- Modify: `go/internal/query/code_dead_code_analysis.go`
- Test: `go/internal/parser/javascript_dead_code_ambiguity_test.go`
- Test: `go/internal/query/code_dead_code_javascript_ambiguity_test.go`

- [ ] **Step 1: Write ambiguity tests**

Cover:

```ts
await import(pluginName);
handlers[method](request);
container.register('name', Handler);
server.events.on(eventName, callback);
```

Expected behavior:

- dynamic imports with non-literal module specifiers become ambiguous evidence
- computed property dispatch becomes ambiguous evidence
- obvious event callback registration can root the named callback only when both emitter method and callback identifier are visible in the same file

- [ ] **Step 2: Add metadata without suppressing too much**

Prefer `dead_code_ambiguity_kinds` or similar metadata if existing result classification has a place for ambiguity. Do not hide all candidates in a file just because one dynamic expression appears.

- [ ] **Step 3: Report ambiguity in analysis**

API/MCP responses should include counts and categories so callers can see why truth remains `derived`.

- [ ] **Step 4: Verify**

Run:

```bash
cd go
go test ./internal/parser ./internal/query -run 'JavaScript.*Ambigu|DeadCode.*Ambigu' -count=1
```

Expected: PASS.

## Chunk 5: Fixture And API/MCP Proof Gate

**Owner:** Subagent E.

**Depends On:** Chunks 1 through 4.

**Scope:** Turn JS/TS/TSX fixtures into enforced dead-code truth gates.

**Files:**

- Modify: `tests/fixtures/deadcode/README.md`
- Modify: `tests/fixtures/deadcode/javascript/README.md`
- Modify: `tests/fixtures/deadcode/typescript/README.md`
- Modify: `tests/fixtures/deadcode/tsx/README.md`
- Add fixture source files as needed
- Add tests under `go/internal/query` or `go/cmd/eshu` for fixture-backed API/local proof

- [ ] **Step 1: Normalize fixture intent**

Each JS-family fixture must include:

- truly unused symbol that should be reported
- direct call/reference that should not be reported
- Node package entrypoint
- package public API export
- Hapi handler export
- Express or Next.js root where appropriate
- static import and re-export
- CommonJS require
- generated/test exclusion
- dynamic ambiguous case

- [ ] **Step 2: Add parser fixture tests**

Assert each fixture emits expected `dead_code_root_kinds` or ambiguity metadata.

- [ ] **Step 3: Add query/API fixture tests**

Assert:

- known live symbols are not returned
- known unused symbols are returned
- generated/test symbols are excluded by default
- ambiguous symbols carry explanation metadata
- language maturity remains `derived`

- [ ] **Step 4: Add MCP-shaped proof**

Add or extend a local-authoritative test that exercises the same result envelope the MCP `find_dead_code` tool uses. It must require a bounded `repo_id` or workspace target and a limit.

- [ ] **Step 5: Verify**

Run:

```bash
cd go
go test ./internal/parser ./internal/query ./cmd/eshu -run 'DeadCode.*JavaScript|DeadCode.*TypeScript|LocalAuthoritativeDeadCode' -count=1
```

Expected: PASS.

## Chunk 6: Local Dogfood Against api-node Services

**Owner:** Subagent F, or current-session coordinator after code slices merge.

**Depends On:** Chunks 1 through 5.

**Scope:** Prove the behavior on representative local service repos without requiring the full `/Users/allen/repos/services` tree in every developer environment.

**Dogfood targets:**

- `/Users/allen/repos/services/api-node-jwt`
- `/Users/allen/repos/services/api-node-geo`
- `/Users/allen/repos/services/api-node-ai-provider`
- `/Users/allen/repos/services/api-node-chat`
- `/Users/allen/repos/services/api-node-whisper`
- `/Users/allen/repos/services/api-node-forex`

These cover mixed JS/TS, Hapi handlers, plugin specs, resource modules, OpenAPI specs, generated clients, and larger service shape.

- [ ] **Step 1: Capture discovery baselines**

Run for each target:

```bash
eshu index /Users/allen/repos/services/api-node-jwt --discovery-report /tmp/eshu-api-node-jwt-before.json
```

Inspect:

- `summary.content_files`
- `summary.content_entities`
- `top_noisy_directories`
- generated/client directories
- test/fixture directories

- [ ] **Step 2: Run local-authoritative indexing**

Before runtime proof:

```bash
./scripts/install-local-binaries.sh
export PATH="$(go env GOPATH)/bin:$PATH"
```

Then index one repo at a time. Do not run compose verification lanes in parallel.

- [ ] **Step 3: Query dead code through API/MCP shape**

Use bounded calls:

- scope by `repo_id`
- set a small limit first
- request summary/analysis metadata before large payloads
- record duration and truncation

Expected:

- Hapi handlers such as `_status.get`, route `post`, and dynamic handler-file exports are not reported as dead only because no caller edge exists.
- Resource helpers directly imported by handlers are not reported as dead.
- Generated and test-owned symbols are excluded by default.
- Dynamic registry cases are labeled ambiguous.

- [ ] **Step 4: Capture false positives**

For each repo, record a table:

| Repo | Candidate | File | Why live/dead/ambiguous | Missing root kind |
| --- | --- | --- | --- | --- |

Only turn evidence-backed false positives into fixtures.

- [ ] **Step 5: Performance acceptance**

Record:

- indexing wall time
- dead-code query duration
- result count
- truncation state
- whether source fallback was used

The target is a bounded call that returns under the `code_quality.dead_code` local-authoritative envelope in `specs/capability-matrix.v1.yaml`.

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
- Chunk 6 local dogfood evidence against selected `api-node*` repos
- Docs/ADR updates and capability-matrix language notes

## Subagent Assignment Prompt Template

Use this prompt for each worker:

```text
You are working in /Users/allen/personal-repos/eshu-hq/eshu on the docs/js-ts-dead-code-plan branch or its implementation successor branch.

Read AGENTS.md, docs/docs/reference/dead-code-reachability-spec.md, docs/docs/adrs/2026-05-07-dead-code-root-model-and-language-reachability.md, and docs/superpowers/plans/2026-05-08-javascript-typescript-dead-code-maturity.md before editing.

Own only Chunk <N>. Do not edit files owned by other chunks unless the plan says they are shared and you coordinate the root-kind names. Use TDD: write the failing focused test first, run it, implement the smallest correct change, rerun the focused test, then report changed files and validation output. Do not claim exact dead-code support. Keep JavaScript/TypeScript/TSX maturity at derived unless the coordinator explicitly changes the acceptance target.
```

## Open Questions For The Coordinator

- Should Hapi/lib-api-hapi roots live under the generic `javascript.*` root namespace or a more specific `node.hapi.*` namespace?
- Should package entrypoint rooting map `dist/api-node-*.js` back to `api-node-*.ts` by basename, or should that require `tsup` config evidence first?
- Should dynamic ambiguity metadata be stored as `dead_code_ambiguity_kinds`, or folded into `dead_code_root_kinds` with `*.ambiguous` suffixes?
- Which three `api-node*` repos become mandatory local dogfood gates for this feature branch?
