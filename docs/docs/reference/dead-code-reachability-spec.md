# Dead Code Reachability Spec

This document defines the reachability model required before Eshu can claim an
`exact` dead-code answer.

## Why This Exists

"No incoming calls" is not the same as "dead."

Frameworks, workers, routers, reflection, and configuration-driven dispatch all
create valid reachability roots that do not look like ordinary direct calls.

Eshu must not claim authoritative dead-code truth until those roots are modeled
for the relevant language and framework.

## Root Categories

Every dead-code analysis must classify roots into one or more of these groups:

- language entrypoints
  - `main`
  - `__main__`
  - `init()` or equivalent initializer hooks
  - equivalent executable roots
- CLI command roots
  - Cobra commands
  - Click/Typer commands
  - equivalent command registrations
  - Go direct Cobra run-signature handlers are currently modeled as derived
    roots via `*cobra.Command` + `[]string` function signatures
  - Go direct Cobra `Run` / `RunE` registrations are also currently modeled as
    derived roots when the Go parser sees `cobra.Command{Run|RunE: fn}` or a
    proven `cmd.Run|RunE = fn` assignment in the same file
- HTTP and RPC roots
  - route handlers
  - FastAPI/Django/Flask registrations
  - gRPC service handlers
  - Go stdlib HTTP handlers are currently modeled as derived roots via
    `http.ResponseWriter` + `*http.Request` function signatures
  - Go stdlib HTTP registrations are also currently modeled as derived roots
    when the Go parser sees direct `http.HandleFunc`, `http.Handle`, or a
    proven `ServeMux` registration in the same file
- background worker roots
  - Celery tasks
  - Sidekiq jobs
  - cron and scheduler registrations
- framework callback roots
  - Kubernetes admission webhooks
  - controller-runtime reconciler methods
  - ArgoCD/Crossplane hook registrations
  - Go controller-runtime reconciler callbacks are currently modeled as
    derived roots via `Reconcile(context.Context, ctrl|reconcile.Request)
    (ctrl|reconcile.Result, error)` signatures
- generated and tool-owned roots
  - gRPC stubs
  - sqlc output
  - protobuf/OpenAPI generated clients where configured
- SQL and stored-program roots
  - SQL routines explicitly invoked from application code or runtime wiring
- reflection and dynamic-dispatch roots
  - explicit allowlisted reflection or registry patterns
- library public API roots
  - exported symbols in library-mode packages
  - per-language public-surface rules
  - Go (currently modeled, default-on): Functions, Structs, Interfaces, and
    Classes whose first rune is uppercase are public-API roots when the file
    path is outside `cmd/`, `internal/`, and `vendor/` subtrees. Binary
    entrypoints (`cmd/`) and internal packages (`internal/`) remain subject
    to reachability rules. Other languages (Python, Rust, Java, TypeScript)
    are not yet modeled; their exported-symbol rules are a Chunk 4
    follow-up
- conditional roots
  - build-tag, platform, or environment-specific reachability
- user-declared roots
  - repository-specific overrides in configuration

## Exactness Rule

Dead-code truth is `exact` only when:

1. the language/framework root model is implemented for the target scope
2. the authoritative call graph is present
3. the runtime can explain which root categories were applied

If those conditions are not met, Eshu must either:

- return `derived`, or
- reject the request as unsupported for that profile

It must not pretend a partial root model is authoritative.

## Required Output Metadata

Any dead-code result should be able to report:

- root categories used
- frameworks recognized
- dead-code maturity for each parser-supported source language
- whether reflection/dynamic patterns were modeled
- whether tests or generated code were excluded
- applied user overrides
- how many candidate entities skipped framework-root evaluation because source
  text was unavailable
- how many framework roots came from parser metadata versus legacy query-time
  source fallback
- whether IaC reachability was modeled by this analysis

That explanation must be returned in structured form, not just text prose.

Language maturity is separate from parser support. A parser-supported language
can still be `derived_candidate_only` for dead-code cleanup until it has a
dead-code fixture suite, root model, reachability proof, and API/MCP evidence.
The initial maturity states are:

- `derived`: current Go, Python, JavaScript, TypeScript, and TSX candidate
  scans with partial root modeling
- `derived_candidate_only`: parser-supported source languages where Eshu can
  return graph-backed candidates but has not implemented enough language roots
  and fixtures for cleanup-safe answers
- `unsupported_language`: languages outside Eshu's parser/indexing contract for
  this capability
- future `exact`: language scopes whose fixture and runtime gates prove the
  answer is cleanup-safe
- future `ambiguous_only`: language scopes where Eshu can identify uncertainty
  but cannot return actionable unused symbols

The current `code_quality.dead_code` capability is code-call oriented. It must
not classify Terraform, Helm, Kustomize, Kubernetes, ArgoCD, or other IaC
artifacts as dead simply because they lack code-call inbound edges. Dead-IaC
requires the separate IaC usage and reachability graph described in
`../adrs/2026-04-24-iac-usage-reachability-and-refactor-impact.md`.

## Default Scope Policy

- tests are excluded from dead-code roots by default
- generated code is excluded by default unless the repo explicitly opts in
- library-mode exported symbols are roots by default unless a stricter rule is
  configured
- Go `init()` reachability follows import side effects; symbols made reachable
  only by side-effect imports are not dead if that import path is active

## User Overrides

Repos may declare additional roots or exclusions in `.eshu.yaml`.

Initial keys:

```yaml
dead_code:
  roots: []
  exclude_paths: []
  include_generated: false
```

Generated code detection should begin with:

- standard generated-file headers
- common generated path patterns such as `gen/`, `generated/`, and tool-owned
  output directories

## Deliverable For Chunk 4

Chunk 4 must produce a framework-aware root registry or rules layer that is
explicitly testable per language and framework family.

Minimum initial coverage should include:

- Go CLI/HTTP/controller patterns
- Python web and worker patterns
- JavaScript/TypeScript web route patterns

Current branch status:

- Go direct Cobra run signatures are modeled
- Go direct Cobra `Run` / `RunE` registrations are modeled
- Go stdlib HTTP handler signatures are modeled
- Go stdlib HTTP direct and proven `ServeMux` registrations are modeled
- Go controller-runtime `Reconcile` signatures are modeled
- Go composite-literal type references are modeled as `REFERENCES` edges, not
  `CALLS`, so type usage can protect struct/interface candidates without
  claiming invocation semantics
- Python FastAPI route decorators are modeled
- Python Flask route decorators are modeled
- Python Celery task decorators are modeled
- JavaScript/TypeScript Next.js route exports are modeled
- JavaScript/TypeScript Express handler registrations are modeled
- JavaScript/TypeScript Node package entrypoints, package `bin` targets,
  package public exports, and exported functions under configured
  Hapi/lib-api-hapi handler directories are modeled when `package.json` and
  `server/init/plugins/spec*` provide bounded local evidence
- those Go signature roots are now emitted by the Go parser into entity
  metadata when imports, registrations, and signatures match directly; mixed
  native+SCIP indexing now preserves `dead_code_root_kinds` through the
  supplement merge path; Python route/task decorators and
  JavaScript/TypeScript Next.js/Express/Node/Hapi roots are also emitted as
  parser-backed `dead_code_root_kinds`; Go query-time source heuristics remain
  as a fallback while broader registry coverage lands
- broader Go router, webhook, worker, reflection, and build-tag roots plus
  broader Python worker/CLI/public-API roots and broader JavaScript/TypeScript
  worker, static module graph, and dynamic-dispatch roots remain
  open, so dead-code truth stays `derived`

Initial MVP is explicitly limited to those families. Other parser-supported
languages and frameworks should return `derived_candidate_only`, `derived`, or
`ambiguous_only` dead-code results until their root models exist.

Chunk 4 should also add a diff-oriented dead-code mode for CI-style questions
such as "did this change introduce dead code?"

## Dead-Code Fixtures

Each parser-supported source language needs a dedicated dead-code fixture suite
before exactness can be claimed. The parser fixture matrix proves syntax
extraction coverage; it does not prove cleanup safety.

The fixture inventory lives at
`../../../tests/fixtures/deadcode/README.md`.

Every language fixture should include:

- a truly unused symbol that should be reported
- a direct call/reference that should not be reported
- an executable entrypoint or initializer
- an exported/public API surface
- a common framework, router, worker, annotation, decorator, or callback root
- a language-specific semantic dispatch case such as function values, method
  values, interfaces, traits, dynamic imports, or generated registries
- a generated-code or test-owned exclusion
- an ambiguous dynamic case that keeps truth non-exact

Fixture intent must be asserted at three layers when the language supports
them: parser evidence, graph/query classification, and API/MCP/local proof.
A language with parser fixtures but no dead-code fixtures remains `derived` or
`derived_candidate_only` for `code_quality.dead_code`.

## Test Requirements

- positive case: reachable symbol preserved by framework root
- negative case: truly unreachable symbol flagged
- ambiguous case: unresolved dynamic or framework registration forces a
  non-exact answer
