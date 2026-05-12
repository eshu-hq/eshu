# Scala Dead-Code Fixture Intent

Maturity: `derived`.

Expected symbols:

| Case | Symbol |
| --- | --- |
| `unused` | `unusedCleanupCandidate` |
| `direct_reference` | `directlyUsedHelper` |
| `entrypoint` | `Main.main` |
| `public_api` | `PublicService.status` |
| `framework_root` | `JobEndpoint.handle`, `WorkerActor.receive`, `FixtureSuite.exercisedByTestRunner`, `ScriptMain` |
| `semantic_dispatch` | `Task.run` |
| `excluded` | `generatedExcludedHelper` |
| `ambiguous` | `dynamicMethodName` |
