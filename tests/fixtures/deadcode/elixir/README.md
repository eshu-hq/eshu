# Elixir Dead-Code Fixture

Maturity: `derived`.

Expected symbols:

| Case | Symbol |
| --- | --- |
| `unused` | `unused_elixir_helper/0` |
| `direct_reference` | `direct_elixir_helper/0` |
| `entrypoint` | `start/2` |
| `public_api` | `public_elixir_api/0` |
| `framework_root` | `handle_call/3` |
| `semantic_dispatch` | `selected_elixir_handler/0` |
| `excluded` | `generated_elixir_stub/0` |
| `ambiguous` | `dynamic_elixir_dispatch/1` |

This fixture records parser-backed Elixir root metadata and dynamic-dispatch
ambiguity. It keeps Elixir non-exact until macro expansion, protocol dispatch,
Phoenix route resolution, supervision trees, Mix environment selection, and
broad public API surfaces are modeled or scoped out.
