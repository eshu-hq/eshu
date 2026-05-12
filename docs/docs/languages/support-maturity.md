# Parser Support Matrix

This page tracks the checked-in Go parser support-maturity matrix in the current repository state.

This matrix is intentionally coarse. It does not replace the per-language
capability pages.

Use:

- the language pages under `docs/docs/languages/` for exact partial or
  unsupported capability details
- the `Dead-code Support` section on each parser page for root modeling,
  query evidence, checked fixtures, and bounded limitations

This matrix tracks the higher-level support bar for each parser beyond
the raw capability checklist. `-` means the page does not currently make a
specific support assertion for that dimension.

For audited family-level closure status and bounded gaps, see
[`../reference/parity-closure-matrix.md`](../reference/parity-closure-matrix.md).

| Parser | Parser Class | Grammar Routing | Normalization | Framework Packs | Pack Names | Query Surfacing | Real-Repo Validation | End-to-End Indexing |
|--------|--------------|-----------------|---------------|-----------------|------------|-----------------|----------------------|---------------------|
| ArgoCD | `DefaultEngine (yaml)` | - | - | - | - | - | - | - |
| C | `DefaultEngine (c)` | - | - | - | - | - | - | - |
| CloudFormation | `DefaultEngine (yaml)` | - | - | - | - | - | - | - |
| C++ | `DefaultEngine (cpp)` | - | - | - | - | - | - | - |
| Crossplane | `DefaultEngine (yaml)` | - | - | - | - | - | - | - |
| C# | `DefaultEngine (c_sharp)` | - | - | - | - | - | - | - |
| Dart | `DefaultEngine (dart)` | - | - | - | - | - | - | - |
| Elixir | `DefaultEngine (elixir)` | - | - | - | - | - | - | - |
| Go | `DefaultEngine (go)` | supported | supported | - | - | supported | supported | supported |
| Groovy | `DefaultEngine (groovy)` | - | - | - | - | - | - | - |
| Haskell | `DefaultEngine (haskell)` | - | - | - | - | - | - | - |
| Helm | `DefaultEngine (yaml)` | - | - | - | - | - | - | - |
| Java | `DefaultEngine (java)` | supported | supported | supported | Spring, Gradle, JUnit, Jenkins, Stapler, ServiceLoader, serialization, bounded reflection | supported | supported | supported |
| JavaScript | `DefaultEngine (javascript)` | supported | supported | supported | `react-base`, `nextjs-app-router-base`, `express-base`, `hapi-base`, `aws-sdk-base`, `gcp-sdk-base` | supported | supported | supported |
| JSON Config | `DefaultEngine (json)` | - | - | - | - | - | - | - |
| Kotlin | `DefaultEngine (kotlin)` | supported | supported | supported | Spring, Gradle, JUnit, lifecycle, interfaces | supported | fixture-backed | supported |
| Kubernetes | `DefaultEngine (yaml)` | - | - | - | - | - | - | - |
| Kustomize | `DefaultEngine (yaml)` | - | - | - | - | - | - | - |
| Perl | `DefaultEngine (perl)` | - | - | - | - | - | - | - |
| PHP | `DefaultEngine (php)` | - | - | - | - | - | - | - |
| Python | `DefaultEngine (python)` | supported | supported | supported | `fastapi-base`, `flask-base` | supported | supported | supported |
| Ruby | `DefaultEngine (ruby)` | - | - | - | - | - | - | - |
| Rust | `DefaultEngine (rust)` | - | - | - | - | - | - | - |
| Scala | `DefaultEngine (scala)` | supported | supported | supported | Play, Akka, JUnit, ScalaTest, lifecycle, traits | supported | fixture-backed | supported |
| SQL | `DefaultEngine (sql)` | supported | supported | unsupported | - | supported | supported | supported |
| Swift | `DefaultEngine (swift)` | supported | supported | supported | SwiftUI, UIKit, Vapor, XCTest, Swift Testing, protocols | supported | fixture-backed | supported |
| Terraform | `DefaultEngine (hcl)` | - | - | - | - | - | - | - |
| Terragrunt | `DefaultEngine (hcl)` | - | - | - | - | - | - | - |
| TypeScript | `DefaultEngine (typescript)` | supported | supported | supported | `react-base`, `nextjs-app-router-base`, `express-base`, `hapi-base`, `aws-sdk-base`, `gcp-sdk-base` | supported | supported | supported |
| TypeScript JSX | `DefaultEngine (tsx)` | supported | supported | supported | `react-base`, `nextjs-app-router-base` | supported | supported | supported |

For JavaScript, TypeScript, TypeScript JSX, Python, and Java, query surfacing is now
`supported` because the shared Go query outputs expose enriched metadata,
semantic summaries, and a structured `semantic_profile` on the normal
language-query, code-search, entities-resolve, and entity-context surfaces.
JavaScript method-kind rows now also get a dedicated `javascript_method`
surface kind in those shared query outputs.
Java dead-code support remains `derived`, but parser and reducer coverage now
models main/constructor/override roots, Gradle, Spring, JUnit, Jenkins,
Stapler, serialization hooks, bounded literal reflection, ServiceLoader
providers, and Spring metadata roots with checked fixtures.
Kotlin dead-code support is `derived`: parser metadata models top-level main,
secondary constructors, interface methods and same-file implementations,
overrides, Gradle plugin/task callbacks, Spring component and method callbacks,
lifecycle callbacks, and JUnit methods, while exact cleanup remains blocked by
reflection, dependency injection, annotation processing, compiler plugins,
dynamic dispatch, Gradle source sets, Kotlin multiplatform targets, and broad
public API surfaces.
Scala dead-code support is `derived`: parser metadata models main methods,
`App` objects, trait methods and same-file implementations, overrides, Play
controller actions, Akka actor `receive` methods, lifecycle callbacks, JUnit
methods, and ScalaTest suite classes, while exact cleanup remains blocked by
macros, implicit/given resolution, dynamic dispatch, reflection, sbt source
sets, framework route files, compiler plugins, and broad public API surfaces.
Issue #105 dogfood validated this path against Play Framework and the Scala
compiler with fresh `derived` dead-code API truth after queue drain.
SQL real-repo and end-to-end indexing are `supported` on the current Go
parser/query path. The remaining dbt lineage limits are bounded non-goals for
the documented SQL surface.
Swift dead-code support is `derived`: parser metadata models `@main`, top-level
`main`, SwiftUI app/body roots, protocol methods and same-file implementations,
constructors, overrides, UIKit application delegate callbacks, Vapor route
handlers, XCTest methods, and Swift Testing `@Test` methods. Exact cleanup
remains blocked by macro expansion, conditional compilation, SwiftPM target
membership, protocol witness resolution, dynamic dispatch, property-wrapper and
result-builder generated code, Objective-C runtime dispatch, and broad public
API surfaces.

This matrix stays intentionally coarse and should not be read as the
canonical signoff checklist.
