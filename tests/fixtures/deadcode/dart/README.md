# Dart Dead-Code Fixture

Maturity: `derived_candidate_only`.

Expected symbols:

| Case | Symbol |
| --- | --- |
| `unused` | `unusedDartHelper` |
| `direct_reference` | `directDartHelper` |
| `entrypoint` | `main` |
| `public_api` | `PublicDartWidget` |
| `framework_root` | `onPressedRoot` |
| `semantic_dispatch` | `selectedDartHandler` |
| `excluded` | `generatedDartStub` |
| `ambiguous` | `dynamicDartDispatch` |

This fixture is candidate-only. It documents expected root categories without
claiming exact Dart cleanup safety.
