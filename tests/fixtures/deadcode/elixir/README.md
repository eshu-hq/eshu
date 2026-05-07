# Elixir Dead-Code Fixture

Maturity: `derived_candidate_only`.

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

This fixture is candidate-only. It records root expectations for later parser
and query proof without promoting Elixir beyond `derived_candidate_only`.
