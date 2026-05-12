# Ruby Dead-Code Fixture

Maturity: `derived`.

Expected symbols:

| Case | Symbol |
| --- | --- |
| `unused` | `unused_ruby_helper` |
| `direct_reference` | `direct_ruby_helper` |
| `entrypoint` | `main` |
| `public_api` | `PublicRubyController` |
| `framework_root` | `index` |
| `framework_callback` | `authenticate_ruby_user!` |
| `semantic_dispatch` | `selected_ruby_handler` |
| `excluded` | `generated_ruby_stub` |
| `ambiguous` | `dynamic_ruby_dispatch`, `method_missing` |

This fixture is derived. Rails controller actions, Rails callback symbols,
literal method references, dynamic-dispatch hooks, and script entrypoints are
modeled as parser-backed roots. Ruby remains non-exact until broader
metaprogramming, autoload, framework routing, gem public API, and constant
resolution are modeled or scoped out.
