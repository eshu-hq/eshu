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

### Content-metadata sniffing is skipped for non-IaC-signal source files

`Engine.ParsePath` calls `inferContentMetadata` after every parse to populate
`artifact_type`, `template_dialect`, and `iac_relevant`. That inference runs
several regex scans (a Go template line-control scan, a Terraform
`templatefile()` scan, and the marker scans described below), costing roughly
7.5ms on a large PHP/JS file -- about 5% of `ParsePath` wall time across a full
corpus (issue #4768). Most source files carry no IaC/template signal at all,
so `shouldSkipContentMetadata` (`go/internal/parser/content_metadata_gate.go`)
gates the call: it skips `inferContentMetadata` and uses the zero-value
`contentMetadata{}` only when the file's extension, path segments, basename,
and content contain none of the signals `inferContentMetadata`'s own
predicates match on.

For a file this gate correctly declines to skip, `inferContentMetadata` used
to call root-family detection (`inferRootFamily`), which internally
re-scanned the same five marker regexes (Go-template expression, Jinja
statement, and Terraform interpolation/directive/`templatefile()`) that
`inferContentMetadata` itself scans again a few lines later -- pure
duplicated work, since root-family detection has exactly one call site
(issue #4805). The fix hoists those marker scans to run once and passes the
results down via a small struct, preserving root-family detection's original
unfiltered match semantics exactly (see `go/internal/parser/README.md#hoisted-marker-scan`
for the full before/after proof).

This gate does not simply skip "source-code languages." A `.py`, `.js`, or
`.php` file living under an Ansible (`roles/`, `playbooks/`, `handlers/`,
`tasks/`, `group_vars/`, `host_vars/`, `inventory/`, `inventories/`), Dagster
(`dagster/`, `assets/`, `data_quality/`, `data_lakehouse/`), Helm/Argo
(`chart/`, `templates/`, `argocd/`), GitHub Actions (`.github/workflows/`), or
bare `iac/` path segment is legitimately reclassified by path alone (for
example `ansible_role` or `iac_relevant=true`) and always runs the real
inference. So does any file whose content contains an Ansible-playbook marker
substring (`hosts:`, `roles:`, `vars_files:`, or `import_playbook:` at the
start of a line) regardless of its path or extension, since that
classification is content-based, not path-based. The extension check itself
must test every dot-suffix a filename carries, not only the last one (a
`vars.tf.json` file is real `terraform_hcl` via its `.tf` suffix even though
`.json` is last), and must include `.conf`/`.cfg`/`.cnf` (raw config files
reclassified to `nginx_config`/`apache_config`/`generic_config`) and `.kcl`
(which gets template-marker handling identical to YAML).

An earlier version of this gate skipped by last-suffix only and omitted
`.conf`/`.cfg`/`.cnf`/`.kcl` entirely, silently corrupting persisted
`artifact_type`/`iac_relevant` for those files -- caught by a hostile
`eshu-code-review` before merge, not by the equivalence test, because that
test's hand-picked case list did not happen to include those shapes. The gate
is now proven, not just asserted, against those trigger classes: in addition
to the hand-picked equivalence cases, `content_metadata_gate_test.go` runs a
generative differential (`TestShouldSkipContentMetadataGeneratedEquivalence`)
over a cartesian product of every gated/plain extension (including multi-dot
shapes like `vars.tf.json`), every signal/plain directory context, and every
content signal, asserting `shouldSkip==true => inferContentMetadata(path,
content) == contentMetadata{}` for every generated combination -- plus two
tests proving that assertion is not a tautology by showing it fails red
against a too-wide gate and against the pre-fix gate shape.

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
| Parser syntax or metadata only | `cd go && go test ./internal/parser/... -run <focused-test> -count=1` |
| Parser behavior used by collection or content shape | `cd go && go test ./internal/parser/... ./internal/collector/discovery ./internal/content/shape -run <focused-test> -count=1` |
| Language query, entity resolve/context, story, or relationships | Parser test plus focused `go/internal/query` test for the public route or helper. |
| Dead-code root or maturity change | Parser root test plus focused `go/internal/query` dead-code test and maturity-doc update. |
| Infrastructure/config language evidence | Parser test plus relationship or query proof when the evidence feeds those surfaces. |

External parser tests that exercise the parent Engine contract can reuse
`go/internal/parser/parsertest` for fixture setup and map-shaped payload
assertions. The package is test-only: production parser packages must not
import it, and language-specific fixtures and expectations stay in their owning
family. It imports `internal/parser`, so only external `<lang>_test` packages
can use it — a root-internal `package parser` test importing it is an import
cycle, which is why the parent package keeps its own copies of some helpers.

`AssertBucketItemByFieldValue` fails closed on a malformed payload rather than
treating it as absent, and new helpers here should do the same — some older
ones in this package still use the discarded form and have not been hardened
yet, so do not read the guarantee as package-wide.

A field that is present but of an unexpected type is reported, not skipped:
the older discarded-assertion form let a wrongly-typed value read as the zero
value, so a negative assertion passed and a lookup for the empty string matched
a broken row. When adding a helper here, distinguish absent from
present-but-malformed, and split the predicate out of the assertion so the
malformed branch is reachable by a test — `t.Fatalf` cannot be driven from one
otherwise.

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
tree-sitter parse skipped entirely in the normal parse stage, mirroring the
SQL segment byte cap (`go/internal/parser/sql/segments.go`, #4422). Large
generated files -- a minified webpack bundle, a bundled vendor library, a CID
font map -- parse superlinearly under tree-sitter in that stage; full-corpus
discovery finds pathological files in this size range across real
repositories (#4766). Normal hand-written source is tens of KB, so 1 MiB is
generous headroom above any legitimate single file.

The cap lives in each language family's `Parse` entry point
(`go/internal/parser/javascript/javascript_language.go`'s `jsParseByteCap`,
`go/internal/parser/php/parser.go`'s `phpParseByteCap`), covering TypeScript
and TSX through the shared javascript-family parser. A bounded file returns an
otherwise-empty payload with no extracted entities; the bound is recorded in
`payload["js_parse_bounded"]` or `payload["php_parse_bounded"]` and logged, so
a dropped parse is observable rather than silent.

The same cap also covers the repository pre-scan stage (`preScanNames` in
`go/internal/parser/javascript/prescan.go` and
`go/internal/parser/php/prescan.go`, closing the gap tracked by #4808).
Pre-scan runs across the full repository on every delta sync -- unlike the
normal parse stage, which only visits changed targets -- so an over-cap file
would otherwise still pay the same superlinear parse cost there even after
#4766 bounded the normal parse stage. A bounded file contributes no pre-scan
names, mirroring `Parse`'s bounded (empty) payload for the same file; the
bound is logged (`javascript-family pre-scan file bounded` /
`php pre-scan file bounded`) since pre-scan has no payload map to carry a
`*_parse_bounded` row.

## Line Endings

`shared.ReadSource` is the single read boundary every language `Parse` in
`go/internal/parser` goes through, and it rewrites carriage returns to `\n`
before any parser sees the bytes -- but only in a source that contains **no
`\n` at all** (#6306). Any source with a newline in it is returned unchanged,
as the caller's own slice.

This exists because nothing downstream treats a lone `\r` as a line break.
tree-sitter advances `StartPosition().Row` only on `\n`, so on a classic-Mac
file every AST-derived `line_number` in every language was 1; measured
directly, three C functions on physical lines 1, 2 and 3 all reported row 0.
`bufio.ScanLines` splits only on `\n` (it merely trims a trailing `\r`),
and `strings.Split(src, "\n")`, `strings.IndexByte(rest, '\n')` and the
`(?m)$` regex anchor share the same blind spot. The failure was silent
throughout: a lockfile yielding zero dependencies, a header yielding zero
public roots, or every row stamped line 1 -- never an error.

### The rule is file-scoped, not byte-scoped

A `\r` byte carries no meaning on its own. The same byte terminates a line in
a classic-Mac file and carries payload inside a Go raw string, a regex, or a
route or wire-format constant. Nothing local to the byte separates the two, so
the decision is made about the whole file: the absence of `\n` is the signal.
A source with even one newline already has a working LF or CRLF convention and
correct line numbers, so every `\r` in it is data or half a CRLF pair; a
source with no newline has no other candidate terminator.

Getting this wrong is not theoretical. A byte-scoped rewrite turned the Go
route `` `GET /foo<CR>bar` `` -- whose runtime value is `/foobar`, because
`strconv.Unquote` drops `\r` from a raw string but keeps `\n` -- into the
path `GET /foo\nbar` with its method degraded from `GET` to `ANY`. It is
pinned by `TestParsePathPreservesDataCarriageReturnInsideGoRawString`.

Two limits follow, and neither is fixable at the byte level:

- A **mixed** file -- LF or CRLF lines plus a `\r` the author meant as a
  separator -- keeps that `\r`, and the line it merges stays merged. That
  shape is byte-for-byte identical to a data `\r` in an LF file. Leaving one
  line of a malformed file merged is the smaller loss.
- A classic-Mac file that embeds a data `\r` inside a literal has that byte
  rewritten along with the separators around it. Such a file parses as a
  single line today, so there is no working behavior to protect.

Two rules follow for new parser code:

- **Do not add your own line-ending handling to a language parser.** By the
  time `Parse` has its bytes there is no bare CR left, so a `\r` case in a
  scanner is dead weight at best and a second, divergent normalization at
  worst.
- **A file read outside `shared.ReadSource` does not inherit this.** A parser
  that reaches for a second file with its own `os.ReadFile` -- a C/C++ header,
  a sibling module, a tsconfig, a Terragrunt include -- must call
  `shared.NormalizeLineEndings` on those bytes itself. Six call sites do this
  today. Five of them lost real data without it:

    - the C and C++ public-header scans, which strip line comments with
      `(?m)//.*$`; on a bare-CR header that deletes from the first `//` to end
      of file, so the file yields zero public-header roots;
    - `goModulePath`, which splits `go.mod` on `\n` and returns an empty
      module path for the whole module when the file arrives as one line;
    - the Python package-`__init__` scan, which finds no `from ... import`
      statement and drops the `python.package_init_export` root from every
      symbol the package re-exports;
    - the Terragrunt include-chain walk in `hcl/include_chain.go`, which reads
      both the starting `terragrunt.hcl` and every parent it follows with its
      own `os.ReadFile` and hands the bytes to `hclparse.ParseHCL`. `hclsyntax`
      recognises only `\n` and `\r\n` as a newline -- its scanner defines
      `Newline = '\r' ? '\n'` and terminates a `#` or `//` comment at
      end-of-line -- so on a classic-Mac file one leading comment swallows the
      rest of the file. Every `remote_state` block after it disappears, the
      parse reports no error, and anything that does survive is stamped
      `line_number` 1.

  The sixth, the JavaScript sibling parser, normalizes for consistency rather
  than for a currently observable defect -- its consumers read names by byte
  offset, not by line -- but its rows would otherwise all be 0.

  Note that a text fallback can mask one of these and hide the loss. The
  include walk recovers its parent target through a raw-source regex
  (`collectNormalizedHelperPaths`) that never looks at line endings, so the
  include *declaration* survived a bare-CR file by accident while the
  `remote_state` *block*, which has no such fallback, did not. Do not read a
  passing end-to-end case as proof the parse is intact.

The rewrite is length-preserving on purpose: a `\r` becomes `\n` in place and
no byte is added or removed, so a byte offset into the returned buffer still
addresses the same byte on disk. The JSONC offset translator
(#5358), the SQL entity spans and every `IndexSource` snippet depend on that.
Collapsing CRLF to LF would shift all of them, which is why normalization
stops at the bare CR.

Only the parser's view is normalized. The git collector hands its own raw
bytes to the content snapshot whose digest becomes content identity, and
`NormalizeLineEndings` never mutates its input, so content identity does not
move for any file.

## Parser Worker Sizing

Parser subsystems that fan work across goroutines size their worker pools from
`cpubudget.UsableCPUs()`, not `runtime.GOMAXPROCS`/`runtime.NumCPU`, so parser
concurrency respects the container's effective cgroup CPU limit instead of the
host core count (#4456, #4759). This prevents oversubscription when the
ingester runs under a constrained CPU quota. When adding a new concurrent
parser stage, size its pool from `cpubudget.UsableCPUs()` and honor any
existing worker-count override (e.g. `ESHU_PARSE_WORKERS`) with the same
precedence as `go/internal/parser/go_package_interface_prescan.go`. (One
exception: `go/internal/parser/interproc/solve.go` stays on
`runtime.GOMAXPROCS(0)` directly — `interproc` is a standard-library-only
package, and `GOMAXPROCS(0)` is already cgroup-aware under Go 1.25+.) This is a
concurrency-sizing contract, not a language/support-maturity claim.

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
go test ./internal/parser/... ./internal/collector ./internal/content/shape -count=1
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
