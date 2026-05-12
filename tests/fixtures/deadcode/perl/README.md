# Perl Dead-Code Fixture

Maturity: `derived`.

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

This fixture now carries parser-backed Perl roots for package namespaces,
Exporter-backed public functions, script `main`, constructors, special blocks,
`AUTOLOAD`, and `DESTROY`. It remains non-exact because symbolic references,
AUTOLOAD dispatch, inheritance, Moose/Moo metadata, import side effects,
runtime `eval`, and broad public API surfaces are not resolved.
