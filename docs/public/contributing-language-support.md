# Contributing Parser Support

Parser support lives in the Go parser registry, parser implementation, and
tests. Public language pages document that runtime contract; they are not a
separate source of truth.

For source-language relationship resolution, SCIP corroboration, or golden audit
work, also follow the
[Source-Language Resolver Contract](reference/source-language-resolver-contract.md).
Use `go/internal/parser/goldenaudit` for source-language golden graph fixtures:
expected nodes and edges must be authored from the fixture contract, not copied
from observed Eshu output.

Primary files:

```text
go/internal/parser/registry.go
go/internal/parser/*.go
go/internal/parser/*_test.go
docs/public/languages/
```

## Contract Model

Every parser claim needs three anchors:

- registry metadata in `go/internal/parser/registry.go`
- implementation in the language-specific parser path
- parser-level and integration tests that prove the emitted rows and query
  surface

Read a file's bytes through `shared.ReadSource` (or the engine-local
`readSource`), never a raw `os.ReadFile`. `Engine.ParsePath` reads the file once
up front and primes a call-scoped cache keyed by absolute path, so a parser that
reads through `shared.ReadSource` reuses that single physical read instead of
issuing its own; a parser that bypasses it forces a redundant second read of the
same file.

Repository-wide config that many files share (a language's project manifest,
not a single source file) should be memoized per resolved config path, not
recomputed per file and not collapsed to one value per repository root. The
JavaScript-family parser's `tsconfig.json`/`package.json` resolution
(`go/internal/parser/javascript/config_scope_cache.go`) is the reference
pattern: it caches parsed config content keyed by the resolved absolute config
file path, invalidates the entry on `(mtime, size)` change so a re-scanned
repository never serves a stale generation, and coalesces concurrent same-path
callers onto one in-flight computation instead of racing duplicate reads.
Single-flight coalescing for a shared cache like this MUST key the in-flight
computation by the full (path, generation-fingerprint) tuple, not by path
alone: keying by path alone lets a second goroutine observing a changed
generation for the same path overwrite the first goroutine's still-in-flight
slot, so a waiter blocked on the wrong goroutine's completion signal can read
back a value from the wrong generation. A process-global cache like this also
MUST be bounded (an LRU cap, not an unbounded map), since a long-running
ingester scans many repositories over its lifetime; evicting a config only
means the next file under it recomputes, which never affects correctness.

Keying by repo root instead of resolved config path is a correctness bug in a
monorepo: nested packages can each own a distinct config file, and a repo-root
key would leak one package's config into a sibling package's files.

Parse-only behavior is not supported query behavior. A parser can recognize a
syntax shape and still be unsupported for language-query, entity context,
story, relationship, or dead-code answers until those read paths have focused
tests and documentation.

Status words mean:

| Status | Meaning |
| --- | --- |
| `supported` | Extracted, surfaced end to end, and covered by tests. |
| `partial` | Only the documented subset is promised. |
| `unsupported` | Intentionally not claimed. |

## Parser And Language Contribution Checklist

Use this checklist for a small language feature, framework root, entity kind, or
parser-family extension.

1. Name the exact surface. Examples: new entity kind, parser metadata field,
   framework root, import form, relationship input, or query entity type.
2. Write the parser fixture or regression test first. The failing test should
   prove the row, metadata, or root kind that the contribution claims.
3. Implement the parser change in the owning package without changing graph,
   reducer, or query behavior by side effect.
4. Add query or integration proof before calling the feature supported on a
   read surface. Parser rows alone are not enough.
5. Update the affected language page under `docs/public/languages/`.
6. Update [Language Query DSL](reference/language-query-dsl.md) when accepted
   languages, entity types, backing-store behavior, errors, or truth ceilings
   change.
7. Update [Parser Feature Matrix](languages/feature-matrix.md),
   [Parser Support Matrix](languages/support-maturity.md), and
   [Dead Code Language Maturity](reference/dead-code-language-maturity.md) when
   the support summary, framework/root evidence, or dead-code maturity changes.
8. For call, import, inheritance, interface, overload, or framework relationship
   claims, add source-authored golden audit fixtures whose expected nodes and
   edges are independent from Eshu output. The `goldenaudit` helper reports
   missing, unexpected, and duplicate graph facts deterministically so CI does
   not compare Eshu output to itself. Its `ScoreAccuracy` scorer adds per-type
   and overall precision/recall with a wrong-target vs missing vs extra
   breakdown, so an edge resolved to the wrong callee fails even when its tier
   distribution looks healthy.
9. Run the focused Go tests, `scripts/verify-parser-relationship-kit.sh`, the
   docs build, and `git diff --check`.

### Import/symbol names: derived from parse for tree-sitter languages

The repository `ImportsMap` (declared name → declaring paths, sent as one fact
per generation) is built two ways depending on the language:

- **php, javascript, typescript, tsx** derive their `ImportsMap` names from the
  parse-stage declaration buckets (`functions`/`classes`/`interfaces`/`traits`)
  during the normal parse pass — they do **not** run a separate pre-scan
  tree-sitter parse. This avoids parsing every file twice on a full ingest.
- All other pre-scan languages (json, groovy) keep the dedicated pre-scan pass.

If you add a new language to the derive-from-parse set
(`parser.IsDerivedPreScanLanguage`), its parse-stage buckets must carry exactly
the same names its `PreScan` would have collected — prove it output-preserving
(a 0/0 symmetric-set-difference test against the legacy `PreScan`, as in
`prescan_derive_test.go`) before removing the second pass. Delta syncs keep the
legacy pre-scan path regardless, because pre-scan spans the whole repo while
parse visits only changed targets.

## Test Path

Start narrow, then prove the read path you claim.

| Claim | Minimum focused proof |
| --- | --- |
| Parser syntax or metadata only | `cd go && go test ./internal/parser -run <focused-test> -count=1` |
| Parser behavior used by collection or content shape | `cd go && go test ./internal/parser ./internal/collector/discovery ./internal/content/shape -run <focused-test> -count=1` |
| Language query, entity resolve/context, story, or relationships | Parser test plus focused `go/internal/query` test for the public route or helper. |
| Dead-code root or maturity change | Parser root test plus focused `go/internal/query` dead-code test and maturity-doc update. |
| Infrastructure/config language evidence | Parser test plus relationship or query proof when the evidence feeds those surfaces. |

The public language page should cite the main test names or fixture proof a
reviewer can rerun. Avoid broad "covered by tests" claims that do not point to
the owned package or query surface.

## Support-Maturity Promotion Rules

Treat maturity as a contract, not a confidence word.

| Promotion | Required proof |
| --- | --- |
| `unsupported` to `partial` | Parser registry entry, parser fixture, language page with exact limits, and negative cases showing what remains unsupported. |
| `partial` to `supported` | Parser proof, integration or graph/content-backed query proof, updated language page, and updated matrix rows. |
| Framework/root evidence increase | Positive fixture for the supported root, negative fixture for a similar unsupported pattern, and ambiguous fixture when heuristics could over-admit. |
| Dead-code maturity increase | Parser roots, query suppression or candidate proof, language page update, dead-code maturity map update, and exactness blockers reviewed. |
| New query language or entity type | Query handler or registry test, Language Query DSL update, language page update, and unsupported-language/error behavior preserved. |

Do not promote support because a parser emits rows. Support starts when the
normal user-facing read path can answer with documented truth and limitations.
If the feature is source evidence only, call it source evidence only.
Relationship-resolution promotions must satisfy the
[Source-Language Resolver Contract](reference/source-language-resolver-contract.md):
direct or corroborated evidence can be admitted only with reducer and read-path
proof, while ambiguous or unsupported evidence stays reviewable but non-canonical.

## Query DSL And Language Page Updates

Update the affected language page whenever a parser contribution changes:

- supported surfaces, partial surfaces, or unclaimed behavior
- parser entrypoint, fixture repository, registry metadata, or main tests
- query surfacing through language-query, search, entity resolve/context,
  repository story, relationship, or dead-code paths
- framework or root evidence and exactness blockers
- generated-code, dynamic behavior, or plugin-loading boundaries

Update [Language Query DSL](reference/language-query-dsl.md) when a contribution
changes:

- accepted `language` values
- accepted `entity_type` values
- graph-backed, graph-first, or content-only backing behavior
- limit, timeout, ordering, error, unsupported-language, or truth-label behavior
- MCP and HTTP parity for `execute_language_query`

## Dynamic And Framework Guardrails

Dynamic imports, plugin loading, reflection, generated code, and
framework-specific roots are support boundaries. Do not document them as
supported unless the PR includes focused proof for that exact pattern.

Guardrails:

- Dynamic imports, dynamic `require`, reflective dispatch, runtime plugin
  loading, generated source, macro expansion, dependency injection, and
  framework discovery stay unsupported or exactness blockers until tested.
- Framework roots need positive, negative, and ambiguous cases. A route,
  callback, lifecycle hook, package export, or public API shape should be named
  in the test and the docs.
- Generated code must not make source symbols cleanup-safe unless Eshu indexes
  the generated files or has a tested source-to-generated mapping.
- Source-only parser metadata must not be used to claim graph, query, story, or
  dead-code support without the consumer path that reads it.

## TypeScript Public-Surface Cache (Package-Root Scoped)

TypeScript dead-code analysis (`dead_code_root_kinds` for exported
declarations) resolves whether a declaration is reachable through a package's
public entry point (`package.json` `exports`/`types`) by walking the barrel
re-export graph rooted at that entry point, up to 8 hops deep. That closure
graph is identical for every source file inside one package: the walk starts
at the same public entry file and follows the same re-export edges regardless
of which file is currently being parsed.

To avoid re-parsing that identical closure once per file (issue #4765), the
per-node facts the walk needs from each file it visits -- its static re-export
edges, its named-import bindings, and which imported names each of its public
declarations mentions -- are memoized in a package-root-scoped cache
(`go/internal/parser/javascript/typescript_public_surface_cache.go`). The
cache key is `(package root, file path, mtime, size)`, so:

- Two different packages in a monorepo never share cache entries, even if a
  re-exported path happens to collide.
- Editing a barrel or a re-exported module invalidates only that file's entry
  (a changed `mtime`/`size` is a different generation), so the next file
  parsed after the edit recomputes rather than reusing a stale extraction.
- Concurrent parse workers touching the same package coalesce onto one
  in-flight computation per `(package root, file path)` key instead of racing
  a duplicate parse (single-flight, mirroring the tsconfig/package.json
  config-scope cache from issue #4515 P2a).

Contributors changing the TypeScript public-surface walk
(`javaScriptTypeScriptSurfaceRootKinds`, the reexport and imported-type-reference
BFS functions, or their per-node fact extraction) must keep the cached facts
(`typeScriptPublicSurfaceNodeFacts`) equivalent to what a fresh, uncached parse
of that file would produce -- the cache must never change which declarations
are marked as public-surface reachable, only how many times each file in the
closure is parsed.

## JS/TS/PHP Parse Byte Cap

JavaScript, TypeScript, TSX, and PHP source files larger than 1 MiB have their
tree-sitter parse skipped entirely, mirroring the SQL segment byte cap
(`go/internal/parser/sql/segments.go`, #4422). Large generated files -- a
minified webpack bundle, a bundled vendor library, a CID font map -- parse
superlinearly under tree-sitter; full-corpus discovery finds pathological
files in this size range across real repositories (#4766). Normal
hand-written source is tens of KB, so 1 MiB is generous headroom above any
legitimate single file.

The cap lives in each language family's `Parse` entry point
(`go/internal/parser/javascript/javascript_language.go`'s `jsParseByteCap`,
`go/internal/parser/php/parser.go`'s `phpParseByteCap`), covering TypeScript
and TSX through the shared javascript-family parser. A bounded file returns an
otherwise-empty payload with no extracted entities; the bound is recorded in
`payload["js_parse_bounded"]` or `payload["php_parse_bounded"]` and logged, so
a dropped parse is observable rather than silent.

## Workflow

1. Add or update Go parser tests first.
2. Implement or adjust parser behavior.
3. Add integration or query coverage for surfaced graph behavior.
4. Update the affected language page and, when needed, the feature or support
   matrix.
5. Run focused Go tests and the docs build.
6. Back support-maturity promotions with real indexing or compose-backed proof,
   not parser fixtures alone.

## Writing Language Pages

Keep pages short and factual:

- identify the parser, entrypoint, fixture, and main tests
- summarize supported surfaces without duplicating the full matrix
- name the dead-code maturity and exactness blockers
- link to canonical references for deep detail

Use [Parser Feature Matrix](languages/feature-matrix.md),
[Parser Support Matrix](languages/support-maturity.md), and
[Dead Code Language Maturity](reference/dead-code-language-maturity.md) as the
canonical summaries.

## Verification

```bash
cd go
go test ./internal/parser ./internal/collector ./internal/content/shape -count=1
```

Run the parser and relationship kit verifier:

```bash
scripts/verify-parser-relationship-kit.sh
```

Then build docs:

```bash
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
```

If YAML, tests, generated docs, and public pages disagree, fix the disagreement
before merging.
