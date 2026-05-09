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
| Kotlin | `DefaultEngine (kotlin)` | - | - | - | - | - | - | - |
| Kubernetes | `DefaultEngine (yaml)` | - | - | - | - | - | - | - |
| Kustomize | `DefaultEngine (yaml)` | - | - | - | - | - | - | - |
| Perl | `DefaultEngine (perl)` | - | - | - | - | - | - | - |
| PHP | `DefaultEngine (php)` | - | - | - | - | - | - | - |
| Python | `DefaultEngine (python)` | supported | supported | supported | `fastapi-base`, `flask-base` | supported | supported | supported |
| Ruby | `DefaultEngine (ruby)` | - | - | - | - | - | - | - |
| Rust | `DefaultEngine (rust)` | - | - | - | - | - | - | - |
| Scala | `DefaultEngine (scala)` | - | - | - | - | - | - | - |
| SQL | `DefaultEngine (sql)` | supported | supported | unsupported | - | supported | supported | supported |
| Swift | `DefaultEngine (swift)` | - | - | - | - | - | - | - |
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
SQL real-repo and end-to-end indexing are `supported` on the current Go
parser/query path. The remaining dbt lineage limits are bounded non-goals for
the documented SQL surface.

This matrix stays intentionally coarse and should not be read as the
canonical signoff checklist.
