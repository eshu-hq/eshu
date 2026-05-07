# PHP Dead-Code Fixture

Maturity: `derived_candidate_only`.

Expected symbols:

| Case | Symbol |
| --- | --- |
| `unused` | `unusedPhpHelper` |
| `direct_reference` | `directPhpHelper` |
| `entrypoint` | `main` |
| `public_api` | `PublicPhpController` |
| `framework_root` | `indexAction` |
| `semantic_dispatch` | `selectedPhpHandler` |
| `excluded` | `generatedPhpStub` |
| `ambiguous` | `dynamicPhpDispatch` |

This fixture is candidate-only. PHP attributes, framework controllers, and
dynamic calls need parser/query proof before promotion.
