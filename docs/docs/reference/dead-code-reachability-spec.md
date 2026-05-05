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
- whether reflection/dynamic patterns were modeled
- whether tests or generated code were excluded
- applied user overrides
- how many candidate entities skipped framework-root evaluation because source
  text was unavailable
- how many framework roots came from parser metadata versus legacy query-time
  source fallback
- whether IaC reachability was modeled by this analysis

That explanation must be returned in structured form, not just text prose.

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
- Python FastAPI route decorators are modeled
- Python Flask route decorators are modeled
- Python Celery task decorators are modeled
- JavaScript/TypeScript Next.js route exports are modeled
- JavaScript/TypeScript Express handler registrations are modeled
- those Go signature roots are now emitted by the Go parser into entity
  metadata when imports, registrations, and signatures match directly; mixed
  native+SCIP indexing now preserves `dead_code_root_kinds` through the
  supplement merge path; Python route/task decorators and
  JavaScript/TypeScript Next.js/Express route roots are also emitted as
  parser-backed `dead_code_root_kinds`; Go query-time source heuristics remain
  as a fallback while broader registry coverage lands
- broader Go router, webhook, worker, reflection, and build-tag roots plus
  broader Python worker/CLI/public-API roots and broader JavaScript/TypeScript
  worker/public-API roots remain
  open, so dead-code truth stays `derived`

Initial MVP is explicitly limited to those families. Other languages and
frameworks should return non-exact or unsupported dead-code results until
their root models exist.

Chunk 4 should also add a diff-oriented dead-code mode for CI-style questions
such as "did this change introduce dead code?"

## Test Requirements

- positive case: reachable symbol preserved by framework root
- negative case: truly unreachable symbol flagged
- ambiguous case: unresolved dynamic or framework registration forces a
  non-exact answer
