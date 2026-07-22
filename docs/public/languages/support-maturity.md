# Parser Support Matrix

This page tracks the checked-in Go parser support-maturity matrix in the current repository state.

This matrix is intentionally coarse. It does not replace the per-language
capability pages.

Use:

- the language pages under `docs/public/languages/` for exact partial or
  unsupported capability details
- the `Dead-code Support` section on each parser page for root modeling,
  query evidence, checked fixtures, and bounded limitations
- [Source-Language Resolver Contract](../reference/source-language-resolver-contract.md)
  for call, import, inheritance, interface, overload, SCIP, and golden-audit
  proof rules

This matrix tracks the higher-level support bar for each parser beyond
the raw capability checklist. `-` means this page does not currently make a
specific support assertion for that dimension.

## Promotion Rules

Maturity is promoted only when the normal consumer path is proven. Parse-only
behavior is not supported query behavior.

| Target state | Required evidence |
| --- | --- |
| `unsupported` to documented source evidence | Parser registry entry, parser fixture, language page update, and explicit limitations. Query and graph columns stay `-` until a read path is proven. |
| Source evidence to `partial` | The documented subset has parser proof and at least one consumer proof. Unsupported adjacent cases remain named on the language page. |
| `partial` to `supported` | Parser proof, query or graph/content-backed proof, language page update, matrix update, and docs build in the same change. |
| Framework or root evidence increase | Positive, negative, and ambiguous fixtures for the exact framework root, callback, route, lifecycle hook, package export, or public API shape. |
| Source-language relationship resolution | Resolver contract entrypoint, source-authored golden audit fixtures, no self-comparison goldens, reducer admission proof, read-surface proof, and explicit ambiguity behavior. |
| Dead-code maturity increase | Parser root proof, query suppression or candidate proof, [Dead Code Language Maturity](../reference/dead-code-language-maturity.md) update, and exactness blockers reviewed. |
| `fixture-backed` to `real-repo-validated` to `supported` | `fixture-backed`: parser `go test` against the language's `*_comprehensive` fixture. `real-repo-validated`: a committed dogfood script under `scripts/` plus a checked-in expected-output snapshot or cassette, with any external repo and pinned SHA recorded as provenance metadata only. `supported`: the fixture is staged in `corpus_fixtures` in `scripts/verify-golden-corpus-gate.sh` and asserted by at least one language-attributed `required_correlations` or `query_shapes` entry in the B-12 snapshot (`testdata/golden/e2e-20repo-snapshot.json`). |

Dynamic imports, plugin loading, reflection, generated code, and
framework-specific roots remain blockers until the exact pattern has parser and
query proof. Unsupported framework/root rows must not claim query surfacing,
canonical relationship resolution, or end-to-end indexing.

## Grade Definitions

The `Real-Repo Validation` and `End-to-End Indexing` columns of the matrix below
use a three-grade scale. The grades are defined mechanically against the live
B-7 golden-corpus gate, not asserted by hand, and `scripts/verify-maturity-drift-guard.sh`
mechanically re-derives the `supported` set on every CI run and fails when
these two columns drift from it in either direction (#5400) — a language
whose fixture leaves `corpus_fixtures` or loses its B-12 attribution, or one
that gains both without a matching matrix update. This is the same class of
drift #5336 fixed by hand-grading the matrix once; the gate exists so the
next drift is caught mechanically instead of by another manual re-grade.

- **`supported`** — the language is routed through the full live pipeline
  (`discover -> reduce -> project -> query`) by the golden-corpus gate. A
  language earns `supported` only when it is BOTH staged as a `corpus_fixtures`
  entry in `scripts/verify-golden-corpus-gate.sh` AND asserted by at least one
  language-attributed `required_correlations` or `query_shapes` entry in the
  B-12 snapshot (`testdata/golden/e2e-20repo-snapshot.json`). The two columns
  read this same live bar through different lenses:
    - *End-to-End Indexing* `supported` = the fixture is staged in B-7 and its
      projected graph or query truth is asserted in B-12.
    - *Real-Repo Validation* `supported` = a `corpus_fixtures` entry staged as
      its own repository in the corpus, for that language, clears the same
      live gate with a language-attributed B-12 assertion. This is exactly
      what `scripts/verify-maturity-drift-guard.sh` mechanically re-derives
      and checks the matrix against — the guard has no signal for a fixture's
      internal shape or richness, only for `corpus_fixtures` staging plus B-12
      attribution, so that intersection is the entire bar this page may claim
      here.

      By authoring convention, not as part of the checked bar above, corpus
      repos are written app-shaped where the language permits — for example
      `ruby_rails_app`, `orders-api`, `lib-common`, `api-svc` — or as a
      comprehensive infrastructure/config corpus for IaC languages (for
      example `terraform_comprehensive`, `terragrunt_comprehensive`);
      single-feature parser files live in `*_comprehensive` fixtures instead.
      A language may earn `supported` here off a `*_comprehensive` fixture
      alone, as Python currently does. Eshu's "real repos" are in-repo
      synthetic corpora, not external third-party checkouts; the bar here is
      the full-pipeline gate, not third-party provenance.
- **`real-repo-validated`** — an intermediate grade for a language that has a
  committed, offline-reproducible dogfood artifact but is not yet wired into the
  live gate: a script under `scripts/` PLUS a checked-in expected-output
  snapshot or cassette. Any external repository and pinned commit SHA is
  recorded only as provenance metadata, never as a CI-fetched dependency. This
  grade defines the promotion path off `fixture-backed`. Dart, Java, Scala, and
  Swift earned it in #5399 (spun off from #5336's downgrade); Swift has since
  been promoted to `supported` via its golden-corpus staging in #5378. Each has a
  `scripts/dogfood-<language>.sh` script that runs a standing
  `TestDogfood<Language>RealRepoSnapshot` regression test
  (`go/internal/parser/<language>/dogfood_real_repo_test.go`) against a
  committed, app-shaped corpus under `tests/fixtures/dogfood/<language>_real_repo/`
  and diffs the parser's bucket counts against a checked-in snapshot at
  `go/internal/parser/<language>/testdata/dogfood_real_repo_snapshot.txt`,
  requiring no network access or Docker. See the language pages for each
  corpus's provenance notes.
- **`fixture-backed`** — parser-level `go test` against a `*_comprehensive`
  fixture only. The parser is proven, but the language is never routed through
  the live pipeline gate.

## Framework Support Boundary

On this page, "framework support" means Eshu has parser or query evidence for a
framework entrypoint, callback, route, lifecycle hook, package export, or public
API shape. It does not mean Eshu fully models the framework runtime, plugin
system, dependency injection container, generated code, build-target selection,
or every version-specific extension point.

If a framework or library is not named here or on the language page, Eshu does
not currently make an audited support claim for it. Community pull requests that
add framework support should update the parser tests, fixture or dogfood
evidence, this matrix, the language page, and
[Dead Code Language Maturity](../reference/dead-code-language-maturity.md) when
the change affects dead-code roots.

For audited family-level closure status and bounded gaps, see
[`../reference/parity-closure-matrix.md`](../reference/parity-closure-matrix.md).

## Parser Backing Ledger

The machine-readable backing ledger lives at
`specs/parser-backing-ledger.v1.yaml`. It distinguishes source-language
tree-sitter parser claims from declarative configuration parsers where a
structured decoder, official format AST, or bounded manifest scanner is the
more accurate implementation. These rows are intentionally not "tree-sitter
debt"; they are documented exceptions with fixture-backed deterministic output.

| Parser Key | Implementation Class | Decision | Evidence |
| --- | --- | --- | --- |
| cloudformation | `structured-parser-backed-exception` | CloudFormation and SAM are decoded as YAML/JSON documents, then evaluated with bounded template extraction. | `go/internal/parser/cloudformation/*`, `docs/public/languages/cloudformation.md`, `specs/parser-backing-ledger.v1.yaml` |
| dockerfile | `structured-parser-backed-exception` | Dockerfile runtime evidence comes from bounded instruction scanning over the build manifest. | `go/internal/parser/dockerfile/*`, `go/internal/parser/dockerfile_language.go`, `specs/parser-backing-ledger.v1.yaml` |
| hcl | `structured-parser-backed-exception` | Terraform, tfvars, lockfile, and Terragrunt evidence uses HashiCorp's official HCL v2 parser and expression AST. | `go/internal/parser/hcl/*`, `docs/public/languages/terraform.md`, `docs/public/languages/terragrunt.md`, `specs/parser-backing-ledger.v1.yaml` |
| yaml | `structured-parser-backed-exception` | YAML-family evidence uses YAML v3 document decoding plus bounded Kubernetes, Argo CD, Crossplane, Kustomize, Helm, CloudFormation, GitLab CI, Atlantis, Pub, and observability walkers. | `go/internal/parser/yaml/*`, `docs/public/languages/{argocd,crossplane,helm,kubernetes,kustomize}.md`, `specs/parser-backing-ledger.v1.yaml` |

## Language Feature Parity Ledger

The machine-readable language claim ledger lives at
`specs/language-feature-parity-ledger.v1.yaml`. It maps each language page to
the supported, partial, and derived feature ids that page may claim, plus the
implementation files, test files, docs, parser-backing class, deterministic
no-provider requirement, read surfaces, and follow-up issues for gaps. The
parser relationship kit verifier fails when a language page claims a supported,
partial, or derived feature that is missing from this ledger, or when a ledger
row points at stale implementation, test, or docs paths.

This ledger is deliberately narrower than the product-claim ledger in #4060:
it only gates language and configuration parser claims. Broad route parity,
outbound contract extraction, and cross-repo impact workflows remain tracked in
#4038 through #4042, #4043, and #4046 unless the current ledger row marks a
feature supported with deterministic proof.

| Parser | Parser Class | Grammar Routing | Normalization | Framework Or Root Evidence | Modeled Evidence | Query Surfacing | Real-Repo Validation | End-to-End Indexing |
|--------|--------------|-----------------|---------------|----------------------------|------------------|-----------------|----------------------|---------------------|
| ArgoCD | `DefaultEngine (yaml)` | - | - | unsupported | Application manifests and sync metadata only | - | - | - |
| C | `DefaultEngine (c)` | supported | supported | derived roots | `main`, local header API, signal handlers, callback arguments, function-pointer targets | supported | fixture-backed | fixture-backed |
| CloudFormation | `DefaultEngine (yaml)` | - | - | unsupported | template/resource evidence only | - | - | - |
| C++ | `DefaultEngine (cpp)` | supported | supported | derived roots | `main`, local header API, virtual/override methods, callbacks, function pointers, Node native add-ons | supported | fixture-backed | fixture-backed |
| Crossplane | `DefaultEngine (yaml)` | - | - | unsupported | composition and resource evidence only | - | - | - |
| C# | `DefaultEngine (c_sharp)` | supported | supported | derived roots plus exact ASP.NET route entries | ASP.NET controller actions, hosted-service callbacks, tests, serialization, constructors, overrides, same-file interfaces, literal ASP.NET attributes, literal minimal API handlers | supported | fixture-backed | fixture-backed |
| Dart | `DefaultEngine (dart)` | supported | supported | derived roots | Flutter `build`/`createState`, public `lib/` API, constructors, overrides | supported | real-repo-validated | fixture-backed |
| Elixir | `DefaultEngine (elixir)` | supported | supported | derived roots | Phoenix, LiveView, GenServer, Supervisor, Mix, protocols, behaviours, public macros/guards | supported | fixture-backed | fixture-backed |
| Go | `DefaultEngine (go)` | supported | supported | derived roots | `net/http`, Cobra, controller-runtime `Reconcile`, package exports, interfaces, function values, dependency-injection callbacks | supported | supported | supported |
| Groovy | `DefaultEngine (groovy)` | supported | supported | derived roots | Jenkins Pipeline entrypoints, shared-library calls, deployment hints | supported | supported | supported |
| Haskell | `DefaultEngine (haskell)` | supported | supported | derived roots | module exports, typeclasses, instances, `main` | supported | fixture-backed | fixture-backed |
| Helm | `DefaultEngine (yaml)` | - | - | unsupported | chart/template evidence only | - | - | - |
| Java | `DefaultEngine (java)` | supported | supported | derived roots plus exact Spring MVC/WebFlux, JAX-RS, and Micronaut route entries | Spring, Gradle, JUnit, Jenkins, Stapler, ServiceLoader, serialization, bounded reflection | supported | real-repo-validated | fixture-backed |
| JavaScript | `DefaultEngine (javascript)` | supported | supported | derived roots | React/TSX evidence, Next.js routes/app exports, Express, Koa, Fastify, NestJS, Hapi, AMQP consumers, package/bin/exports, migrations, seeds, AWS/GCP SDK evidence | supported | fixture-backed | fixture-backed |
| JSON Config | `DefaultEngine (json)` | - | - | unsupported | JSON metadata/config evidence only | - | - | - |
| Kotlin | `DefaultEngine (kotlin)` | supported | supported | derived roots | Spring, Gradle, JUnit, lifecycle callbacks, interfaces, overrides, constructors | supported | fixture-backed | fixture-backed |
| Kubernetes | `DefaultEngine (yaml)` | - | - | unsupported | workload and resource evidence only | - | - | - |
| Kustomize | `DefaultEngine (yaml)` | - | - | unsupported | overlay/resource evidence only | - | - | - |
| Perl | `DefaultEngine (perl)` | supported | supported | derived roots | Exporter, package namespaces, constructors, special blocks, `AUTOLOAD`, `DESTROY` | supported | fixture-backed | fixture-backed |
| PHP | `DefaultEngine (php)` | supported | supported | derived roots plus exact Symfony and Laravel route entries | Symfony route attributes, exact literal Symfony and Laravel `route_entries`, Laravel `Controller@method` handler resolution, route-backed controller actions, magic methods, interfaces, traits, WordPress hooks (dead-code suppression only, not `route_entries`) | supported | supported | supported |
| Python | `DefaultEngine (python)` | supported | supported | derived roots | FastAPI, Flask, bounded Django/DRF/aiohttp/Tornado route entries, Celery, Click, Typer, AWS Lambda, dataclasses, properties, dunder protocols, `__all__`, package reexports | supported | supported | supported |
| Ruby | `DefaultEngine (ruby)` | supported | supported | derived roots plus exact Rails/Sinatra route entries | Rails controller actions, Rails callbacks, script guards, literal method-reference targets, dynamic dispatch hooks, literal Rails `to: "controller#action"` route entries, named Sinatra `&method(:handler)` routes | supported | supported | supported |
| Rust | `DefaultEngine (rust)` | supported | supported | derived roots plus exact Axum/Actix/Rocket route entries | Cargo entrypoints, tests, Tokio, Criterion, `pub` API, trait implementations, exact literal Axum/Actix/Rocket `route_entries` | supported | fixture-backed | fixture-backed |
| Scala | `DefaultEngine (scala)` | supported | supported | derived roots plus exact Play/http4s route entries | Play, Akka, JUnit, ScalaTest, lifecycle callbacks, traits, `App` objects, literal Play route files, literal http4s `HttpRoutes.of` routes | supported | real-repo-validated | fixture-backed |
| SQL | `DefaultEngine (sql)` | supported | supported | derived roots | stored routines and trigger-to-function evidence | supported | supported | supported |
| Swift | `DefaultEngine (swift)` | supported | supported | derived roots | SwiftUI, UIKit, Vapor, XCTest, Swift Testing, protocols, constructors, overrides | supported | supported | supported |
| Terraform | `DefaultEngine (hcl)` | supported | supported | non-code evidence | resources, modules, variables, outputs, providers, backend and state evidence | supported | supported | supported |
| Terragrunt | `DefaultEngine (hcl)` | supported | supported | non-code evidence | includes, dependency blocks, remote state, Terraform source evidence | supported | supported | supported |
| TypeScript | `DefaultEngine (typescript)` | supported | supported | derived roots | JavaScript-family framework roots plus interface implementations, module-contract exports, public API exports/reexports, type references | supported | fixture-backed | fixture-backed |
| TypeScript JSX | `DefaultEngine (tsx)` | supported | supported | derived roots | React component evidence, component wrappers, Next.js routes/app exports, generated/test exclusions | supported | fixture-backed | fixture-backed |

## Reading The Matrix

The table is a support summary, not a release plan. Detailed parser behavior
lives with the language package READMEs and focused tests under
`go/internal/parser`; query behavior lives under `go/internal/query`.

Current high-level boundaries:

- JavaScript, TypeScript, TSX, Python, and Java expose graph-backed query
  metadata, semantic summaries, and `semantic_profile` fields on the documented
  query surfaces.
- Code dead-code support remains `derived` for source languages listed in
  [Dead Code Language Maturity](../reference/dead-code-language-maturity.md).
  `derived` means Eshu can return graph-backed candidates with modeled roots,
  not cleanup-safe exact truth.
- Terraform and Terragrunt are `non_code_iac_evidence` for dead-code purposes:
  their parser/query surfaces expose infrastructure evidence, but
  `code_quality.dead_code` does not treat HCL entities as source-code cleanup
  candidates.
- Empty cells mean this page makes no audited support claim for that dimension.

This matrix stays intentionally coarse and should not be read as the
canonical signoff checklist.

## Adding Framework Support

Framework-support pull requests should include:

- a parser or query test that names the exact framework pattern being modeled
- a fixture or dogfood note when the framework claim depends on repository shape
- a language-page update that states supported, partial, and unclaimed behavior
- an update to this matrix
- an update to
  [Dead Code Language Maturity](../reference/dead-code-language-maturity.md)
  when the change affects `code_quality.dead_code`
