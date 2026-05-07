# Java Dead-Code Fixture Intent

Maturity: `derived_candidate_only`.

Expected symbols:

| Case | Symbol |
| --- | --- |
| `unused` | `unusedCleanupCandidate` |
| `direct_reference` | `directlyUsedHelper` |
| `entrypoint` | `App.main` |
| `public_api` | `PublicService.status` |
| `framework_root` | `JobController.handle` |
| `semantic_dispatch` | `Task.run` |
| `excluded` | `GeneratedExcludedHelper.render` |
| `ambiguous` | `dynamicMethodName` |
