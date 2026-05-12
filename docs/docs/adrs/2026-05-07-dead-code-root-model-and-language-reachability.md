# ADR: Dead-Code Root Model And Language Reachability

**Date:** 2026-05-07
**Status:** Proposed
**Authors:** Allen Sanabria
**Deciders:** Platform Engineering
**Related:**

- `../reference/dead-code-reachability-spec.md`
- `../languages/feature-matrix.md`
- `../../superpowers/plans/2026-05-07-dead-code-root-model-and-language-reachability.md`
- `2026-04-24-iac-usage-reachability-and-refactor-impact.md`

---

## Context

The MCP `find_dead_code` workflow returned false-positive candidates for the
Eshu repository itself. The reported candidates included local wiring
functions, bootstrap interfaces, structs, and methods such as `wireAPI`,
`ExecuteCypher`, `Claim`, `Close`, `applySchema`, `openBootstrapDB`, and
`openBootstrapGraph`.

Local validation showed those candidates are not confidently dead. They are
reachable through Go function-value wiring, dependency-injection structs,
interface satisfaction, method dispatch, or local bootstrap orchestration. The
focused package gate still passed:

```bash
cd go && go test ./cmd/api ./cmd/bootstrap-data-plane ./cmd/bootstrap-index -count=1
```

The MCP envelope was truthful about its confidence: `code_quality.dead_code`
returned `truth.level=derived`, and the analysis metadata said the result used
partial root modeling. The product issue is that users expect dead-code
analysis to work for every Eshu-supported language, while the current
implementation only has partial root policy coverage:

- Go entrypoints, selected Cobra roots, selected `net/http` roots,
  controller-runtime `Reconcile` roots, and Go public API roots.
- Python FastAPI, Flask, and Celery decorator roots.
- JavaScript and TypeScript Next.js route exports and Express registrations.
- Test and generated-code exclusions.

The parser feature matrix covers many more source languages than this policy
does: C, C++, C#, Dart, Elixir, Go, Haskell, Java, JavaScript, Kotlin, Perl,
PHP, Python, Ruby, Rust, Scala, Swift, TypeScript, TSX, and related IaC/config
parsers. Parser support is not the same thing as exact dead-code support.

## Tree-Sitter Research

Tree-sitter is the right syntax substrate for this work, but it is not by
itself a complete dead-code oracle.

The official Tree-sitter documentation describes the core objects as
languages, parsers, syntax trees, and syntax nodes:
<https://tree-sitter.github.io/tree-sitter/using-parsers/1-getting-started.html>.
The parser produces concrete syntax trees with nodes, source positions, and
named/anonymous node distinctions:
<https://tree-sitter.github.io/tree-sitter/using-parsers/2-basic-parsing.html>.

Tree-sitter queries can capture definitions and references. Its code-navigation
guide describes captures such as `@definition.function`,
`@definition.interface`, `@reference.call`, and
`@reference.implementation`:
<https://tree-sitter.github.io/tree-sitter/4-code-navigation.html>. The query
language supports alternation, captures, grouping, quantifiers, and anchors:
<https://tree-sitter.github.io/tree-sitter/using-parsers/queries/2-operators.html>.

Those capabilities are enough to extract syntax evidence across languages.
They are not enough to prove cross-file type resolution, Go interface
satisfaction, dynamic registration, reflection, build-tag reachability, or
framework-specific roots without additional language adapters, SCIP evidence,
configuration rules, and conservative ambiguity handling. Eshu already states
this in `go/internal/parser/README.md`: SCIP supplements native Tree-sitter
output where Tree-sitter alone cannot reliably produce type-qualified call
graphs.

## Problem

The current dead-code query starts from this graph condition:

```cypher
MATCH (e)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(r:Repository)
WHERE (e:Function OR e:Class OR e:Struct OR e:Interface)
  AND NOT ()-[:CALLS|IMPORTS|REFERENCES]->(e)
```

That is a useful candidate scan, not a dead-code proof.

The missing model has three parts:

1. **Root completeness:** each supported language needs a tested set of
   language entrypoints, public API roots, framework roots, callback roots,
   dynamic registration roots, and generated/tool-owned exclusions.
2. **Reachability semantics:** candidates must be evaluated against transitive
   reachability from roots, not just absence of direct inbound call/reference
   edges.
3. **Evidence grading:** when a parser-supported language or framework lacks
   enough proof, the result must be `derived_candidate_only`, `derived`, or
   `ambiguous`. It must not look like a confident cleanup list.

Go exposed the gap first because its real-world code frequently uses function
values, local interfaces, method sets, and dependency injection. Other
languages have equivalent dynamic surfaces: Python decorators and imports,
JavaScript/TypeScript module exports and framework routers, Java annotations,
Rust traits, Ruby metaprogramming, PHP framework controllers, and Swift/Scala
framework callbacks.

## Decision

Eshu will implement a language-aware dead-code reachability model instead of
adding more ad hoc query filters.

This ADR owns the dead-code maturity PR. The PR is not a narrow Go-only fix.
It should make the dead-code capability visibly mature across the full
parser-supported source-language surface by adding:

- language maturity metadata in the API/MCP response
- a dedicated dead-code fixture strategy and checked-in fixture inventory for
  every parser-supported source language
- dogfood regressions for the Eshu false positives that exposed the gap
- first-wave Go semantic root handling for function values, interfaces, method
  dispatch, and dependency-injection callbacks
- a promotion path from candidate-only to derived to exact that is backed by
  fixtures, root modeling, and runtime proof

The implementation may land exactness language by language, but the PR scope is
the maturity framework for the whole dead-code capability.

Dead-code analysis will be split into four explicit layers:

1. **Syntax evidence extraction.** Tree-sitter and existing language adapters
   emit definitions, calls, references, exported/public surfaces, decorators or
   annotations, registrations, type declarations, interface or trait
   declarations, and source spans.
2. **Language semantic evidence.** Language adapters and SCIP, where available,
   enrich syntax evidence with package/module identity, receiver or class
   context, method ownership, interface/trait implementation evidence, import
   resolution, and known framework registrations.
3. **Reachability materialization.** Reducer-owned work materializes root facts
   and reachable entities from language roots, framework roots, public API
   roots, generated exclusions, test exclusions, user overrides, and graph
   call/reference edges.
4. **Query classification.** HTTP, MCP, and CLI dead-code surfaces return
   unused, reachable, ambiguous, derived-candidate-only, unsupported-language,
   or excluded results with structured evidence and truth labels.

For a language to be called exact for dead-code, it must have:

- definition extraction for the candidate kinds it reports
- call/reference extraction or SCIP-backed equivalent
- a tested root policy for executable entrypoints and public API surfaces
- framework or dynamic-dispatch rules for common roots in that ecosystem
- a dedicated dead-code fixture corpus for that language, separate from the
  general parser feature fixture
- positive, negative, and ambiguous fixtures
- API/MCP/CLI tests that prove the truth label and limitations metadata
- backend conformance proof for both NornicDB and Neo4j query shapes

The exactness contract is mandatory for each parser-supported source language,
not optional per dogfood slice. If a language supports a syntax or semantic
path that can define, reference, export, register, decorate, annotate, import,
inherit, implement, dispatch to, or otherwise make code reachable, Eshu must
model that path before returning an exact dead-code result. If Eshu cannot
model it yet, the response must stay non-exact and name the blocker. Valid
language behavior cannot be silently ignored in an exact result.

The language contract is static, but the exact result is scope-specific. A
language adapter may support exactness only when the indexed repo or query
scope has the required evidence and no unresolved blockers such as unresolved
macros, dynamic imports, build tags, target features, annotation processors,
reflection, metaprogramming, or framework auto-discovery.

Until a language meets those gates, Eshu may still return candidate findings,
but the response must remain non-exact and must include which language/root
categories were not modeled. Parser-supported languages without dedicated
dead-code fixtures are `derived_candidate_only`, not unsupported.

## Go Acceptance Slice

The first implementation slice will close the false positives seen in the Eshu
repo and make that regression permanent.

Required Go evidence:

- function values passed into wiring structs and constructor fields
- functions used as dependency-injection callbacks
- local interfaces used as parameter, field, or return types
- structs that satisfy local interfaces by method set
- methods reachable through interface dispatch
- exported symbols outside `cmd/`, `internal/`, and `vendor/`
- `main`, `init`, Cobra, `net/http`, and controller-runtime roots already
  modeled today
- ambiguous cases for reflection, build tags, generated registries, and dynamic
  maps of function names

The exact candidates from the 2026-05-07 MCP run must become regression
fixtures. Eshu should not report the following as dead in the dogfood fixture
unless a future code change truly removes their usage:

- `wireAPI`
- `ExecuteCypher`
- `bootstrapDB`
- `bootstrapExecutor`
- `neo4jDeps`
- `neo4jSchemaExecutor`
- `openBootstrapDB`
- `openNeo4j`
- `bootstrapCanonicalWriterConfig`
- `Claim`
- `Close`
- `applySchema`
- `bootstrapCommitter`
- `collectorDeps`
- `drainingWorkSource`
- `openBootstrapGraph`

Current local-authoritative evidence on 2026-05-07 found and fixed a runtime
selection regression that had made local graph proof look like a dead-code
canonical writer regression.

Fast baseline runs in this worktree before the later dead-code additions stayed
inside the existing local-authoritative envelope:

- `phase=files` completed in `7.89s`.
- `Function` canonical entity writes completed `8,424` rows as `1,320`
  statements and `268` grouped executions in `1.01s`, with average grouped
  execution `0.0038s`.
- `phase=entities` completed in `6.19s`.
- Source-local projection succeeded in `20.79s`.

Later live validation on the same branch exposed a slow canonical write profile:

- `phase=files` rose to about `16.9s`.
- `Function` canonical entity writes rose to `8,467` rows as `1,333`
  statements and `272` grouped executions in `431.24s`, with average grouped
  execution `1.585s`.
- Removing the speculative pre-entity retract experiment did not restore the
  fast path.
- Keeping `dead_code_root_kinds` out of canonical graph properties is still a
  useful graph hot-path cleanup, but by itself did not restore performance.
- Enabling `ESHU_NORNICDB_BATCHED_ENTITY_CONTAINMENT=true` reduced statement
  shape but still showed slow grouped Function executions, so it is not the
  acceptance fix for this PR.

The root cause was not the dead-code payload or canonical writer shape. It was
sticky process-mode selection: setting `ESHU_NORNICDB_BINARY` alone selected
external-process NornicDB for `eshu graph start`, even though normal
local-authoritative proof is expected to use the embedded runtime when
available. On this machine that process-mode lane reproduced the slow profile;
the embedded lane stayed inside the previous envelope.

Accepted fix: process-mode local graph now requires
`ESHU_NORNICDB_RUNTIME=process`. `ESHU_NORNICDB_BINARY` is only a binary path
override after process mode is selected.

Post-fix live proof on 2026-05-07 intentionally left
`ESHU_NORNICDB_BINARY=/Users/allen/Library/Application Support/pcg/bin/nornicdb-headless`
set while omitting `ESHU_NORNICDB_RUNTIME`. Eshu selected embedded NornicDB and
restored the fast profile:

- `phase=files` completed in `7.85s`.
- `Function` canonical entity writes completed `8,467` rows as `1,333`
  statements and `272` grouped executions in `1.04s`, with average grouped
  execution `0.0038s`.
- `phase=entities` completed in `6.14s`.
- Canonical phase-group write completed in `14.00s`.
- Source-local projection succeeded in `19.42s`.

Therefore this PR must add or use a local-authoritative canonical-write
regression gate before it is ready. The existing synthetic dead-code query
perf gate proves API latency on a small synthetic graph; it does not exercise
repo-scale canonical `File` and `Function` writes and did not catch this
runtime-selection regression.

## Cross-Language Rollout

Roll out exactness by language family, not by marketing claim.

1. **Go:** function values, interfaces, method sets, public API, entrypoints,
   Cobra, HTTP, controller-runtime, build tags, and reflection ambiguity.
2. **Python:** module entrypoints, decorators, web/worker/CLI frameworks,
   imports, class methods, public package surfaces, and dynamic import
   ambiguity.
3. **JavaScript/TypeScript/TSX:** Node package entrypoints, package `bin`
   targets, package public exports, exported functions under configured
   Hapi/lib-api-hapi handler directories, ESM/CommonJS exports, Next.js,
   Express, route handlers, class methods, framework callbacks, and dynamic
   property access ambiguity.
4. **Java:** annotations, framework callbacks, Gradle/Jenkins/Spring/JUnit
   roots, serialization hooks, bounded literal reflection, ServiceLoader
   providers, Spring auto-configuration metadata, and compiler/indexer-backed
   semantics where available.
5. **Rust/C#/Scala/Swift/Kotlin:** traits/interfaces, annotations,
   exported/public symbols, package/module roots, framework callbacks, and
   compiler/indexer-backed semantics where available.
6. **C/C++/Dart/Elixir/Haskell/Perl/PHP/Ruby:** exactness only after each
   language has tested entrypoints, public API rules, call/reference evidence,
   and framework root policies.

IaC remains outside `code_quality.dead_code`. Terraform, Helm, Kustomize,
Kubernetes, ArgoCD, CloudFormation, Crossplane, and related artifacts use the
separate IaC usage and reachability model.

## Fixture Strategy

Every parser-supported source language must get a dedicated dead-code fixture
suite before Eshu claims exactness for that language. The existing parser
fixtures prove that Eshu can extract definitions, imports, calls, variables,
types, and other syntax-level facts. They do not prove cleanup safety.

Dead-code fixtures must be purpose-built around reachability. Each language
fixture should include:

- one truly unused symbol that should be returned as `unused`
- one direct call/reference case that should be reachable
- one language entrypoint or initializer
- one public API/exported surface case
- one framework or callback root common to that ecosystem
- one function-value, method-value, trait/interface, annotation/decorator, or
  dynamic dispatch case where the language supports it
- one generated or test-owned exclusion
- one ambiguous dynamic case that forces non-exact truth
- every valid language construct that can affect reachability, or an explicit
  exactness blocker for constructs Eshu cannot model yet

The fixture output must be asserted through the same product surfaces users
touch: parser/unit tests for emitted evidence, graph/query tests for
classification, and at least one API/MCP or local-authoritative proof for the
language family before promotion. A language with parser coverage but no
dead-code fixture remains `derived_candidate_only` for dead-code.

## Output Contract

Dead-code responses will include per-result classification:

- `unused`: no modeled root reaches the entity and required language gates are
  satisfied
- `reachable`: the entity is reachable from modeled roots
- `ambiguous`: Eshu found possible dynamic reachability or missing semantic
  evidence
- `derived_candidate_only`: Eshu can parse the language and return graph-backed
  candidates, but exact cleanup semantics are not fixture/root-model complete
- `excluded`: test, generated, public API, or user-configured exclusion
- `unsupported_language`: the language is outside the parser/indexing contract
  for this capability

The response analysis will include:

- root categories applied
- root categories unavailable
- languages included and their dead-code maturity
- framework roots recognized
- public API rules applied
- reflection and dynamic-dispatch status
- parser metadata root count
- query-time fallback count
- candidates suppressed by root evidence
- candidates downgraded to ambiguous
- generated and test exclusions
- user overrides

## Observability

Runtime-affecting implementation must add operator-visible telemetry:

- root extraction duration histogram
- roots extracted counter by language and root kind
- unreachable candidates counter by language and classification
- candidates suppressed counter by root category
- ambiguous candidates counter by ambiguity reason
- reachability materialization duration histogram
- graph traversal duration histogram
- structured logs with `failure_class`, query profile, backend, language, and
  root category

Metric labels must stay low-cardinality. File paths, function names, and entity
IDs belong in spans or structured logs, not metric labels.

## Performance Gate

Dead-code maturity work is allowed to add parser metadata and query
classification, but it must not make local dogfood indexing unusable. The
acceptance bar for this PR includes a repo-scale local-authoritative graph
proof, not only synthetic query latency. Classify dogfood runs with the [local
performance tiers](../reference/local-performance-envelope.md#dogfood-tiers).

The required performance gate must prove:

- clean local-authoritative startup resets Postgres and the NornicDB graph store
  before indexing
- collector discovery, pre-scan, parse, materialize, fact commit, content
  write, canonical graph write, and reducer enqueue timings are captured from
  the same run
- canonical `files` and `entities` phases stay within the previous self-repo
  envelope, with special attention to `Function` rows, statement count, grouped
  execution count, average execution duration, and max execution duration
- API or MCP dead-code proof runs only after indexing reaches a healthy,
  drained state

The first remediation step is diagnostic, not another query-shape guess:

1. Reproduce the slow `Function` canonical write path with no unrelated local
   owner process contending for CPU or graph storage.
2. Compare the same worktree with and without the dead-code branch data shape.
3. Compare file-scoped containment, cross-file batched containment, and any
   NornicDB latest-main hot-path requirements using the same corpus.
4. Fix the narrow owner from evidence: parser metadata shape, canonical writer
   query shape, NornicDB hot-path eligibility, reset/dirty graph behavior, or
   local process contention.
5. Keep the rejected experiments recorded in this ADR or the implementation
   plan so the team does not repeat them.

## Implementation Evidence

The Python/Java slice is still a derived dead-code maturity step, not an
exactness claim. It adds root metadata, query exclusions, and dogfood proof, but
the language gates stay conservative while dynamic dispatch, broad reflection,
and dependency injection remain intentionally bounded.

Local and dogfood evidence gathered in this branch so far:

- Ansible indexed as the large Python dogfood repo and returned a bounded
  `dead-code` result window in `4.9s`.
- Jenkins indexed as the first Java dogfood repo and returned a bounded
  `dead-code` result window in `9.8s`.
- Spring Boot indexed from a fresh local worktree through `eshu graph start
  --workspace-root <spring-boot-worktree> --progress plain --logs file` on
  2026-05-08 local time against NornicDB `v1.0.44`. The run drained healthy
  with collector `1/1`, projector `1/1`, reducer `10/10`, queue `pending=0`,
  `in_flight=0`, `retrying=0`, `failed=0`, and `dead_letter=0`.
- Spring Boot collector stream completed in `27.352s`. Source-local projection
  loaded `190459` facts in `3.444s`, wrote content in `16.848s`, completed the
  canonical phase-group write in `31.846s`, and finished `project_generation`
  in `51.179s`.
- The first Spring Boot proof after exact code-call endpoint labels reduced
  code-call projection from `236.842s` to `21.612s`, but semantic and SQL
  materialization still spent about `60.5s` each in fact loading.
- The current page-size fix keeps `FactStore.ListFactsByKind` on bounded
  keyset pages, but raises those pages to the existing 500-row fact batch size.
  On the fresh Spring Boot proof, semantic fact loading dropped from `60.510s`
  to `13.547s`, and SQL fact loading dropped from `60.573s` to `13.396s`.
  Semantic total dropped from `72.769s` to `26.210s`; SQL total dropped from
  `60.644s` to `13.464s`.
- Code-call shared projection completed after reducer queue drain, writing
  `44988` rows in `21.187s` with `write_duration_seconds=19.133` and
  `mark_completed_duration_seconds=1.113`. This confirms that readiness for
  dead-code queries must include shared projection completion, not only the
  reducer work-item queue.
- The Java completion slice added parser/reducer fixtures for serialization
  hooks, bounded literal reflection, ServiceLoader provider files, Spring Boot
  `AutoConfiguration.imports`, and legacy `spring.factories`. These surfaces now
  emit parser-backed roots or `REFERENCES` edges instead of relying on query-time
  string matching.
- Elasticsearch is now classified as Tier 3 Java stress evidence in the local
  performance envelope: `32966` files, `1093371` content entities, `1153024`
  facts, `399.154s` source-local projection, and `117.932s` code-call
  materialization before the run was stopped for bottleneck analysis.
- The PHP slice promotes PHP to `derived`, not exact. Parser metadata now
  suppresses script entrypoints, constructors, known magic methods, same-file
  interface and trait methods, route-backed controller actions, Symfony route
  attributes, literal route handlers, and WordPress hook callbacks. A regression
  fixture covers PSR-style type declarations whose opening brace is on the next
  line so real Laravel and Symfony constructors keep their owning class context.
- PHP dogfood used a unique Compose project,
  `eshu-php104-dogfood-20260512085625-56178`, against Laravel Framework,
  Symfony, and WordPress develop. The run drained healthy with queue
  `pending=0`, `in_flight=0`, `retrying=0`, `failed=0`, and `dead_letter=0`.
  Bootstrap collected `2964`, `10355`, and `1773` files respectively, parsed
  `2964`, `10335`, and `1772` files, and finished the bootstrap pipeline in
  `186.741s`.
- API `code_quality.dead_code` checks after queue drain returned
  `truth.level=derived` and `dead_code_language_maturity.php=derived` for all
  three PHP dogfood repositories. Parser-root suppressions were observed in the
  analysis payload (`164` for Laravel, `224` for Symfony, `69` for WordPress),
  and the first `500` returned candidates for each repo contained no
  constructors or known PHP magic methods.
- The Kotlin slice promotes Kotlin to `derived`, not exact. Parser metadata now
  suppresses top-level `main`, secondary constructors, interface methods,
  same-file interface implementations, overrides, Gradle plugin/task callbacks,
  Spring component and method callbacks, lifecycle callbacks, and JUnit methods.
  Query policy suppresses the same `kotlin.*` roots from graph and content
  metadata before classifying remaining candidates.
- Kotlin dogfood used a unique Compose project,
  `eshu-kotlin102-dogfood-retry-20260512105955`, against Ktor. The checked-out
  corpus had `2827` files and `2370` Kotlin/KTS files; discovery indexed `2297`
  files after default skips, parsed `2297` files, materialized `28354` content
  entities, and emitted `32956` facts. The first run exposed a Neo4j transaction
  memory limit at canonical projection; the accepted retry used two projection
  workers and a `2G` Neo4j transaction cap, then completed with queue
  `outstanding=0`, `in_flight=0`, `retrying=0`, `failed=0`, and `dead_letter=0`.
  Stage timings were discovery `0.063s`, pre-scan `7.715s`, parse `7.062s`,
  materialize `0.152s`, canonical write `5.926s`, content write `2.867s`, and
  bootstrap projection `29.238s`.
- API `code_quality.dead_code` after the Ktor run returned `truth.level=derived`,
  `dead_code_language_maturity.kotlin=derived`, `query_seconds=0.087625`,
  `candidate_scan_rows=1000`, `candidate_scan_pages=4`,
  `candidate_scan_truncated=false`, and `framework_roots_from_parser_metadata=359`.
  The returned `500` candidates carried no `dead_code_root_kinds`, proving
  parser-backed Kotlin roots were suppressed before cleanup classification.
- The Scala slice promotes Scala to `derived`, not exact. Parser metadata now
  suppresses `main` methods, objects extending `App`, traits and trait methods,
  same-file trait implementations, overrides, Play controller actions, Akka
  actor `receive` methods, lifecycle callbacks, JUnit methods, and ScalaTest
  suite classes. Query policy treats materialized `Trait` entities as
  dead-code candidates so trait roots are counted and explained instead of
  being dropped before root classification.
- Scala remains non-exact until macros, implicit and given/using resolution,
  dynamic dispatch, reflection, sbt source-set resolution, framework route
  files, compiler plugin output, and broad public API surfaces are modeled or
  scoped out. The fixture-backed tests cover parser metadata, graph-shaped
  query suppression, and content-metadata query suppression.
- Scala dogfood used unique Compose project names and ports for issue #105.
  The default NornicDB Compose stack did not reach indexing on this local
  Docker host because the NornicDB container exited with code `2` during
  startup; the accepted Scala parser proof therefore used the Neo4j Compose
  stack with explicit transaction-memory caps following Neo4j's documented
  `NEO4J_*` Docker configuration mapping.
- Play Framework dogfood used `eshu_scala105_play_neo` against
  `playframework/playframework` at
  `bcdc682de2250bbd0f2788bc5acc06f6d66ad5a7`. The checked-out corpus had
  `2557` files and `935` Scala/SBT/Sc files; discovery indexed `1644` files,
  parsed `1644` files, materialized `27924` content entities, and emitted
  `31220` facts. Stage timings were discovery `0.043s`, pre-scan `5.145s`,
  parse `4.276s`, materialize `0.163s`, canonical write `4.706s`, content
  write `2.452s`, and bootstrap projection `23.071s`. The final status was
  healthy with queue `outstanding=0`, `in_flight=0`, `retrying=0`,
  `failed=0`, and `dead_letter=0`. The scoped dead-code API returned
  `truth.level=derived`, `dead_code_language_maturity.scala=derived`, and
  `framework_roots_from_parser_metadata=112`. Content metadata contained
  Scala root buckets for overrides, trait methods/types, Play controller
  actions, trait implementations, ScalaTest suites, main methods, Akka
  `receive`, and objects extending `App`.
- Scala compiler dogfood used `eshu_scala105_compiler_neo` against
  `scala/scala` branch `2.13.x` at
  `25075e9b9b79954a0f99de515618901818822e62`. The checked-out corpus had
  `13611` files and `9382` Scala/SBT/Sc files; discovery indexed `10037`
  files, parsed `10036` files with one skip, materialized `157759` content
  entities, and emitted `177839` facts. Stage timings were discovery `0.100s`,
  pre-scan `24.488s`, parse `19.701s`, materialize `1.384s`, canonical write
  `15.134s`, content write `10.058s`, and bootstrap projection `91.994s`.
  The final status was healthy with queue `outstanding=0`, `in_flight=0`,
  `retrying=0`, `failed=0`, and `dead_letter=0`. The scoped dead-code API
  returned `truth.level=derived`, `dead_code_language_maturity.scala=derived`,
  and `framework_roots_from_parser_metadata=54`. Content metadata contained
  root buckets for trait methods/types, overrides, JUnit methods, objects
  extending `App`, main methods, and trait implementation methods.

Open proof work before this branch can close:

- Complete the Tier 3 Elasticsearch run after canonical projection and
  code-call fixes.
- Re-run at least one Python large-repo proof after the same storage change.
- Keep the language matrix at `derived` for Python and Java until dynamic
  dispatch, broad reflection, and dependency-injection categories have positive,
  negative, and ambiguous fixtures.

## Consequences

This ADR makes the current product contract more honest. Eshu can still support
dead-code analysis locally and through MCP, but exactness becomes a
language-scoped claim with proof gates.

The implementation will require parser, content-shape, fact, reducer, query,
MCP, CLI, spec, and documentation changes. That is larger than a query-only
patch, but it prevents dead-code cleanup workflows from deleting live code.

The capability matrix should continue to report `derived` until the language
gates and backend conformance evidence justify promotion.

## Non-Goals

- Do not delete or auto-fix code based on derived candidates.
- Do not infer IaC deadness from code-call edges.
- Do not claim Tree-sitter alone proves dead-code exactness.
- Do not mark all parser-supported languages exact at once.
- Do not hide missing parser/indexer support behind a generic derived response
  when a scoped `unsupported_language` classification is more accurate.

## Acceptance Criteria

- The Eshu dogfood false-positive list is encoded as a regression fixture.
- Every parser-supported source language has a dedicated dead-code fixture
  plan, and exact languages have checked-in fixtures with positive, negative,
  and ambiguous cases.
- Go root and reachability tests cover function values, DI callbacks, local
  interfaces, method-set satisfaction, public API roots, and ambiguous dynamic
  cases.
- Python and JavaScript/TypeScript keep their existing modeled roots, add
  Node/Hapi parser-backed roots for the local service shape, and retain
  language-maturity metadata in the response.
- All parser-supported source languages have explicit dead-code maturity rows:
  exact, derived, derived-candidate-only, or ambiguous-only.
- `find_dead_code` responses explain modeled and unmodeled root categories.
- Local-authoritative self-repo graph indexing is back inside the pre-regression
  envelope, or the PR remains blocked with the regression explicitly documented.
- The canonical-write gate captures at least `phase=files`, `phase=entities`,
  `Function` label rows/statements/executions/durations, and total projection
  duration from a single local-authoritative run.
- Local-authoritative dead-code proof passes:

  ```bash
  ESHU_NORNICDB_BINARY=/tmp/eshu-bare-install-smoke/bin/nornicdb-headless \
    ESHU_LOCAL_AUTHORITATIVE_PERF=true \
    go test ./cmd/eshu -run TestLocalAuthoritativeDeadCodeSyntheticEnvelope -count=1 -v
  ```

- Compose graph-analysis proof passes:

  ```bash
  ./scripts/verify_graph_analysis_compose.sh
  ```

- Capability and backend conformance specs remain honest about exactness.
