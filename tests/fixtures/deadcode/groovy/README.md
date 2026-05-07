# Groovy Dead-Code Fixture

Maturity: `derived_candidate_only`.

Expected symbols:

| Case | Symbol |
| --- | --- |
| `unused` | `unusedGroovyHelper` |
| `direct_reference` | `directGroovyHelper` |
| `entrypoint` | `main` |
| `public_api` | `publicGroovyApi` |
| `framework_root` | `pipelineRoot` |
| `semantic_dispatch` | `selectedGroovyHandler` |
| `excluded` | `generatedGroovyStub` |
| `ambiguous` | `dynamicGroovyDispatch` |

This fixture is candidate-only. Jenkins-style and dynamic Groovy roots still
need parser and query proof before any promotion.
