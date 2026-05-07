# Perl Dead-Code Fixture

Maturity: `derived_candidate_only`.

Expected symbols:

| Case | Symbol |
| --- | --- |
| `unused` | `unused_perl_helper` |
| `direct_reference` | `direct_perl_helper` |
| `entrypoint` | `main` |
| `public_api` | `public_perl_api` |
| `framework_root` | `route_perl_root` |
| `semantic_dispatch` | `selected_perl_handler` |
| `excluded` | `generated_perl_stub` |
| `ambiguous` | `dynamic_perl_dispatch` |

This fixture is candidate-only. Perl package exports and symbolic calls remain
ambiguous until parser and query proof exists.
