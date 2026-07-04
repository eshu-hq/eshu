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
