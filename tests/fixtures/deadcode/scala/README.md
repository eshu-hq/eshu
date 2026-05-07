# Scala Dead-Code Fixture Intent

Maturity: `derived_candidate_only`.

Expected symbols:

| Case | Symbol |
| --- | --- |
| `unused` | `unusedCleanupCandidate` |
| `direct_reference` | `directlyUsedHelper` |
| `entrypoint` | `Main.main` |
| `public_api` | `PublicService.status` |
| `framework_root` | `JobEndpoint.handle` |
| `semantic_dispatch` | `Task.run` |
| `excluded` | `generatedExcludedHelper` |
| `ambiguous` | `dynamicMethodName` |
