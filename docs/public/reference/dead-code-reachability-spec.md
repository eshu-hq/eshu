# Dead Code Reachability Spec

This page defines the contract for Eshu's `code_quality.dead_code` capability.
It is a current runtime contract, not a historical implementation plan.

The short version: "no incoming calls" is not enough to call code dead. A
symbol can be reachable through entrypoints, public API rules, framework
registration, callbacks, generated runtime wiring, reflection, configuration,
or language dispatch. Eshu only claims `exact` dead-code truth when those roots
are modeled for the queried scope.

## Current Status

`code_quality.dead_code` is supported in graph-backed profiles and currently
returns `derived` truth.

The capability is exposed through:

- `POST /api/v0/code/dead-code`
- `POST /api/v0/code/dead-code/investigate`
- MCP tools `find_dead_code` and `investigate_dead_code`
- CLI command `eshu analyze dead-code`

The capability matrix marks `local_authoritative`, `local_full_stack`, and
`production` as supported with `max_truth_level: derived`. `local_lightweight`
is unsupported because dead-code analysis requires the graph plus root metadata.

The implementation scans graph or content-backed entity candidates, removes
symbols with incoming `CALLS`, `IMPORTS`, `REFERENCES`, `INHERITS`, or
`EXECUTES` edges, applies the default root policy, and returns bounded results
with truncation metadata.

## Exactness Rule

Dead-code truth is `exact` only when all of these are true for the queried
scope:

1. The language and framework root model is implemented.
2. The authoritative call and reference graph is present.
3. Generated code and test-code policy is known.
4. Dynamic behavior that can affect reachability is modeled or scoped out.
5. The response can explain the root categories, modeled roots, maturity, and
   exactness blockers that were applied.

If those conditions are not met, Eshu must return `derived`,
`derived_candidate_only`, `non_code_iac_evidence`, `unsupported_language`, or an
explicit unsupported-capability error. It must not silently promote a partial
root model to exact cleanup-safe truth.

## Root Categories

Every dead-code result explains the root categories used by the analyzer. The
current response includes:

| Category | What it protects |
| --- | --- |
| `language_entrypoints` | `main`, `init`, `__main__`, runtime startup hooks, and equivalent language entrypoints. |
| `generated_and_tool_owned` | Generated or tool-owned symbols that should not be judged by ordinary inbound code calls. |
| `library_public_api` | Exported or public package surfaces where external consumers can call the symbol. |
| `cli_command_roots` | Cobra, Click, Typer, package-bin, and equivalent command handlers. |
| `http_and_rpc_roots` | HTTP route handlers, RPC handlers, framework controller actions, and equivalent request handlers. |
| `framework_callback_roots` | Workers, schedulers, lifecycle hooks, interface/trait/protocol callbacks, DI callbacks, tests, and runtime callback registrations. |

The implementation also reports modeled entrypoints, modeled public API roots,
modeled framework roots, Go semantic roots, and notes describing the current
derived model.

## Output Contract

Dead-code responses include analysis metadata so humans and agents can see why a
candidate is safe, ambiguous, or suppressed.

Important response fields:

| Field | Meaning |
| --- | --- |
| `truth.level` | Current truth level. Dead-code is `derived` unless a future scope is proven exact. |
| `results[].classification` | One of `unused`, `ambiguous`, `derived_candidate_only`, or `unsupported_language` for returned candidates. |
| `truncated` | True when either displayed results or the candidate scan window was truncated. |
| `candidate_scan_limit`, `candidate_scan_pages`, `candidate_scan_rows` | Bounded scan coverage. |
| `analysis.root_categories_used` | Root categories applied by the analyzer. |
| `analysis.frameworks_recognized` | Framework values observed in result metadata. |
| `analysis.modeled_entrypoints` | Entrypoint root kinds currently modeled. |
| `analysis.modeled_framework_roots` | Framework and callback root kinds currently modeled. |
| `analysis.modeled_public_api` | Public API root kinds currently modeled. |
| `analysis.dead_code_language_maturity` | Per-language maturity from the query package. |
| `analysis.dead_code_language_exactness_blockers` | Named blockers that prevent exact cleanup-safe truth. |
| `analysis.dead_code_observed_exactness_blockers` | Blockers observed on returned candidates. |
| `analysis.tests_excluded` | Whether test-owned code is excluded by default. |
| `analysis.generated_code_excluded` | Whether generated code is excluded by default. |
| `analysis.user_overrides_applied` | Whether request-level exclusions were applied. |
| `analysis.iac_reachability_mode` | Always `not_modeled_by_code_dead_code` for this capability. |

See [Dead Code Language Maturity](dead-code-language-maturity.md) for the
current language-by-language model.

## Default Policy

The default policy is intentionally conservative:

- Tests are excluded by default.
- Generated code is excluded by default.
- Parser-backed `dead_code_root_kinds` metadata suppresses cleanup candidates.
- Content metadata is preferred when available; graph metadata is still used
  when content is not available.
- Direct incoming code or reference edges suppress candidates.
- SQL trigger routines are protected when reducer materialization creates
  parser-proven trigger-to-function `EXECUTES` edges.
- JavaScript and TypeScript candidates remain conservative because dynamic
  imports, property dispatch, framework loading, and declaration surfaces are
  not fully exact.
- HCL, Terraform, Terragrunt, Dockerfiles, Helm, Kustomize, Kubernetes, ArgoCD,
  and other infrastructure artifacts are not code dead-code candidates.

IaC cleanup is a separate workflow. Use `find_dead_iac` and the IaC
reachability docs instead of inferring infrastructure deadness from missing code
call edges.

## User Overrides

Repositories can declare additional dead-code roots or exclusions in
`.eshu.yaml`:

```yaml
dead_code:
  roots: []
  exclude_paths: []
  include_generated: false
```

Request-level decorator exclusions also set `analysis.user_overrides_applied`.

## Fixture Contract

Dead-code exactness is language scoped. A language cannot claim exact
cleanup-safe results until its fixture suite covers:

- a truly unused symbol that should be returned
- a direct call, import, reference, inheritance, or implementation edge that
  should suppress a candidate
- an executable entrypoint or initializer
- an exported or public API surface
- a framework route, worker, scheduler, annotation, decorator, command, or
  callback root
- semantic dispatch such as function values, method values, interfaces, traits,
  protocols, dynamic imports, or generated registries
- test and generated-code exclusions
- an ambiguous dynamic case that prevents exact truth
- every valid language construct that can affect reachability, or a named
  exactness blocker explaining why it prevents exact output

The fixture inventory lives at `tests/fixtures/deadcode/README.md`. Parser
fixtures prove syntax extraction. Dead-code fixtures prove cleanup safety.

## Proof Gates

Promoting any language or scope above `derived` requires all of these proofs:

1. Parser or SCIP evidence for definitions, calls, references, imports, and
   root metadata.
2. Query tests for unused, reachable, excluded, and ambiguous results.
3. API, MCP, and CLI output proving truth labels, classifications, maturity, and
   blockers.
4. Backend conformance for NornicDB and Neo4j query shapes.
5. Performance evidence for bounded candidate scans on representative input.

Until those gates pass, Eshu can help find cleanup candidates, but humans and
agents must treat the result as derived evidence, not an authoritative delete
list.
