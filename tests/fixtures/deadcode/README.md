# Dead-Code Fixture Corpus

Fixture corpus for `code_quality.dead_code` maturity. Parser coverage proves
syntax extraction; these fixtures prove whether Eshu can avoid cleanup-unsafe
answers for each language.

## Required Cases

Each language directory should name at least one symbol for:

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

## Language Inventory

| Language | Fixture status | Maturity |
| --- | --- | --- |
| C | active | `derived` |
| C# | active | `derived` |
| C++ | active | `derived` |
| Dart | active | `derived` |
| Elixir | active | `derived` |
| Go | active | `derived` |
| Groovy | active | `derived_candidate_only` |
| Haskell | active | `derived` |
| Java | active | `derived` |
| JavaScript | active | `derived` |
| Kotlin | active | `derived` |
| Perl | active | `derived` |
| PHP | active | `derived` |
| Python | active | `derived` |
| Ruby | active | `derived` |
| Rust | active | `derived` |
| Scala | active | `derived` |
| Swift | active | `derived` |
| TSX | active | `derived` |
| TypeScript | active | `derived` |

## Where It Is Asserted

- Parser root metadata is asserted by language-specific tests in
  `go/internal/parser/*dead_code*_test.go`.
- API/query maturity and filtering behavior is asserted under
  `go/internal/query/*dead_code*_test.go`.
- Backend query-shape readiness is tracked by the dead-code backend
  conformance corpus.

Exactness is language-scoped. Promoting one language requires fixture cases,
parser/root evidence, query tests for unused/reachable/excluded/ambiguous
results, API/MCP maturity metadata, and backend conformance.
