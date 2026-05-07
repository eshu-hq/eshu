# C++ Dead-Code Fixture Intent

Maturity: `derived_candidate_only`.

Expected symbols:

| Case | Symbol |
| --- | --- |
| `unused` | `unusedCleanupCandidate` |
| `direct_reference` | `directlyUsedHelper` |
| `entrypoint` | `main` |
| `public_api` | `PublicWidget::render` |
| `framework_root` | `Command::run` |
| `semantic_dispatch` | `CallbackRunner::invoke` |
| `excluded` | `generatedExcludedHelper` |
| `ambiguous` | `dynamicMethodName` |
