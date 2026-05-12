# C++ Dead-Code Fixture Intent

Maturity: `derived`.

Expected symbols:

| Case | Symbol |
| --- | --- |
| `unused` | `unusedCleanupCandidate` |
| `direct_reference` | `directlyUsedHelper` |
| `entrypoint` | `main` |
| `public_api` | `eshuCppPublicAPI`, `HeaderWidget::render` |
| `framework_root` | `Command::run`, `DerivedCommand::run`, `NAPI_MODULE_INIT` |
| `semantic_dispatch` | `directlyUsedHelper`, `dispatchTarget`, `CallbackRunner::invoke` |
| `excluded` | `generatedExcludedHelper` |
| `ambiguous` | `dynamicMethodName` |
