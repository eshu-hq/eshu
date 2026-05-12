# PHP Dead-Code Fixture

Maturity: `derived`.

Expected symbols:

| Case | Symbol |
| --- | --- |
| `unused` | `unusedPhpHelper` |
| `direct_reference` | `directPhpHelper` |
| `entrypoint` | `main` |
| `public_api` | `PublicPhpController` |
| `framework_root` | `indexAction` |
| `semantic_dispatch` | `selectedPhpHandler` |
| `interface_method` | `PhpRenderable::render` |
| `interface_implementation` | `PublicPhpController::render` |
| `trait_method` | `PublicPhpTrait::bootPublicPhpController` |
| `magic_method` | `PublicPhpController::__invoke` |
| `excluded` | `generatedPhpStub` |
| `ambiguous` | `dynamicPhpDispatch` |

This fixture exercises parser-backed PHP roots for script entrypoints,
route-backed controller actions, literal route handlers, WordPress hook callbacks,
interfaces, traits, and magic methods. Dynamic calls remain non-exact and must
surface as blockers rather than cleanup-safe truth.
