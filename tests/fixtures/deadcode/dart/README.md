# Dart Dead-Code Fixture

Maturity: `derived`.

Expected symbols:

| Case | Symbol |
| --- | --- |
| `unused` | `unusedDartHelper` |
| `direct_reference` | `directDartHelper` |
| `entrypoint` | `main` |
| `public_api` | `PublicDartWidget` |
| `framework_root` | `createState`, `build` |
| `semantic_dispatch` | `selectedDartHandler` |
| `excluded` | `generatedDartStub` |
| `ambiguous` | `dynamicDartDispatch` |

The derived root model protects top-level `main()`, constructors, `@override`
methods, Flutter widget callbacks, and public `lib/` API declarations. Exact
cleanup remains blocked by part-file resolution, conditional imports and
exports, package export surfaces, dynamic dispatch, Flutter route/lifecycle
wiring, generated code, reflection/mirrors, and broad public API surfaces.
