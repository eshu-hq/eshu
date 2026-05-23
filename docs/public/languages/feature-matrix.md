# Parser Feature Matrix

This page tracks the checked-in Go parser contract matrix in the current repository state.

Coverage counts on this page describe checked-in fixture contract coverage, not
blanket parity claims for every end-to-end graph surface. Use each
language-specific page as the source of truth for current limitations, partial
real-repo validation, and end-to-end indexing status.

For audited family-level closure status, see
[`../reference/parity-closure-matrix.md`](../reference/parity-closure-matrix.md).

## Language Parsers

| Parser | Parser Class | Functions | Classes | Interfaces | Traits | Imports | Calls | Variables | Structs | Enums | Macros | Unit Coverage | Integration Coverage | Fixture |
|--------|--------------|-----------|---------|------------|--------|---------|-------|-----------|---------|-------|--------|---------------|----------------------|---------|
| C | `DefaultEngine (c)` | Y | - | - | - | Y | Y | Y | Y | Y | Y | 9/9 | 9/9 | `tests/fixtures/ecosystems/c_comprehensive/` |
| C++ | `DefaultEngine (cpp)` | Y | Y | - | - | Y | Y | Y | Y | Y | Y | 12/12 | 12/12 | `tests/fixtures/ecosystems/cpp_comprehensive/` |
| C# | `DefaultEngine (c_sharp)` | Y | Y | Y | - | Y | Y | - | Y | Y | - | 13/13 | 13/13 | `tests/fixtures/ecosystems/csharp_comprehensive/` |
| Dart | `DefaultEngine (dart)` | Y | Y | - | - | Y | Y | Y | - | Y | - | 11/11 | 11/11 | `tests/fixtures/ecosystems/dart_comprehensive/` |
| Elixir | `DefaultEngine (elixir)` | Y | Y | - | - | Y | Y | P | - | - | - | 7/7 | 7/7 | `tests/fixtures/ecosystems/elixir_comprehensive/` |
| Go | `DefaultEngine (go)` | Y | - | Y | - | Y | Y | Y | Y | - | - | 9/9 | 9/9 | `tests/fixtures/ecosystems/go_comprehensive/` |
| Groovy | `DefaultEngine (groovy)` | - | - | - | - | - | - | - | - | - | - | 6/6 | 6/6 | `tests/fixtures/ecosystems/groovy_comprehensive/` |
| Haskell | `DefaultEngine (haskell)` | Y | - | - | Y | Y | Y | Y | Y | Y | - | 9/9 | 9/9 | `tests/fixtures/ecosystems/haskell_comprehensive/` |
| Java | `DefaultEngine (java)` | Y | Y | Y | - | Y | Y | Y | - | Y | - | 11/11 | 11/11 | `tests/fixtures/ecosystems/java_comprehensive/` |
| JavaScript | `DefaultEngine (javascript)` | Y | Y | - | - | Y | Y | Y | - | - | - | 9/9 | 9/9 | `tests/fixtures/ecosystems/javascript_comprehensive/` |
| JSON Config | `DefaultEngine (json)` | Y | - | - | - | - | - | Y | - | - | - | 5/5 | 5/5 | `tests/fixtures/ecosystems/json_comprehensive/` |
| Kotlin | `DefaultEngine (kotlin)` | Y | Y | - | - | Y | Y | Y | - | - | - | 8/8 | 8/8 | `tests/fixtures/ecosystems/kotlin_comprehensive/` |
| Perl | `DefaultEngine (perl)` | Y | Y | - | - | Y | Y | Y | - | - | - | 9/9 | 9/9 | `tests/fixtures/ecosystems/perl_comprehensive/` |
| PHP | `DefaultEngine (php)` | Y | Y | Y | Y | Y | Y | Y | - | - | - | 10/10 | 10/10 | `tests/fixtures/ecosystems/php_comprehensive/` |
| Python | `DefaultEngine (python)` | Y | Y | - | - | Y | Y | Y | - | - | - | 6/6 | 6/6 | `tests/fixtures/ecosystems/python_comprehensive/` |
| Ruby | `DefaultEngine (ruby)` | Y | Y | - | - | Y | Y | Y | - | - | - | 9/9 | 9/9 | `tests/fixtures/ecosystems/ruby_comprehensive/` |
| Rust | `DefaultEngine (rust)` | Y | - | - | Y | Y | Y | - | Y | Y | - | 8/8 | 8/8 | `tests/fixtures/ecosystems/rust_comprehensive/` |
| Scala | `DefaultEngine (scala)` | Y | Y | - | Y | Y | Y | Y | - | - | - | 10/10 | 10/10 | `tests/fixtures/ecosystems/scala_comprehensive/` |
| SQL | `DefaultEngine (sql)` | - | - | - | - | - | - | - | - | - | - | 8/8 | 8/8 | `tests/fixtures/ecosystems/sql_comprehensive/` |
| Swift | `DefaultEngine (swift)` | Y | Y | - | Y | Y | Y | Y | Y | Y | - | 10/10 | 10/10 | `tests/fixtures/ecosystems/swift_comprehensive/` |
| TypeScript | `DefaultEngine (typescript)` | Y | Y | Y | - | Y | Y | Y | - | Y | - | 7/7 | 7/7 | `tests/fixtures/ecosystems/typescript_comprehensive/` |
| TypeScript JSX | `DefaultEngine (tsx)` | Y | Y | Y | - | Y | Y | Y | - | - | - | 6/6 | 6/6 | `tests/fixtures/ecosystems/tsx_comprehensive/` |

## IaC Parsers

| Parser | Parser Class | Resources | Variables | Outputs | Modules | Unit Coverage | Integration Coverage | Fixture |
|--------|--------------|-----------|-----------|---------|---------|---------------|----------------------|---------|
| ArgoCD | `DefaultEngine (yaml)` | Y | - | - | Y | 7/7 | 7/7 | `tests/fixtures/ecosystems/argocd_comprehensive/` |
| CloudFormation | `DefaultEngine (yaml)` | Y | Y | Y | - | 7/7 | 7/7 | `tests/fixtures/ecosystems/cloudformation_comprehensive/` |
| Crossplane | `DefaultEngine (yaml)` | Y | - | - | Y | 7/7 | 7/7 | `tests/fixtures/ecosystems/crossplane_comprehensive/` |
| Helm | `DefaultEngine (yaml)` | Y | - | - | Y | 5/5 | 5/5 | `tests/fixtures/ecosystems/helm_comprehensive/` |
| Kubernetes | `DefaultEngine (yaml)` | Y | - | - | - | 6/6 | 6/6 | `tests/fixtures/ecosystems/kubernetes_comprehensive/` |
| Kustomize | `DefaultEngine (yaml)` | Y | - | - | - | 4/4 | 4/4 | `tests/fixtures/ecosystems/kustomize_comprehensive/` |
| Terraform | `DefaultEngine (hcl)` | Y | Y | Y | Y | 7/7 | 7/7 | `tests/fixtures/ecosystems/terraform_comprehensive/` |
| Terragrunt | `DefaultEngine (hcl)` | Y | - | - | - | 3/3 | 3/3 | `tests/fixtures/ecosystems/terragrunt_comprehensive/` |
