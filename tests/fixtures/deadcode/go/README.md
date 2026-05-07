# Go Dead-Code Fixture

This fixture exercises Go-specific root evidence for
`code_quality.dead_code`.

Expected symbols:

| Case | Symbol | Notes |
| --- | --- | --- |
| `unused` | `unusedHelper` | No direct call, registration, function value, or interface evidence. |
| `direct_reference` | `directReference` | Reached by an ordinary call from `main`. |
| `entrypoint` | `main` | Executable package entrypoint. |
| `public_api` | `PublicAPI` | Exported package function. |
| `framework_root` | `serveHTTP` | Registered through `http.HandleFunc`. |
| `semantic_dispatch` | `functionValueRoot`, `worker.run`, `localWorker.Handle` | Reached through function value, method value, and local interface evidence. |
| `excluded` | `generatedExcluded`, `testExcluded` | Generated and test-owned symbols are fixture-only exclusions. |
| `ambiguous` | `reflectiveAmbiguous` | Reflection by string keeps the case non-exact. |

The parser evidence in this directory is intentionally conservative. It models
deterministic local syntax only and does not claim build-tag, reflection, or
cross-package method-set exactness.
