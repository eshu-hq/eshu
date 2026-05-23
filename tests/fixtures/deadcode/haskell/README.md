# Haskell Dead-Code Fixture

Maturity: `derived`.

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

This fixture records expected Haskell root classes. Exact cleanup-safe truth is
still blocked by Template Haskell, CPP conditionals, Cabal component selection,
implicit exports, typeclass dispatch, module reexports, and FFI.
