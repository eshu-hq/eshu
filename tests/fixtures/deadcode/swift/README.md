# Swift Dead-Code Fixture Intent

Maturity: `derived`.

Expected symbols:

| Case | Symbol |
| --- | --- |
| `unused` | `unusedCleanupCandidate` |
| `direct_reference` | `directlyUsedHelper` |
| `entrypoint` | `AppMain.main` |
| `public_api` | `PublicService.status` |
| `framework_root` | `ViewController.viewDidLoad` |
| `semantic_dispatch` | `Task.run` |
| `excluded` | `generatedExcludedHelper` |
| `ambiguous` | `dynamicMethodName` |

Swift remains derived rather than exact. Parser and query tests now prove
`@main`, top-level `main`, SwiftUI `App`/`body`, protocol methods and same-file
implementations, constructors, overrides, UIKit application delegate callbacks,
Vapor route handlers, XCTest methods, and Swift Testing `@Test` functions as
modeled roots.
