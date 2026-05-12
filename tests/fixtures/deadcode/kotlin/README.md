# Kotlin Dead-Code Fixture Intent

Maturity: `derived`.

Expected symbols:

| Case | Symbol |
| --- | --- |
| `unused` | `unusedCleanupCandidate` |
| `direct_reference` | `directlyUsedHelper` |
| `entrypoint` | `main` |
| `public_api` | `PublicService.status` |
| `framework_root` | `JobRoute`, `JobRoute.handle`, `FixtureTests.exercisedByTestRunner`, `DefaultTaskFixture.execute` |
| `semantic_dispatch` | `Task.run` |
| `excluded` | `generatedExcludedHelper` |
| `ambiguous` | `dynamicMethodName` |
