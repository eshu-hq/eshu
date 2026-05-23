# Parser Support Matrix

This page tracks the checked-in Go parser support-maturity matrix in the current repository state.

This matrix is intentionally coarse. It does not replace the per-language
capability pages.

Use:

- the language pages under `docs/public/languages/` for exact partial or
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
| Dart | `DefaultEngine (dart)` | supported | supported | supported | Flutter, public library API | supported | supported | supported |
| Elixir | `DefaultEngine (elixir)` | supported | supported | supported | Phoenix, GenServer, Supervisor, Mix, protocols | supported | fixture-backed | supported |
| Go | `DefaultEngine (go)` | supported | supported | - | - | supported | supported | supported |
| Groovy | `DefaultEngine (groovy)` | - | - | - | - | - | - | - |
| Haskell | `DefaultEngine (haskell)` | supported | supported | supported | module exports, typeclasses, instances | supported | fixture-backed | supported |
| Helm | `DefaultEngine (yaml)` | - | - | - | - | - | - | - |
| Java | `DefaultEngine (java)` | supported | supported | supported | Spring, Gradle, JUnit, Jenkins, Stapler, ServiceLoader, serialization, bounded reflection | supported | supported | supported |
| JavaScript | `DefaultEngine (javascript)` | supported | supported | supported | `react-base`, `nextjs-app-router-base`, `express-base`, `hapi-base`, `aws-sdk-base`, `gcp-sdk-base` | supported | supported | supported |
| JSON Config | `DefaultEngine (json)` | - | - | - | - | - | - | - |
| Kotlin | `DefaultEngine (kotlin)` | supported | supported | supported | Spring, Gradle, JUnit, lifecycle, interfaces | supported | fixture-backed | supported |
| Kubernetes | `DefaultEngine (yaml)` | - | - | - | - | - | - | - |
| Kustomize | `DefaultEngine (yaml)` | - | - | - | - | - | - | - |
| Perl | `DefaultEngine (perl)` | supported | supported | supported | Exporter, package namespaces, lifecycle hooks | supported | supported | supported |
| PHP | `DefaultEngine (php)` | - | - | - | - | - | - | - |
| Python | `DefaultEngine (python)` | supported | supported | supported | `fastapi-base`, `flask-base` | supported | supported | supported |
| Ruby | `DefaultEngine (ruby)` | - | - | - | - | - | - | - |
| Rust | `DefaultEngine (rust)` | - | - | - | - | - | - | - |
| Scala | `DefaultEngine (scala)` | supported | supported | supported | Play, Akka, JUnit, ScalaTest, lifecycle, traits | supported | fixture-backed | supported |
| SQL | `DefaultEngine (sql)` | supported | supported | unsupported | - | supported | supported | supported |
| Swift | `DefaultEngine (swift)` | supported | supported | supported | SwiftUI, UIKit, Vapor, XCTest, Swift Testing, protocols | supported | fixture-backed | supported |
| Terraform | `DefaultEngine (hcl)` | supported | supported | unsupported | - | supported | supported | supported |
| Terragrunt | `DefaultEngine (hcl)` | supported | supported | unsupported | - | supported | supported | supported |
| TypeScript | `DefaultEngine (typescript)` | supported | supported | supported | `react-base`, `nextjs-app-router-base`, `express-base`, `hapi-base`, `aws-sdk-base`, `gcp-sdk-base` | supported | supported | supported |
| TypeScript JSX | `DefaultEngine (tsx)` | supported | supported | supported | `react-base`, `nextjs-app-router-base` | supported | supported | supported |

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
