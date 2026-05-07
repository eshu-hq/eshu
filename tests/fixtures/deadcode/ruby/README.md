# Ruby Dead-Code Fixture

Maturity: `derived_candidate_only`.

Expected symbols:

| Case | Symbol |
| --- | --- |
| `unused` | `unused_ruby_helper` |
| `direct_reference` | `direct_ruby_helper` |
| `entrypoint` | `main` |
| `public_api` | `PublicRubyController` |
| `framework_root` | `index` |
| `semantic_dispatch` | `selected_ruby_handler` |
| `excluded` | `generated_ruby_stub` |
| `ambiguous` | `dynamic_ruby_dispatch` |

This fixture is candidate-only. Rails-style roots and metaprogramming remain
non-exact until modeled and tested.
