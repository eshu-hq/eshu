# Dead-Code Fixture Corpus

Fixture corpus for `code_quality.dead_code` maturity. Parser fixtures prove
syntax extraction; this corpus proves whether each language has enough root and
reachability evidence to avoid cleanup-unsafe answers.

## Required Cases

Each language directory should identify at least one symbol for each case it
claims:

| Case | Meaning |
| --- | --- |
| `unused` | Candidate that may be returned as dead code when no root or reachability evidence protects it. |
| `direct_reference` | Symbol reached by a direct call, import, or reference. |
| `entrypoint` | Executable entrypoint, initializer, or module root. |
| `public_api` | Exported/public surface that should not be treated as dead. |
| `framework_root` | Route, command, worker, annotation, decorator, callback, or equivalent ecosystem root. |
| `semantic_dispatch` | Function value, method value, interface, trait, dynamic import, generated registry, or equivalent dispatch. |
| `excluded` | Generated or test-owned code excluded by default. |
| `ambiguous` | Dynamic case that must keep truth non-exact. |

## Directory Map

| Directory | Language | Maturity |
| --- | --- | --- |
| `c/` | C | `derived` |
| `csharp/` | C# | `derived` |
| `cpp/` | C++ | `derived` |
| `dart/` | Dart | `derived` |
| `elixir/` | Elixir | `derived` |
| `go/` | Go | `derived` |
| `groovy/` | Groovy | `derived_candidate_only` |
| `haskell/` | Haskell | `derived` |
| `java/` | Java | `derived` |
| `javascript/` | JavaScript | `derived` |
| `kotlin/` | Kotlin | `derived` |
| `perl/` | Perl | `derived` |
| `php/` | PHP | `derived` |
| `python/` | Python | `derived` |
| `ruby/` | Ruby | `derived` |
| `rust/` | Rust | `derived` |
| `scala/` | Scala | `derived` |
| `swift/` | Swift | `derived` |
| `tsx/` | TSX | `derived` |
| `typescript/` | TypeScript | `derived` |

## Assertion Owners

- Parser root metadata: `go/internal/parser/*dead_code*_test.go`.
- API/query maturity and filtering: `go/internal/query/*dead_code*_test.go`.
- Backend query-shape readiness: the dead-code backend conformance corpus.

Exactness is language-scoped. Promoting one language requires fixture cases,
parser/root evidence, query tests for unused/reachable/excluded/ambiguous
results, API/MCP maturity metadata, and backend conformance.
