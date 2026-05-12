# C# Dead-Code Fixture Intent

Maturity: `derived`.

Expected symbols:

| Case | Symbol |
| --- | --- |
| `unused` | `UnusedCleanupCandidate` |
| `direct_reference` | `DirectlyUsedHelper` |
| `entrypoint` | `Program.Main` |
| `aspnet_controller_action` | `PublicController.Get` |
| `framework_root` | `Worker.ExecuteAsync` |
| `semantic_dispatch` | `IJob.Run` |
| `test_root` | `FixtureTests.ExercisedByTestRunner` |
| `serialization_root` | `SerializationHooks.Restore` |
| `excluded` | `GeneratedExcludedHelper` |
| `ambiguous` | `DynamicActionName` |
