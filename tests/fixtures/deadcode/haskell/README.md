# Haskell Dead-Code Fixture

Maturity: `derived_candidate_only`.

Expected symbols:

| Case | Symbol |
| --- | --- |
| `unused` | `unusedHaskellHelper` |
| `direct_reference` | `directHaskellHelper` |
| `entrypoint` | `main` |
| `public_api` | `publicHaskellApi` |
| `framework_root` | `runCommandRoot` |
| `semantic_dispatch` | `selectedHaskellHandler` |
| `excluded` | `generatedHaskellStub` |
| `ambiguous` | `dynamicHaskellDispatch` |

This fixture is candidate-only. It records expected Haskell root classes while
module exports, typeclasses, and dynamic dispatch remain unproven.
