# C# Dead-Code Fixture Intent

Maturity: `derived_candidate_only`.

Expected symbols:

| Case | Symbol |
| --- | --- |
| `unused` | `UnusedCleanupCandidate` |
| `direct_reference` | `DirectlyUsedHelper` |
| `entrypoint` | `Program.Main` |
| `public_api` | `PublicController.Get` |
| `framework_root` | `Worker.ExecuteAsync` |
| `semantic_dispatch` | `IJob.Run` |
| `excluded` | `GeneratedExcludedHelper` |
| `ambiguous` | `DynamicActionName` |
